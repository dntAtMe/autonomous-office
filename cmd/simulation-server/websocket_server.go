package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"simulation/shared"

	"github.com/gorilla/websocket"
)

// WebSocketEntity implements EntityInterface for WebSocket connections
type WebSocketEntity struct {
	BaseEntity
	connection *websocket.Conn
}

// NewWebSocketEntity creates a new WebSocket entity
func NewWebSocketEntity(id int32, conn *websocket.Conn) *WebSocketEntity {
	return &WebSocketEntity{
		BaseEntity: NewBaseEntity(id),
		connection: conn,
	}
}

// WebSocketServer wraps the simulation core with WebSocket functionality
type WebSocketServer struct {
	core              *SimulationCore
	upgrader          websocket.Upgrader
	entityConnections map[int32]*websocket.Conn
	mu                sync.Mutex
	isRunning         bool
	stopChan          chan bool
}

// NewWebSocketServer creates a new WebSocket simulation server
func NewWebSocketServer(core *SimulationCore) *WebSocketServer {
	return &WebSocketServer{
		core: core,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow connections from any origin
			},
		},
		entityConnections: make(map[int32]*websocket.Conn),
		isRunning:         false,
		stopChan:          make(chan bool),
	}
}

// RegisterEntity handles entity registration via WebSocket
func (s *WebSocketServer) RegisterEntity(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}

	// Read registration request
	var req shared.EntityRegistrationRequest
	err = conn.ReadJSON(&req)
	if err != nil {
		log.Printf("Failed to read registration request: %v", err)
		conn.Close()
		return
	}

	// Create WebSocket entity
	entityID := s.core.GetNextEntityID()
	entity := NewWebSocketEntity(entityID, conn)

	// Register with core
	err = s.core.RegisterEntity(entity)
	if err != nil {
		response := shared.EntityRegistrationResponse{
			Success: false,
			Message: err.Error(),
		}
		conn.WriteJSON(response)
		conn.Close()
		return
	}

	s.mu.Lock()
	s.entityConnections[entityID] = conn
	s.mu.Unlock()

	// Send registration response
	response := shared.EntityRegistrationResponse{
		Success:  true,
		Message:  "Entity registered successfully",
		EntityID: int(entityID),
	}

	err = conn.WriteJSON(response)
	if err != nil {
		log.Printf("Failed to send registration response: %v", err)
		s.core.UnregisterEntity(entityID)
		s.mu.Lock()
		delete(s.entityConnections, entityID)
		s.mu.Unlock()
		conn.Close()
		return
	}

	log.Printf("WebSocket entity %d registered and connected", entityID)

	// Handle entity disconnection
	go s.handleEntityConnection(entity)
}

// handleEntityConnection manages the WebSocket connection for an entity
func (s *WebSocketServer) handleEntityConnection(entity *WebSocketEntity) {
	defer func() {
		s.core.UnregisterEntity(entity.GetID())
		s.mu.Lock()
		delete(s.entityConnections, entity.GetID())
		s.mu.Unlock()
		entity.connection.Close()
		log.Printf("WebSocket entity %d disconnected", entity.GetID())
	}()

	// Set up ping/pong to detect disconnections
	entity.connection.SetPongHandler(func(string) error {
		return nil
	})

	// Send periodic pings to keep connection alive and detect disconnections
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := entity.connection.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("WebSocket entity %d ping failed: %v", entity.GetID(), err)
				return
			}
		}
	}
}

// RequestEntityDecisions sends decision requests to all entities
func (s *WebSocketServer) RequestEntityDecisions(tickNumber int32) map[int32]shared.Action {
	s.mu.Lock()
	connections := make(map[int32]*websocket.Conn)
	for id, conn := range s.entityConnections {
		connections[id] = conn
	}
	s.mu.Unlock()

	gridState := s.core.GetGridState()
	decisions := make(map[int32]shared.Action)
	var decisionsMu sync.Mutex
	var wg sync.WaitGroup

	// Define possible moves
	possibleMoves := []shared.Position{
		{X: 0, Y: 1},  // Up
		{X: 0, Y: -1}, // Down
		{X: 1, Y: 0},  // Right
		{X: -1, Y: 0}, // Left
	}

	for entityID, conn := range connections {
		wg.Add(1)
		go func(id int32, connection *websocket.Conn) {
			defer wg.Done()

			entity := s.core.Entities[id]
			if entity == nil {
				return
			}

			entity.SetDeciding(true)
			startTime := time.Now()

			// Send decision request
			request := shared.EntityDecisionRequest{
				EntityID:      int(id),
				GridState:     gridState,
				TickNumber:    int(tickNumber),
				PossibleMoves: possibleMoves,
			}

			err := connection.WriteJSON(request)
			if err != nil {
				log.Printf("Failed to send decision request to entity %d: %v", id, err)
				entity.SetDeciding(false)
				return
			}

			// Wait for response with timeout
			responseChan := make(chan shared.EntityDecisionResponse, 1)
			errorChan := make(chan error, 1)

			go func() {
				var response shared.EntityDecisionResponse
				connection.SetReadDeadline(time.Now().Add(s.core.DecisionTimeout))
				err := connection.ReadJSON(&response)
				connection.SetReadDeadline(time.Time{})
				if err != nil {
					errorChan <- err
				} else {
					responseChan <- response
				}
			}()

			var response shared.EntityDecisionResponse
			select {
			case response = <-responseChan:
				// Got response successfully
			case err := <-errorChan:
				log.Printf("Failed to receive decision from entity %d: %v", id, err)
				// Default action: wait
				response = shared.EntityDecisionResponse{
					EntityID: int(id),
					Action: shared.Action{
						Type:     shared.ActionWait,
						EntityID: int(id),
					},
				}
			case <-time.After(s.core.DecisionTimeout):
				log.Printf("Decision timeout for entity %d", id)
				// Default action: wait
				response = shared.EntityDecisionResponse{
					EntityID: int(id),
					Action: shared.Action{
						Type:     shared.ActionWait,
						EntityID: int(id),
					},
				}
			}

			entity.SetLastDecisionTime(time.Since(startTime))
			entity.SetDeciding(false)

			decisionsMu.Lock()
			decisions[id] = response.Action
			decisionsMu.Unlock()
		}(entityID, conn)
	}

	wg.Wait()
	return decisions
}

// Tick advances the simulation by one step
func (s *WebSocketServer) Tick() {
	s.core.Tick()

	// Get tick number for decision requests
	tickNumber := s.core.tickCount

	// Request decisions from all entities
	decisions := s.RequestEntityDecisions(tickNumber)

	// Execute all actions using the core
	for entityID, action := range decisions {
		s.core.ExecuteAction(entityID, action)
	}

	log.Printf("WebSocket simulation tick %d completed", tickNumber)
}

// Start begins the simulation loop
func (s *WebSocketServer) Start() {
	s.mu.Lock()
	if s.isRunning {
		s.mu.Unlock()
		return
	}
	s.isRunning = true
	s.mu.Unlock()

	go s.simulationLoop()
}

// simulationLoop runs the main simulation loop
func (s *WebSocketServer) simulationLoop() {
	ticker := time.NewTicker(s.core.TickRate)
	defer ticker.Stop()

	// Initial state
	s.core.PrintState()

	totalTicks := 1000
	for i := 0; i < totalTicks; i++ {
		select {
		case <-s.stopChan:
			log.Println("WebSocket simulation stopped")
			return
		case tickTime := <-ticker.C:
			log.Printf("Tick %d starting at %s...", i+1, tickTime.Format(time.RFC3339))

			tickStart := time.Now()
			s.Tick()
			s.core.PrintState()
			log.Printf("Tick %d processing completed in %v", i+1, time.Since(tickStart))
		}
	}

	log.Println("WebSocket simulation finished.")
}

// Stop gracefully shuts down the simulation
func (s *WebSocketServer) Stop() {
	log.Println("Shutting down WebSocket simulation server...")

	s.mu.Lock()
	if s.isRunning {
		s.isRunning = false
		close(s.stopChan)
	}

	// Close all entity connections
	for _, conn := range s.entityConnections {
		conn.Close()
	}
	s.mu.Unlock()

	s.core.Stop()
}

// Health check endpoint
func (s *WebSocketServer) HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

// Status endpoint
func (s *WebSocketServer) Status(w http.ResponseWriter, r *http.Request) {
	gridState := s.core.GetGridState()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(gridState)
}

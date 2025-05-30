package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	pb "simulation/proto"
	"simulation/shared"

	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
)

// Cell represents a single cell in the grid that can contain entities
type Cell struct {
	Position shared.Position
	Occupant *RemoteEntity
}

// IsOccupied checks if this cell is currently occupied
func (c *Cell) IsOccupied() bool {
	return c.Occupant != nil
}

// OnEnter handles an entity entering this cell
func (c *Cell) OnEnter(entity *RemoteEntity) {
	c.Occupant = entity
}

// OnExit handles an entity exiting this cell
func (c *Cell) OnExit(entity *RemoteEntity) {
	c.Occupant = nil
}

// Grid represents the 2D space where entities move
type Grid struct {
	Width  int
	Height int
	Cells  [][]Cell
}

// GetCell returns the cell at the specified position
func (g *Grid) GetCell(x, y int) *Cell {
	if x >= 0 && x < g.Width && y >= 0 && y < g.Height {
		return &g.Cells[y][x]
	}
	return nil
}

// RemoteEntity represents an entity that exists on a remote client
type RemoteEntity struct {
	ID                   int
	Position             shared.Position
	DecidedActionDisplay string
	LastDecisionTime     time.Duration
	IsDeciding           bool
	Connection           *websocket.Conn
	mu                   sync.Mutex
}

// SimulationServer manages the grid and remote entities
type SimulationServer struct {
	Grid              Grid
	Entities          map[int]*RemoteEntity
	TickRate          time.Duration
	DecisionTimeout   time.Duration
	Rand              *rand.Rand
	outputFile        *os.File
	mu                sync.Mutex
	nextEntityID      int
	tickCount         int
	DevMode           bool
	upgrader          websocket.Upgrader
	entityConnections map[int]*websocket.Conn
	isRunning         bool
	stopChan          chan bool
}

// NewSimulationServer creates a new simulation server
func NewSimulationServer(width, height int, tickRate time.Duration, decisionTimeout time.Duration, rng *rand.Rand, devMode bool) *SimulationServer {
	outputFileName := "grid_output.txt"
	file, err := os.OpenFile(outputFileName, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		log.Fatalf("Failed to open output file %s: %v", outputFileName, err)
	}

	server := &SimulationServer{
		Grid: Grid{
			Width:  width,
			Height: height,
		},
		TickRate:          tickRate,
		DecisionTimeout:   decisionTimeout,
		Rand:              rng,
		outputFile:        file,
		mu:                sync.Mutex{},
		Entities:          make(map[int]*RemoteEntity),
		entityConnections: make(map[int]*websocket.Conn),
		nextEntityID:      0,
		tickCount:         0,
		DevMode:           devMode,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow connections from any origin
			},
		},
		isRunning: false,
		stopChan:  make(chan bool),
	}

	server.Grid.Cells = make([][]Cell, height)
	for y := 0; y < height; y++ {
		server.Grid.Cells[y] = make([]Cell, width)
		for x := 0; x < width; x++ {
			server.Grid.Cells[y][x] = Cell{
				Position: shared.Position{X: x, Y: y},
			}
		}
	}

	log.Printf("Simulation server initialized. Grid output will be written to %s", outputFileName)
	return server
}

// RegisterEntity handles entity registration via WebSocket
func (s *SimulationServer) RegisterEntity(w http.ResponseWriter, r *http.Request) {
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

	s.mu.Lock()
	defer s.mu.Unlock()

	// Find an empty position for the entity
	var emptyCell *Cell
	for {
		x := s.Rand.Intn(s.Grid.Width)
		y := s.Rand.Intn(s.Grid.Height)
		cell := s.Grid.GetCell(x, y)
		if !cell.IsOccupied() {
			emptyCell = cell
			break
		}
	}

	// Create the remote entity
	entity := &RemoteEntity{
		ID:                   s.nextEntityID,
		Position:             emptyCell.Position,
		DecidedActionDisplay: "--",
		Connection:           conn,
	}

	s.nextEntityID++
	s.Entities[entity.ID] = entity
	s.entityConnections[entity.ID] = conn
	emptyCell.OnEnter(entity)

	// Send registration response
	response := shared.EntityRegistrationResponse{
		Success:  true,
		Message:  "Entity registered successfully",
		EntityID: entity.ID,
	}

	err = conn.WriteJSON(response)
	if err != nil {
		log.Printf("Failed to send registration response: %v", err)
		delete(s.Entities, entity.ID)
		delete(s.entityConnections, entity.ID)
		conn.Close()
		return
	}

	log.Printf("Entity %d registered and connected", entity.ID)

	// Handle entity disconnection
	go s.handleEntityConnection(entity)
}

// handleEntityConnection manages the WebSocket connection for an entity
func (s *SimulationServer) handleEntityConnection(entity *RemoteEntity) {
	defer func() {
		s.mu.Lock()
		// Remove from grid
		cell := s.Grid.GetCell(entity.Position.X, entity.Position.Y)
		if cell != nil && cell.Occupant == entity {
			cell.OnExit(entity)
		}
		// Remove from entities
		delete(s.Entities, entity.ID)
		delete(s.entityConnections, entity.ID)
		s.mu.Unlock()

		entity.Connection.Close()
		log.Printf("Entity %d disconnected", entity.ID)
	}()

	// Set up ping/pong to detect disconnections
	entity.Connection.SetPongHandler(func(string) error {
		return nil
	})

	// Send periodic pings to keep connection alive and detect disconnections
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := entity.Connection.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("Entity %d ping failed: %v", entity.ID, err)
				return
			}
		}
	}
}

// GetGridState returns the current state of the grid
func (s *SimulationServer) GetGridState() shared.GridState {
	s.mu.Lock()
	defer s.mu.Unlock()

	entities := make([]shared.EntityState, 0, len(s.Entities))
	for _, entity := range s.Entities {
		entity.mu.Lock()
		entities = append(entities, shared.EntityState{
			ID:                   entity.ID,
			Position:             entity.Position,
			DecidedActionDisplay: entity.DecidedActionDisplay,
			LastDecisionTime:     entity.LastDecisionTime,
			IsDeciding:           entity.IsDeciding,
		})
		entity.mu.Unlock()
	}

	return shared.GridState{
		Width:    s.Grid.Width,
		Height:   s.Grid.Height,
		Entities: entities,
	}
}

// RequestEntityDecisions sends decision requests to all entities
func (s *SimulationServer) RequestEntityDecisions(tickNumber int) map[int]shared.Action {
	s.mu.Lock()
	entities := make([]*RemoteEntity, 0, len(s.Entities))
	for _, entity := range s.Entities {
		entities = append(entities, entity)
	}
	s.mu.Unlock()

	gridState := s.GetGridState()
	decisions := make(map[int]shared.Action)
	var decisionsMu sync.Mutex // Mutex to protect the decisions map
	var wg sync.WaitGroup

	// Define possible moves
	possibleMoves := []shared.Position{
		{X: 0, Y: 1},  // Up
		{X: 0, Y: -1}, // Down
		{X: 1, Y: 0},  // Right
		{X: -1, Y: 0}, // Left
	}

	for _, entity := range entities {
		wg.Add(1)
		go func(e *RemoteEntity) {
			defer wg.Done()

			e.mu.Lock()
			e.IsDeciding = true
			e.mu.Unlock()

			startTime := time.Now()

			// Send decision request
			request := shared.EntityDecisionRequest{
				EntityID:      e.ID,
				GridState:     gridState,
				TickNumber:    tickNumber,
				PossibleMoves: possibleMoves,
			}

			err := e.Connection.WriteJSON(request)
			if err != nil {
				log.Printf("Failed to send decision request to entity %d: %v", e.ID, err)
				e.mu.Lock()
				e.IsDeciding = false
				e.mu.Unlock()
				return
			}

			// Wait for response with timeout using a channel
			responseChan := make(chan shared.EntityDecisionResponse, 1)
			errorChan := make(chan error, 1)

			go func() {
				var response shared.EntityDecisionResponse
				// Set a read deadline for the response
				e.Connection.SetReadDeadline(time.Now().Add(s.DecisionTimeout))
				err := e.Connection.ReadJSON(&response)
				e.Connection.SetReadDeadline(time.Time{}) // Clear deadline
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
				log.Printf("Failed to receive decision from entity %d: %v", e.ID, err)
				// Default action: wait
				response = shared.EntityDecisionResponse{
					EntityID: e.ID,
					Action: shared.Action{
						Type:     shared.ActionWait,
						EntityID: e.ID,
					},
				}
			case <-time.After(s.DecisionTimeout):
				log.Printf("Decision timeout for entity %d", e.ID)
				// Default action: wait
				response = shared.EntityDecisionResponse{
					EntityID: e.ID,
					Action: shared.Action{
						Type:     shared.ActionWait,
						EntityID: e.ID,
					},
				}
			}

			e.mu.Lock()
			e.LastDecisionTime = time.Since(startTime)
			e.IsDeciding = false
			e.mu.Unlock()

			decisionsMu.Lock()
			decisions[e.ID] = response.Action
			decisionsMu.Unlock()
		}(entity)
	}

	wg.Wait()
	return decisions
}

// ExecuteActions processes all entity decisions
func (s *SimulationServer) ExecuteActions(decisions map[int]shared.Action) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for entityID, action := range decisions {
		entity, exists := s.Entities[entityID]
		if !exists {
			continue
		}

		// Update display action
		entity.DecidedActionDisplay = s.FormatActionForDisplay(action)

		// Execute the action
		switch action.Type {
		case shared.ActionMove:
			s.moveEntity(entity, action.Direction)
		case shared.ActionWait:
			// Entity waits, display already set
		}
	}
}

// moveEntity attempts to move an entity in the specified direction
func (s *SimulationServer) moveEntity(entity *RemoteEntity, direction shared.Position) {
	newX := entity.Position.X + direction.X
	newY := entity.Position.Y + direction.Y

	targetCell := s.Grid.GetCell(newX, newY)
	if targetCell != nil && !targetCell.IsOccupied() {
		currentCell := s.Grid.GetCell(entity.Position.X, entity.Position.Y)
		currentCell.OnExit(entity)

		entity.Position.X = newX
		entity.Position.Y = newY

		targetCell.OnEnter(entity)
	}
	// If the move is invalid, entity stays in place
}

// FormatActionForDisplay converts an Action to a short string representation
func (s *SimulationServer) FormatActionForDisplay(action shared.Action) string {
	switch action.Type {
	case shared.ActionMove:
		switch action.Direction {
		case shared.Position{X: 0, Y: 1}: // Up
			return "UP"
		case shared.Position{X: 0, Y: -1}: // Down
			return "DW"
		case shared.Position{X: -1, Y: 0}: // Left
			return "LT"
		case shared.Position{X: 1, Y: 0}: // Right
			return "RT"
		default:
			return "M?" // Unknown move direction
		}
	case shared.ActionWait:
		return "ST" // Stay
	default:
		return "--" // Unknown action type
	}
}

// Tick advances the simulation by one step
func (s *SimulationServer) Tick() {
	s.mu.Lock()
	s.tickCount++
	tickNumber := s.tickCount
	s.mu.Unlock()

	log.Printf("Simulation tick %d starting...", tickNumber)

	// Request decisions from all entities
	decisions := s.RequestEntityDecisions(tickNumber)

	// Execute all actions
	s.ExecuteActions(decisions)

	log.Printf("Simulation tick %d completed", tickNumber)
}

// PrintState writes the current state of the simulation to the provided file
func (s *SimulationServer) PrintState() {
	gridState := s.GetGridState()

	_, err := s.outputFile.Seek(0, 0)
	if err != nil {
		log.Printf("Error seeking in output file: %v", err)
		return
	}
	err = s.outputFile.Truncate(0)
	if err != nil {
		log.Printf("Error truncating output file: %v", err)
		return
	}

	fmt.Fprintf(s.outputFile, "Tick: %s - Current Grid State & Entity Actions:\n", time.Now().Format(time.RFC3339))

	// Create a grid representation
	grid := make([][]string, s.Grid.Height)
	for y := 0; y < s.Grid.Height; y++ {
		grid[y] = make([]string, s.Grid.Width)
		for x := 0; x < s.Grid.Width; x++ {
			grid[y][x] = ".      "
		}
	}

	// Place entities on the grid
	for _, entity := range gridState.Entities {
		if entity.Position.Y >= 0 && entity.Position.Y < s.Grid.Height &&
			entity.Position.X >= 0 && entity.Position.X < s.Grid.Width {
			grid[entity.Position.Y][entity.Position.X] = fmt.Sprintf("E%d[%s] ", entity.ID, entity.DecidedActionDisplay)
		}
	}

	// Print grid (top to bottom)
	for y := s.Grid.Height - 1; y >= 0; y-- {
		for x := 0; x < s.Grid.Width; x++ {
			fmt.Fprint(s.outputFile, grid[y][x])
			if x < s.Grid.Width-1 {
				fmt.Fprint(s.outputFile, " ")
			}
		}
		fmt.Fprintln(s.outputFile)
	}

	// Add entity decision statistics
	fmt.Fprintln(s.outputFile, "\nEntity Decision Times:")
	for _, entity := range gridState.Entities {
		status := "idle"
		if entity.IsDeciding {
			status = "DECIDING"
		}
		fmt.Fprintf(s.outputFile, "Entity %d: %v (%s)\n", entity.ID, entity.LastDecisionTime, status)
	}

	err = s.outputFile.Sync()
	if err != nil {
		log.Printf("Error syncing output file: %v", err)
	}
}

// Start begins the simulation loop
func (s *SimulationServer) Start() {
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
func (s *SimulationServer) simulationLoop() {
	ticker := time.NewTicker(s.TickRate)
	defer ticker.Stop()

	// Initial state
	s.PrintState()

	totalTicks := 1000
	for i := 0; i < totalTicks; i++ {
		select {
		case <-s.stopChan:
			log.Println("Simulation stopped")
			return
		case tickTime := <-ticker.C:
			log.Printf("Tick %d starting at %s...", i+1, tickTime.Format(time.RFC3339))

			tickStart := time.Now()
			s.Tick()
			s.PrintState()
			log.Printf("Tick %d processing completed in %v", i+1, time.Since(tickStart))
		}
	}

	log.Println("Simulation finished.")
}

// Stop gracefully shuts down the simulation
func (s *SimulationServer) Stop() {
	log.Println("Shutting down simulation server...")

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

	if s.outputFile != nil {
		log.Printf("Closing output file: %s", s.outputFile.Name())
		s.outputFile.Close()
	}
}

// Health check endpoint
func (s *SimulationServer) HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

// Status endpoint
func (s *SimulationServer) Status(w http.ResponseWriter, r *http.Request) {
	gridState := s.GetGridState()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(gridState)
}

func main() {
	// Parse command line flags
	devModePtr := flag.Bool("dev", true, "Run in development mode")
	portPtr := flag.String("port", "8080", "Port to run the server on")
	grpcModePtr := flag.Bool("grpc", true, "Use gRPC instead of WebSocket")
	grpcPortPtr := flag.String("grpc-port", "9090", "Port to run the gRPC server on")
	flag.Parse()

	if *devModePtr {
		log.Println("Running in development mode")
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	if *grpcModePtr {
		// Start gRPC server
		log.Println("Starting gRPC simulation server...")
		grpcServer := NewGRPCSimulationServer(5, 5, time.Second, 5*time.Second, rng, *devModePtr)
		defer grpcServer.Stop()

		// Create gRPC server
		s := grpc.NewServer()
		pb.RegisterSimulationServiceServer(s, grpcServer)

		// Listen on the specified port
		lis, err := net.Listen("tcp", ":"+*grpcPortPtr)
		if err != nil {
			log.Fatalf("Failed to listen on port %s: %v", *grpcPortPtr, err)
		}

		log.Printf("gRPC server listening on port %s", *grpcPortPtr)

		// Start HTTP health endpoint for Kubernetes probes
		go func() {
			http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("healthy"))
			})
			http.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
				gridState, _ := grpcServer.GetGridState(r.Context(), &pb.Empty{})
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"width":    gridState.Width,
					"height":   gridState.Height,
					"entities": len(gridState.Entities),
				})
			})
			log.Printf("HTTP health server listening on port %s", *portPtr)
			log.Fatal(http.ListenAndServe(":"+*portPtr, nil))
		}()

		// Start the simulation loop in a separate goroutine
		go func() {
			ticker := time.NewTicker(grpcServer.TickRate)
			defer ticker.Stop()

			// Add some test entities
			for i := 0; i < 3; i++ {
				_, err := grpcServer.RegisterEntity(nil, &pb.EntityRegistrationRequest{})
				if err != nil {
					log.Printf("Failed to register test entity %d: %v", i, err)
				}
			}

			// Initial state output
			grpcServer.PrintState()

			for {
				select {
				case <-ticker.C:
					grpcServer.Tick()
				}
			}
		}()

		// Start the gRPC server
		if err := s.Serve(lis); err != nil {
			log.Fatalf("Failed to serve gRPC: %v", err)
		}
	} else {
		log.Println("Starting WebSocket simulation server...")
		server := NewSimulationServer(5, 5, time.Second, 5*time.Second, rng, *devModePtr)
		defer server.Stop()

		// Set up HTTP routes
		http.HandleFunc("/register", server.RegisterEntity)
		http.HandleFunc("/health", server.HealthCheck)
		http.HandleFunc("/status", server.Status)

		// Start the simulation
		server.Start()

		// Start HTTP server
		log.Printf("Starting WebSocket simulation server on port %s", *portPtr)
		log.Fatal(http.ListenAndServe(":"+*portPtr, nil))
	}
}

package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"

	pb "simulation/proto"
	"simulation/shared"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Helper functions to convert between protobuf and shared types
func pbPositionToShared(pos *pb.Position) shared.Position {
	if pos == nil {
		return shared.Position{}
	}
	return shared.Position{X: int(pos.X), Y: int(pos.Y)}
}

func sharedPositionToPb(pos shared.Position) *pb.Position {
	return &pb.Position{X: int32(pos.X), Y: int32(pos.Y)}
}

// GRPCSimulationServer implements the SimulationServiceServer interface
type GRPCSimulationServer struct {
	pb.UnimplementedSimulationServiceServer

	Grid            Grid
	Entities        map[int32]*GRPCEntity
	TickRate        time.Duration
	DecisionTimeout time.Duration
	Rand            *rand.Rand
	mu              sync.RWMutex
	nextEntityID    int32
	tickCount       int32
	DevMode         bool
	outputFile      *os.File

	// Entity streams for bidirectional communication
	entityStreams map[int32]pb.SimulationService_EntityStreamServer
	streamsMu     sync.RWMutex
}

// GRPCEntity represents an entity in the gRPC-based simulation
type GRPCEntity struct {
	ID                   int32
	Position             *pb.Position
	DecidedActionDisplay string
	LastDecisionTime     time.Duration
	IsDeciding           bool
	mu                   sync.Mutex
}

// NewGRPCSimulationServer creates a new gRPC simulation server
func NewGRPCSimulationServer(width, height int, tickRate time.Duration, decisionTimeout time.Duration, rng *rand.Rand, devMode bool) *GRPCSimulationServer {
	outputFileName := "grid_output.txt"
	file, err := os.OpenFile(outputFileName, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		log.Fatalf("Failed to open output file %s: %v", outputFileName, err)
	}

	server := &GRPCSimulationServer{
		Grid: Grid{
			Width:  width,
			Height: height,
		},
		TickRate:        tickRate,
		DecisionTimeout: decisionTimeout,
		Rand:            rng,
		Entities:        make(map[int32]*GRPCEntity),
		entityStreams:   make(map[int32]pb.SimulationService_EntityStreamServer),
		nextEntityID:    1,
		tickCount:       0,
		DevMode:         devMode,
		outputFile:      file,
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

	log.Printf("gRPC Simulation server initialized with %dx%d grid", width, height)
	log.Printf("Grid output will be written to %s", outputFileName)
	return server
}

// RegisterEntity implements the RegisterEntity RPC
func (s *GRPCSimulationServer) RegisterEntity(ctx context.Context, req *pb.EntityRegistrationRequest) (*pb.EntityRegistrationResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Find an empty position for the entity
	var emptyPos *pb.Position
	for attempts := 0; attempts < 100; attempts++ {
		x := int32(s.Rand.Intn(s.Grid.Width))
		y := int32(s.Rand.Intn(s.Grid.Height))
		cell := s.Grid.GetCell(int(x), int(y))
		if cell != nil && !cell.IsOccupied() {
			emptyPos = &pb.Position{X: x, Y: y}
			break
		}
	}

	if emptyPos == nil {
		return &pb.EntityRegistrationResponse{
			Success: false,
			Message: "No empty positions available",
		}, status.Error(codes.ResourceExhausted, "grid is full")
	}

	// Create the entity
	entity := &GRPCEntity{
		ID:                   s.nextEntityID,
		Position:             emptyPos,
		DecidedActionDisplay: "--",
	}

	s.nextEntityID++
	s.Entities[entity.ID] = entity

	// Mark the cell as occupied
	cell := s.Grid.GetCell(int(emptyPos.X), int(emptyPos.Y))
	if cell != nil {
		cell.OnEnter(&RemoteEntity{
			ID:       int(entity.ID),
			Position: pbPositionToShared(emptyPos),
		})
	}

	log.Printf("Entity %d registered at position (%d, %d)", entity.ID, emptyPos.X, emptyPos.Y)

	return &pb.EntityRegistrationResponse{
		Success:  true,
		Message:  "Entity registered successfully",
		EntityId: entity.ID,
	}, nil
}

// RequestDecision implements the RequestDecision RPC
func (s *GRPCSimulationServer) RequestDecision(ctx context.Context, req *pb.EntityDecisionRequest) (*pb.EntityDecisionResponse, error) {
	s.mu.RLock()
	entity, exists := s.Entities[req.EntityId]
	s.mu.RUnlock()

	if !exists {
		return nil, status.Error(codes.NotFound, "entity not found")
	}

	entity.mu.Lock()
	entity.IsDeciding = true
	entity.mu.Unlock()

	startTime := time.Now()

	// For now, implement a simple decision logic
	// In a real implementation, this would forward the request to the entity client
	action := &pb.Action{
		Type:     pb.ActionType_ACTION_WAIT,
		EntityId: req.EntityId,
	}

	// Simple random movement decision
	if s.Rand.Float32() < 0.7 { // 70% chance to move
		directions := []*pb.Position{
			{X: 0, Y: 1},  // Up
			{X: 0, Y: -1}, // Down
			{X: 1, Y: 0},  // Right
			{X: -1, Y: 0}, // Left
		}
		direction := directions[s.Rand.Intn(len(directions))]
		action = &pb.Action{
			Type:      pb.ActionType_ACTION_MOVE,
			Direction: direction,
			EntityId:  req.EntityId,
		}
	}

	entity.mu.Lock()
	entity.LastDecisionTime = time.Since(startTime)
	entity.IsDeciding = false
	entity.DecidedActionDisplay = s.FormatActionForDisplay(action)
	entity.mu.Unlock()

	return &pb.EntityDecisionResponse{
		EntityId: req.EntityId,
		Action:   action,
	}, nil
}

// GetGridState implements the GetGridState RPC
func (s *GRPCSimulationServer) GetGridState(ctx context.Context, req *pb.Empty) (*pb.GridState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entities := make([]*pb.EntityState, 0, len(s.Entities))
	for _, entity := range s.Entities {
		entity.mu.Lock()
		entities = append(entities, &pb.EntityState{
			Id:                   entity.ID,
			Position:             entity.Position,
			DecidedActionDisplay: entity.DecidedActionDisplay,
			LastDecisionTimeNs:   entity.LastDecisionTime.Nanoseconds(),
			IsDeciding:           entity.IsDeciding,
		})
		entity.mu.Unlock()
	}

	return &pb.GridState{
		Width:    int32(s.Grid.Width),
		Height:   int32(s.Grid.Height),
		Entities: entities,
	}, nil
}

// HealthCheck implements the HealthCheck RPC
func (s *GRPCSimulationServer) HealthCheck(ctx context.Context, req *pb.Empty) (*pb.HealthResponse, error) {
	return &pb.HealthResponse{
		Status: "healthy",
	}, nil
}

// EntityStream implements the bidirectional streaming RPC
func (s *GRPCSimulationServer) EntityStream(stream pb.SimulationService_EntityStreamServer) error {
	// This would be used for real-time bidirectional communication with entity clients
	// For now, we'll implement a basic version

	log.Println("New entity stream connected")

	// Keep the stream alive and handle incoming messages
	for {
		response, err := stream.Recv()
		if err != nil {
			log.Printf("Entity stream error: %v", err)
			return err
		}

		log.Printf("Received decision from entity %d: %v", response.EntityId, response.Action)

		// Process the decision and potentially send back new requests
		// This is where you'd implement the real-time decision loop
	}
}

// FormatActionForDisplay converts a protobuf Action to a short string representation
func (s *GRPCSimulationServer) FormatActionForDisplay(action *pb.Action) string {
	switch action.Type {
	case pb.ActionType_ACTION_MOVE:
		if action.Direction != nil {
			switch {
			case action.Direction.X == 0 && action.Direction.Y == 1: // Up
				return "UP"
			case action.Direction.X == 0 && action.Direction.Y == -1: // Down
				return "DW"
			case action.Direction.X == -1 && action.Direction.Y == 0: // Left
				return "LT"
			case action.Direction.X == 1 && action.Direction.Y == 0: // Right
				return "RT"
			default:
				return "M?" // Unknown move direction
			}
		}
		return "M?"
	case pb.ActionType_ACTION_WAIT:
		return "ST" // Stay
	default:
		return "--" // Unknown action type
	}
}

// ExecuteAction processes a single entity action
func (s *GRPCSimulationServer) ExecuteAction(entityID int32, action *pb.Action) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entity, exists := s.Entities[entityID]
	if !exists {
		return
	}

	// Update display action
	entity.DecidedActionDisplay = s.FormatActionForDisplay(action)

	// Execute the action
	switch action.Type {
	case pb.ActionType_ACTION_MOVE:
		if action.Direction != nil {
			s.moveGRPCEntity(entity, action.Direction)
		}
	case pb.ActionType_ACTION_WAIT:
		// Entity waits, display already set
	}
}

// moveGRPCEntity attempts to move an entity in the specified direction
func (s *GRPCSimulationServer) moveGRPCEntity(entity *GRPCEntity, direction *pb.Position) {
	newX := int(entity.Position.X + direction.X)
	newY := int(entity.Position.Y + direction.Y)

	targetCell := s.Grid.GetCell(newX, newY)
	if targetCell != nil && !targetCell.IsOccupied() {
		currentCell := s.Grid.GetCell(int(entity.Position.X), int(entity.Position.Y))
		if currentCell != nil {
			currentCell.OnExit(&RemoteEntity{ID: int(entity.ID)})
		}

		entity.Position.X = int32(newX)
		entity.Position.Y = int32(newY)

		targetCell.OnEnter(&RemoteEntity{
			ID:       int(entity.ID),
			Position: shared.Position{X: newX, Y: newY},
		})
	}
	// If the move is invalid, entity stays in place
}

// Tick advances the simulation by one step for gRPC entities
func (s *GRPCSimulationServer) Tick() {
	s.mu.Lock()
	s.tickCount++
	tickNumber := s.tickCount
	entities := make([]*GRPCEntity, 0, len(s.Entities))
	for _, entity := range s.Entities {
		entities = append(entities, entity)
	}
	s.mu.Unlock()

	log.Printf("gRPC Simulation tick %d starting...", tickNumber)

	// Get current grid state
	gridState, _ := s.GetGridState(context.Background(), &pb.Empty{})

	// Request decisions from all entities
	var wg sync.WaitGroup
	for _, entity := range entities {
		wg.Add(1)
		go func(e *GRPCEntity) {
			defer wg.Done()

			// Create decision request
			req := &pb.EntityDecisionRequest{
				EntityId:   e.ID,
				GridState:  gridState,
				TickNumber: tickNumber,
				PossibleMoves: []*pb.Position{
					{X: 0, Y: 1},  // Up
					{X: 0, Y: -1}, // Down
					{X: 1, Y: 0},  // Right
					{X: -1, Y: 0}, // Left
				},
			}

			// Get decision (this calls our own RequestDecision method for now)
			ctx, cancel := context.WithTimeout(context.Background(), s.DecisionTimeout)
			defer cancel()

			response, err := s.RequestDecision(ctx, req)
			if err != nil {
				log.Printf("Failed to get decision from entity %d: %v", e.ID, err)
				return
			}

			// Execute the action
			s.ExecuteAction(e.ID, response.Action)
		}(entity)
	}

	wg.Wait()

	// Print the current state to file
	s.PrintState()

	log.Printf("gRPC Simulation tick %d completed", tickNumber)
}

// PrintState writes the current state of the simulation to the output file
func (s *GRPCSimulationServer) PrintState() {
	gridState, _ := s.GetGridState(context.Background(), &pb.Empty{})

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
		if entity.Position.Y >= 0 && entity.Position.Y < int32(s.Grid.Height) &&
			entity.Position.X >= 0 && entity.Position.X < int32(s.Grid.Width) {
			grid[entity.Position.Y][entity.Position.X] = fmt.Sprintf("E%d[%s] ", entity.Id, entity.DecidedActionDisplay)
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
		decisionTime := time.Duration(entity.LastDecisionTimeNs)
		fmt.Fprintf(s.outputFile, "Entity %d: %v (%s)\n", entity.Id, decisionTime, status)
	}

	err = s.outputFile.Sync()
	if err != nil {
		log.Printf("Error syncing output file: %v", err)
	}
}

// Stop gracefully shuts down the gRPC simulation server
func (s *GRPCSimulationServer) Stop() {
	log.Println("Shutting down gRPC simulation server...")

	if s.outputFile != nil {
		log.Printf("Closing output file: %s", s.outputFile.Name())
		s.outputFile.Close()
	}
}

package main

import (
	"context"
	"log"
	"math"
	"math/rand"
	"simulation/shared"
	"sync"
	"time"

	pb "simulation/proto"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Helper functions to convert between protobuf and shared types
func pbPositionToShared(pos *pb.Position) shared.Position {
	if pos == nil {
		return shared.Position{}
	}
	return shared.Position{X: pos.X, Y: pos.Y}
}

func sharedPositionToPb(pos shared.Position) *pb.Position {
	// Check for overflow and clamp values
	x := pos.X
	if x > math.MaxInt32 {
		x = math.MaxInt32
	} else if x < math.MinInt32 {
		x = math.MinInt32
	}

	y := pos.Y
	if y > math.MaxInt32 {
		y = math.MaxInt32
	} else if y < math.MinInt32 {
		y = math.MinInt32
	}

	return &pb.Position{X: int32(x), Y: int32(y)} //nolint:gosec // Overflow protection above
}

func sharedEntityStateToPb(entity shared.EntityInterface) *pb.EntityState {
	return &pb.EntityState{
		Id:                   entity.GetID(),
		Position:             sharedPositionToPb(entity.GetPosition()),
		DecidedActionDisplay: entity.GetDecidedActionDisplay(),
		LastDecisionTimeNs:   entity.GetLastDecisionTime().Nanoseconds(),
		IsDeciding:           entity.IsDeciding(),
	}
}

func pbActionToShared(action *pb.Action) shared.Action {
	sharedAction := shared.Action{
		EntityID: int(action.EntityId),
	}

	switch action.Type {
	case pb.ActionType_ACTION_MOVE:
		sharedAction.Type = shared.ActionMove
		if action.Direction != nil {
			sharedAction.Direction = pbPositionToShared(action.Direction)
		}
	case pb.ActionType_ACTION_WAIT:
		sharedAction.Type = shared.ActionWait
	}

	return sharedAction
}

// GRPCEntity implements EntityInterface for gRPC connections
type GRPCEntity struct {
	BaseEntity
}

// NewGRPCEntity creates a new gRPC entity
func NewGRPCEntity(id int32) *GRPCEntity {
	return &GRPCEntity{
		BaseEntity: NewBaseEntity(id),
	}
}

// GRPCSimulationServer implements the SimulationServiceServer interface
type GRPCSimulationServer struct {
	pb.UnimplementedSimulationServiceServer

	core *SimulationCore
	Rand *rand.Rand

	// Entity streams for bidirectional communication
	entityStreams map[int32]pb.SimulationService_EntityStreamServer
	streamsMu     sync.RWMutex

	// Decision timing tracking
	decisionStartTimes map[int32]time.Time
	timingMu           sync.RWMutex
}

// NewGRPCSimulationServer creates a new gRPC simulation server
func NewGRPCSimulationServer(core *SimulationCore) *GRPCSimulationServer {
	server := &GRPCSimulationServer{
		core:               core,
		Rand:               core.Rand,
		entityStreams:      make(map[int32]pb.SimulationService_EntityStreamServer),
		decisionStartTimes: make(map[int32]time.Time),
	}

	log.Printf("gRPC Simulation server initialized")
	return server
}

// RegisterEntity implements the RegisterEntity RPC
func (s *GRPCSimulationServer) RegisterEntity(ctx context.Context, req *pb.EntityRegistrationRequest) (*pb.EntityRegistrationResponse, error) {
	// Create gRPC entity
	entityID := s.core.GetNextEntityID()
	entity := NewGRPCEntity(entityID)

	// Register with core
	err := s.core.RegisterEntity(entity)
	if err != nil {
		return &pb.EntityRegistrationResponse{
			Success: false,
			Message: err.Error(),
		}, status.Error(codes.ResourceExhausted, err.Error())
	}

	log.Printf("gRPC Entity %d registered at position (%d, %d)", entityID, entity.GetPosition().X, entity.GetPosition().Y)

	return &pb.EntityRegistrationResponse{
		Success:  true,
		Message:  "Entity registered successfully",
		EntityId: entityID,
	}, nil
}

// RequestDecision implements the RequestDecision RPC
func (s *GRPCSimulationServer) RequestDecision(ctx context.Context, req *pb.EntityDecisionRequest) (*pb.EntityDecisionResponse, error) {
	return nil, status.Error(codes.Unimplemented, "RequestDecision is not used in the new architecture")
}

// GetGridState implements the GetGridState RPC
func (s *GRPCSimulationServer) GetGridState(ctx context.Context, req *pb.Empty) (*pb.GridState, error) {
	gridState := s.core.GetGridState()

	cells := make([]*pb.Cell, 0, gridState.Width*gridState.Height)
	for _, cell := range gridState.Cells {
		cells = append(cells, &pb.Cell{
			Position: sharedPositionToPb(cell.Position),
			Occupant: sharedEntityStateToPb(cell.Occupant),
		})
	}

	return &pb.GridState{
		Width:  int32(gridState.Width),
		Height: int32(gridState.Height),
		Cells:  cells,
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
	// The first message from the client identifies the entity.
	// This message is expected to be an EntityDecisionResponse with an entity ID and no action.
	initialMsg, err := stream.Recv()
	if err != nil {
		log.Printf("Failed to receive initial message from entity stream: %v", err)
		return err
	}
	entityID := initialMsg.EntityId

	// Validate and register the stream
	if _, exists := s.core.Entities[entityID]; !exists {
		return status.Errorf(codes.NotFound, "entity %d not found", entityID)
	}

	s.streamsMu.Lock()
	if _, exists := s.entityStreams[entityID]; exists {
		s.streamsMu.Unlock()
		return status.Errorf(codes.AlreadyExists, "stream for entity %d already exists", entityID)
	}
	s.entityStreams[entityID] = stream
	s.streamsMu.Unlock()
	log.Printf("Entity stream for entity %d registered", entityID)

	defer func() {
		s.streamsMu.Lock()
		delete(s.entityStreams, entityID)
		s.streamsMu.Unlock()
		log.Printf("Entity stream for entity %d deregistered", entityID)
	}()

	// Loop to receive decisions from the client
	for {
		response, err := stream.Recv()
		if err != nil {
			log.Printf("Entity stream for entity %d disconnected: %v", entityID, err)
			return err
		}

		// Track decision completion timing
		if entity, exists := s.core.Entities[entityID]; exists {
			entity.SetDeciding(false)

			// Calculate decision time
			s.timingMu.Lock()
			if startTime, hasStartTime := s.decisionStartTimes[entityID]; hasStartTime {
				decisionDuration := time.Since(startTime)
				entity.SetLastDecisionTime(decisionDuration)
				delete(s.decisionStartTimes, entityID)
			}
			s.timingMu.Unlock()
		}

		// Process the decision
		log.Printf("Received decision from entity %d: %v", response.EntityId, response.Action)
		sharedAction := pbActionToShared(response.Action)
		s.core.ExecuteAction(response.EntityId, sharedAction)
	}
}

// requestDecisionFromClient sends a decision request to a specific client
func (s *GRPCSimulationServer) requestDecisionFromClient(entityID int32) {
	s.streamsMu.RLock()
	stream, exists := s.entityStreams[entityID]
	s.streamsMu.RUnlock()

	if !exists {
		log.Printf("Cannot request decision: no stream for entity %d", entityID)
		return
	}

	// Mark entity as deciding and track start time
	startTime := time.Now()
	if entity, exists := s.core.Entities[entityID]; exists {
		entity.SetDeciding(true)
		s.timingMu.Lock()
		s.decisionStartTimes[entityID] = startTime
		s.timingMu.Unlock()
	}

	// Get latest grid state for the request
	gridState, _ := s.GetGridState(context.Background(), &pb.Empty{})

	req := &pb.EntityDecisionRequest{
		EntityId:   entityID,
		GridState:  gridState,
		TickNumber: s.core.GetTickCount(),
		// Simplified possible moves for now, can be expanded later
		PossibleMoves: []*pb.Position{
			{X: 0, Y: 1}, {X: 0, Y: -1}, {X: 1, Y: 0}, {X: -1, Y: 0},
		},
	}

	if err := stream.Send(req); err != nil {
		log.Printf("Failed to send decision request to entity %d: %v", entityID, err)
		// If sending fails, mark as not deciding
		if entity, exists := s.core.Entities[entityID]; exists {
			entity.SetDeciding(false)
		}
	}
}

// Tick advances the simulation by one step for gRPC entities
func (s *GRPCSimulationServer) Tick() {
	tickNumber := s.core.Tick()
	log.Printf("gRPC server processing tick %d", tickNumber)

	// Request decisions from all connected clients
	s.streamsMu.RLock()
	defer s.streamsMu.RUnlock()
	for entityID := range s.entityStreams {
		go s.requestDecisionFromClient(entityID)
	}
}

// Stop gracefully shuts down the gRPC simulation server
func (s *GRPCSimulationServer) Stop() {
	log.Println("Shutting down gRPC simulation server...")
	s.core.Stop()
}

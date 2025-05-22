package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"
)

// Position represents a 2D coordinate on the grid
type Position struct {
	X, Y int
}

// ActionType defines the type of action an entity can take
type ActionType int

const (
	ActionMove ActionType = iota
	ActionWait
	// Future actions will be added here
)

// Action represents a decision made by an entity
type Action struct {
	Type      ActionType
	Direction Position
	Entity    *Entity
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

// Simulation manages the grid and entities
type Simulation struct {
	Grid                Grid
	Entities            map[int]*Entity // Change to map for easier management
	TickRate            time.Duration
	DecisionTimeout     time.Duration // Maximum time to wait for decisions
	Rand                *rand.Rand
	EventBus            chan Event // Changed from CellEvent to general Event
	stopEventProcessing chan bool
	outputFile          *os.File
	mu                  sync.Mutex // Mutex to protect state during concurrent ticks
	nextEntityID        int        // For generating unique entity IDs
	tickCount           int        // Counter for simulation ticks
	DevMode             bool       // Flag to enable development mode (mock API calls)
}

// RegisterEntity adds an entity to the simulation
func (s *Simulation) RegisterEntity(entity *Entity) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if the entity is already registered
	if _, exists := s.Entities[entity.ID]; exists {
		return fmt.Errorf("entity with ID %d is already registered", entity.ID)
	}

	// Add the entity to the simulation
	s.Entities[entity.ID] = entity
	log.Printf("Entity %d registered with simulation", entity.ID)
	return nil
}

// UnregisterEntity removes an entity from the simulation
func (s *Simulation) UnregisterEntity(entity *Entity) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if the entity is registered
	if _, exists := s.Entities[entity.ID]; !exists {
		return fmt.Errorf("entity with ID %d is not registered", entity.ID)
	}

	// Remove from its current cell if any
	cell := s.Grid.GetCell(entity.Position.X, entity.Position.Y)
	if cell != nil {
		cell.OnExit(entity)
	}

	// Remove the entity from the simulation
	delete(s.Entities, entity.ID)
	log.Printf("Entity %d unregistered from simulation", entity.ID)
	return nil
}

// PublishEvent distributes an event to all registered entities
func (s *Simulation) PublishEvent(event Event) {
	s.EventBus <- event
}

// ProcessEntityDecision handles an entity's decision
func (s *Simulation) ProcessEntityDecision(event EntityDecisionEvent) {
	entity := event.Entity
	action := event.Action

	// Update the display action
	entity.DecidedActionDisplay = FormatActionForDisplay(action)

	// Execute the action
	s.ExecuteAction(action)
}

// NewSimulation creates a new simulation with specified parameters
func NewSimulation(width, height int, tickRate time.Duration, decisionTimeout time.Duration, rng *rand.Rand, devMode bool) *Simulation {
	outputFileName := "grid_output.txt"
	file, err := os.OpenFile(outputFileName, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		log.Fatalf("Failed to open output file %s: %v", outputFileName, err)
	}

	sim := &Simulation{
		Grid: Grid{
			Width:  width,
			Height: height,
		},
		TickRate:            tickRate,
		DecisionTimeout:     decisionTimeout,
		Rand:                rng,
		EventBus:            make(chan Event, 100),
		stopEventProcessing: make(chan bool),
		outputFile:          file,
		mu:                  sync.Mutex{},
		Entities:            make(map[int]*Entity),
		nextEntityID:        0,
		tickCount:           0,
		DevMode:             devMode,
	}

	sim.Grid.Cells = make([][]Cell, height)
	for y := 0; y < height; y++ {
		sim.Grid.Cells[y] = make([]Cell, width)
		for x := 0; x < width; x++ {
			sim.Grid.Cells[y][x] = Cell{
				Position: Position{X: x, Y: y},
			}
		}
	}

	go sim.processEvents()
	log.Printf("Simulation initialized. Grid output will be written to %s", outputFileName)
	return sim
}

// CreateEntity creates a new entity at a random empty position
func (s *Simulation) CreateEntity() *Entity {
	s.mu.Lock()
	defer s.mu.Unlock()

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

	entity := NewEntity(s.nextEntityID, emptyCell.Position)
	s.nextEntityID++

	// Register the entity with the simulation
	s.Entities[entity.ID] = entity

	// Set the entity's reference to the simulation
	entity.sim = s

	emptyCell.OnEnter(entity)

	return entity
}

// processEvents listens on the EventBus and broadcasts events to all entities.
func (s *Simulation) processEvents() {
	log.Println("[EventBus] Starting event processor...")
	for {
		select {
		case event := <-s.EventBus:
			// Handle different event types
			switch event.GetType() {
			case EventEntityDecision:
				// Process entity decision
				decisionEvent := event.(EntityDecisionEvent)
				s.ProcessEntityDecision(decisionEvent)
			}

			// Broadcast the event to all entities
			s.mu.Lock()
			for _, entity := range s.Entities {
				// Create a copy of the event reference for the goroutine
				currentEvent := event
				currentEntity := entity

				// Skip sending the event to the entity that triggered it in some cases
				if event.GetType() == EventEntityDecision {
					decisionEvent := event.(EntityDecisionEvent)
					if decisionEvent.Entity == currentEntity {
						continue
					}
				}

				// Send the event asynchronously to prevent blocking
				go func(e *Entity, evt Event) {
					select {
					case e.eventChan <- evt:
						// Event delivered
					default:
						// Entity's event channel is full, events might be dropped
						log.Printf("Entity %d event channel full, dropping event of type %v", e.ID, evt.GetType())
					}
				}(currentEntity, currentEvent)
			}
			s.mu.Unlock()
		case <-s.stopEventProcessing:
			log.Println("[EventBus] Stopping event processor...")
			return
		}
	}
}

// Stop gracefully shuts down the simulation
func (s *Simulation) Stop() {
	log.Println("Shutting down simulation...")

	// Stop all running entities
	s.mu.Lock()
	for _, entity := range s.Entities {
		entity.Stop()
	}
	s.mu.Unlock()

	close(s.stopEventProcessing)
	if s.outputFile != nil {
		log.Printf("Closing output file: %s", s.outputFile.Name())
		s.outputFile.Close()
	}
}

// ExecuteAction performs the action chosen by an entity
func (s *Simulation) ExecuteAction(action Action) {
	switch action.Type {
	case ActionMove:
		s.MoveEntity(action.Entity, action.Direction)
	case ActionWait:
		// Entity waits, display already set during action collection
	}
}

// MoveEntity attempts to move an entity in the specified direction
func (s *Simulation) MoveEntity(entity *Entity, direction Position) {
	newX := entity.Position.X + direction.X
	newY := entity.Position.Y + direction.Y

	targetCell := s.Grid.GetCell(newX, newY)
	if targetCell != nil && entity.CanMoveTo(targetCell) {
		currentCell := s.Grid.GetCell(entity.Position.X, entity.Position.Y)
		currentCell.OnExit(entity)

		entity.Position.X = newX
		entity.Position.Y = newY

		targetCell.OnEnter(entity)
		s.PublishEvent(CellEvent{Type: EventCellEnter, PreviousCell: currentCell, NewCell: targetCell, TriggeringEntity: entity})
	}
	// If the move is invalid, entity stays in place
}

// Tick advances the simulation by one step
func (s *Simulation) Tick() {
	s.mu.Lock()
	s.tickCount++
	tickNumber := s.tickCount
	s.mu.Unlock()

	// Publish a tick event to all entities
	tickEvent := SimulationTickEvent{
		TickNumber: tickNumber,
		Timestamp:  time.Now(),
	}
	s.PublishEvent(tickEvent)

	// No need to process decisions here - they are handled by the event system
	log.Printf("Simulation tick %d completed", tickNumber)
}

// PrintState writes the current state of the simulation to the provided file
func (s *Simulation) PrintState(file *os.File) {
	// Take a snapshot of the relevant state to minimize lock time
	s.mu.Lock()

	// Create a snapshot
	type EntitySnapshot struct {
		ID                   int
		Position             Position
		LastDecisionTime     time.Duration
		DecidedActionDisplay string
		IsDeciding           bool
	}

	snapshots := make([]EntitySnapshot, 0, len(s.Entities))
	gridWidth := s.Grid.Width
	gridHeight := s.Grid.Height

	// Create a copy of the grid cell occupancy
	gridSnapshot := make([][]bool, gridHeight)
	gridOccupants := make([][]int, gridHeight)
	gridActions := make([][]string, gridHeight)

	for y := 0; y < gridHeight; y++ {
		gridSnapshot[y] = make([]bool, gridWidth)
		gridOccupants[y] = make([]int, gridWidth)
		gridActions[y] = make([]string, gridWidth)

		for x := 0; x < gridWidth; x++ {
			cell := s.Grid.GetCell(x, y)
			gridSnapshot[y][x] = cell.IsOccupied()
			if cell.IsOccupied() {
				gridOccupants[y][x] = cell.Occupant.ID
				gridActions[y][x] = cell.Occupant.DecidedActionDisplay
			}
		}
	}

	// Take a snapshot of each entity
	for _, entity := range s.Entities {
		entity.mu.Lock()
		snapshots = append(snapshots, EntitySnapshot{
			ID:                   entity.ID,
			Position:             entity.Position,
			LastDecisionTime:     entity.LastDecisionTime,
			DecidedActionDisplay: entity.DecidedActionDisplay,
			IsDeciding:           entity.IsDeciding,
		})
		entity.mu.Unlock()
	}

	// Done with the simulation lock
	s.mu.Unlock()

	// Now process the snapshot without holding the lock
	_, err := file.Seek(0, 0)
	if err != nil {
		log.Printf("Error seeking in output file: %v", err)
		return
	}
	err = file.Truncate(0)
	if err != nil {
		log.Printf("Error truncating output file: %v", err)
		return
	}

	fmt.Fprintf(file, "Tick: %s - Current Grid State & Entity Actions:\n", time.Now().Format(time.RFC3339))
	for y := gridHeight - 1; y >= 0; y-- {
		for x := 0; x < gridWidth; x++ {
			if gridSnapshot[y][x] {
				fmt.Fprintf(file, "E%d[%s] ", gridOccupants[y][x], gridActions[y][x])
			} else {
				fmt.Fprint(file, ".      ")
			}
			if x < gridWidth-1 {
				fmt.Fprint(file, " ")
			}
		}
		fmt.Fprintln(file)
	}

	// Add entity decision statistics
	fmt.Fprintln(file, "\nEntity Decision Times:")
	for _, snapshot := range snapshots {
		status := "idle"
		if snapshot.IsDeciding {
			status = "DECIDING"
		}

		fmt.Fprintf(file, "Entity %d: %v (%s)\n", snapshot.ID, snapshot.LastDecisionTime, status)
	}

	err = file.Sync()
	if err != nil {
		log.Printf("Error syncing output file: %v", err)
	}
}

func main() {
	// Parse command line flags using the flag package
	devModePtr := flag.Bool("dev", true, "Run in development mode")
	flag.Parse()

	if *devModePtr {
		log.Println("Running in development mode")
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	sim := NewSimulation(5, 5, time.Second, 5*time.Second, rng, *devModePtr)
	defer sim.Stop()

	// Create 3 entities
	for i := 0; i < 3; i++ {
		entity := sim.CreateEntity()
		// Start each entity in its own goroutine
		entity.Start()
	}

	// Initial state before any ticks
	sim.PrintState(sim.outputFile)

	// Use a ticker for constant interval ticks
	ticker := time.NewTicker(sim.TickRate)
	defer ticker.Stop()

	totalTicks := 30
	for i := 0; i < totalTicks; i++ {
		// Wait for the next tick interval
		tickTime := <-ticker.C
		log.Printf("Tick %d starting at %s...", i+1, tickTime.Format(time.RFC3339))

		// Process the tick
		tickStart := time.Now()
		sim.Tick()
		sim.PrintState(sim.outputFile)
		log.Printf("Tick %d processing completed in %v", i+1, time.Since(tickStart))
	}

	log.Println("Simulation finished.")
}

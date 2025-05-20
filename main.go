package main

import (
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

// Entity represents an agent in the simulation
type Entity struct {
	ID                   int
	Position             Position
	Decision             chan Action   // Buffered channel for decisions
	DecidedActionDisplay string        // Stores the string representation of the action for the current tick
	LastDecisionTime     time.Duration // Time taken for the last decision
	IsDeciding           bool          // Flag to track if entity is currently making a decision
	mu                   sync.Mutex    // Mutex to protect the entity state
}

// CanMoveTo checks if an entity can move to a given cell
func (e *Entity) CanMoveTo(cell *Cell) bool {
	return !cell.IsOccupied()
}

// DecideAction represents the entity's decision-making process
func (e *Entity) DecideAction(sim *Simulation) {
	e.mu.Lock()
	if e.IsDeciding {
		log.Printf("Entity %d is already deciding, skipping this tick", e.ID)
		e.mu.Unlock()
		return
	}
	e.IsDeciding = true
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.IsDeciding = false
		e.mu.Unlock()
	}()

	startTime := time.Now()

	sleepDuration := time.Duration(sim.Rand.Intn(3000)) * time.Millisecond
	time.Sleep(sleepDuration)

	moves := []Position{
		{0, 1},
		{0, -1},
		{1, 0},
		{-1, 0},
	}

	direction := moves[sim.Rand.Intn(len(moves))]

	action := Action{
		Type:      ActionMove,
		Direction: direction,
		Entity:    e,
	}

	e.LastDecisionTime = time.Since(startTime)
	log.Printf("Entity %d decided in %v", e.ID, e.LastDecisionTime)

	select {
	case e.Decision <- action:
	default:
		log.Printf("Entity %d decision channel full, dropping decision", e.ID)
		select {
		case <-e.Decision:
		default:
		}
		e.Decision <- action
	}
}

// HandleCellEvent processes notifications about cell events
func (e *Entity) HandleCellEvent(event CellEvent) {
	// For now, entities ignore these events.
}

// FormatActionForDisplay converts an Action to a short string representation
func FormatActionForDisplay(action Action) string {
	switch action.Type {
	case ActionMove:
		switch action.Direction {
		case Position{0, 1}: // Up
			return "UP"
		case Position{0, -1}: // Down
			return "DW"
		case Position{-1, 0}: // Left
			return "LT"
		case Position{1, 0}: // Right
			return "RT"
		default:
			return "M?" // Unknown move direction
		}
	case ActionWait:
		return "ST" // Stay
	default:
		return "--" // Unknown action type
	}
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
	Entities            []Entity
	TickRate            time.Duration
	DecisionTimeout     time.Duration // Maximum time to wait for decisions
	Rand                *rand.Rand
	EventBus            chan CellEvent
	stopEventProcessing chan bool
	outputFile          *os.File
	mu                  sync.Mutex // Mutex to protect state during concurrent ticks
}

// NewSimulation creates a new simulation with specified parameters
func NewSimulation(width, height int, numEntities int, tickRate time.Duration, decisionTimeout time.Duration, rng *rand.Rand) *Simulation {
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
		EventBus:            make(chan CellEvent, 100),
		stopEventProcessing: make(chan bool),
		outputFile:          file,
		mu:                  sync.Mutex{},
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

	for i := 0; i < numEntities; i++ {
		var emptyCell *Cell
		for {
			x := sim.Rand.Intn(width)
			y := sim.Rand.Intn(height)
			cell := sim.Grid.GetCell(x, y)
			if !cell.IsOccupied() {
				emptyCell = cell
				break
			}
		}

		entity := Entity{
			ID:                   i,
			Position:             emptyCell.Position,
			Decision:             make(chan Action, 1), // Buffer size 1 to avoid blocking
			DecidedActionDisplay: "--",                 // Initial state
		}
		sim.Entities = append(sim.Entities, entity)
		emptyCell.OnEnter(&sim.Entities[i])
	}

	go sim.processEvents()
	log.Printf("Simulation initialized. Grid output will be written to %s", outputFileName)
	return sim
}

// processEvents listens on the EventBus and broadcasts events to all entities.
func (s *Simulation) processEvents() {
	log.Println("[EventBus] Starting event processor...")
	for {
		select {
		case event := <-s.EventBus:
			log.Printf("[EventBus] Received event: Entity %d moved from (%d,%d) to (%d,%d), broadcasting to all entities",
				event.TriggeringEntity.ID,
				event.PreviousCell.Position.X, event.PreviousCell.Position.Y,
				event.NewCell.Position.X, event.NewCell.Position.Y)
			for i := range s.Entities {
				entity := &s.Entities[i]
				if event.TriggeringEntity != nil && entity.ID == event.TriggeringEntity.ID {
					continue
				}
				go entity.HandleCellEvent(event)
			}
		case <-s.stopEventProcessing:
			log.Println("[EventBus] Stopping event processor...")
			return
		}
	}
}

// Stop gracefully shuts down the simulation
func (s *Simulation) Stop() {
	log.Println("Shutting down simulation...")
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
		s.EventBus <- CellEvent{Type: CellEnterEvent, PreviousCell: currentCell, NewCell: targetCell, TriggeringEntity: entity}
	}
	// If the move is invalid, entity stays in place
}

// Tick advances the simulation by one step without waiting for new decisions
func (s *Simulation) Tick() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// All entities that aren't deciding will start a new decision process
	for i := range s.Entities {
		entity := &s.Entities[i]

		entity.mu.Lock()
		isDeciding := entity.IsDeciding
		entity.mu.Unlock()

		if !isDeciding {
			// Start a new decision process in the background
			go entity.DecideAction(s)
		}
	}

	// Collect all actions that have already been decided
	actions := make([]Action, 0, len(s.Entities))
	for i := range s.Entities {
		currentEntity := &s.Entities[i]
		select {
		case action := <-currentEntity.Decision:
			// Got a decision that was ready
			actions = append(actions, action)
			currentEntity.DecidedActionDisplay = FormatActionForDisplay(action)
			log.Printf("Entity %d decision processed in this tick", currentEntity.ID)
		default:
			// No decision ready yet, entity is either still deciding or waiting
			currentEntity.mu.Lock()
			isDeciding := currentEntity.IsDeciding
			currentEntity.mu.Unlock()

			if isDeciding {
				log.Printf("Entity %d is still deciding", currentEntity.ID)
			} else {
				// Entity is not deciding anymore but didn't provide an action
				// This means their previous action was already consumed in an earlier tick
				// or they're between decisions
				waitAction := Action{Type: ActionWait, Entity: currentEntity}
				actions = append(actions, waitAction)
				currentEntity.DecidedActionDisplay = FormatActionForDisplay(waitAction)
			}
		}
	}

	// Shuffle the actions to avoid bias
	s.Rand.Shuffle(len(actions), func(i, j int) {
		actions[i], actions[j] = actions[j], actions[i]
	})

	// Execute all actions
	for _, action := range actions {
		s.ExecuteAction(action)
	}
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

	snapshots := make([]EntitySnapshot, len(s.Entities))
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
	for i := range s.Entities {
		entity := &s.Entities[i]
		entity.mu.Lock()
		snapshots[i] = EntitySnapshot{
			ID:                   entity.ID,
			Position:             entity.Position,
			LastDecisionTime:     entity.LastDecisionTime,
			DecidedActionDisplay: entity.DecidedActionDisplay,
			IsDeciding:           entity.IsDeciding,
		}
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
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	sim := NewSimulation(5, 5, 3, time.Second, 5*time.Second, rng)
	defer sim.Stop()

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

		// Process the tick immediately - no need for a goroutine since it's non-blocking now
		tickStart := time.Now()
		sim.Tick()
		sim.PrintState(sim.outputFile)
		log.Printf("Tick %d processing completed in %v", i+1, time.Since(tickStart))
	}

	log.Println("Simulation finished.")
}

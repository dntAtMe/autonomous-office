package main

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"simulation/shared"
	"sync"
	"time"
)

// Grid represents the 2D space where entities move
type Grid struct {
	Width  int32
	Height int32
	Cells  [][]shared.Cell
}

// GetCell returns the cell at the specified position
func (g *Grid) GetCell(x, y int32) *shared.Cell {
	if x >= 0 && x < g.Width && y >= 0 && y < g.Height {
		return &g.Cells[y][x]
	}
	return nil
}

// RemoteEntity represents an entity that exists on a remote client (used for grid cells)
type RemoteEntity struct {
	ID       int
	Position shared.Position
}

// BaseEntity provides common entity functionality that all entity types can embed
type BaseEntity struct {
	id                   int32
	position             shared.Position
	decidedActionDisplay string
	lastDecisionTime     time.Duration
	isDeciding           bool
}

// GetID returns the entity ID
func (e *BaseEntity) GetID() int32                               { return e.id }
func (e *BaseEntity) GetPosition() shared.Position               { return e.position }
func (e *BaseEntity) SetPosition(pos shared.Position)            { e.position = pos }
func (e *BaseEntity) GetDecidedActionDisplay() string            { return e.decidedActionDisplay }
func (e *BaseEntity) SetDecidedActionDisplay(display string)     { e.decidedActionDisplay = display }
func (e *BaseEntity) GetLastDecisionTime() time.Duration         { return e.lastDecisionTime }
func (e *BaseEntity) SetLastDecisionTime(duration time.Duration) { e.lastDecisionTime = duration }
func (e *BaseEntity) IsDeciding() bool                           { return e.isDeciding }
func (e *BaseEntity) SetDeciding(deciding bool)                  { e.isDeciding = deciding }

// NewBaseEntity creates a new base entity with the given ID
func NewBaseEntity(id int32) BaseEntity {
	return BaseEntity{
		id:                   id,
		decidedActionDisplay: "--",
	}
}

// SimulationCore represents the core simulation engine that can work with any transport
type SimulationCore struct {
	Grid            Grid
	Entities        map[int32]shared.EntityInterface
	TickRate        time.Duration
	DecisionTimeout time.Duration
	Rand            *rand.Rand
	mu              sync.RWMutex
	nextEntityID    int32
	tickCount       int32
	DevMode         bool
	outputFile      *os.File
}

// NewSimulationCore creates a new core simulation engine
func NewSimulationCore(width, height int32, tickRate, decisionTimeout time.Duration, rng *rand.Rand, devMode bool) *SimulationCore {
	outputFileName := "grid_output.txt"
	file, err := os.OpenFile(outputFileName, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		log.Fatalf("Failed to open output file %s: %v", outputFileName, err)
	}

	core := &SimulationCore{
		Grid: Grid{
			Width:  width,
			Height: height,
		},
		TickRate:        tickRate,
		DecisionTimeout: decisionTimeout,
		Rand:            rng,
		Entities:        make(map[int32]shared.EntityInterface),
		nextEntityID:    1,
		tickCount:       0,
		DevMode:         devMode,
		outputFile:      file,
	}

	core.Grid.Cells = make([][]shared.Cell, height)
	for y := int32(0); y < height; y++ {
		core.Grid.Cells[y] = make([]shared.Cell, width)
		for x := int32(0); x < width; x++ {
			core.Grid.Cells[y][x] = shared.Cell{
				Position: shared.Position{X: x, Y: y},
			}
		}
	}

	log.Printf("Simulation core initialized with %dx%d grid", width, height)
	log.Printf("Grid output will be written to %s", outputFileName)
	return core
}

// RegisterEntity adds an entity to the simulation
func (s *SimulationCore) RegisterEntity(entity shared.EntityInterface) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Find an empty position for the entity
	var emptyPos shared.Position
	found := false
	for attempts := 0; attempts < 100; attempts++ {
		x := s.Rand.Int31n(s.Grid.Width)
		y := s.Rand.Int31n(s.Grid.Height)
		cell := s.Grid.GetCell(x, y)
		if cell != nil && !cell.IsOccupied() {
			emptyPos = shared.Position{X: x, Y: y}
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("no empty positions available")
	}

	// Set entity position and register
	entity.SetPosition(emptyPos)
	entity.SetDecidedActionDisplay("--")
	s.Entities[entity.GetID()] = entity

	// Mark the cell as occupied
	cell := s.Grid.GetCell(emptyPos.X, emptyPos.Y)
	if cell != nil {
		cell.OnEnter(entity)
	}

	log.Printf("Entity %d registered at position (%d, %d)", entity.GetID(), emptyPos.X, emptyPos.Y)
	return nil
}

// UnregisterEntity removes an entity from the simulation
func (s *SimulationCore) UnregisterEntity(entityID int32) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entity, exists := s.Entities[entityID]
	if !exists {
		return
	}

	pos := entity.GetPosition()
	cell := s.Grid.GetCell(pos.X, pos.Y)
	if cell != nil {
		cell.OnExit(entity)
	}

	// Remove from entities
	delete(s.Entities, entityID)
	log.Printf("Entity %d unregistered", entityID)
}

// GetNextEntityID returns the next available entity ID
func (s *SimulationCore) GetNextEntityID() int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextEntityID
	s.nextEntityID++
	return id
}

// GetTickCount returns the current tick count
func (s *SimulationCore) GetTickCount() int32 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tickCount
}

// GetGridState returns the current state of the grid
func (s *SimulationCore) GetGridState() shared.GridState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cells := make([]shared.Cell, 0, s.Grid.Width*s.Grid.Height)
	for _, row := range s.Grid.Cells {
		for _, cell := range row {
			cells = append(cells, shared.Cell{
				Position: cell.Position,
				Occupant: cell.Occupant,
			})
		}
	}

	return shared.GridState{
		Width:  s.Grid.Width,
		Height: s.Grid.Height,
		Cells:  cells,
	}
}

// ExecuteAction processes a single entity action
func (s *SimulationCore) ExecuteAction(entityID int32, action shared.Action) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entity, exists := s.Entities[entityID]
	if !exists {
		log.Printf("ExecuteAction: Entity %d not found", entityID)
		return
	}

	log.Printf("ExecuteAction: Entity %d executing action type %d with direction (%d, %d)",
		entityID, action.Type, action.Direction.X, action.Direction.Y)

	oldPos := entity.GetPosition()

	// Update display action
	entity.SetDecidedActionDisplay(s.FormatActionForDisplay(action))

	// Execute the action
	switch action.Type {
	case shared.ActionMove:
		s.moveEntity(entity, action.Direction)
		newPos := entity.GetPosition()
		log.Printf("ExecuteAction: Entity %d moved from (%d, %d) to (%d, %d)",
			entityID, oldPos.X, oldPos.Y, newPos.X, newPos.Y)
	case shared.ActionWait:
		log.Printf("ExecuteAction: Entity %d waiting", entityID)
		// Entity waits, display already set
	}
}

// moveEntity attempts to move an entity in the specified direction
func (s *SimulationCore) moveEntity(entity shared.EntityInterface, direction shared.Position) {
	currentPos := entity.GetPosition()
	newX := currentPos.X + direction.X
	newY := currentPos.Y + direction.Y

	log.Printf("moveEntity: Entity %d trying to move from (%d, %d) to (%d, %d)",
		entity.GetID(), currentPos.X, currentPos.Y, newX, newY)

	targetCell := s.Grid.GetCell(newX, newY)
	if targetCell == nil {
		log.Printf("moveEntity: Entity %d move blocked - target cell (%d, %d) is out of bounds",
			entity.GetID(), newX, newY)
		return
	}

	if targetCell.IsOccupied() {
		log.Printf("moveEntity: Entity %d move blocked - target cell (%d, %d) is occupied",
			entity.GetID(), newX, newY)
		return
	}

	currentCell := s.Grid.GetCell(currentPos.X, currentPos.Y)
	if currentCell != nil {
		currentCell.OnExit(entity)
	}

	newPos := shared.Position{X: newX, Y: newY}
	entity.SetPosition(newPos)

	targetCell.OnEnter(entity)

	log.Printf("moveEntity: Entity %d successfully moved to (%d, %d)",
		entity.GetID(), newX, newY)
}

// FormatActionForDisplay converts an Action to a short string representation
func (s *SimulationCore) FormatActionForDisplay(action shared.Action) string {
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
func (s *SimulationCore) Tick() int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tickCount++
	return s.tickCount
}

// writeHeader writes the header information to the output file
func (s *SimulationCore) writeHeader() error {
	_, err := fmt.Fprintf(s.outputFile, "Tick: %s - Current Grid State & Entity Actions:\n", time.Now().Format(time.RFC3339))
	return err
}

// createGridRepresentation creates a string grid representation with entities
func (s *SimulationCore) createGridRepresentation(gridState shared.GridState) [][]string {
	grid := make([][]string, s.Grid.Height)
	for y := int32(0); y < s.Grid.Height; y++ {
		grid[y] = make([]string, s.Grid.Width)
		for x := int32(0); x < s.Grid.Width; x++ {
			grid[y][x] = ".      "
		}
	}

	// Place entities on the grid
	for _, cell := range gridState.Cells {
		if cell.Occupant != nil && cell.Position.Y >= 0 && cell.Position.Y < s.Grid.Height &&
			cell.Position.X >= 0 && cell.Position.X < s.Grid.Width {
			grid[cell.Position.Y][cell.Position.X] = fmt.Sprintf("E%d[%s] ", cell.Occupant.GetID(), cell.Occupant.GetDecidedActionDisplay())
		}
	}

	return grid
}

// writeGridToFile writes the grid representation to the output file
func (s *SimulationCore) writeGridToFile(grid [][]string) error {
	for y := s.Grid.Height - 1; y >= 0; y-- {
		for x := int32(0); x < s.Grid.Width; x++ {
			if _, err := fmt.Fprint(s.outputFile, grid[y][x]); err != nil {
				return err
			}
			if x < s.Grid.Width-1 {
				if _, err := fmt.Fprint(s.outputFile, " "); err != nil {
					return err
				}
			}
		}
		if _, err := fmt.Fprintln(s.outputFile); err != nil {
			return err
		}
	}
	return nil
}

// writeEntityStats writes entity decision statistics to the output file
func (s *SimulationCore) writeEntityStats(gridState shared.GridState) error {
	if _, err := fmt.Fprintln(s.outputFile, "\nEntity Decision Times:"); err != nil {
		return err
	}

	for _, cell := range gridState.Cells {
		if cell.Occupant == nil {
			continue
		}
		status := "idle"
		if cell.Occupant.IsDeciding() {
			status = "deciding"
		}
		if _, err := fmt.Fprintf(s.outputFile, "Entity %d: %v (%s)\n", cell.Occupant.GetID(), cell.Occupant.GetLastDecisionTime(), status); err != nil {
			return err
		}
	}
	return nil
}

// PrintState writes the current state of the simulation to the output file
func (s *SimulationCore) PrintState() {
	gridState := s.GetGridState()

	if _, err := s.outputFile.Seek(0, 0); err != nil {
		log.Printf("Error seeking in output file: %v", err)
		return
	}

	if err := s.outputFile.Truncate(0); err != nil {
		log.Printf("Error truncating output file: %v", err)
		return
	}

	if err := s.writeHeader(); err != nil {
		log.Printf("Error writing header: %v", err)
		return
	}

	grid := s.createGridRepresentation(gridState)

	if err := s.writeGridToFile(grid); err != nil {
		log.Printf("Error writing grid: %v", err)
		return
	}

	if err := s.writeEntityStats(gridState); err != nil {
		log.Printf("Error writing entity stats: %v", err)
		return
	}

	if err := s.outputFile.Sync(); err != nil {
		log.Printf("Error syncing output file: %v", err)
	}
}

// Stop gracefully shuts down the simulation core
func (s *SimulationCore) Stop() {
	log.Println("Shutting down simulation core...")

	if s.outputFile != nil {
		log.Printf("Closing output file: %s", s.outputFile.Name())
		if err := s.outputFile.Close(); err != nil {
			log.Printf("Error closing output file: %v", err)
		}
	}
}

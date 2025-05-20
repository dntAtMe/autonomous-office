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
	ID       int
	Position Position
	Decision chan Action // Channel for asynchronous decision making
}

// CanMoveTo checks if an entity can move to a given cell
func (e *Entity) CanMoveTo(cell *Cell) bool {
	return !cell.IsOccupied()
}

// DecideAction represents the entity's decision-making process
func (e *Entity) DecideAction(sim *Simulation) {
	// In the future, this will be replaced with LLM-based decision making
	// For now, we just decide to move in a random direction
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

	e.Decision <- action
}

// HandleCellEvent processes notifications about cell events
func (e *Entity) HandleCellEvent(event CellEvent) {
	// For now, entities ignore these events.
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
	Rand                *rand.Rand
	EventBus            chan CellEvent
	stopEventProcessing chan bool
	outputFile          *os.File
}

// NewSimulation creates a new simulation with specified parameters
func NewSimulation(width, height int, numEntities int, tickRate time.Duration, rng *rand.Rand) *Simulation {
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
		Rand:                rng,
		EventBus:            make(chan CellEvent, 100),
		stopEventProcessing: make(chan bool),
		outputFile:          file,
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
			ID:       i,
			Position: emptyCell.Position,
			Decision: make(chan Action, 1),
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
		// Do nothing, entity stays in place
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

// Tick advances the simulation by one step with asynchronous decision making
func (s *Simulation) Tick() {
	var wg sync.WaitGroup

	// All entities decide their actions asynchronously
	for i := range s.Entities {
		wg.Add(1)
		entity := &s.Entities[i]

		go func(e *Entity) {
			defer wg.Done()
			e.DecideAction(s)
		}(entity)
	}

	// Wait for all decisions to be made
	wg.Wait()

	// Collect all actions decided this tick
	actions := make([]Action, 0, len(s.Entities))
	for i := range s.Entities {
		select {
		case action := <-s.Entities[i].Decision:
			actions = append(actions, action)
		default:
			// If no decision was made, use a wait action
			actions = append(actions, Action{
				Type:   ActionWait,
				Entity: &s.Entities[i],
			})
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
	// Seek to the beginning of the file and truncate it to overwrite content
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

	fmt.Fprintf(file, "Current state:\n")
	for y := s.Grid.Height - 1; y >= 0; y-- {
		for x := 0; x < s.Grid.Width; x++ {
			cell := s.Grid.Cells[y][x]
			if cell.IsOccupied() {
				fmt.Fprintf(file, "E%d ", cell.Occupant.ID)
			} else {
				fmt.Fprint(file, ".  ")
			}
		}
		fmt.Fprintln(file)
	}
	err = file.Sync()
	if err != nil {
		log.Printf("Error syncing output file: %v", err)
	}
}

func main() {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	sim := NewSimulation(5, 5, 3, time.Second, rng)
	defer sim.Stop()

	for i := 0; i < 10; i++ {
		log.Printf("Tick %d", i+1)
		sim.Tick()
		sim.PrintState(sim.outputFile)
		time.Sleep(sim.TickRate)
	}
	log.Println("Simulation finished.")
}

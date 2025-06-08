// Package shared contains common types and data structures used across the simulation system.
// It defines entities, grid states, actions, and communication structures for the
// simulation server and entity clients.
package shared

import "time"

// Position represents a 2D coordinate on the grid
type Position struct {
	X int32 `json:"x"`
	Y int32 `json:"y"`
}

// ActionType defines the type of action an entity can take
type ActionType int

const (
	ActionMove ActionType = iota
	ActionWait
)

// Action represents a decision made by an entity
type Action struct {
	Type      ActionType `json:"type"`
	Direction Position   `json:"direction"`
	EntityID  int        `json:"entity_id"`
}

// EntityInterface defines what any entity must implement
type EntityInterface interface {
	GetID() int32
	GetPosition() Position
	SetPosition(pos Position)
	GetDecidedActionDisplay() string
	SetDecidedActionDisplay(display string)
	GetLastDecisionTime() time.Duration
	SetLastDecisionTime(duration time.Duration)
	IsDeciding() bool
	SetDeciding(deciding bool)
}

// Cell represents a single cell in the grid that can contain entities
type Cell struct {
	Position Position
	Occupant EntityInterface
}

// IsOccupied checks if this cell is currently occupied
func (c *Cell) IsOccupied() bool {
	return c.Occupant != nil
}

// OnEnter handles an entity entering this cell
func (c *Cell) OnEnter(entity EntityInterface) {
	c.Occupant = entity
}

// OnExit handles an entity exiting this cell
func (c *Cell) OnExit(entity EntityInterface) {
	if entity.GetID() == c.Occupant.GetID() {
		c.Occupant = nil
	}
}

// GridState represents the current state of the simulation grid
type GridState struct {
	Width  int32  `json:"width"`
	Height int32  `json:"height"`
	Cells  []Cell `json:"cells"`
}

// EventType defines the type of events that can occur in the simulation
type EventType int

const (
	EventCellEnter EventType = iota
	EventSimulationTick
	EventEntityDecision
)

// SimulationTickEvent represents a tick of the simulation
type SimulationTickEvent struct {
	TickNumber int       `json:"tick_number"`
	Timestamp  time.Time `json:"timestamp"`
	GridState  GridState `json:"grid_state"`
}

// EntityRegistrationRequest represents a request to register an entity
type EntityRegistrationRequest struct {
	EntityID int      `json:"entity_id"`
	Position Position `json:"position"`
}

// EntityRegistrationResponse represents the response to entity registration
type EntityRegistrationResponse struct {
	Success  bool   `json:"success"`
	Message  string `json:"message"`
	EntityID int    `json:"entity_id"`
}

// EntityDecisionRequest represents a request for an entity to make a decision
type EntityDecisionRequest struct {
	EntityID      int        `json:"entity_id"`
	GridState     GridState  `json:"grid_state"`
	TickNumber    int        `json:"tick_number"`
	PossibleMoves []Position `json:"possible_moves"`
}

// EntityDecisionResponse represents an entity's decision
type EntityDecisionResponse struct {
	EntityID int    `json:"entity_id"`
	Action   Action `json:"action"`
}

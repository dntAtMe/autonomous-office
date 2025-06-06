// Package shared contains common types and data structures used across the simulation system.
// It defines entities, grid states, actions, and communication structures for the
// simulation server and entity clients.
package shared

import "time"

// Position represents a 2D coordinate on the grid
type Position struct {
	X int `json:"x"`
	Y int `json:"y"`
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

// EntityState represents the current state of an entity
type EntityState struct {
	ID                   int           `json:"id"`
	Position             Position      `json:"position"`
	DecidedActionDisplay string        `json:"decided_action_display"`
	LastDecisionTime     time.Duration `json:"last_decision_time"`
	IsDeciding           bool          `json:"is_deciding"`
}

// GridState represents the current state of the simulation grid
type GridState struct {
	Width    int           `json:"width"`
	Height   int           `json:"height"`
	Entities []EntityState `json:"entities"`
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

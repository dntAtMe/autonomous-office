package main

import (
	"time"
)

// EventType defines the type of events that can occur in the simulation
type EventType int

const (
	EventCellEnter EventType = iota
	EventSimulationTick
	EventEntityDecision
	// Future event types will be added here
)

// Event represents any event that can occur in the simulation
type Event interface {
	GetType() EventType
}

// CellEvent represents an event occurring at a cell, triggered by an entity
type CellEvent struct {
	Type             EventType
	PreviousCell     *Cell
	NewCell          *Cell
	TriggeringEntity *Entity
}

func (e CellEvent) GetType() EventType {
	return e.Type
}

// SimulationTickEvent represents a tick of the simulation
type SimulationTickEvent struct {
	TickNumber int
	Timestamp  time.Time
}

func (e SimulationTickEvent) GetType() EventType {
	return EventSimulationTick
}

// EntityDecisionEvent represents a decision made by an entity
type EntityDecisionEvent struct {
	Entity *Entity
	Action Action
}

func (e EntityDecisionEvent) GetType() EventType {
	return EventEntityDecision
}

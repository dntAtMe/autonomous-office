package main

// CellEventType defines the type of cell event
type CellEventType int

const (
	CellEnterEvent CellEventType = iota
)

// CellEvent represents an event occurring at a cell, triggered by an entity
type CellEvent struct {
	Type             CellEventType
	PreviousCell     *Cell
	NewCell          *Cell
	TriggeringEntity *Entity
}

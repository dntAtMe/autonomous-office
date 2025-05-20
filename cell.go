package main

// Interactable defines behaviors for objects that can interact with entities
type Interactable interface {
	IsOccupied() bool
	OnEnter(entity *Entity)
	OnExit(entity *Entity)
}

// Cell represents a single cell in the grid that can contain entities
type Cell struct {
	Position Position
	Occupant *Entity
}

// IsOccupied checks if this cell is currently occupied
func (c *Cell) IsOccupied() bool {
	return c.Occupant != nil
}

// OnEnter handles an entity entering this cell
func (c *Cell) OnEnter(entity *Entity) {
	c.Occupant = entity
}

// OnExit handles an entity exiting this cell
func (c *Cell) OnExit(entity *Entity) {
	c.Occupant = nil
}

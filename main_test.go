package main

import (
	"math/rand"
	"testing"
	"time"
)

func TestNewSimulation(t *testing.T) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	width, height := 5, 5
	numEntities := 3
	sim := NewSimulation(width, height, numEntities, time.Second, rng)

	if sim.Grid.Width != width {
		t.Errorf("Expected grid width %d, got %d", width, sim.Grid.Width)
	}
	if sim.Grid.Height != height {
		t.Errorf("Expected grid height %d, got %d", height, sim.Grid.Height)
	}
	if len(sim.Entities) != numEntities {
		t.Errorf("Expected %d entities, got %d", numEntities, len(sim.Entities))
	}
	for _, entity := range sim.Entities {
		if entity.Decision == nil {
			t.Errorf("Entity %d decision channel not initialized", entity.ID)
		}
	}
	if sim.Rand == nil {
		t.Error("Simulation random number generator not initialized")
	}

	// Check if cells are initialized
	if len(sim.Grid.Cells) != height {
		t.Errorf("Expected %d rows of cells, got %d", height, len(sim.Grid.Cells))
	}
	for y, row := range sim.Grid.Cells {
		if len(row) != width {
			t.Errorf("Expected %d cells in row %d, got %d", width, y, len(row))
		}
	}

	// Check if entities are placed on the grid and cells are updated
	occupiedCount := 0
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			cell := sim.Grid.GetCell(x, y)
			if cell.IsOccupied() {
				occupiedCount++
				if cell.Occupant == nil {
					t.Errorf("Cell at (%d, %d) is occupied but has no occupant reference", x, y)
				}
			}
		}
	}
	if occupiedCount != numEntities {
		t.Errorf("Expected %d occupied cells, got %d", numEntities, occupiedCount)
	}
}

func TestEntity_CanMoveTo(t *testing.T) {
	entity := &Entity{}
	occupiedCell := &Cell{Occupant: &Entity{}}
	emptyCell := &Cell{}

	if entity.CanMoveTo(occupiedCell) {
		t.Error("Entity should not be able to move to an occupied cell")
	}
	if !entity.CanMoveTo(emptyCell) {
		t.Error("Entity should be able to move to an empty cell")
	}
}

package main

import (
	"strings"
	"testing"

	pb "simulation/proto"
)

func TestLoadPromptTemplate(t *testing.T) {
	tests := []struct {
		name       string
		entityID   int32
		gridState  *pb.GridState
		expectText []string
		expectErr  bool
	}{
		{
			name:     "Single entity on small grid",
			entityID: 1,
			gridState: &pb.GridState{
				Width:  3,
				Height: 3,
				Cells: []*pb.Cell{
					{
						Position: &pb.Position{X: 1, Y: 1},
						Occupant: &pb.EntityState{
							Id:       1,
							Position: &pb.Position{X: 1, Y: 1},
						},
					},
				},
			},
			expectText: []string{
				"You are entity 1 in a 3x3 grid simulation",
				"You are at position (1, 1)",
				"U, D, L, R",
			},
			expectErr: false,
		},
		{
			name:     "Multiple entities on larger grid",
			entityID: 2,
			gridState: &pb.GridState{
				Width:  10,
				Height: 8,
				Cells: []*pb.Cell{
					{
						Position: &pb.Position{X: 0, Y: 0},
						Occupant: &pb.EntityState{
							Id:       1,
							Position: &pb.Position{X: 0, Y: 0},
						},
					},
					{
						Position: &pb.Position{X: 5, Y: 3},
						Occupant: &pb.EntityState{
							Id:       2,
							Position: &pb.Position{X: 5, Y: 3},
						},
					},
					{
						Position: &pb.Position{X: 9, Y: 7},
						Occupant: &pb.EntityState{
							Id:       3,
							Position: &pb.Position{X: 9, Y: 7},
						},
					},
				},
			},
			expectText: []string{
				"You are entity 2 in a 10x8 grid simulation",
				"You are at position (5, 3)",
				"Entity 1 is at position (0, 0)",
				"Entity 3 is at position (9, 7)",
			},
			expectErr: false,
		},
		{
			name:     "Entity ID 0 (edge case)",
			entityID: 0,
			gridState: &pb.GridState{
				Width:  5,
				Height: 5,
				Cells: []*pb.Cell{
					{
						Position: &pb.Position{X: 2, Y: 2},
						Occupant: &pb.EntityState{
							Id:       0,
							Position: &pb.Position{X: 2, Y: 2},
						},
					},
				},
			},
			expectText: []string{
				"You are entity 0 in a 5x5 grid simulation",
				"You are at position (2, 2)",
			},
			expectErr: false,
		},
		{
			name:     "Large grid with many entities",
			entityID: 5,
			gridState: &pb.GridState{
				Width:  100,
				Height: 50,
				Cells: []*pb.Cell{
					{
						Position: &pb.Position{X: 10, Y: 20},
						Occupant: &pb.EntityState{
							Id:       1,
							Position: &pb.Position{X: 10, Y: 20},
						},
					},
					{
						Position: &pb.Position{X: 30, Y: 40},
						Occupant: &pb.EntityState{
							Id:       2,
							Position: &pb.Position{X: 30, Y: 40},
						},
					},
					{
						Position: &pb.Position{X: 50, Y: 25},
						Occupant: &pb.EntityState{
							Id:       3,
							Position: &pb.Position{X: 50, Y: 25},
						},
					},
					{
						Position: &pb.Position{X: 70, Y: 35},
						Occupant: &pb.EntityState{
							Id:       4,
							Position: &pb.Position{X: 70, Y: 35},
						},
					},
					{
						Position: &pb.Position{X: 90, Y: 45},
						Occupant: &pb.EntityState{
							Id:       5,
							Position: &pb.Position{X: 90, Y: 45},
						},
					},
				},
			},
			expectText: []string{
				"You are entity 5 in a 100x50 grid simulation",
				"You are at position (90, 45)",
				"Entity 1 is at position (10, 20)",
				"Entity 2 is at position (30, 40)",
				"Entity 3 is at position (50, 25)",
				"Entity 4 is at position (70, 35)",
			},
			expectErr: false,
		},
		{
			name:     "Empty entities list",
			entityID: 1,
			gridState: &pb.GridState{
				Width:  2,
				Height: 2,
				Cells: []*pb.Cell{
					{
						Position: &pb.Position{X: 0, Y: 0},
						Occupant: nil,
					},
				},
			},
			expectText: []string{
				"You are entity 1 in a 2x2 grid simulation",
				"Current entities on the grid:",
			},
			expectErr: false,
		},
		{
			name:     "Entity not in the grid (but still should work)",
			entityID: 99,
			gridState: &pb.GridState{
				Width:  5,
				Height: 5,
				Cells: []*pb.Cell{
					{
						Position: &pb.Position{X: 1, Y: 1},
						Occupant: &pb.EntityState{
							Id:       1,
							Position: &pb.Position{X: 1, Y: 1},
						},
					},
					{
						Position: &pb.Position{X: 2, Y: 2},
						Occupant: &pb.EntityState{
							Id:       2,
							Position: &pb.Position{X: 2, Y: 2},
						},
					},
				},
			},
			expectText: []string{
				"You are entity 99 in a 5x5 grid simulation",
				"Entity 1 is at position (1, 1)",
				"Entity 2 is at position (2, 2)",
			},
			expectErr: false,
		},
		{
			name:     "Negative coordinates",
			entityID: 1,
			gridState: &pb.GridState{
				Width:  10,
				Height: 10,
				Cells: []*pb.Cell{
					{
						Position: &pb.Position{X: -1, Y: -2},
						Occupant: &pb.EntityState{
							Id:       1,
							Position: &pb.Position{X: -1, Y: -2},
						},
					},
					{
						Position: &pb.Position{X: 0, Y: 0},
						Occupant: &pb.EntityState{
							Id:       2,
							Position: &pb.Position{X: 0, Y: 0},
						},
					},
				},
			},
			expectText: []string{
				"You are entity 1 in a 10x10 grid simulation",
				"You are at position (-1, -2)",
				"Entity 2 is at position (0, 0)",
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &EntityClient{
				ID: tt.entityID,
			}

			result, err := client.loadPromptTemplate(tt.gridState)

			if tt.expectErr && err == nil {
				t.Errorf("Expected error but got none")
				return
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if tt.expectErr {
				return
			}

			for _, expectedText := range tt.expectText {
				if !strings.Contains(result, expectedText) {
					t.Errorf("Expected text '%s' not found in result:\n%s", expectedText, result)
				}
			}

			if !strings.Contains(result, "Answer with a single letter: U, D, L, R and nothing else") {
				t.Errorf("Expected instruction text not found in result:\n%s", result)
			}
		})
	}
}

func TestLoadPromptTemplate_NilGridState(t *testing.T) {
	client := &EntityClient{ID: 1}

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected panic when gridState is nil")
		}
	}()

	_, _ = client.loadPromptTemplate(nil)
}

func TestLoadPromptTemplate_NilPosition(t *testing.T) {
	client := &EntityClient{ID: 1}

	gridState := &pb.GridState{
		Width:  5,
		Height: 5,
		Cells: []*pb.Cell{
			{
				Position: nil,
				Occupant: &pb.EntityState{
					Id: 1,
				},
			},
		},
	}

	result, err := client.loadPromptTemplate(gridState)

	if err == nil {
		if !strings.Contains(result, "You are entity 1 in a 5x5 grid simulation") {
			t.Errorf("Basic entity information missing from result: %s", result)
		}
	}
}

func BenchmarkLoadPromptTemplate(b *testing.B) {
	client := &EntityClient{ID: 1}

	gridState := &pb.GridState{
		Width:  10,
		Height: 10,
		Cells: []*pb.Cell{
			{
				Position: &pb.Position{X: 1, Y: 1},
				Occupant: &pb.EntityState{
					Id: 1,
				},
			},
			{
				Position: &pb.Position{X: 2, Y: 2},
				Occupant: &pb.EntityState{
					Id: 2,
				},
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.loadPromptTemplate(gridState)
		if err != nil {
			b.Fatalf("Unexpected error: %v", err)
		}
	}
}

func TestLoadPromptTemplate_ManyEntities(t *testing.T) {
	client := &EntityClient{ID: 50}

	entities := make([]*pb.EntityState, 100)
	for i := 0; i < 100; i++ {
		entities[i] = &pb.EntityState{
			Id:       int32(i + 1),
			Position: &pb.Position{X: int32(i % 10), Y: int32(i / 10)},
		}
	}

	cells := make([]*pb.Cell, 100)
	for i := 0; i < 100; i++ {
		cells[i] = &pb.Cell{
			Position: &pb.Position{X: int32(i % 10), Y: int32(i / 10)},
			Occupant: entities[i],
		}
	}

	gridState := &pb.GridState{
		Width:  10,
		Height: 10,
		Cells:  cells,
	}

	result, err := client.loadPromptTemplate(gridState)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should contain our entity info
	if !strings.Contains(result, "You are entity 50 in a 10x10 grid simulation") {
		t.Errorf("Expected entity 50 information not found")
	}

	// Should contain the first entity
	if !strings.Contains(result, "Entity 1 is at position (0, 0)") {
		t.Errorf("Expected entity 1 information not found")
	}

	// Should contain the last entity
	if !strings.Contains(result, "Entity 100 is at position (9, 9)") {
		t.Errorf("Expected entity 100 information not found")
	}

	// Count the number of entity lines to make sure all are included
	entityLines := strings.Count(result, "Entity ") + strings.Count(result, "You are at position")
	if entityLines != 100 {
		t.Errorf("Expected 100 entity lines, got %d", entityLines)
	}
}

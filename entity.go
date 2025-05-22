package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"google.golang.org/genai"
)

// Entity represents an agent in the simulation
type Entity struct {
	ID                   int
	Position             Position
	Decision             chan Action   // Buffered channel for decisions
	DecidedActionDisplay string        // Stores the string representation of the action for the current tick
	LastDecisionTime     time.Duration // Time taken for the last decision
	IsDeciding           bool          // Flag to track if entity is currently making a decision
	eventChan            chan Event    // Channel to receive simulation events
	sim                  *Simulation   // Reference to the simulation
	mu                   sync.Mutex    // Mutex to protect the entity state
	isRunning            bool          // Flag to track if the entity is actively running
	stopSignal           chan bool     // Channel to signal the entity to stop
	config               *Config       // Entity-specific configuration
}

// NewEntity creates a new entity
func NewEntity(id int, pos Position) *Entity {
	// Create default config file if it doesn't exist
	configPath := GetDefaultConfigPath()
	if err := SaveDefaultConfig(configPath); err != nil {
		log.Fatalf("Warning: Failed to create default config file: %v", err)
	}

	// Load the configuration
	config := LoadConfig(configPath)

	return &Entity{
		ID:                   id,
		Position:             pos,
		Decision:             make(chan Action, 1), // Buffer size 1 to avoid blocking
		DecidedActionDisplay: "--",                 // Initial state
		eventChan:            make(chan Event, 10), // Buffer for events
		isRunning:            false,
		stopSignal:           make(chan bool),
		config:               config,
	}
}

// Register connects this entity to a simulation
func (e *Entity) Register(sim *Simulation) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.sim != nil {
		return fmt.Errorf("entity %d is already registered with a simulation", e.ID)
	}

	e.sim = sim
	return sim.RegisterEntity(e)
}

// Unregister disconnects this entity from its simulation
func (e *Entity) Unregister() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.sim == nil {
		return fmt.Errorf("entity %d is not registered with any simulation", e.ID)
	}

	// Stop the entity if it's running
	if e.isRunning {
		e.Stop()
	}

	sim := e.sim
	e.sim = nil
	return sim.UnregisterEntity(e)
}

// Start begins the entity's autonomous decision-making loop
func (e *Entity) Start() {
	e.mu.Lock()
	if e.isRunning {
		e.mu.Unlock()
		return
	}

	if e.sim == nil {
		log.Printf("Entity %d cannot start: not registered with a simulation", e.ID)
		e.mu.Unlock()
		return
	}

	e.isRunning = true
	e.mu.Unlock()

	go e.run()
}

// Stop ends the entity's autonomous decision-making loop
func (e *Entity) Stop() {
	e.mu.Lock()
	if !e.isRunning {
		e.mu.Unlock()
		return
	}
	e.isRunning = false
	e.mu.Unlock()

	e.stopSignal <- true
}

// run is the main entity loop
func (e *Entity) run() {
	log.Printf("Entity %d started running", e.ID)

	// Main loop
	for {
		select {
		case <-e.stopSignal:
			log.Printf("Entity %d stopping", e.ID)
			return
		case event := <-e.eventChan:
			e.handleEvent(event)
		default:
			// Small sleep to prevent CPU hogging
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// handleEvent processes events from the simulation
func (e *Entity) handleEvent(event Event) {
	switch event.GetType() {
	case EventCellEnter:
		// Currently no action needed
	case EventSimulationTick:
		// The simulation has ticked, make a decision for this tick
		if !e.IsDeciding {
			e.makeDecision()
		}
	case EventEntityDecision:
		// Another entity has made a decision, no action needed
	}
}

// CanMoveTo checks if an entity can move to a given cell
func (e *Entity) CanMoveTo(cell *Cell) bool {
	return !cell.IsOccupied()
}

// GeminiPrompt represents the structure for a Gemini API request
type GeminiPrompt struct {
	Contents []Content `json:"contents"`
}

// Content represents a content element in the Gemini API request
type Content struct {
	Role  string `json:"role"`
	Parts []Part `json:"parts"`
}

// Part represents a part element in the Gemini API request
type Part struct {
	Text string `json:"text"`
}

// GeminiResponse represents the structure for a Gemini API response
type GeminiResponse struct {
	Candidates []Candidate `json:"candidates"`
}

// Candidate represents a candidate element in the Gemini API response
type Candidate struct {
	Content Content `json:"content"`
}

// makeDecision represents the entity's decision-making process
func (e *Entity) makeDecision() {
	e.mu.Lock()
	if e.IsDeciding {
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

	// Ensure we have a simulation reference
	e.mu.Lock()
	sim := e.sim
	e.mu.Unlock()

	if sim == nil {
		log.Printf("Entity %d cannot make a decision: not registered with a simulation", e.ID)
		return
	}

	// Define all possible moves
	moves := []Position{
		{0, 1},  // Up
		{0, -1}, // Down
		{1, 0},  // Right
		{-1, 0}, // Left
	}

	var direction Position
	switch {
	case sim != nil && sim.DevMode:
		direction = e.callDevDecision(moves)
	case sim != nil && !sim.DevMode:
		direction = e.callGeminiForDecision(moves)
	}

	action := Action{
		Type:      ActionMove,
		Direction: direction,
		Entity:    e,
	}

	e.LastDecisionTime = time.Since(startTime)
	log.Printf("Entity %d decided in %v", e.ID, e.LastDecisionTime)

	// Send the decision to the simulation
	decisionEvent := EntityDecisionEvent{
		Entity: e,
		Action: action,
	}

	sim.PublishEvent(decisionEvent)
}

// callDevDecision is a development mode function that returns a random direction
func (e *Entity) callDevDecision(moves []Position) Position {
	randomIdx := e.sim.Rand.Intn(len(moves))
	log.Printf("Dev mode: Entity %d randomly selected direction %v", e.ID, moves[randomIdx])
	return moves[randomIdx]
}

// callGeminiForDecision calls the Gemini API to get a movement direction
func (e *Entity) callGeminiForDecision(moves []Position) Position {
	// Get the API key from the configuration
	e.mu.Lock()
	sim := e.sim
	e.mu.Unlock()

	if sim == nil {
		log.Println("Error: Entity not connected to simulation")
		return Position{0, 0}
	}

	// Check if API key is available
	apiKey := e.config.GeminiAPIKey
	if apiKey == "" {
		log.Println("Error: No Gemini API key configured, using fallback direction")
		return Position{0, 0} // Default to staying in place
	}

	ctx := context.Background()

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})

	if err != nil {
		log.Printf("Error creating Gemini client: %v", err)
		return Position{0, 0}
	}

	temperature := float32(1.0)

	result, err := client.Models.GenerateContent(
		ctx,
		"gemini-2.0-flash",
		genai.Text("You are in a 2D simulation. You can move up, down, left, or right. Answer with a single letter: U, D, L, R and nothing else. Pick randomly if you can move in multiple directions."),
		&genai.GenerateContentConfig{
			Temperature: &temperature,
		},
	)
	if err != nil {
		log.Printf("Error generating content: %v", err)
		return Position{0, 0}
	}

	response := result.Text()
	response = strings.TrimSpace(response)
	log.Printf("Gemini response: %s", response)

	switch response {
	case "U":
		return Position{0, 1}
	case "D":
		return Position{0, -1}
	case "L":
		return Position{-1, 0}
	case "R":
		return Position{1, 0}
	default:
		return Position{0, 0}
	}
}

// HandleCellEvent processes notifications about cell events
func (e *Entity) HandleCellEvent(event CellEvent) {
	// For now, entities ignore these events.
	// This is kept for backward compatibility and can dispatch to the new event handler
	e.handleEvent(event)
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

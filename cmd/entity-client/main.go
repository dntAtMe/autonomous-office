package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"simulation/shared"

	"github.com/gorilla/websocket"
	"google.golang.org/genai"
)

// Config holds the application configuration
type Config struct {
	GeminiAPIKey string `json:"gemini_api_key"`
}

// LoadConfig loads the configuration from a file
func LoadConfig(configPath string) *Config {
	// Default config with empty values
	config := &Config{}

	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		log.Printf("Config file not found at %s, checking environment variables", configPath)
		// Try to load from environment variables
		apiKey := os.Getenv("GEMINI_API_KEY")
		if apiKey != "" {
			log.Println("Loaded API key from environment variable")
			config.GeminiAPIKey = apiKey
		} else {
			log.Println("WARNING: No API key found in environment variables")
		}
		return config
	}

	// Open and read the config file
	file, err := os.Open(configPath)
	if err != nil {
		log.Printf("Error opening config file: %v", err)
		return config
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(config)
	if err != nil {
		log.Printf("Error parsing config file: %v", err)
		return config
	}

	if config.GeminiAPIKey == "" {
		log.Println("WARNING: No API key found in config file")
	}

	log.Println("Configuration loaded successfully")
	return config
}

// GetDefaultConfigPath returns the default path for the config file
func GetDefaultConfigPath() string {
	// Get the current executable directory
	execPath, err := os.Executable()
	if err != nil {
		log.Printf("Warning: Could not determine executable path: %v", err)
		return "config.json" // Fallback to current directory
	}

	execDir := filepath.Dir(execPath)
	return filepath.Join(execDir, "config.json")
}

// SaveDefaultConfig creates a default config file if it doesn't exist
func SaveDefaultConfig(configPath string) error {
	// Check if file already exists
	if _, err := os.Stat(configPath); err == nil {
		// File exists, don't overwrite
		return nil
	}

	// Create a default config
	defaultConfig := &Config{
		GeminiAPIKey: "", // Empty by default, user needs to fill this in
	}

	// Create the file
	file, err := os.Create(configPath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Write the default config as JSON
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "    ")
	if err := encoder.Encode(defaultConfig); err != nil {
		return err
	}

	log.Printf("Created default config file at %s", configPath)
	log.Printf("Please add your Gemini API key to this file using the following options:")
	log.Printf("1. Edit the config.json file directly and add your key to the 'gemini_api_key' field")
	log.Printf("2. Set the GEMINI_API_KEY environment variable")
	return nil
}

// EntityClient represents a client that connects to the simulation server
type EntityClient struct {
	ID             int
	ServerURL      string
	Connection     *websocket.Conn
	Config         *Config
	DevMode        bool
	mu             sync.Mutex
	isRunning      bool
	stopChan       chan bool
	reconnectDelay time.Duration
}

// NewEntityClient creates a new entity client
func NewEntityClient(serverURL string, devMode bool) *EntityClient {
	// Create default config file if it doesn't exist
	configPath := GetDefaultConfigPath()
	if err := SaveDefaultConfig(configPath); err != nil {
		log.Printf("Warning: Failed to create default config file: %v", err)
	}

	// Load the configuration
	config := LoadConfig(configPath)

	return &EntityClient{
		ServerURL:      serverURL,
		Config:         config,
		DevMode:        devMode,
		isRunning:      false,
		stopChan:       make(chan bool),
		reconnectDelay: 5 * time.Second,
	}
}

// Connect establishes a WebSocket connection to the simulation server
func (e *EntityClient) Connect() error {
	u, err := url.Parse(e.ServerURL)
	if err != nil {
		return fmt.Errorf("invalid server URL: %v", err)
	}

	// Convert HTTP URL to WebSocket URL
	if u.Scheme == "http" {
		u.Scheme = "ws"
	} else if u.Scheme == "https" {
		u.Scheme = "wss"
	}
	u.Path = "/register"

	log.Printf("Connecting to simulation server at %s", u.String())

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to connect to server: %v", err)
	}

	e.Connection = conn

	// Send registration request
	registrationReq := shared.EntityRegistrationRequest{
		EntityID: 0,                           // Server will assign ID
		Position: shared.Position{X: 0, Y: 0}, // Server will assign position
	}

	err = conn.WriteJSON(registrationReq)
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to send registration request: %v", err)
	}

	// Wait for registration response
	var response shared.EntityRegistrationResponse
	err = conn.ReadJSON(&response)
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to read registration response: %v", err)
	}

	if !response.Success {
		conn.Close()
		return fmt.Errorf("registration failed: %s", response.Message)
	}

	e.ID = response.EntityID
	log.Printf("Successfully registered as entity %d", e.ID)

	return nil
}

// Start begins the entity client's main loop
func (e *EntityClient) Start() {
	e.mu.Lock()
	if e.isRunning {
		e.mu.Unlock()
		return
	}
	e.isRunning = true
	e.mu.Unlock()

	go e.run()
}

// Stop gracefully shuts down the entity client
func (e *EntityClient) Stop() {
	e.mu.Lock()
	if !e.isRunning {
		e.mu.Unlock()
		return
	}
	e.isRunning = false
	e.mu.Unlock()

	close(e.stopChan)

	if e.Connection != nil {
		e.Connection.Close()
	}
}

// run is the main entity client loop
func (e *EntityClient) run() {
	for {
		select {
		case <-e.stopChan:
			log.Printf("Entity %d stopping", e.ID)
			return
		default:
			// Try to connect if not connected
			if e.Connection == nil {
				err := e.Connect()
				if err != nil {
					log.Printf("Failed to connect: %v. Retrying in %v", err, e.reconnectDelay)
					time.Sleep(e.reconnectDelay)
					continue
				}
			}

			// Handle incoming messages
			err := e.handleMessages()
			if err != nil {
				log.Printf("Connection error: %v. Reconnecting...", err)
				if e.Connection != nil {
					e.Connection.Close()
					e.Connection = nil
				}
				time.Sleep(e.reconnectDelay)
			}
		}
	}
}

// handleMessages processes incoming messages from the simulation server
func (e *EntityClient) handleMessages() error {
	for {
		select {
		case <-e.stopChan:
			return nil
		default:
			// Set a read timeout to avoid hanging indefinitely
			e.Connection.SetReadDeadline(time.Now().Add(10 * time.Second))

			var request shared.EntityDecisionRequest
			err := e.Connection.ReadJSON(&request)
			if err != nil {
				return fmt.Errorf("failed to read message: %v", err)
			}

			// Clear the read deadline
			e.Connection.SetReadDeadline(time.Time{})

			// Process the decision request
			response := e.makeDecision(request)

			// Send the response with a write timeout
			e.Connection.SetWriteDeadline(time.Now().Add(5 * time.Second))
			err = e.Connection.WriteJSON(response)
			e.Connection.SetWriteDeadline(time.Time{})

			if err != nil {
				return fmt.Errorf("failed to send decision: %v", err)
			}
		}
	}
}

// makeDecision processes a decision request and returns a decision
func (e *EntityClient) makeDecision(request shared.EntityDecisionRequest) shared.EntityDecisionResponse {
	log.Printf("Entity %d making decision for tick %d", e.ID, request.TickNumber)

	var direction shared.Position

	switch {
	case e.DevMode:
		direction = e.callDevDecision(request.PossibleMoves)
	default:
		direction = e.callGeminiForDecision(request.PossibleMoves, request.GridState)
	}

	action := shared.Action{
		Type:      shared.ActionMove,
		Direction: direction,
		EntityID:  e.ID,
	}

	return shared.EntityDecisionResponse{
		EntityID: e.ID,
		Action:   action,
	}
}

// callDevDecision is a development mode function that returns a random direction
func (e *EntityClient) callDevDecision(moves []shared.Position) shared.Position {
	if len(moves) == 0 {
		return shared.Position{X: 0, Y: 0}
	}

	randomIdx := rand.Intn(len(moves))
	log.Printf("Dev mode: Entity %d randomly selected direction %v", e.ID, moves[randomIdx])
	return moves[randomIdx]
}

// callGeminiForDecision calls the Gemini API to get a movement direction
func (e *EntityClient) callGeminiForDecision(moves []shared.Position, gridState shared.GridState) shared.Position {
	// Check if API key is available
	apiKey := e.Config.GeminiAPIKey
	if apiKey == "" {
		log.Println("Error: No Gemini API key configured, using fallback direction")
		return shared.Position{X: 0, Y: 0} // Default to staying in place
	}

	ctx := context.Background()

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})

	if err != nil {
		log.Printf("Error creating Gemini client: %v", err)
		return shared.Position{X: 0, Y: 0}
	}

	// Create a more detailed prompt with grid information
	prompt := fmt.Sprintf(`You are entity %d in a %dx%d grid simulation. 
Current entities on the grid:
`, e.ID, gridState.Width, gridState.Height)

	for _, entity := range gridState.Entities {
		if entity.ID == e.ID {
			prompt += fmt.Sprintf("- You are at position (%d, %d)\n", entity.Position.X, entity.Position.Y)
		} else {
			prompt += fmt.Sprintf("- Entity %d is at position (%d, %d)\n", entity.ID, entity.Position.X, entity.Position.Y)
		}
	}

	prompt += "\nYou can move up, down, left, or right. Answer with a single letter: U, D, L, R and nothing else. Pick randomly if you can move in multiple directions."

	temperature := float32(1.0)

	result, err := client.Models.GenerateContent(
		ctx,
		"gemini-2.0-flash",
		genai.Text(prompt),
		&genai.GenerateContentConfig{
			Temperature: &temperature,
		},
	)
	if err != nil {
		log.Printf("Error generating content: %v", err)
		return shared.Position{X: 0, Y: 0}
	}

	response := result.Text()
	response = strings.TrimSpace(response)
	log.Printf("Gemini response: %s", response)

	switch response {
	case "U":
		return shared.Position{X: 0, Y: 1}
	case "D":
		return shared.Position{X: 0, Y: -1}
	case "L":
		return shared.Position{X: -1, Y: 0}
	case "R":
		return shared.Position{X: 1, Y: 0}
	default:
		return shared.Position{X: 0, Y: 0}
	}
}

func main() {
	// Parse command line flags
	devModePtr := flag.Bool("dev", true, "Run in development mode")
	serverURLPtr := flag.String("server", "http://localhost:8080", "Simulation server URL")
	numEntitiesPtr := flag.Int("entities", 3, "Number of entities to create")
	flag.Parse()

	if *devModePtr {
		log.Println("Running in development mode")
	}

	log.Printf("Creating %d entities to connect to %s", *numEntitiesPtr, *serverURLPtr)

	// Create and start multiple entity clients
	var clients []*EntityClient
	for i := 0; i < *numEntitiesPtr; i++ {
		client := NewEntityClient(*serverURLPtr, *devModePtr)
		clients = append(clients, client)
		client.Start()

		// Small delay between connections to avoid overwhelming the server
		time.Sleep(100 * time.Millisecond)
	}

	// Wait for interrupt signal
	log.Println("Entity clients running. Press Ctrl+C to stop.")

	// Keep the main goroutine alive
	select {}
}

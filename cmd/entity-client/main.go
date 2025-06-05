package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	pb "simulation/proto"

	"google.golang.org/genai"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

//go:embed prompt_template.txt
var promptTemplate string

// PromptData holds the data for the prompt template
type PromptData struct {
	EntityID   int32
	GridWidth  int32
	GridHeight int32
	Entities   []*pb.EntityState
}

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
		log.Println("No API key found in config file, checking environment variables")
		apiKey := os.Getenv("GEMINI_API_KEY")
		if apiKey != "" {
			log.Println("Loaded API key from environment variable")
			config.GeminiAPIKey = apiKey
		} else {
			log.Println("WARNING: No API key found in config file or environment variables")
		}
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

// EntityClient represents a client that connects to the simulation server via gRPC
type EntityClient struct {
	ID             int32
	ServerURL      string
	Connection     *grpc.ClientConn
	Client         pb.SimulationServiceClient
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

	// If not in dev mode, require API key
	if !devMode && config.GeminiAPIKey == "" {
		log.Fatal("FATAL: Running in production mode but no Gemini API key found. Please set GEMINI_API_KEY environment variable or add it to config.json")
	}

	return &EntityClient{
		ServerURL:      serverURL,
		Config:         config,
		DevMode:        devMode,
		isRunning:      false,
		stopChan:       make(chan bool),
		reconnectDelay: 5 * time.Second,
	}
}

// Connect establishes a gRPC connection to the simulation server
func (e *EntityClient) Connect() error {
	log.Printf("Connecting to simulation server at %s", e.ServerURL)

	conn, err := grpc.Dial(e.ServerURL, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to server: %v", err)
	}

	e.Connection = conn
	e.Client = pb.NewSimulationServiceClient(conn)

	// Test the connection with a health check
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = e.Client.HealthCheck(ctx, &pb.Empty{})
	if err != nil {
		conn.Close()
		return fmt.Errorf("health check failed: %v", err)
	}

	// Register with the server
	registrationReq := &pb.EntityRegistrationRequest{
		EntityId: 0,                        // Server will assign ID
		Position: &pb.Position{X: 0, Y: 0}, // Server will assign position
	}

	response, err := e.Client.RegisterEntity(ctx, registrationReq)
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to register entity: %v", err)
	}

	if !response.Success {
		conn.Close()
		return fmt.Errorf("registration failed: %s", response.Message)
	}

	e.ID = response.EntityId
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

			// Handle decision making loop
			err := e.handleDecisionLoop()
			if err != nil {
				log.Printf("Connection error: %v. Reconnecting...", err)
				if e.Connection != nil {
					e.Connection.Close()
					e.Connection = nil
					e.Client = nil
				}
				time.Sleep(e.reconnectDelay)
			}
		}
	}
}

// handleDecisionLoop continuously polls for decision requests and responds
func (e *EntityClient) handleDecisionLoop() error {
	log.Printf("Entity %d establishing stream connection", e.ID)

	// Establish bidirectional stream
	ctx := context.Background()
	stream, err := e.Client.EntityStream(ctx)
	if err != nil {
		return fmt.Errorf("failed to establish entity stream: %v", err)
	}

	// Send initial identification message
	initialMsg := &pb.EntityDecisionResponse{
		EntityId: e.ID,
		// Action is nil for identification message
	}

	err = stream.Send(initialMsg)
	if err != nil {
		return fmt.Errorf("failed to send identification message: %v", err)
	}

	log.Printf("Entity %d stream established, waiting for decision requests", e.ID)

	// Listen for decision requests and respond
	for {
		select {
		case <-e.stopChan:
			return nil
		default:
			// Receive decision request from server
			decisionRequest, err := stream.Recv()
			if err != nil {
				return fmt.Errorf("failed to receive decision request: %v", err)
			}

			log.Printf("Entity %d received decision request for tick %d", e.ID, decisionRequest.TickNumber)

			// Make decision
			action := e.makeDecision(decisionRequest)

			// Send decision response
			response := &pb.EntityDecisionResponse{
				EntityId: e.ID,
				Action:   action,
			}

			err = stream.Send(response)
			if err != nil {
				return fmt.Errorf("failed to send decision response: %v", err)
			}

			log.Printf("Entity %d sent decision: %v", e.ID, action.Type)
		}
	}
}

// makeDecision processes a grid state and returns a decision
func (e *EntityClient) makeDecision(request *pb.EntityDecisionRequest) *pb.Action {
	log.Printf("Entity %d making decision", e.ID)

	possibleMoves := []*pb.Position{
		{X: 0, Y: 1},  // Up
		{X: 0, Y: -1}, // Down
		{X: 1, Y: 0},  // Right
		{X: -1, Y: 0}, // Left
	}

	var direction *pb.Position

	switch {
	case e.DevMode:
		direction = e.callDevDecision(possibleMoves)
	default:
		direction = e.callGeminiForDecision(possibleMoves, request.GridState)
	}

	action := &pb.Action{
		Type:      pb.ActionType_ACTION_MOVE,
		Direction: direction,
		EntityId:  e.ID,
	}

	return action
}

// callDevDecision is a development mode function that returns a random direction
func (e *EntityClient) callDevDecision(moves []*pb.Position) *pb.Position {
	if len(moves) == 0 {
		return &pb.Position{X: 0, Y: 0}
	}

	randomIdx := rand.Intn(len(moves))
	log.Printf("Dev mode: Entity %d randomly selected direction %v", e.ID, moves[randomIdx])
	return moves[randomIdx]
}

func (e *EntityClient) loadPromptTemplate(gridState *pb.GridState) (string, error) {
	promptData := PromptData{
		EntityID:   e.ID,
		GridWidth:  gridState.Width,
		GridHeight: gridState.Height,
		Entities:   gridState.Entities,
	}

	var prompt bytes.Buffer
	tmpl, err := template.New("prompt").Parse(promptTemplate)
	if err != nil {
		log.Printf("Error parsing prompt template: %v", err)
		return "", err
	}

	err = tmpl.Execute(&prompt, promptData)
	if err != nil {
		log.Printf("Error executing prompt template: %v", err)
		return "", err
	}

	return prompt.String(), nil
}

// callGeminiForDecision calls the Gemini API to get a movement direction
func (e *EntityClient) callGeminiForDecision(moves []*pb.Position, gridState *pb.GridState) *pb.Position {
	// Check if API key is available
	apiKey := e.Config.GeminiAPIKey
	if apiKey == "" {
		log.Fatal("FATAL: No Gemini API key configured for production mode")
	}

	ctx := context.Background()

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})

	if err != nil {
		log.Printf("Error creating Gemini client: %v", err)
		return &pb.Position{X: 0, Y: 0}
	}

	temperature := float32(1.0)

	prompt, err := e.loadPromptTemplate(gridState)
	if err != nil {
		log.Printf("Error loading prompt template: %v", err)
		return &pb.Position{X: 0, Y: 0}
	}

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
		return &pb.Position{X: 0, Y: 0}
	}

	response := result.Text()
	response = strings.TrimSpace(response)
	log.Printf("Gemini response: %s", response)

	switch response {
	case "U":
		return &pb.Position{X: 0, Y: 1}
	case "D":
		return &pb.Position{X: 0, Y: -1}
	case "L":
		return &pb.Position{X: -1, Y: 0}
	case "R":
		return &pb.Position{X: 1, Y: 0}
	default:
		return &pb.Position{X: 0, Y: 0}
	}
}

func main() {
	// Parse command line flags
	devModePtr := flag.Bool("dev", true, "Run in development mode")
	serverURLPtr := flag.String("server", "simulation-server-service:9090", "Simulation server URL (gRPC)")
	numEntitiesPtr := flag.Int("entities", 3, "Number of entities to create")
	flag.Parse()

	if *devModePtr {
		log.Println("Running in development mode")
	}

	log.Printf("Creating %d entities to connect to gRPC server at %s", *numEntitiesPtr, *serverURLPtr)

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

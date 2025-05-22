package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
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
			log.Fatalf("WARNING: No API key found in environment variables")
		}
		return config
	}

	// Open and read the config file
	file, err := os.Open(configPath)
	if err != nil {
		log.Fatalf("Error opening config file: %v", err)
		return config
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(config)
	if err != nil {
		log.Fatalf("Error parsing config file: %v", err)
		return config
	}

	if config.GeminiAPIKey == "" {
		log.Fatalf("ERROR: No API key found in config file")
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

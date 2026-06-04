package main

import (
	"fmt"
	"log"
	"os"
)

func main() {
	fmt.Println("Starting NAFAS BOT API...")
	
	// Print simple config state
	env := os.Getenv("APP_ENV")
	if env == "" {
		env = "development"
	}
	fmt.Printf("Environment: %s\n", env)
	
	// Server simulation setup
	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}
	
	log.Printf("Server listening on port %s", port)
}

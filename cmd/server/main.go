package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/kayushkin/multichat/internal/api"
	"github.com/kayushkin/multichat/internal/db"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	// Initialize SQLite database
	database, err := db.Initialize("multichat.db")
	if err != nil {
		log.Fatal("Failed to initialize database:", err)
	}
	defer database.Close()

	// Initialize API router
	router := api.NewRouter(database)

	// Set up static file serving for frontend
	staticDir := "frontend/dist"
	if _, err := os.Stat(staticDir); os.IsNotExist(err) {
		log.Printf("Warning: Static directory %s does not exist", staticDir)
	}
	
	http.Handle("/", http.FileServer(http.Dir(staticDir)))
	http.Handle("/api/", http.StripPrefix("/api", router))

	// Health check endpoint
	http.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	port := ":8402"
	fmt.Printf("Starting MultiChat server on port %s...\n", port)
	fmt.Printf("Health check available at http://localhost%s/api/health\n", port)
	
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatal("Server failed to start:", err)
	}
}
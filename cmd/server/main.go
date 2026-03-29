package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/kayushkin/multichat/internal/api"
	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/matrix"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	database, err := db.Initialize("multichat.db")
	if err != nil {
		log.Fatal("Failed to initialize database:", err)
	}
	defer database.Close()

	// Matrix client — connects to Synapse
	var matrixClient *matrix.Client
	synapse := os.Getenv("SYNAPSE_URL")
	token := os.Getenv("MATRIX_TOKEN")
	if synapse != "" && token != "" {
		matrixClient = matrix.NewClient(synapse, token)
		log.Printf("Matrix client configured: %s", synapse)
	} else {
		log.Println("Warning: SYNAPSE_URL or MATRIX_TOKEN not set, Matrix features disabled")
	}

	tgDBPath := os.Getenv("TELEGRAM_DB")
	if tgDBPath != "" {
		log.Printf("Telegram DB for phone lookups: %s", tgDBPath)
	}

	router := api.NewRouter(database, matrixClient, tgDBPath)

	http.Handle("/api/", http.StripPrefix("/api", router))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8402"
	}
	addr := ":" + port
	fmt.Printf("MultiChat server listening on %s\n", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}

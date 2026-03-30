package main

import (
	"database/sql"
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
	metaDBPath := os.Getenv("META_DB")
	if metaDBPath != "" {
		log.Printf("Meta DB for platform lookups: %s", metaDBPath)
	}

	// Build self-identity map from bridge DBs
	matrix.SelfUserIDs["@admin:chat.kayushkin.com"] = true
	serverName := "chat.kayushkin.com"
	// Telegram bridge — get own puppet ID
	if tgDBPath != "" {
		if tgDB, err := sql.Open("sqlite3", tgDBPath+"?mode=ro"); err == nil {
			var tgID int64
			tgDB.QueryRow("SELECT id FROM puppet WHERE displayname LIKE '%Slava%' LIMIT 1").Scan(&tgID)
			if tgID != 0 {
				matrix.SelfUserIDs[fmt.Sprintf("@telegram_%d:%s", tgID, serverName)] = true
			}
			tgDB.Close()
		}
	}
	// WhatsApp — phone number is the ID
	matrix.SelfUserIDs[fmt.Sprintf("@whatsapp_15550100001:%s", serverName)] = true
	// Signal
	matrix.SelfUserIDs[fmt.Sprintf("@signal_15550100001:%s", serverName)] = true
	// Meta — login IDs from bridge DB
	if metaDBPath != "" {
		if metaDB, err := sql.Open("sqlite3", metaDBPath+"?mode=ro"); err == nil {
			rows, _ := metaDB.Query("SELECT id FROM user_login")
			if rows != nil {
				for rows.Next() {
					var id string
					rows.Scan(&id)
					matrix.SelfUserIDs[fmt.Sprintf("@meta_%s:%s", id, serverName)] = true
				}
				rows.Close()
			}
			metaDB.Close()
		}
	}
	log.Printf("Self user IDs: %d identities", len(matrix.SelfUserIDs))
	for id := range matrix.SelfUserIDs {
		log.Printf("  %s", id)
	}

	router := api.NewRouter(database, matrixClient, tgDBPath, metaDBPath)

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

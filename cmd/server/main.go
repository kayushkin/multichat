package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/kayushkin/bus"
	"github.com/kayushkin/llm-bridge/servicesettings"
	"github.com/kayushkin/multichat/internal/api"
	"github.com/kayushkin/multichat/internal/config"
	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/inbound"
	"github.com/kayushkin/multichat/internal/matrix"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	// Every variable this reads is declared in internal/config and served at
	// GET /api/settings. A misspelled owned variable, a port that is not a
	// whole number, or NATS_URL without what inbound needs stops the start
	// here, naming the variable.
	settings, err := config.NewSettingsRegistry(servicesettings.ProcessEnvironment())
	if err != nil {
		log.Fatal(err)
	}
	loaded, err := config.Load(settings)
	if err != nil {
		log.Fatal(err)
	}

	// MULTICHAT_DB pins the database to an absolute path. Left unset it falls back
	// to the relative "multichat.db" this has always used, so behaviour is unchanged
	// for anyone running the binary by hand from the repo.
	//
	// It exists because a RELATIVE database path makes the process's working
	// directory a load-bearing, invisible part of the config: start the server from
	// anywhere else and SQLite does not fail, it CREATES a brand-new empty database
	// and the service comes up looking perfectly healthy -- schema migrated, every
	// route answering, zero rows. The live DB (contacts, tags, loads) simply stops
	// being the one that is open. A deploy is exactly when a working directory
	// changes, which is exactly when you would not notice.
	dbPath := loaded.DatabasePath
	database, err := db.Initialize(dbPath)
	if err != nil {
		log.Fatal("Failed to initialize database:", err)
	}
	defer database.Close()
	log.Printf("Database: %s", dbPath)

	// Matrix client — connects to Synapse
	var matrixClient *matrix.Client
	if loaded.MatrixConfigured() {
		matrixClient = matrix.NewClient(loaded.SynapseURL, loaded.MatrixToken)
		log.Printf("Matrix client configured: %s", loaded.SynapseURL)
	} else {
		log.Println("Warning: SYNAPSE_URL or MATRIX_TOKEN not set, Matrix features disabled")
	}

	tgDBPath := loaded.TelegramBridgeDatabasePath
	if tgDBPath != "" {
		log.Printf("Telegram DB for phone lookups: %s", tgDBPath)
	}
	metaDBPath := loaded.MetaBridgeDatabasePath
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

	router := api.NewRouter(database, matrixClient, loaded, settings)

	// Inbound: /sync -> messaging.inbound -> rules -> agent sessions. Gated on
	// NATS_URL because the bus is the contract; with it set, a bus that cannot
	// be reached is fatal rather than a silent "inbound disabled". config.Load
	// has already refused NATS_URL without the Matrix client or LLM_BRIDGE_URL.
	if natsURL := loaded.NATSURL; natsURL != "" {
		busClient, err := bus.Connect(bus.Options{URL: natsURL, Name: "multichat"})
		if err != nil {
			log.Fatalf("NATS: %v", err)
		}
		if err := busClient.EnsureStreams(); err != nil {
			log.Fatalf("NATS streams: %v", err)
		}
		log.Printf("NATS connected: %s", natsURL)
		go (&inbound.SyncWorker{Matrix: matrixClient, Database: database, Bus: busClient}).Run()
		go (&inbound.Dispatcher{Database: database, Bus: busClient, LLMBridgeURL: strings.TrimRight(loaded.LLMBridgeServerURL, "/"), LLMBridgeServiceToken: loaded.LLMBridgeServiceToken, HTTPClient: &http.Client{Timeout: 10 * time.Second}}).Run()
	} else {
		log.Println("NATS_URL not set: inbound messaging (sync worker, rule dispatcher) is off")
	}

	http.Handle("/api/", http.StripPrefix("/api", router))

	addr := fmt.Sprintf(":%d", loaded.ListenPort)
	fmt.Printf("MultiChat server listening on %s\n", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}

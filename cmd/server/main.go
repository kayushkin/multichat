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
	// WhatsApp now names people by LID, us included; the bridge's whatsmeow
	// device row holds ours as "<lid>:<device>@lid".
	if whatsAppDBPath := loaded.WhatsAppBridgeDatabasePath; whatsAppDBPath != "" {
		whatsAppDB, err := sql.Open("sqlite3", "file:"+whatsAppDBPath+"?mode=ro")
		if err != nil {
			log.Fatalf("open WHATSAPP_DB %s: %v", whatsAppDBPath, err)
		}
		rows, err := whatsAppDB.Query(`SELECT lid FROM whatsmeow_device WHERE lid IS NOT NULL AND lid != ''`)
		if err != nil {
			log.Fatalf("read own WhatsApp LID from WHATSAPP_DB %s: %v", whatsAppDBPath, err)
		}
		for rows.Next() {
			var deviceLID string
			if err := rows.Scan(&deviceLID); err != nil {
				log.Fatalf("read own WhatsApp LID from WHATSAPP_DB %s: %v", whatsAppDBPath, err)
			}
			lid, _, _ := strings.Cut(strings.TrimSuffix(deviceLID, "@lid"), ":")
			matrix.SelfUserIDs[fmt.Sprintf("@whatsapp_lid-%s:%s", lid, serverName)] = true
		}
		rows.Close()
		whatsAppDB.Close()
	}
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
	// Discord — our own Discord user id from the bridge DB
	if discordDBPath := loaded.DiscordBridgeDatabasePath; discordDBPath != "" {
		discordDB, err := sql.Open("sqlite3", discordDBPath+"?mode=ro")
		if err != nil {
			log.Fatalf("open DISCORD_DB %s: %v", discordDBPath, err)
		}
		rows, err := discordDB.Query(`SELECT dcid FROM "user" WHERE dcid IS NOT NULL AND dcid != ''`)
		if err != nil {
			log.Fatalf("read own Discord ids from DISCORD_DB %s: %v", discordDBPath, err)
		}
		for rows.Next() {
			var discordUserID string
			if err := rows.Scan(&discordUserID); err != nil {
				log.Fatalf("read own Discord ids from DISCORD_DB %s: %v", discordDBPath, err)
			}
			matrix.SelfUserIDs[fmt.Sprintf("@discord_%s:%s", discordUserID, serverName)] = true
		}
		rows.Close()
		discordDB.Close()
	}
	// Google Messages names each phone-side participant "<id_prefix>.<participant id>",
	// and a login's metadata lists which participant ids are us.
	if googleMessagesDBPath := loaded.GoogleMessagesBridgeDatabasePath; googleMessagesDBPath != "" {
		googleMessagesDB, err := sql.Open("sqlite3", "file:"+googleMessagesDBPath+"?mode=ro")
		if err != nil {
			log.Fatalf("open GMESSAGES_DB %s: %v", googleMessagesDBPath, err)
		}
		rows, err := googleMessagesDB.Query(`SELECT json_extract(user_login.metadata, '$.id_prefix') || '.' || self_participant.value
			FROM user_login, json_each(json_extract(user_login.metadata, '$.self_participant_ids')) AS self_participant`)
		if err != nil {
			log.Fatalf("read own Google Messages participant ids from GMESSAGES_DB %s: %v", googleMessagesDBPath, err)
		}
		for rows.Next() {
			var ghostID string
			if err := rows.Scan(&ghostID); err != nil {
				log.Fatalf("read own Google Messages participant ids from GMESSAGES_DB %s: %v", googleMessagesDBPath, err)
			}
			matrix.SelfUserIDs[fmt.Sprintf("@gmessages_%s:%s", ghostID, serverName)] = true
		}
		if err := rows.Err(); err != nil {
			log.Fatalf("read own Google Messages participant ids from GMESSAGES_DB %s: %v", googleMessagesDBPath, err)
		}
		rows.Close()
		googleMessagesDB.Close()
	}
	log.Printf("Self user IDs: %d identities", len(matrix.SelfUserIDs))
	for id := range matrix.SelfUserIDs {
		log.Printf("  %s", id)
	}

	router := api.NewRouter(database, matrixClient, loaded, settings)
	if matrixClient != nil {
		// Read every room now, so the first GET /conversations does not wait.
		matrixClient.KeepConversationsCurrent()
	}
	if matrixClient != nil && loaded.PeopleStoreURL != "" {
		go func() {
			for {
				result, err := router.LinkByPhoneNumber()
				if err != nil {
					log.Printf("contact links: phone-number pass failed: %v", err)
				} else if result.Linked > 0 || result.Skipped > 0 {
					log.Printf("contact links: phone-number pass attached %d accounts, %d refused, left %d numbers on two or more people", result.Linked, result.Refused, result.Skipped)
				}
				time.Sleep(10 * time.Minute)
			}
		}()
	}

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
		go (&inbound.AutoReactor{Database: database, Bus: busClient, Matrix: matrixClient}).Run()
		go (&inbound.Dispatcher{Database: database, Bus: busClient, LLMBridgeURL: strings.TrimRight(loaded.LLMBridgeServerURL, "/"), LLMBridgeServiceToken: loaded.LLMBridgeServiceToken, HTTPClient: &http.Client{Timeout: 10 * time.Second}}).Run()
	} else {
		log.Println("NATS_URL not set: inbound messaging (sync worker, rule dispatcher, auto-reactor) is off")
	}

	http.Handle("/api/", http.StripPrefix("/api", router))

	addr := fmt.Sprintf(":%d", loaded.ListenPort)
	fmt.Printf("MultiChat server listening on %s\n", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}

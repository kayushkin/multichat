package db

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

// Initialize creates and configures the SQLite database
func Initialize(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %v", err)
	}

	// Test the connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %v", err)
	}

	// Run schema migrations
	if err := createSchema(db); err != nil {
		return nil, fmt.Errorf("failed to create schema: %v", err)
	}

	if err := createInboundSchema(db); err != nil {
		return nil, err
	}

	if err := createMessageLogSchema(db); err != nil {
		return nil, err
	}

	if err := createEmojiSchema(db); err != nil {
		return nil, err
	}

	if err := createTagSchema(db); err != nil {
		return nil, err
	}

	return db, nil
}

// createSchema creates all the necessary tables
func createSchema(db *sql.DB) error {
	schema := `
	-- Users table
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Platforms table (Telegram, WhatsApp, etc.)
	CREATE TABLE IF NOT EXISTS platforms (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT UNIQUE NOT NULL,
		display_name TEXT NOT NULL,
		enabled BOOLEAN DEFAULT TRUE
	);

	-- User platform connections
	CREATE TABLE IF NOT EXISTS user_platforms (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		platform_id INTEGER NOT NULL,
		platform_user_id TEXT NOT NULL,
		platform_username TEXT,
		access_token TEXT,
		refresh_token TEXT,
		connected_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_sync DATETIME,
		FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
		FOREIGN KEY (platform_id) REFERENCES platforms (id) ON DELETE CASCADE,
		UNIQUE(user_id, platform_id)
	);

	-- Conversations
	CREATE TABLE IF NOT EXISTS conversations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		platform_id INTEGER NOT NULL,
		platform_conversation_id TEXT NOT NULL,
		conversation_type TEXT NOT NULL, -- 'direct', 'group', 'channel'
		title TEXT,
		description TEXT,
		avatar_url TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_activity DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
		FOREIGN KEY (platform_id) REFERENCES platforms (id) ON DELETE CASCADE,
		UNIQUE(user_id, platform_id, platform_conversation_id)
	);

	-- Messages
	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		conversation_id INTEGER NOT NULL,
		platform_message_id TEXT NOT NULL,
		sender_id TEXT NOT NULL,
		sender_name TEXT,
		message_type TEXT NOT NULL DEFAULT 'text', -- 'text', 'image', 'file', 'sticker', etc.
		content TEXT,
		media_url TEXT,
		media_type TEXT,
		reply_to_id TEXT,
		sent_at DATETIME NOT NULL,
		received_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (conversation_id) REFERENCES conversations (id) ON DELETE CASCADE
	);

	-- Message reactions
	CREATE TABLE IF NOT EXISTS message_reactions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		message_id INTEGER NOT NULL,
		user_id TEXT NOT NULL,
		reaction TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (message_id) REFERENCES messages (id) ON DELETE CASCADE,
		UNIQUE(message_id, user_id, reaction)
	);

	-- Sessions for web authentication
	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		expires_at DATETIME NOT NULL,
		FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
	);

	-- Insert default platforms
	INSERT OR IGNORE INTO platforms (name, display_name) VALUES 
		('telegram', 'Telegram'),
		('whatsapp', 'WhatsApp'),
		('matrix', 'Matrix');

	-- Loads table (3PL freight loads)
	CREATE TABLE IF NOT EXISTS loads (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		reference_number TEXT UNIQUE NOT NULL,
		shipper TEXT NOT NULL,
		consignee TEXT NOT NULL,
		origin_city TEXT NOT NULL,
		origin_state TEXT NOT NULL,
		destination_city TEXT NOT NULL,
		destination_state TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'open',
		rate REAL NOT NULL,
		weight INTEGER,
		pickup_date TEXT,
		delivery_date TEXT,
		equipment_type TEXT NOT NULL DEFAULT 'dry_van',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Junction table: attach loads to conversations
	CREATE TABLE IF NOT EXISTS conversation_loads (
		conversation_id TEXT NOT NULL,
		load_id INTEGER NOT NULL REFERENCES loads(id) ON DELETE CASCADE,
		attachment_status TEXT NOT NULL DEFAULT 'tentative',
		attached_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (conversation_id, load_id)
	);

	-- Create indexes for better performance
	CREATE INDEX IF NOT EXISTS idx_conversations_user_platform ON conversations(user_id, platform_id);
	CREATE INDEX IF NOT EXISTS idx_messages_conversation ON messages(conversation_id);
	CREATE INDEX IF NOT EXISTS idx_messages_sent_at ON messages(sent_at);
	CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
	CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
	CREATE INDEX IF NOT EXISTS idx_loads_status ON loads(status);
	CREATE INDEX IF NOT EXISTS idx_loads_reference ON loads(reference_number);
	CREATE INDEX IF NOT EXISTS idx_conversation_loads_conv ON conversation_loads(conversation_id);
	CREATE INDEX IF NOT EXISTS idx_conversation_loads_load ON conversation_loads(load_id);
	`

	_, err := db.Exec(schema)
	if err != nil {
		return err
	}

	// Migrate: add attachment_status column if missing
	db.Exec(`ALTER TABLE conversation_loads ADD COLUMN attachment_status TEXT NOT NULL DEFAULT 'tentative'`)

	// Seed loads if table is empty
	var count int
	db.QueryRow("SELECT COUNT(*) FROM loads").Scan(&count)
	if count == 0 {
		seedLoads(db)
	}

	return nil
}

func seedLoads(db *sql.DB) {
	loads := []struct {
		ref, shipper, consignee, origCity, origState, destCity, destState, status string
		rate                                                                      float64
		weight                                                                    int
		pickup, delivery, equipment                                               string
	}{
		{"LD-2026-0001", "Sysco Foods", "HEB Grocery", "Houston", "TX", "San Antonio", "TX", "delivered", 1800, 42000, "2026-03-25", "2026-03-26", "reefer"},
		{"LD-2026-0002", "Tyson Foods", "Walmart DC", "Springdale", "AR", "Bentonville", "AR", "delivered", 1500, 38000, "2026-03-26", "2026-03-26", "reefer"},
		{"LD-2026-0003", "US Steel", "Turner Construction", "Gary", "IN", "Chicago", "IL", "in_transit", 4200, 44000, "2026-04-02", "2026-04-03", "flatbed"},
		{"LD-2026-0004", "Amazon FBA", "Amazon MDW2", "Los Angeles", "CA", "Joliet", "IL", "in_transit", 6500, 32000, "2026-04-01", "2026-04-04", "dry_van"},
		{"LD-2026-0005", "John Deere", "CAT Dealer", "Moline", "IL", "Peoria", "IL", "open", 2200, 35000, "2026-04-05", "2026-04-05", "flatbed"},
		{"LD-2026-0006", "Procter & Gamble", "Target DC", "Cincinnati", "OH", "Minneapolis", "MN", "open", 3800, 40000, "2026-04-06", "2026-04-08", "dry_van"},
		{"LD-2026-0007", "Cargill", "Perdue Farms", "Wichita", "KS", "Salisbury", "MD", "open", 5200, 43000, "2026-04-07", "2026-04-10", "reefer"},
		{"LD-2026-0008", "Georgia-Pacific", "Home Depot DC", "Atlanta", "GA", "Dallas", "TX", "open", 3200, 41000, "2026-04-06", "2026-04-08", "dry_van"},
		{"LD-2026-0009", "Nucor Steel", "McCarthy Building", "Charlotte", "NC", "Phoenix", "AZ", "open", 7500, 44000, "2026-04-08", "2026-04-12", "flatbed"},
		{"LD-2026-0010", "Anheuser-Busch", "Total Wine DC", "St. Louis", "MO", "Miami", "FL", "in_transit", 4800, 39000, "2026-04-01", "2026-04-04", "reefer"},
		{"LD-2026-0011", "Tesla", "Service Center", "Fremont", "CA", "Seattle", "WA", "open", 3500, 28000, "2026-04-07", "2026-04-08", "power_only"},
		{"LD-2026-0012", "Nestle", "Kroger DC", "Glendale", "CA", "Memphis", "TN", "cancelled", 4100, 37000, "2026-04-03", "2026-04-06", "reefer"},
		{"LD-2026-0013", "Caterpillar", "United Rentals", "Peoria", "IL", "Nashville", "TN", "open", 2800, 42000, "2026-04-09", "2026-04-10", "flatbed"},
		{"LD-2026-0014", "FedEx", "FedEx Ground Hub", "Memphis", "TN", "Indianapolis", "IN", "in_transit", 2100, 30000, "2026-04-02", "2026-04-03", "dry_van"},
		{"LD-2026-0015", "Kraft Heinz", "Costco DC", "Chicago", "IL", "Portland", "OR", "open", 5800, 41000, "2026-04-10", "2026-04-14", "reefer"},
		{"LD-2026-0016", "3M", "Grainger DC", "St. Paul", "MN", "Lake Forest", "IL", "open", 1950, 25000, "2026-04-08", "2026-04-09", "dry_van"},
		{"LD-2026-0017", "Weyerhaeuser", "Toll Brothers", "Tacoma", "WA", "Scottsdale", "AZ", "open", 4600, 43000, "2026-04-11", "2026-04-14", "flatbed"},
		{"LD-2026-0018", "Coca-Cola", "QuikTrip DC", "Atlanta", "GA", "Tulsa", "OK", "delivered", 3100, 40000, "2026-03-28", "2026-03-30", "dry_van"},
	}

	for _, l := range loads {
		db.Exec(`INSERT INTO loads (reference_number, shipper, consignee, origin_city, origin_state, destination_city, destination_state, status, rate, weight, pickup_date, delivery_date, equipment_type) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			l.ref, l.shipper, l.consignee, l.origCity, l.origState, l.destCity, l.destState, l.status, l.rate, l.weight, l.pickup, l.delivery, l.equipment)
	}
}

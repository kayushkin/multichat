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

	-- Create indexes for better performance
	CREATE INDEX IF NOT EXISTS idx_conversations_user_platform ON conversations(user_id, platform_id);
	CREATE INDEX IF NOT EXISTS idx_messages_conversation ON messages(conversation_id);
	CREATE INDEX IF NOT EXISTS idx_messages_sent_at ON messages(sent_at);
	CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
	CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
	`

	_, err := db.Exec(schema)
	return err
}
package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Inbound schema: the /sync resume token, the rules that turn a message from
// a person into an agent session, and the log of every dispatch a rule made.
const inboundSchema = `
CREATE TABLE IF NOT EXISTS matrix_sync_state (
	key        TEXT PRIMARY KEY,
	value      TEXT NOT NULL,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS inbound_rules (
	id                   INTEGER PRIMARY KEY AUTOINCREMENT,
	name                 TEXT    NOT NULL,
	platform             TEXT    NOT NULL DEFAULT '',   -- '' = any; else one of matrix.KnownPlatforms
	sender_user_id       TEXT    NOT NULL DEFAULT '',   -- '' = any; else the exact puppet id
	body_pattern         TEXT    NOT NULL DEFAULT '',   -- '' = any; else RE2 matched against the body
	direct_messages_only INTEGER NOT NULL DEFAULT 1,
	instance_id          TEXT    NOT NULL,              -- llm-bridge-server instance id, checked with it on write
	agent_id             TEXT    NOT NULL DEFAULT '',
	prompt_template      TEXT    NOT NULL,              -- Go text/template over inbound.PromptFields
	enabled              INTEGER NOT NULL DEFAULT 1,
	created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS inbound_dispatches (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	rule_id        INTEGER NOT NULL REFERENCES inbound_rules(id),
	event_id       TEXT    NOT NULL,
	room_id        TEXT    NOT NULL,
	sender_user_id TEXT    NOT NULL,
	bus_session_id TEXT    NOT NULL,
	error          TEXT    NOT NULL DEFAULT '',
	created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE (rule_id, event_id)  -- JetStream is at-least-once; one dispatch per rule per message
);
`

func createInboundSchema(database *sql.DB) error {
	if _, err := database.Exec(inboundSchema); err != nil {
		return fmt.Errorf("inbound schema: %w", err)
	}
	return nil
}

// --- sync state ---------------------------------------------------------------

const syncStateNextBatchKey = "next_batch"

// SyncNextBatch returns the stored /sync resume token, "" when there is none.
func SyncNextBatch(database *sql.DB) (string, error) {
	var value string
	err := database.QueryRow(`SELECT value FROM matrix_sync_state WHERE key = ?`, syncStateNextBatchKey).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read sync next_batch: %w", err)
	}
	return value, nil
}

// SaveSyncNextBatch stores the /sync resume token.
func SaveSyncNextBatch(database *sql.DB, nextBatch string) error {
	_, err := database.Exec(`INSERT INTO matrix_sync_state (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP`, syncStateNextBatchKey, nextBatch)
	if err != nil {
		return fmt.Errorf("save sync next_batch: %w", err)
	}
	return nil
}

// --- inbound rules ------------------------------------------------------------

// InboundRule turns a message from a person into an agent session. Every
// filter field is optional; an empty one matches anything. A rule with no
// filters at all fires on every message, which is allowed but loud.
type InboundRule struct {
	ID                 int64     `json:"id"`
	Name               string    `json:"name"`
	Platform           string    `json:"platform"`
	SenderUserID       string    `json:"sender_user_id"`
	BodyPattern        string    `json:"body_pattern"`
	DirectMessagesOnly bool      `json:"direct_messages_only"`
	InstanceID         string    `json:"instance_id"`
	AgentID            string    `json:"agent_id"`
	PromptTemplate     string    `json:"prompt_template"`
	Enabled            bool      `json:"enabled"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

const inboundRuleColumns = `id, name, platform, sender_user_id, body_pattern, direct_messages_only, instance_id, agent_id, prompt_template, enabled, created_at, updated_at`

func scanInboundRule(row interface{ Scan(...any) error }) (InboundRule, error) {
	var r InboundRule
	err := row.Scan(&r.ID, &r.Name, &r.Platform, &r.SenderUserID, &r.BodyPattern, &r.DirectMessagesOnly,
		&r.InstanceID, &r.AgentID, &r.PromptTemplate, &r.Enabled, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

func ListInboundRules(database *sql.DB, enabledOnly bool) ([]InboundRule, error) {
	query := `SELECT ` + inboundRuleColumns + ` FROM inbound_rules`
	if enabledOnly {
		query += ` WHERE enabled = 1`
	}
	query += ` ORDER BY id`
	rows, err := database.Query(query)
	if err != nil {
		return nil, fmt.Errorf("list inbound rules: %w", err)
	}
	defer rows.Close()
	rules := []InboundRule{}
	for rows.Next() {
		r, err := scanInboundRule(rows)
		if err != nil {
			return nil, fmt.Errorf("scan inbound rule: %w", err)
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

func GetInboundRule(database *sql.DB, id int64) (InboundRule, error) {
	r, err := scanInboundRule(database.QueryRow(`SELECT `+inboundRuleColumns+` FROM inbound_rules WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return InboundRule{}, ErrNotFound
	}
	if err != nil {
		return InboundRule{}, fmt.Errorf("get inbound rule %d: %w", id, err)
	}
	return r, nil
}

func CreateInboundRule(database *sql.DB, r InboundRule) (InboundRule, error) {
	result, err := database.Exec(`INSERT INTO inbound_rules
		(name, platform, sender_user_id, body_pattern, direct_messages_only, instance_id, agent_id, prompt_template, enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Name, r.Platform, r.SenderUserID, r.BodyPattern, r.DirectMessagesOnly, r.InstanceID, r.AgentID, r.PromptTemplate, r.Enabled)
	if err != nil {
		return InboundRule{}, fmt.Errorf("create inbound rule: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return InboundRule{}, fmt.Errorf("create inbound rule: %w", err)
	}
	return GetInboundRule(database, id)
}

// UpdateInboundRule writes every field of r; the caller merges a partial
// update onto the stored row first.
func UpdateInboundRule(database *sql.DB, r InboundRule) (InboundRule, error) {
	result, err := database.Exec(`UPDATE inbound_rules SET
		name = ?, platform = ?, sender_user_id = ?, body_pattern = ?, direct_messages_only = ?,
		instance_id = ?, agent_id = ?, prompt_template = ?, enabled = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		r.Name, r.Platform, r.SenderUserID, r.BodyPattern, r.DirectMessagesOnly, r.InstanceID, r.AgentID, r.PromptTemplate, r.Enabled, r.ID)
	if err != nil {
		return InboundRule{}, fmt.Errorf("update inbound rule %d: %w", r.ID, err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return InboundRule{}, ErrNotFound
	}
	return GetInboundRule(database, r.ID)
}

func DeleteInboundRule(database *sql.DB, id int64) error {
	result, err := database.Exec(`DELETE FROM inbound_rules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete inbound rule %d: %w", id, err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// InboundPromptField is one field a rule's prompt_template may reference, as
// GET /inbound-prompt-fields serves it. The list is read off the struct the
// dispatcher renders the template with (inbound.PromptFields), so an editor
// never carries its own copy.
type InboundPromptField struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// --- dispatches ---------------------------------------------------------------

// InboundDispatch is one session a rule started (or tried to) for one message.
type InboundDispatch struct {
	ID           int64     `json:"id"`
	RuleID       int64     `json:"rule_id"`
	EventID      string    `json:"event_id"`
	RoomID       string    `json:"room_id"`
	SenderUserID string    `json:"sender_user_id"`
	BusSessionID string    `json:"bus_session_id"`
	Error        string    `json:"error"`
	CreatedAt    time.Time `json:"created_at"`
}

// ErrAlreadyDispatched means this rule already acted on this event — a
// JetStream redelivery, not a new message.
var ErrAlreadyDispatched = errors.New("already dispatched")

// ClaimInboundDispatch records that rule acted on event. The UNIQUE index
// makes the insert the claim: a second insert for the same pair fails with
// ErrAlreadyDispatched and the caller must not act again.
func ClaimInboundDispatch(database *sql.DB, d InboundDispatch) (int64, error) {
	result, err := database.Exec(`INSERT OR IGNORE INTO inbound_dispatches
		(rule_id, event_id, room_id, sender_user_id, bus_session_id) VALUES (?, ?, ?, ?, ?)`,
		d.RuleID, d.EventID, d.RoomID, d.SenderUserID, d.BusSessionID)
	if err != nil {
		return 0, fmt.Errorf("claim dispatch: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return 0, ErrAlreadyDispatched
	}
	return result.LastInsertId()
}

// RecordInboundDispatchError stores why a claimed dispatch did not start a session.
func RecordInboundDispatchError(database *sql.DB, id int64, dispatchError string) error {
	if _, err := database.Exec(`UPDATE inbound_dispatches SET error = ? WHERE id = ?`, dispatchError, id); err != nil {
		return fmt.Errorf("record dispatch error %d: %w", id, err)
	}
	return nil
}

func ListInboundDispatches(database *sql.DB, limit int) ([]InboundDispatch, error) {
	rows, err := database.Query(`SELECT id, rule_id, event_id, room_id, sender_user_id, bus_session_id, error, created_at
		FROM inbound_dispatches ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list dispatches: %w", err)
	}
	defer rows.Close()
	out := []InboundDispatch{}
	for rows.Next() {
		var d InboundDispatch
		if err := rows.Scan(&d.ID, &d.RuleID, &d.EventID, &d.RoomID, &d.SenderUserID, &d.BusSessionID, &d.Error, &d.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan dispatch: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

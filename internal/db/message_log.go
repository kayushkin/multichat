package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// The message log keeps every message a bridge brings in, as it arrives, so
// that its text survives a later delete. On Matrix a delete is a redaction and
// Synapse strips the event's content, so once a message is deleted on the
// platform nothing else on this host still has what it said.
//
// Each row has its own id. A live row (source "matrix") also carries the
// Matrix event id, which Synapse owns and which is unique; a row imported
// from a Discord history fetch (source "discord-archive") has no event and
// carries the Discord message id instead, also unique. A redaction does not
// remove the row: it sets redacted_at and names the redaction event.
//
// event_id and discord_message_id are NULL when absent, so that UNIQUE holds
// only among rows that have one; they read back as "".
const messageLogColumnDefinitions = `(
	id                   INTEGER PRIMARY KEY AUTOINCREMENT,
	event_id             TEXT UNIQUE,                    -- Matrix event id; NULL for archive rows
	discord_message_id   TEXT UNIQUE,                    -- Discord message id; set for archive rows only (live rows: discord_message_parts)
	source               TEXT NOT NULL,                  -- "matrix" or "discord-archive"
	room_id              TEXT NOT NULL,
	room_name            TEXT NOT NULL,
	platform             TEXT NOT NULL,                  -- one of matrix.KnownPlatforms
	network_id           TEXT NOT NULL,                  -- from the room's m.bridge state; on Discord the server id
	network_name         TEXT NOT NULL,
	channel_id           TEXT NOT NULL,                  -- from m.bridge; on Discord the channel id
	channel_name         TEXT NOT NULL,
	channel_external_url TEXT NOT NULL,
	sender_user_id       TEXT NOT NULL,                  -- the bridge puppet
	sender_display_name  TEXT NOT NULL,
	message_type         TEXT NOT NULL,                  -- msgtype: m.text, m.image, ...; "" for archive rows
	body                 TEXT NOT NULL,                  -- plain text; for archive rows the Discord markdown
	format               TEXT NOT NULL DEFAULT '',       -- org.matrix.custom.html, discord-markdown or ""
	formatted_body       TEXT NOT NULL DEFAULT '',       -- the Matrix HTML when format is org.matrix.custom.html
	media_url            TEXT NOT NULL DEFAULT '',       -- mxc:// URI of an image, file or video
	media_mimetype       TEXT NOT NULL DEFAULT '',
	reply_to_event_id    TEXT NOT NULL DEFAULT '',       -- m.relates_to.m.in_reply_to.event_id
	content_json         TEXT NOT NULL,                  -- the event content exactly as Synapse sent it; for archive rows the Discord message object
	replaces_event_id    TEXT NOT NULL DEFAULT '',       -- set on an edit: the message it edits
	sent_at              DATETIME NOT NULL,              -- origin_server_ts, or the Discord timestamp
	logged_at            DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	redacted_at          DATETIME,                       -- when the redaction was sent; NULL while not deleted
	redaction_event_id   TEXT NOT NULL DEFAULT '',
	redacted_by_user_id  TEXT NOT NULL DEFAULT ''        -- who sent the redaction; on Discord the bridge, not the person who deleted
)`

const messageLogSchema = `CREATE TABLE IF NOT EXISTS message_log ` + messageLogColumnDefinitions + `;
CREATE INDEX IF NOT EXISTS idx_message_log_room_sent ON message_log(room_id, sent_at);
CREATE INDEX IF NOT EXISTS idx_message_log_redacted ON message_log(redacted_at) WHERE redacted_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_message_log_replaces ON message_log(replaces_event_id) WHERE replaces_event_id != '';
`

// Sources of a message log row.
const (
	// MessageLogSourceMatrix is a row the sync worker logged live from a bridge.
	MessageLogSourceMatrix = "matrix"
	// MessageLogSourceDiscordArchive is a row imported from a Discord
	// history fetch (cmd/multichat-import-discord-archive).
	MessageLogSourceDiscordArchive = "discord-archive"
)

// MessageFormatDiscordMarkdown is the format of an archive row's body: raw
// Discord markdown, custom emoji as <:name:id>.
const MessageFormatDiscordMarkdown = "discord-markdown"

func createMessageLogSchema(database *sql.DB) error {
	if err := migrateMessageLogToRowIDs(database); err != nil {
		return err
	}
	if _, err := database.Exec(messageLogSchema); err != nil {
		return fmt.Errorf("message log schema: %w", err)
	}
	if _, err := database.Exec(discordMessagePartsSchema); err != nil {
		return fmt.Errorf("Discord message parts schema: %w", err)
	}
	if _, err := database.Exec(reactionLogSchema); err != nil {
		return fmt.Errorf("reaction log schema: %w", err)
	}
	return nil
}

// LoggedMessage is one row of the message log.
type LoggedMessage struct {
	// ID is the log's own row id: the stable key of a row. EventID is ""
	// for archive rows.
	ID int64 `json:"id"`
	// Ref is ID as other stores cite it: message_000123. GET
	// /message-log/{ref} answers the row, and kanban-store's entity-type
	// registry claims the pattern, so a ref in a journal entry or a chat is a
	// chip.
	Ref     string `json:"ref"`
	EventID string `json:"event_id"`
	Source  string `json:"source"`
	// DiscordMessageID is the Discord message the row is, or for a live
	// Discord row the message it is part of (its text or one attachment;
	// several rows can share it); "" when unknown or not Discord.
	DiscordMessageID   string `json:"discord_message_id"`
	RoomID             string `json:"room_id"`
	RoomName           string `json:"room_name"`
	Platform           string `json:"platform"`
	NetworkID          string `json:"network_id"`
	NetworkName        string `json:"network_name"`
	ChannelID          string `json:"channel_id"`
	ChannelName        string `json:"channel_name"`
	ChannelExternalURL string `json:"channel_external_url"`
	SenderUserID       string `json:"sender_user_id"`
	SenderDisplayName  string `json:"sender_display_name"`
	MessageType        string `json:"message_type"`
	Body               string `json:"body"`
	// Format is "org.matrix.custom.html" when FormattedBody is Matrix HTML,
	// "discord-markdown" for archive rows, "" for plain text.
	Format           string     `json:"format"`
	FormattedBody    string     `json:"formatted_body"`
	MediaURL         string     `json:"media_url"`
	MediaMimetype    string     `json:"media_mimetype"`
	ReplyToEventID   string     `json:"reply_to_event_id"`
	ContentJSON      string     `json:"content_json"`
	ReplacesEventID  string     `json:"replaces_event_id"`
	SentAt           time.Time  `json:"sent_at"`
	LoggedAt         time.Time  `json:"logged_at"`
	RedactedAt       *time.Time `json:"redacted_at"`
	RedactionEventID string     `json:"redaction_event_id"`
	RedactedByUserID string     `json:"redacted_by_user_id"`
}

// MessageReactionGroup is the current reactions to one message with one key.
type MessageReactionGroup struct {
	// Key is a unicode emoji, an mxc:// URI (a custom emoji bridged to
	// Matrix) or, for archive rows, "discord-emoji:<id>".
	Key string `json:"key"`
	// Shortcode is a custom emoji's ":name:" when known, else "".
	Shortcode string `json:"shortcode"`
	Count     int    `json:"count"`
	// SenderDisplayNames lists who reacted, oldest first; empty for archive
	// rows, where Discord gives only the count.
	SenderDisplayNames []string `json:"sender_display_names"`
	// ReactedByMe is true when our own account reacted with this key, from
	// multichat (as the Matrix account) or from the platform's own app (as
	// our puppet).
	ReactedByMe bool `json:"reacted_by_me"`
	// MyReactionEventID is our Matrix account's reaction with this key, which
	// DELETE /conversations/{room_id}/reactions/{event_id} takes back; "" when
	// there is none. A reaction made in the platform's own app belongs to our
	// puppet, which multichat cannot redact, so it has no id here.
	MyReactionEventID string `json:"my_reaction_event_id"`
}

// MessageLogPage is what GET /message-log answers: messages newest first;
// the edits made to each, oldest first, keyed by the event id they edit;
// and the current reactions, keyed by the message's event id or, for an
// archive row, "discord:<discord_message_id>".
type MessageLogPage struct {
	Messages  []LoggedMessage                   `json:"messages"`
	Edits     map[string][]LoggedMessage        `json:"edits"`
	Reactions map[string][]MessageReactionGroup `json:"reactions"`
}

// LogMessage stores a live message. Logging the same event twice (a re-read
// sync batch) keeps the first row.
func LogMessage(database *sql.DB, m LoggedMessage) error {
	if m.EventID == "" {
		return fmt.Errorf("log message: a live message needs its event id")
	}
	_, err := database.Exec(`INSERT INTO message_log (event_id, source, room_id, room_name, platform, network_id,
		network_name, channel_id, channel_name, channel_external_url, sender_user_id, sender_display_name, message_type,
		body, format, formatted_body, media_url, media_mimetype, reply_to_event_id, content_json, replaces_event_id,
		sent_at, redacted_at, redaction_event_id, redacted_by_user_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(event_id) DO NOTHING`,
		m.EventID, MessageLogSourceMatrix, m.RoomID, m.RoomName, m.Platform, m.NetworkID,
		m.NetworkName, m.ChannelID, m.ChannelName, m.ChannelExternalURL, m.SenderUserID, m.SenderDisplayName, m.MessageType,
		m.Body, m.Format, m.FormattedBody, m.MediaURL, m.MediaMimetype, m.ReplyToEventID, m.ContentJSON, m.ReplacesEventID,
		m.SentAt.UTC(), nullableTime(m.RedactedAt), m.RedactionEventID, m.RedactedByUserID)
	if err != nil {
		return fmt.Errorf("log message %s: %w", m.EventID, err)
	}
	return nil
}

// LogArchivedDiscordMessage stores a message imported from a Discord history
// fetch. It reports whether a row was inserted: importing the same Discord
// message twice keeps the first row.
func LogArchivedDiscordMessage(database *sql.DB, m LoggedMessage) (bool, error) {
	if m.DiscordMessageID == "" {
		return false, fmt.Errorf("log archived message: it needs its Discord message id")
	}
	if m.EventID != "" {
		return false, fmt.Errorf("log archived message %s: an archive row has no Matrix event", m.DiscordMessageID)
	}
	result, err := database.Exec(`INSERT INTO message_log (discord_message_id, source, room_id, room_name, platform,
		network_id, network_name, channel_id, channel_name, channel_external_url, sender_user_id, sender_display_name,
		message_type, body, format, formatted_body, media_url, media_mimetype, reply_to_event_id, content_json,
		replaces_event_id, sent_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(discord_message_id) DO NOTHING`,
		m.DiscordMessageID, MessageLogSourceDiscordArchive, m.RoomID, m.RoomName, m.Platform,
		m.NetworkID, m.NetworkName, m.ChannelID, m.ChannelName, m.ChannelExternalURL, m.SenderUserID, m.SenderDisplayName,
		m.MessageType, m.Body, m.Format, m.FormattedBody, m.MediaURL, m.MediaMimetype, m.ReplyToEventID, m.ContentJSON,
		m.ReplacesEventID, m.SentAt.UTC())
	if err != nil {
		return false, fmt.Errorf("log archived message %s: %w", m.DiscordMessageID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

// MarkMessageRedacted records that a logged message was deleted. It reports
// whether the message was in the log; a redaction of a message logged before
// the log existed has nothing to mark.
func MarkMessageRedacted(database *sql.DB, eventID, redactionEventID, redactedByUserID string, redactedAt time.Time) (bool, error) {
	result, err := database.Exec(`UPDATE message_log SET redacted_at = ?, redaction_event_id = ?, redacted_by_user_id = ?
		WHERE event_id = ? AND redacted_at IS NULL`, redactedAt.UTC(), redactionEventID, redactedByUserID, eventID)
	if err != nil {
		return false, fmt.Errorf("mark %s redacted: %w", eventID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

// MessageLogHasEvent reports whether an event is already logged, as a
// message or as a reaction.
func MessageLogHasEvent(database *sql.DB, eventID string) (bool, error) {
	var one int
	err := database.QueryRow(`SELECT 1 FROM message_log WHERE event_id = ?
		UNION ALL SELECT 1 FROM reaction_log WHERE event_id = ? LIMIT 1`, eventID, eventID).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("look up %s in message log: %w", eventID, err)
	}
	return true, nil
}

// loggedMessageColumns reads discord_message_id from the row for an archive
// row and from discord_message_parts for a live Discord row.
const loggedMessageColumns = `id, event_id, COALESCE(discord_message_id,
	(SELECT p.discord_message_id FROM discord_message_parts p WHERE p.event_id = message_log.event_id)), source, room_id, room_name, platform, network_id,
	network_name, channel_id, channel_name, channel_external_url, sender_user_id, sender_display_name, message_type,
	body, format, formatted_body, media_url, media_mimetype, reply_to_event_id, content_json, replaces_event_id,
	sent_at, logged_at, redacted_at, redaction_event_id, redacted_by_user_id`

func scanLoggedMessage(rows *sql.Rows) (LoggedMessage, error) {
	var m LoggedMessage
	var eventID, discordMessageID sql.NullString
	var redactedAt sql.NullTime
	if err := rows.Scan(&m.ID, &eventID, &discordMessageID, &m.Source, &m.RoomID, &m.RoomName, &m.Platform,
		&m.NetworkID, &m.NetworkName, &m.ChannelID, &m.ChannelName, &m.ChannelExternalURL, &m.SenderUserID,
		&m.SenderDisplayName, &m.MessageType, &m.Body, &m.Format, &m.FormattedBody, &m.MediaURL, &m.MediaMimetype,
		&m.ReplyToEventID, &m.ContentJSON, &m.ReplacesEventID, &m.SentAt, &m.LoggedAt, &redactedAt,
		&m.RedactionEventID, &m.RedactedByUserID); err != nil {
		return LoggedMessage{}, fmt.Errorf("scan message log: %w", err)
	}
	m.Ref = MessageRef(m.ID)
	m.EventID = eventID.String
	m.DiscordMessageID = discordMessageID.String
	if redactedAt.Valid {
		t := redactedAt.Time
		m.RedactedAt = &t
	}
	return m, nil
}

// ListLoggedMessages returns messages newest first. Edits are left out
// unless the filter asks for them.
func ListLoggedMessages(database *sql.DB, filter MessageLogFilter) ([]LoggedMessage, error) {
	var where []string
	var args []any
	add := func(clause string, arg any) {
		where = append(where, clause)
		args = append(args, arg)
	}
	if filter.Platform != "" {
		add("platform = ?", filter.Platform)
	}
	if filter.NetworkID != "" {
		add("network_id = ?", filter.NetworkID)
	}
	if filter.RoomID != "" {
		add("room_id = ?", filter.RoomID)
	}
	if filter.SenderUserID != "" {
		add("sender_user_id = ?", filter.SenderUserID)
	}
	if filter.Query != "" {
		add("instr(lower(body), lower(?)) > 0", filter.Query)
	}
	if filter.DeletedOnly {
		where = append(where, "redacted_at IS NOT NULL")
	}
	if !filter.IncludeEdits {
		where = append(where, "replaces_event_id = ''")
	}
	if !filter.Before.IsZero() {
		add("sent_at < ?", filter.Before.UTC())
	}
	query := `SELECT ` + loggedMessageColumns + ` FROM message_log`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY sent_at DESC, id DESC LIMIT ?"
	args = append(args, filter.Limit)
	rows, err := database.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list message log: %w", err)
	}
	defer rows.Close()
	out := []LoggedMessage{}
	for rows.Next() {
		m, err := scanLoggedMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// EditsOf returns the edits made to the given messages, oldest first, keyed
// by the message they edit. Empty ids (archive rows) are skipped.
func EditsOf(database *sql.DB, eventIDs []string) (map[string][]LoggedMessage, error) {
	out := map[string][]LoggedMessage{}
	var args []any
	for _, id := range eventIDs {
		if id != "" {
			args = append(args, id)
		}
	}
	if len(args) == 0 {
		return out, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")
	rows, err := database.Query(`SELECT `+loggedMessageColumns+` FROM message_log
		WHERE replaces_event_id IN (`+placeholders+`) ORDER BY sent_at, id`, args...)
	if err != nil {
		return nil, fmt.Errorf("edits: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		m, err := scanLoggedMessage(rows)
		if err != nil {
			return nil, err
		}
		out[m.ReplacesEventID] = append(out[m.ReplacesEventID], m)
	}
	return out, rows.Err()
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}

const messageLogStartedAtKey = "message_log_started_at"

// MessageLogStartedAt returns when the log first ran, recording now on the
// first call. Filling a gap never reaches further back than this, so the
// first gap after the log starts does not page through each room's whole
// history.
func MessageLogStartedAt(database *sql.DB) (time.Time, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := database.Exec(`INSERT INTO matrix_sync_state (key, value) VALUES (?, ?) ON CONFLICT(key) DO NOTHING`,
		messageLogStartedAtKey, now); err != nil {
		return time.Time{}, fmt.Errorf("record message log start: %w", err)
	}
	return RecordedMessageLogStartedAt(database)
}

// RecordedMessageLogStartedAt returns when the log first ran, without
// recording anything: a log that has never run is an error.
func RecordedMessageLogStartedAt(database *sql.DB) (time.Time, error) {
	var value string
	err := database.QueryRow(`SELECT value FROM matrix_sync_state WHERE key = ?`, messageLogStartedAtKey).Scan(&value)
	if err == sql.ErrNoRows {
		return time.Time{}, fmt.Errorf("the message log has never run: no %s in matrix_sync_state", messageLogStartedAtKey)
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("read message log start: %w", err)
	}
	return time.Parse(time.RFC3339Nano, value)
}

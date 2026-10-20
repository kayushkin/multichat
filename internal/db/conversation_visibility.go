package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// A room is hidden from the conversation list when the operator hid it, or
// when they left it to the auto-hide rule and it has more members than the
// threshold. conversation_visibility holds only the rooms the operator chose
// for by hand; a room with no row follows the rule.
const conversationVisibilitySchema = `
CREATE TABLE IF NOT EXISTS conversation_visibility (
	room_id    TEXT PRIMARY KEY,
	visibility TEXT NOT NULL CHECK (visibility IN ('hidden', 'shown')),
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS conversation_visibility_settings (
	id                               INTEGER PRIMARY KEY CHECK (id = 1),
	auto_hide_enabled                INTEGER NOT NULL,
	auto_hide_member_count_threshold INTEGER NOT NULL
);
`

func createConversationVisibilitySchema(database *sql.DB) error {
	if _, err := database.Exec(conversationVisibilitySchema); err != nil {
		return fmt.Errorf("create conversation visibility schema: %w", err)
	}
	return nil
}

// ConversationVisibility is the operator's choice for one room: "hidden",
// "shown", or "auto" to follow the auto-hide rule.
type ConversationVisibility string

const (
	ConversationVisibilityHidden ConversationVisibility = "hidden"
	ConversationVisibilityShown  ConversationVisibility = "shown"
	ConversationVisibilityAuto   ConversationVisibility = "auto"
)

// ConversationVisibilities lists every value a room's visibility may take.
var ConversationVisibilities = []ConversationVisibility{
	ConversationVisibilityHidden, ConversationVisibilityShown, ConversationVisibilityAuto,
}

// ConversationVisibilitySettings is the auto-hide rule: with AutoHideEnabled,
// a room left on "auto" with more than AutoHideMemberCountThreshold members
// (the conversation list's member_count) is hidden.
type ConversationVisibilitySettings struct {
	AutoHideEnabled              bool `json:"auto_hide_enabled"`
	AutoHideMemberCountThreshold int  `json:"auto_hide_member_count_threshold"`
}

// DefaultConversationVisibilitySettings is the rule before the operator
// changes it.
var DefaultConversationVisibilitySettings = ConversationVisibilitySettings{
	AutoHideEnabled: true, AutoHideMemberCountThreshold: 30,
}

var (
	ErrInvalidConversationVisibility         = errors.New(`visibility must be "hidden", "shown" or "auto"`)
	ErrInvalidConversationVisibilitySettings = errors.New("auto_hide_member_count_threshold must be at least 1")
)

// SetConversationVisibility records the operator's choice for a room; "auto"
// removes the room's row so it follows the rule again.
func SetConversationVisibility(database *sql.DB, roomID string, visibility ConversationVisibility) error {
	if !strings.HasPrefix(roomID, "!") || !strings.Contains(roomID, ":") {
		return ErrInvalidRoomID
	}
	switch visibility {
	case ConversationVisibilityAuto:
		_, err := database.Exec(`DELETE FROM conversation_visibility WHERE room_id = ?`, roomID)
		return err
	case ConversationVisibilityHidden, ConversationVisibilityShown:
		_, err := database.Exec(`INSERT INTO conversation_visibility (room_id, visibility) VALUES (?, ?)
			ON CONFLICT(room_id) DO UPDATE SET visibility = excluded.visibility, updated_at = CURRENT_TIMESTAMP`,
			roomID, string(visibility))
		return err
	default:
		return fmt.Errorf("%w, not %q", ErrInvalidConversationVisibility, visibility)
	}
}

// ConversationVisibilityMap returns every room the operator chose for by
// hand, keyed by room id. A room absent from it is on "auto".
func ConversationVisibilityMap(database *sql.DB) (map[string]ConversationVisibility, error) {
	rows, err := database.Query(`SELECT room_id, visibility FROM conversation_visibility`)
	if err != nil {
		return nil, fmt.Errorf("conversation visibility: %w", err)
	}
	defer rows.Close()
	visibilityByRoomID := map[string]ConversationVisibility{}
	for rows.Next() {
		var roomID, visibility string
		if err := rows.Scan(&roomID, &visibility); err != nil {
			return nil, fmt.Errorf("conversation visibility: %w", err)
		}
		visibilityByRoomID[roomID] = ConversationVisibility(visibility)
	}
	return visibilityByRoomID, rows.Err()
}

// ReadConversationVisibilitySettings returns the auto-hide rule,
// DefaultConversationVisibilitySettings until it is first saved.
func ReadConversationVisibilitySettings(database *sql.DB) (ConversationVisibilitySettings, error) {
	var settings ConversationVisibilitySettings
	err := database.QueryRow(`SELECT auto_hide_enabled, auto_hide_member_count_threshold FROM conversation_visibility_settings WHERE id = 1`).
		Scan(&settings.AutoHideEnabled, &settings.AutoHideMemberCountThreshold)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultConversationVisibilitySettings, nil
	}
	if err != nil {
		return ConversationVisibilitySettings{}, fmt.Errorf("conversation visibility settings: %w", err)
	}
	return settings, nil
}

// SaveConversationVisibilitySettings replaces the auto-hide rule.
func SaveConversationVisibilitySettings(database *sql.DB, settings ConversationVisibilitySettings) error {
	if settings.AutoHideMemberCountThreshold < 1 {
		return fmt.Errorf("%w, not %d", ErrInvalidConversationVisibilitySettings, settings.AutoHideMemberCountThreshold)
	}
	_, err := database.Exec(`INSERT INTO conversation_visibility_settings (id, auto_hide_enabled, auto_hide_member_count_threshold) VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET auto_hide_enabled = excluded.auto_hide_enabled,
			auto_hide_member_count_threshold = excluded.auto_hide_member_count_threshold`,
		settings.AutoHideEnabled, settings.AutoHideMemberCountThreshold)
	return err
}

// ConversationIsHidden applies the operator's choice for a room, then the
// auto-hide rule.
func ConversationIsHidden(visibility ConversationVisibility, memberCount int, settings ConversationVisibilitySettings) bool {
	switch visibility {
	case ConversationVisibilityHidden:
		return true
	case ConversationVisibilityShown:
		return false
	}
	return settings.AutoHideEnabled && memberCount > settings.AutoHideMemberCountThreshold
}

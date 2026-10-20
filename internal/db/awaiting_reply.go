package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// The awaiting-reply list says which conversations hold a message from a
// tagged person that came after the operator's last word. Its settings are
// one row; a room the operator opened or dismissed keeps a mark saying how
// far it was read, so the list can tell a newer message from one already seen.
const awaitingReplySchema = `
CREATE TABLE IF NOT EXISTS awaiting_reply_settings (
	id                     INTEGER PRIMARY KEY CHECK (id = 1),
	tag_ids                TEXT NOT NULL,     -- JSON array of tags.id
	max_group_member_count INTEGER NOT NULL,  -- members including the operator
	max_message_age_days   INTEGER NOT NULL,
	clear_when_opened      INTEGER NOT NULL,
	clear_when_replied     INTEGER NOT NULL,
	updated_at             DATETIME NOT NULL
);
CREATE TABLE IF NOT EXISTS awaiting_reply_room_clears (
	room_id         TEXT PRIMARY KEY,
	cleared_through DATETIME NOT NULL,  -- sent_at of the newest message the operator was shown
	cleared_at      DATETIME NOT NULL,
	reason          TEXT NOT NULL       -- one of AwaitingReplyClearReasons
);
`

func createAwaitingReplySchema(database *sql.DB) error {
	if _, err := database.Exec(awaitingReplySchema); err != nil {
		return fmt.Errorf("create awaiting reply schema: %w", err)
	}
	return nil
}

// AwaitingReplySettings decide who and what the awaiting-reply list counts.
type AwaitingReplySettings struct {
	// TagIDs are multichat tags. A person counts when any of their accounts
	// carries one; a room counts when it carries one itself.
	TagIDs []int64 `json:"tag_ids"`
	// MaxGroupMemberCount is the largest group that counts, the operator
	// included. A direct message has 2. Discord server channels never count.
	MaxGroupMemberCount int `json:"max_group_member_count"`
	// MaxMessageAgeDays leaves out messages older than this many days.
	MaxMessageAgeDays int `json:"max_message_age_days"`
	// ClearWhenOpened lets opening a conversation in dash clear it.
	ClearWhenOpened bool `json:"clear_when_opened"`
	// ClearWhenReplied lets a message the operator sends, from any app,
	// clear what came before it.
	ClearWhenReplied bool `json:"clear_when_replied"`
	// UpdatedAt is when the settings were last saved; zero before the first save.
	UpdatedAt time.Time `json:"updated_at"`
}

// DefaultAwaitingReplySettings are the settings before the first save. No
// tag is chosen, so nothing counts until the operator picks one.
var DefaultAwaitingReplySettings = AwaitingReplySettings{
	TagIDs:              []int64{},
	MaxGroupMemberCount: 5,
	MaxMessageAgeDays:   30,
	ClearWhenOpened:     true,
	ClearWhenReplied:    true,
}

// ErrInvalidAwaitingReplySettings wraps every refusal of a settings write.
var ErrInvalidAwaitingReplySettings = errors.New("invalid awaiting reply settings")

// ReadAwaitingReplySettings returns the saved settings, or the defaults
// before the first save.
func ReadAwaitingReplySettings(database *sql.DB) (AwaitingReplySettings, error) {
	var tagIDsJSON string
	var settings AwaitingReplySettings
	err := database.QueryRow(`SELECT tag_ids, max_group_member_count, max_message_age_days, clear_when_opened, clear_when_replied, updated_at
		FROM awaiting_reply_settings WHERE id = 1`).Scan(&tagIDsJSON, &settings.MaxGroupMemberCount, &settings.MaxMessageAgeDays,
		&settings.ClearWhenOpened, &settings.ClearWhenReplied, &settings.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		defaults := DefaultAwaitingReplySettings
		defaults.TagIDs = []int64{}
		return defaults, nil
	}
	if err != nil {
		return AwaitingReplySettings{}, fmt.Errorf("read awaiting reply settings: %w", err)
	}
	if err := json.Unmarshal([]byte(tagIDsJSON), &settings.TagIDs); err != nil {
		return AwaitingReplySettings{}, fmt.Errorf("awaiting reply settings tag_ids %q: %w", tagIDsJSON, err)
	}
	return settings, nil
}

// SaveAwaitingReplySettings checks and stores the settings whole. Every tag
// must exist, a group must allow at least a direct message (2), and the age
// must be at least one day.
func SaveAwaitingReplySettings(database *sql.DB, settings AwaitingReplySettings, now time.Time) (AwaitingReplySettings, error) {
	if settings.TagIDs == nil {
		return AwaitingReplySettings{}, fmt.Errorf("%w: tag_ids is required (use [] for none)", ErrInvalidAwaitingReplySettings)
	}
	if settings.MaxGroupMemberCount < 2 {
		return AwaitingReplySettings{}, fmt.Errorf("%w: max_group_member_count must be at least 2 (a direct message), got %d", ErrInvalidAwaitingReplySettings, settings.MaxGroupMemberCount)
	}
	if settings.MaxMessageAgeDays < 1 {
		return AwaitingReplySettings{}, fmt.Errorf("%w: max_message_age_days must be at least 1, got %d", ErrInvalidAwaitingReplySettings, settings.MaxMessageAgeDays)
	}
	seen := map[int64]bool{}
	unique := []int64{}
	for _, tagID := range settings.TagIDs {
		if seen[tagID] {
			continue
		}
		seen[tagID] = true
		var exists int
		if err := database.QueryRow(`SELECT COUNT(*) FROM tags WHERE id = ?`, tagID).Scan(&exists); err != nil {
			return AwaitingReplySettings{}, fmt.Errorf("check tag %d: %w", tagID, err)
		}
		if exists == 0 {
			return AwaitingReplySettings{}, fmt.Errorf("%w: no tag has id %d", ErrInvalidAwaitingReplySettings, tagID)
		}
		unique = append(unique, tagID)
	}
	settings.TagIDs = unique
	settings.UpdatedAt = now.UTC()
	tagIDsJSON, err := json.Marshal(settings.TagIDs)
	if err != nil {
		return AwaitingReplySettings{}, err
	}
	if _, err := database.Exec(`INSERT INTO awaiting_reply_settings (id, tag_ids, max_group_member_count, max_message_age_days, clear_when_opened, clear_when_replied, updated_at)
		VALUES (1, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET tag_ids = excluded.tag_ids, max_group_member_count = excluded.max_group_member_count,
			max_message_age_days = excluded.max_message_age_days, clear_when_opened = excluded.clear_when_opened,
			clear_when_replied = excluded.clear_when_replied, updated_at = excluded.updated_at`,
		string(tagIDsJSON), settings.MaxGroupMemberCount, settings.MaxMessageAgeDays, settings.ClearWhenOpened, settings.ClearWhenReplied, settings.UpdatedAt); err != nil {
		return AwaitingReplySettings{}, fmt.Errorf("save awaiting reply settings: %w", err)
	}
	return settings, nil
}

// Why a room was cleared by hand. A reply clears a room without a mark: the
// operator's own message in the log says so.
const (
	AwaitingReplyClearOpened    = "opened"
	AwaitingReplyClearDismissed = "dismissed"
)

// AwaitingReplyClearReasons is every reason a clear may give.
var AwaitingReplyClearReasons = []string{AwaitingReplyClearOpened, AwaitingReplyClearDismissed}

// AwaitingReplyRoomClear is the mark a room keeps after the operator opened
// or dismissed it: messages sent at or before ClearedThrough are seen.
type AwaitingReplyRoomClear struct {
	RoomID         string    `json:"room_id"`
	ClearedThrough time.Time `json:"cleared_through"`
	ClearedAt      time.Time `json:"cleared_at"`
	Reason         string    `json:"reason"`
}

// ErrInvalidAwaitingReplyClear wraps every refusal of a clear.
var ErrInvalidAwaitingReplyClear = errors.New("invalid awaiting reply clear")

// ClearAwaitingReplyRoom records that the operator saw the room up to
// clearedThrough. A mark never moves backwards: a clear naming an older
// message than the room's mark keeps the newer one.
func ClearAwaitingReplyRoom(database *sql.DB, roomID string, clearedThrough time.Time, reason string, now time.Time) (AwaitingReplyRoomClear, error) {
	if !strings.HasPrefix(roomID, "!") || !strings.Contains(roomID, ":") {
		return AwaitingReplyRoomClear{}, fmt.Errorf("%w: %s", ErrInvalidAwaitingReplyClear, ErrInvalidRoomID)
	}
	known := false
	for _, candidate := range AwaitingReplyClearReasons {
		known = known || candidate == reason
	}
	if !known {
		return AwaitingReplyRoomClear{}, fmt.Errorf("%w: reason must be one of %s, got %q", ErrInvalidAwaitingReplyClear, strings.Join(AwaitingReplyClearReasons, ", "), reason)
	}
	if clearedThrough.IsZero() {
		return AwaitingReplyRoomClear{}, fmt.Errorf("%w: cleared_through is required", ErrInvalidAwaitingReplyClear)
	}
	if _, err := database.Exec(`INSERT INTO awaiting_reply_room_clears (room_id, cleared_through, cleared_at, reason) VALUES (?, ?, ?, ?)
		ON CONFLICT(room_id) DO UPDATE SET cleared_through = excluded.cleared_through, cleared_at = excluded.cleared_at, reason = excluded.reason
		WHERE excluded.cleared_through > awaiting_reply_room_clears.cleared_through`,
		roomID, clearedThrough.UTC(), now.UTC(), reason); err != nil {
		return AwaitingReplyRoomClear{}, fmt.Errorf("clear awaiting reply room %s: %w", roomID, err)
	}
	clears, err := AwaitingReplyRoomClears(database)
	if err != nil {
		return AwaitingReplyRoomClear{}, err
	}
	return clears[roomID], nil
}

// AwaitingReplyRoomClears returns every room's mark, keyed by room id.
func AwaitingReplyRoomClears(database *sql.DB) (map[string]AwaitingReplyRoomClear, error) {
	rows, err := database.Query(`SELECT room_id, cleared_through, cleared_at, reason FROM awaiting_reply_room_clears`)
	if err != nil {
		return nil, fmt.Errorf("list awaiting reply clears: %w", err)
	}
	defer rows.Close()
	clears := map[string]AwaitingReplyRoomClear{}
	for rows.Next() {
		var clear AwaitingReplyRoomClear
		if err := rows.Scan(&clear.RoomID, &clear.ClearedThrough, &clear.ClearedAt, &clear.Reason); err != nil {
			return nil, fmt.Errorf("scan awaiting reply clear: %w", err)
		}
		clear.ClearedThrough = clear.ClearedThrough.UTC()
		clear.ClearedAt = clear.ClearedAt.UTC()
		clears[clear.RoomID] = clear
	}
	return clears, rows.Err()
}

// ContactUserIDsWithTags returns every account carrying any of the tags.
func ContactUserIDsWithTags(database *sql.DB, tagIDs []int64) (map[string]bool, error) {
	return idsWithTags(database, `SELECT DISTINCT contact_user_id FROM contact_tags WHERE tag_id IN (%s)`, tagIDs)
}

// RoomIDsWithTags returns every room carrying any of the tags itself.
func RoomIDsWithTags(database *sql.DB, tagIDs []int64) (map[string]bool, error) {
	return idsWithTags(database, `SELECT DISTINCT room_id FROM conversation_tags WHERE tag_id IN (%s)`, tagIDs)
}

func idsWithTags(database *sql.DB, queryTemplate string, tagIDs []int64) (map[string]bool, error) {
	ids := map[string]bool{}
	if len(tagIDs) == 0 {
		return ids, nil
	}
	args := make([]any, len(tagIDs))
	for i, tagID := range tagIDs {
		args[i] = tagID
	}
	rows, err := database.Query(fmt.Sprintf(queryTemplate, placeholders(len(tagIDs))), args...)
	if err != nil {
		return nil, fmt.Errorf("ids with tags %v: %w", tagIDs, err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}

// MessagesFromSendersOrInRoomsSince returns the logged messages sent after
// since by any of the senders, or by anyone in any of the rooms, oldest
// first. Edits and deleted messages are left out.
func MessagesFromSendersOrInRoomsSince(database *sql.DB, senderUserIDs, roomIDs []string, since time.Time) ([]LoggedMessage, error) {
	out := []LoggedMessage{}
	if len(senderUserIDs) == 0 && len(roomIDs) == 0 {
		return out, nil
	}
	var who []string
	args := []any{since.UTC()}
	if len(senderUserIDs) > 0 {
		who = append(who, "sender_user_id IN ("+placeholders(len(senderUserIDs))+")")
		for _, id := range senderUserIDs {
			args = append(args, id)
		}
	}
	if len(roomIDs) > 0 {
		who = append(who, "room_id IN ("+placeholders(len(roomIDs))+")")
		for _, id := range roomIDs {
			args = append(args, id)
		}
	}
	rows, err := database.Query(`SELECT `+loggedMessageColumns+` FROM message_log
		WHERE sent_at > ? AND replaces_event_id = '' AND redacted_at IS NULL AND (`+strings.Join(who, " OR ")+`)
		ORDER BY sent_at, id`, args...)
	if err != nil {
		return nil, fmt.Errorf("messages from senders or in rooms: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		m, err := scanLoggedMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// NewestMessageSentAtByRoomFrom returns, per room, when the newest message
// any of the senders sent after since went out. Edits are left out; a
// deleted message still counts, since it was sent.
func NewestMessageSentAtByRoomFrom(database *sql.DB, senderUserIDs []string, since time.Time) (map[string]time.Time, error) {
	newest := map[string]time.Time{}
	if len(senderUserIDs) == 0 {
		return newest, nil
	}
	args := []any{since.UTC()}
	for _, id := range senderUserIDs {
		args = append(args, id)
	}
	rows, err := database.Query(`SELECT room_id, max(sent_at) FROM message_log
		WHERE sent_at > ? AND replaces_event_id = '' AND sender_user_id IN (`+placeholders(len(senderUserIDs))+`)
		GROUP BY room_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("newest message by room: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var roomID, sentAt string
		if err := rows.Scan(&roomID, &sentAt); err != nil {
			return nil, err
		}
		parsed, err := parseStoredTime(sentAt)
		if err != nil {
			return nil, fmt.Errorf("newest sent_at of room %s: %w", roomID, err)
		}
		newest[roomID] = parsed
	}
	return newest, rows.Err()
}

func placeholders(count int) string {
	return strings.TrimSuffix(strings.Repeat("?,", count), ",")
}

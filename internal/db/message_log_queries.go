package db

// Query inputs and summaries that are not on the wire. They live apart from
// message_log.go because tygo renders every exported type in that file.

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"
)

// MessageLogFilter narrows a message log listing. Empty fields match anything.
type MessageLogFilter struct {
	Platform     string
	NetworkID    string
	RoomID       string
	SenderUserID string
	Query        string // substring of the body
	DeletedOnly  bool
	IncludeEdits bool      // list edits as rows of their own too
	Before       time.Time // sent strictly before; zero means now
	Limit        int
}

// ChannelLogCounts is what the message log holds for one channel.
type ChannelLogCounts struct {
	LoggedMessageCount  int
	DeletedMessageCount int
	LastLoggedMessageAt *time.Time
}

// MessageLogCountsByChannel counts logged messages on one platform, keyed by
// the platform's channel id (from the room's m.bridge state). Edits are not
// counted: they are rows of their own, but not messages anyone sent. Archive
// rows are counted, as GET /message-log lists them. The count is what
// GET /message-log without include_edits lists for the channel.
func MessageLogCountsByChannel(database *sql.DB, platform string) (map[string]ChannelLogCounts, error) {
	rows, err := database.Query(`SELECT channel_id, count(*), count(redacted_at), max(sent_at) FROM message_log
		WHERE platform = ? AND replaces_event_id = '' AND channel_id != '' GROUP BY channel_id`, platform)
	if err != nil {
		return nil, fmt.Errorf("count message log by channel: %w", err)
	}
	defer rows.Close()
	out := map[string]ChannelLogCounts{}
	for rows.Next() {
		var channelID, lastSentAt string
		var counts ChannelLogCounts
		if err := rows.Scan(&channelID, &counts.LoggedMessageCount, &counts.DeletedMessageCount, &lastSentAt); err != nil {
			return nil, fmt.Errorf("scan message log counts: %w", err)
		}
		// max() drops the column's DATETIME type, so the driver hands back
		// the stored text; parse it with the formats the driver writes.
		last, err := parseStoredTime(lastSentAt)
		if err != nil {
			return nil, fmt.Errorf("newest sent_at of channel %s: %w", channelID, err)
		}
		counts.LastLoggedMessageAt = &last
		out[channelID] = counts
	}
	return out, rows.Err()
}

func parseStoredTime(value string) (time.Time, error) {
	for _, format := range sqlite3.SQLiteTimestampFormats {
		if t, err := time.ParseInLocation(format, value, time.UTC); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised stored time %q", value)
}

// MessageCountsByRoom counts the logged messages of every room, edits left
// out and deleted messages and archive rows counted, keyed by Matrix room id.
// A room with nothing logged is absent.
func MessageCountsByRoom(database *sql.DB) (map[string]int, error) {
	rows, err := database.Query(`SELECT room_id, count(*) FROM message_log WHERE replaces_event_id = '' GROUP BY room_id`)
	if err != nil {
		return nil, fmt.Errorf("count logged messages by room: %w", err)
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var roomID string
		var count int
		if err := rows.Scan(&roomID, &count); err != nil {
			return nil, fmt.Errorf("count logged messages by room: %w", err)
		}
		counts[roomID] = count
	}
	return counts, rows.Err()
}

// messageRefPrefix starts every message ref. The six-digit floor keeps a ref
// apart from a bare number and matches kanban-store's `message_\d{6,}`.
const messageRefPrefix = "message_"

// MessageRef renders a message log row id as the ref other stores cite.
func MessageRef(id int64) string { return fmt.Sprintf("%s%06d", messageRefPrefix, id) }

// ParseMessageRef reads a ref back to a row id. It refuses anything that
// MessageRef would not have written.
func ParseMessageRef(ref string) (int64, bool) {
	digits, found := strings.CutPrefix(ref, messageRefPrefix)
	if !found || len(digits) < 6 {
		return 0, false
	}
	id, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || id <= 0 || MessageRef(id) != ref {
		return 0, false
	}
	return id, true
}

// GetLoggedMessage reads one row of the message log by its id.
func GetLoggedMessage(database *sql.DB, id int64) (LoggedMessage, error) {
	rows, err := database.Query(`SELECT `+loggedMessageColumns+` FROM message_log WHERE id = ?`, id)
	if err != nil {
		return LoggedMessage{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return LoggedMessage{}, err
		}
		return LoggedMessage{}, sql.ErrNoRows
	}
	message, err := scanLoggedMessage(rows)
	if err != nil {
		return LoggedMessage{}, err
	}
	return message, rows.Err()
}

// EarliestLiveMessageInRoom is when the first message the live sync logged in
// a room was sent, and false when it has logged none there. An archive import
// keeps only what came before it, so a room bridged after the log began keeps
// the history between the two.
func EarliestLiveMessageInRoom(database *sql.DB, roomID string) (time.Time, bool, error) {
	var earliest sql.NullString
	if err := database.QueryRow(`SELECT MIN(sent_at) FROM message_log WHERE room_id = ? AND source = ?`,
		roomID, MessageLogSourceMatrix).Scan(&earliest); err != nil {
		return time.Time{}, false, err
	}
	if !earliest.Valid {
		return time.Time{}, false, nil
	}
	parsed, err := parseStoredTime(earliest.String)
	if err != nil {
		return time.Time{}, false, err
	}
	return parsed, true, nil
}

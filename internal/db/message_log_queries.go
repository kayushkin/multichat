package db

// Query inputs and summaries that are not on the wire. They live apart from
// message_log.go because tygo renders every exported type in that file.

import (
	"database/sql"
	"fmt"
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

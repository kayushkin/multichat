package db

// The reaction log and its read side. Not rendered to TypeScript: only
// MessageReactionGroup (message_log.go) is on the wire.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// reaction_log keeps every reaction a bridge puppet made, keyed by the
// reaction's own Matrix event id. Removing a reaction is redacting it, which
// sets redacted_at like a deleted message. (The older message_reactions table
// belongs to an earlier design and is not used.)
const reactionLogSchema = `
CREATE TABLE IF NOT EXISTS reaction_log (
	event_id             TEXT PRIMARY KEY,               -- the m.reaction event
	target_event_id      TEXT NOT NULL,                  -- the message reacted to
	room_id              TEXT NOT NULL,
	platform             TEXT NOT NULL,
	sender_user_id       TEXT NOT NULL,
	sender_display_name  TEXT NOT NULL,
	reaction_key         TEXT NOT NULL,                  -- unicode emoji, or mxc:// URI of a custom emoji
	shortcode            TEXT NOT NULL DEFAULT '',       -- com.beeper.reaction.shortcode, e.g. ":this:"
	content_json         TEXT NOT NULL,
	sent_at              DATETIME NOT NULL,
	logged_at            DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	redacted_at          DATETIME,
	redaction_event_id   TEXT NOT NULL DEFAULT '',
	redacted_by_user_id  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_reaction_log_target ON reaction_log(target_event_id);
`

// LoggedReaction is one row of the reaction log.
type LoggedReaction struct {
	EventID           string
	TargetEventID     string
	RoomID            string
	Platform          string
	SenderUserID      string
	SenderDisplayName string
	Key               string
	Shortcode         string
	ContentJSON       string
	SentAt            time.Time
	RedactedAt        *time.Time
	RedactionEventID  string
	RedactedByUserID  string
}

// LogReaction stores a reaction. Logging the same event twice keeps the
// first row.
func LogReaction(database *sql.DB, r LoggedReaction) error {
	_, err := database.Exec(`INSERT INTO reaction_log (event_id, target_event_id, room_id, platform, sender_user_id,
		sender_display_name, reaction_key, shortcode, content_json, sent_at, redacted_at, redaction_event_id,
		redacted_by_user_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(event_id) DO NOTHING`,
		r.EventID, r.TargetEventID, r.RoomID, r.Platform, r.SenderUserID, r.SenderDisplayName, r.Key, r.Shortcode,
		r.ContentJSON, r.SentAt.UTC(), nullableTime(r.RedactedAt), r.RedactionEventID, r.RedactedByUserID)
	if err != nil {
		return fmt.Errorf("log reaction %s: %w", r.EventID, err)
	}
	return nil
}

// MarkReactionRedacted records that a logged reaction was removed, and
// reports whether it was in the log.
func MarkReactionRedacted(database *sql.DB, eventID, redactionEventID, redactedByUserID string, redactedAt time.Time) (bool, error) {
	result, err := database.Exec(`UPDATE reaction_log SET redacted_at = ?, redaction_event_id = ?, redacted_by_user_id = ?
		WHERE event_id = ? AND redacted_at IS NULL`, redactedAt.UTC(), redactionEventID, redactedByUserID, eventID)
	if err != nil {
		return false, fmt.Errorf("mark reaction %s redacted: %w", eventID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

// ReactionsTo returns the current (not removed) reactions to the given
// messages, keyed by message event id, grouped by key in the order each key
// first appeared. Messages with none are absent.
func ReactionsTo(database *sql.DB, eventIDs []string) (map[string][]MessageReactionGroup, error) {
	out := map[string][]MessageReactionGroup{}
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
	rows, err := database.Query(`SELECT target_event_id, reaction_key, shortcode, sender_display_name FROM reaction_log
		WHERE target_event_id IN (`+placeholders+`) AND redacted_at IS NULL ORDER BY sent_at, event_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("reactions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var target, key, shortcode, sender string
		if err := rows.Scan(&target, &key, &shortcode, &sender); err != nil {
			return nil, fmt.Errorf("scan reaction: %w", err)
		}
		groups := out[target]
		index := -1
		for i := range groups {
			if groups[i].Key == key {
				index = i
				break
			}
		}
		if index < 0 {
			groups = append(groups, MessageReactionGroup{Key: key, SenderDisplayNames: []string{}})
			index = len(groups) - 1
		}
		if groups[index].Shortcode == "" {
			groups[index].Shortcode = shortcode
		}
		groups[index].Count++
		groups[index].SenderDisplayNames = append(groups[index].SenderDisplayNames, sender)
		out[target] = groups
	}
	return out, rows.Err()
}

// ArchiveReactionsKey is the key of an archive row in MessageLogPage.Reactions.
func ArchiveReactionsKey(discordMessageID string) string {
	return "discord:" + discordMessageID
}

// ArchivedDiscordReactions reads the reactions array of an archived Discord
// message object: a unicode emoji is its own key; a custom emoji is
// "discord-emoji:<id>" with shortcode ":name:". Discord gives counts, not who
// reacted, so SenderDisplayNames is empty.
func ArchivedDiscordReactions(discordMessageJSON string) ([]MessageReactionGroup, error) {
	var message struct {
		Reactions []struct {
			Emoji struct {
				ID   *string `json:"id"`
				Name string  `json:"name"`
			} `json:"emoji"`
			Count int `json:"count"`
		} `json:"reactions"`
	}
	if err := json.Unmarshal([]byte(discordMessageJSON), &message); err != nil {
		return nil, fmt.Errorf("archived Discord message: %w", err)
	}
	var out []MessageReactionGroup
	for _, reaction := range message.Reactions {
		group := MessageReactionGroup{Key: reaction.Emoji.Name, Count: reaction.Count, SenderDisplayNames: []string{}}
		if reaction.Emoji.ID != nil && *reaction.Emoji.ID != "" {
			group.Key = "discord-emoji:" + *reaction.Emoji.ID
			group.Shortcode = ":" + reaction.Emoji.Name + ":"
		}
		out = append(out, group)
	}
	return out, nil
}

// MessageLogReactions returns the reactions for a page of logged messages:
// from the reaction log for live rows, keyed by event id, and from the
// archived Discord object for archive rows, keyed by ArchiveReactionsKey.
func MessageLogReactions(database *sql.DB, messages []LoggedMessage) (map[string][]MessageReactionGroup, error) {
	eventIDs := make([]string, 0, len(messages))
	for _, m := range messages {
		eventIDs = append(eventIDs, m.EventID)
	}
	out, err := ReactionsTo(database, eventIDs)
	if err != nil {
		return nil, err
	}
	for _, m := range messages {
		if m.Source != MessageLogSourceDiscordArchive {
			continue
		}
		groups, err := ArchivedDiscordReactions(m.ContentJSON)
		if err != nil {
			return nil, fmt.Errorf("row %d: %w", m.ID, err)
		}
		if len(groups) > 0 {
			out[ArchiveReactionsKey(m.DiscordMessageID)] = groups
		}
	}
	return out, nil
}

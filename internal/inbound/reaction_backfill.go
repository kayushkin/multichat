package inbound

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/kayushkin/multichat/internal/matrix"
)

// ReactionBackfillResult is what BackfillReactions found and changed.
type ReactionBackfillResult struct {
	RoomsRead          int
	ReactionsSeen      int
	RedactionsSeen     int
	ReactionsLogged    int // new rows in reaction_log
	ReactionsMarkedOff int // reactions newly marked removed
}

// BackfillReactions logs the reactions, and the redactions that remove them,
// made in every joined room since a moment — for the stretch when the log
// ran but did not yet ask Synapse for m.reaction. It pages each room backward
// from its newest event until it passes since, then writes what it found
// oldest first through the sync worker's own logEvent, so a reaction is
// logged before its removal. Messages and stickers on those pages are left to
// the live log. Running it again changes nothing: a logged reaction keeps its
// first row, and a removed one stays marked.
func (w *SyncWorker) BackfillReactions(since time.Time) (ReactionBackfillResult, error) {
	var result ReactionBackfillResult
	if w.rooms == nil {
		w.rooms = map[string]roomFacts{}
	}
	if w.bridges == nil {
		w.bridges = map[string]matrix.BridgeInfo{}
	}
	before, err := reactionLogCounts(w.Database)
	if err != nil {
		return result, err
	}
	roomIDs, err := w.Matrix.JoinedRoomIDs()
	if err != nil {
		return result, err
	}
	for _, roomID := range roomIDs {
		found, err := w.reactionEventsSince(roomID, since)
		if err != nil {
			return result, fmt.Errorf("room %s: %w", roomID, err)
		}
		result.RoomsRead++
		for i := len(found) - 1; i >= 0; i-- {
			event := found[i]
			if event.Type == "m.reaction" {
				result.ReactionsSeen++
			} else {
				result.RedactionsSeen++
			}
			if err := w.logEvent(roomID, event); err != nil {
				return result, fmt.Errorf("event %s in %s: %w", event.EventID, roomID, err)
			}
		}
	}
	after, err := reactionLogCounts(w.Database)
	if err != nil {
		return result, err
	}
	result.ReactionsLogged = after.rows - before.rows
	result.ReactionsMarkedOff = after.removed - before.removed
	log.Printf("reaction backfill since %s: %+v", since.Format(time.RFC3339), result)
	return result, nil
}

// reactionEventsSince returns a room's reactions and redactions sent at or
// after since, newest first.
func (w *SyncWorker) reactionEventsSince(roomID string, since time.Time) ([]matrix.TimelineEvent, error) {
	var found []matrix.TimelineEvent
	from := ""
	for {
		page, next, err := w.Matrix.RoomEventsBefore(roomID, from, backfillPageSize)
		if err != nil {
			return nil, err
		}
		for _, event := range page {
			if time.UnixMilli(event.OriginServerTS).Before(since) {
				return found, nil
			}
			if event.Type == "m.reaction" || event.Type == "m.room.redaction" {
				found = append(found, event)
			}
		}
		if len(page) == 0 || next == "" || next == from {
			return found, nil
		}
		from = next
	}
}

type reactionCounts struct{ rows, removed int }

func reactionLogCounts(database *sql.DB) (reactionCounts, error) {
	var counts reactionCounts
	err := database.QueryRow(`SELECT count(*), count(redacted_at) FROM reaction_log`).Scan(&counts.rows, &counts.removed)
	if err != nil {
		return counts, fmt.Errorf("count reaction log: %w", err)
	}
	return counts, nil
}

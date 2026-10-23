package inbound

import (
	"fmt"
	"log"

	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/matrix"
)

// RoomBackfillResult says what BackfillRoom found and added.
type RoomBackfillResult struct {
	RoomID string
	// EventsSeen counts every message, sticker, reaction and redaction
	// Synapse holds for the room. Send statuses are left out.
	EventsSeen int
	// MessagesLogged counts rows the log did not have before.
	MessagesLogged int
}

// BackfillRoom logs a room's whole history as Synapse holds it, through the
// worker's own logEvent, so every message in it gets a message log row and a
// ref other stores can cite. The live gap fill stops at
// message_log_started_at; this does not, because a message a timeline cites
// may be older. Send status events are skipped: logging one again would
// count an auto-reaction's failure twice. Events are logged oldest first, so
// a message is logged before its redaction. A row already logged is left as
// it is, so running it again adds nothing.
func (w *SyncWorker) BackfillRoom(roomID string) (RoomBackfillResult, error) {
	result := RoomBackfillResult{RoomID: roomID}
	if w.rooms == nil {
		w.rooms = map[string]roomFacts{}
	}
	if w.bridges == nil {
		w.bridges = map[string]matrix.BridgeInfo{}
	}
	before, err := db.MessageCountsByRoom(w.Database)
	if err != nil {
		return result, err
	}
	var found []matrix.TimelineEvent
	from := ""
	for {
		page, next, err := w.Matrix.RoomEventsBefore(roomID, from, backfillPageSize)
		if err != nil {
			return result, err
		}
		for _, event := range page {
			// A send status changes an auto-reaction rule's count when it is
			// logged, so replaying one would take a failure off twice.
			if event.Type != matrix.MessageSendStatusEventType {
				found = append(found, event)
			}
		}
		if len(page) == 0 || next == "" || next == from {
			break
		}
		from = next
	}
	result.EventsSeen = len(found)
	for i := len(found) - 1; i >= 0; i-- {
		if err := w.logEvent(roomID, found[i]); err != nil {
			return result, fmt.Errorf("event %s in %s: %w", found[i].EventID, roomID, err)
		}
	}
	after, err := db.MessageCountsByRoom(w.Database)
	if err != nil {
		return result, err
	}
	result.MessagesLogged = after[roomID] - before[roomID]
	log.Printf("room backfill: %+v", result)
	return result, nil
}

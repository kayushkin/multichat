package inbound

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/matrix"
)

// backfillPageSize is how many events one backward page asks Synapse for
// when a sync reported a gap.
const backfillPageSize = 100

// logBatch writes a sync batch to the message log: every message a bridge
// puppet sent, and every redaction, in timeline order. A room whose timeline
// was truncated is first filled backward from the gap, so the log does not
// lose what the sync skipped.
func (w *SyncWorker) logBatch(resp *matrix.SyncResponse) error {
	for roomID, events := range resp.Rooms {
		if from, truncated := resp.PrevBatch[roomID]; truncated {
			if err := w.fillGap(roomID, from); err != nil {
				return fmt.Errorf("fill gap in %s: %w", roomID, err)
			}
		}
		for _, event := range events {
			if err := w.logEvent(roomID, event); err != nil {
				return fmt.Errorf("event %s in %s: %w", event.EventID, roomID, err)
			}
		}
	}
	return nil
}

// fillGap pages backward from a truncated timeline until it reaches an event
// already logged or one older than the log itself, then logs what it found
// oldest first, so a message is always logged before its redaction.
func (w *SyncWorker) fillGap(roomID, from string) error {
	var found []matrix.TimelineEvent
	for from != "" {
		page, next, err := w.Matrix.RoomEventsBefore(roomID, from, backfillPageSize)
		if err != nil {
			return err
		}
		reachedLogged := false
		for _, event := range page {
			logged, err := db.MessageLogHasEvent(w.Database, event.EventID)
			if err != nil {
				return err
			}
			if logged || time.UnixMilli(event.OriginServerTS).Before(w.messageLogStartedAt) {
				reachedLogged = true
				break
			}
			found = append(found, event)
		}
		if reachedLogged || len(page) == 0 {
			break
		}
		from = next
	}
	for i := len(found) - 1; i >= 0; i-- {
		if err := w.logEvent(roomID, found[i]); err != nil {
			return fmt.Errorf("event %s: %w", found[i].EventID, err)
		}
	}
	log.Printf("message log: filled a gap in %s with %d events", roomID, len(found))
	return nil
}

func (w *SyncWorker) logEvent(roomID string, event matrix.TimelineEvent) error {
	switch event.Type {
	case "m.room.redaction":
		target, err := event.RedactedEventID()
		if err != nil {
			return err
		}
		marked, err := db.MarkMessageRedacted(w.Database, target, event.EventID, event.Sender, time.UnixMilli(event.OriginServerTS))
		if err != nil {
			return err
		}
		if marked {
			log.Printf("message log: %s was deleted in %s", target, roomID)
		}
		return nil
	case "m.room.message":
		platform := matrix.PlatformOfUserID(event.Sender)
		if platform == "" {
			// Not from a bridge: @admin, a bridge bot, a plain Matrix user.
			return nil
		}
		return w.logMessage(roomID, platform, event)
	default:
		return nil
	}
}

func (w *SyncWorker) logMessage(roomID, platform string, event matrix.TimelineEvent) error {
	var content struct {
		MsgType    string `json:"msgtype"`
		Body       string `json:"body"`
		NewContent *struct {
			Body string `json:"body"`
		} `json:"m.new_content"`
		RelatesTo struct {
			RelType string `json:"rel_type"`
			EventID string `json:"event_id"`
		} `json:"m.relates_to"`
	}
	if len(event.Content) > 0 {
		if err := json.Unmarshal(event.Content, &content); err != nil {
			return fmt.Errorf("content: %w", err)
		}
	}
	facts, err := w.factsOf(roomID)
	if err != nil {
		return err
	}
	bridge, err := w.bridgeOf(roomID)
	if err != nil {
		return err
	}
	displayName, err := w.Matrix.DisplayNameOf(event.Sender)
	if err != nil {
		return err
	}
	message := db.LoggedMessage{
		EventID:            event.EventID,
		RoomID:             roomID,
		RoomName:           facts.Name,
		Platform:           platform,
		NetworkID:          bridge.NetworkID,
		NetworkName:        bridge.NetworkName,
		ChannelID:          bridge.ChannelID,
		ChannelName:        bridge.ChannelName,
		ChannelExternalURL: bridge.ChannelExternalURL,
		SenderUserID:       event.Sender,
		SenderDisplayName:  displayName,
		MessageType:        content.MsgType,
		Body:               content.Body,
		ContentJSON:        string(event.Content),
		SentAt:             time.UnixMilli(event.OriginServerTS),
	}
	if content.RelatesTo.RelType == "m.replace" {
		message.ReplacesEventID = content.RelatesTo.EventID
		if content.NewContent != nil {
			// The top-level body of an edit is a fallback ("* new text");
			// the edited text itself is in m.new_content.
			message.Body = content.NewContent.Body
		}
	}
	if redaction := event.Unsigned.RedactedBecause; redaction != nil {
		// Deleted before this worker read it: the content is already gone.
		redactedAt := time.UnixMilli(redaction.OriginServerTS)
		message.RedactedAt = &redactedAt
		message.RedactionEventID = redaction.EventID
		message.RedactedByUserID = redaction.Sender
	}
	return db.LogMessage(w.Database, message)
}

func (w *SyncWorker) bridgeOf(roomID string) (matrix.BridgeInfo, error) {
	if bridge, ok := w.bridges[roomID]; ok {
		return bridge, nil
	}
	bridge, err := w.Matrix.RoomBridgeInfo(roomID)
	if err != nil {
		return matrix.BridgeInfo{}, err
	}
	w.bridges[roomID] = bridge
	return bridge, nil
}

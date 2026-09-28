package inbound

import (
	"fmt"
	"log"
	"time"

	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/matrix"
)

// backfillPageSize is how many events one backward page asks Synapse for
// when a sync reported a gap.
const backfillPageSize = 100

// logBatch writes a sync batch to the message log: every message, sticker and
// reaction a bridge puppet sent, and every redaction, in timeline order. A room whose timeline
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
		// A redaction hits a message or a reaction; mark whichever it is.
		target, err := event.RedactedEventID()
		if err != nil {
			return err
		}
		redactedAt := time.UnixMilli(event.OriginServerTS)
		marked, err := db.MarkMessageRedacted(w.Database, target, event.EventID, event.Sender, redactedAt)
		if err != nil {
			return err
		}
		if marked {
			log.Printf("message log: %s was deleted in %s", target, roomID)
			return nil
		}
		if _, err := db.MarkReactionRedacted(w.Database, target, event.EventID, event.Sender, redactedAt); err != nil {
			return err
		}
		return nil
	case "m.room.message", "m.sticker":
		// A sticker is a message of its own event type, with no msgtype:
		// body is the sticker's name, url and info.mimetype its image.
		platform := matrix.PlatformOfUserID(event.Sender)
		if platform == "" {
			// Not from a bridge: @admin, a bridge bot, a plain Matrix user.
			return nil
		}
		return w.logMessage(roomID, platform, event)
	case "m.reaction":
		platform := matrix.PlatformOfUserID(event.Sender)
		if platform == "" {
			return nil
		}
		return w.logReaction(roomID, platform, event)
	default:
		return nil
	}
}

func (w *SyncWorker) logMessage(roomID, platform string, event matrix.TimelineEvent) error {
	parsed, err := matrix.ParseMessageContent(event.Content)
	if err != nil {
		return err
	}
	// An edit's top level is a "* new text" fallback; log the edited
	// message itself from m.new_content.
	content := db.LoggedContentOf(parsed)
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
		MessageType:        messageTypeOf(event.Type, content.MsgType),
		Body:               content.Body,
		Format:             content.Format,
		FormattedBody:      content.FormattedBody,
		MediaURL:           content.MediaURL,
		MediaMimetype:      content.MediaMimetype,
		ReplyToEventID:     content.ReplyToEventID,
		ContentJSON:        string(event.Content),
		ReplacesEventID:    content.ReplacesEventID,
		SentAt:             time.UnixMilli(event.OriginServerTS),
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

// messageTypeOf is what the log records as a message's type: the msgtype of
// an m.room.message, and the event type of an m.sticker, which has none.
func messageTypeOf(eventType, msgType string) string {
	if eventType == "m.sticker" {
		return "m.sticker"
	}
	return msgType
}

func (w *SyncWorker) logReaction(roomID, platform string, event matrix.TimelineEvent) error {
	if event.Unsigned.RedactedBecause != nil {
		// Removed before this worker read it: Synapse has stripped which
		// message it was on and which emoji it was, so there is nothing to
		// show.
		log.Printf("message log: reaction %s in %s was removed before it was read", event.EventID, roomID)
		return nil
	}
	content, err := matrix.ParseReactionContent(event.Content)
	if err != nil {
		return err
	}
	displayName, err := w.Matrix.DisplayNameOf(event.Sender)
	if err != nil {
		return err
	}
	return db.LogReaction(w.Database, db.LoggedReaction{
		EventID:           event.EventID,
		TargetEventID:     content.TargetEventID,
		RoomID:            roomID,
		Platform:          platform,
		SenderUserID:      event.Sender,
		SenderDisplayName: displayName,
		Key:               content.Key,
		Shortcode:         content.Shortcode,
		ContentJSON:       string(event.Content),
		SentAt:            time.UnixMilli(event.OriginServerTS),
	})
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

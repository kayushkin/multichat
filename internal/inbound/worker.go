// Package inbound turns messages people send us on bridged platforms into
// bus events and, through rules, into agent sessions.
//
// Two loops run in the server process:
//
//   - SyncWorker long-polls Synapse /sync, accepts every room invite a bridge
//     sends (invites.go), invites a bridge's bot into any room it is missing
//     from (bridgebots.go), writes every bridged message and
//     reaction and every redaction to the message log (message_log.go), and
//     publishes every message (never a reaction) a
//     person sent as messages.MessagingInbound on messaging.inbound
//     (JetStream, stream MESSAGING). Our own sends and our own platform
//     puppets are dropped here, so no consumer ever sees itself.
//   - Dispatcher is a durable consumer of that subject that applies the
//     inbound_rules table and, for each match, asks llm-bridge-adapter to
//     start an autonomous session by publishing messages.BridgeInbound.
//
// They are joined by the bus rather than a function call so that any other
// service can consume messaging.inbound with its own durable name, and so
// that a dispatch survives a restart of this process.
package inbound

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/kayushkin/bus"
	"github.com/kayushkin/bus/messages"
	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/matrix"
)

// syncTimeout is how long Synapse holds each /sync open. Long enough that an
// idle homeserver costs one request every half minute, short enough that a
// stuck connection is noticed.
const syncTimeout = 30 * time.Second

// SyncWorker publishes inbound messages from Synapse onto the bus.
type SyncWorker struct {
	Matrix   *matrix.Client
	Database *sql.DB
	Bus      *bus.Client

	// rooms caches what does not change between messages: the room's name,
	// platform and whether it is a direct message.
	rooms map[string]roomFacts
	// bridges caches each room's m.bridge state for the message log.
	bridges map[string]matrix.BridgeInfo
	// messageLogStartedAt bounds how far back a gap is filled.
	messageLogStartedAt time.Time
	// joinedAt records when this process accepted each room's invite
	// (invites.go), so the history a new room arrives with is not published.
	joinedAt map[string]time.Time
	// bridgeBotChecks queues rooms for the bridge bot checker (bridgebots.go).
	bridgeBotChecks chan string
}

type roomFacts struct {
	Name            string
	IsDirectMessage bool
}

// Run blocks forever. A failed sync is logged and retried after a pause; a
// failed publish stops the token from advancing so the batch is re-read.
func (w *SyncWorker) Run() {
	w.rooms = map[string]roomFacts{}
	w.bridges = map[string]matrix.BridgeInfo{}
	w.joinedAt = map[string]time.Time{}
	w.bridgeBotChecks = make(chan string, bridgeBotQueueSize)
	go w.checkBridgeBots()
	startedAt, err := db.MessageLogStartedAt(w.Database)
	if err != nil {
		log.Fatalf("inbound sync: %v", err)
	}
	w.messageLogStartedAt = startedAt
	since, err := db.SyncNextBatch(w.Database)
	if err != nil {
		log.Fatalf("inbound sync: %v", err)
	}
	if since == "" {
		// First run ever: take the token and drop the history it comes with.
		// Everything before this moment was already read in the UI; replaying
		// it as "new" would fire every rule on old messages.
		initial, err := w.Matrix.Sync("", 0)
		if err != nil {
			log.Fatalf("inbound sync: initial sync: %v", err)
		}
		if err := db.SaveSyncNextBatch(w.Database, initial.NextBatch); err != nil {
			log.Fatalf("inbound sync: %v", err)
		}
		since = initial.NextBatch
		log.Printf("inbound sync: initial sync done, %d rooms with history skipped", len(initial.Rooms))
	}
	if err := w.acceptPendingInvites(); err != nil {
		log.Fatalf("inbound sync: pending invites: %v", err)
	}
	joinedRoomIDs, err := w.Matrix.JoinedRoomIDs()
	if err != nil {
		log.Fatalf("inbound sync: joined rooms: %v", err)
	}
	for _, roomID := range joinedRoomIDs {
		w.queueBridgeBotCheck(roomID)
	}
	log.Printf("inbound sync: running (publishing %s)", messages.MessagingInboundSubject)
	for {
		resp, err := w.Matrix.Sync(since, int(syncTimeout/time.Millisecond))
		var gap *matrix.TimelineGapError
		if errors.As(err, &gap) {
			log.Printf("inbound sync: WARNING %v — those messages were not published", err)
			err = nil
		}
		if err != nil {
			log.Printf("inbound sync: %v (retrying in 5s)", err)
			time.Sleep(5 * time.Second)
			continue
		}
		if err := w.logBatch(resp); err != nil {
			log.Printf("message log: %v (not advancing past %s)", err, since)
			time.Sleep(5 * time.Second)
			continue
		}
		if err := w.publishBatch(resp); err != nil {
			// Do not advance: the next sync re-reads from the same token and
			// the consumer's event_id dedupe absorbs the repeat.
			log.Printf("inbound sync: %v (not advancing past %s)", err, since)
			time.Sleep(5 * time.Second)
			continue
		}
		if err := db.SaveSyncNextBatch(w.Database, resp.NextBatch); err != nil {
			log.Fatalf("inbound sync: %v", err)
		}
		since = resp.NextBatch
		w.acceptInvites(resp.Invites)
	}
}

func (w *SyncWorker) publishBatch(resp *matrix.SyncResponse) error {
	self, err := w.Matrix.WhoAmI()
	if err != nil {
		return err
	}
	for roomID, events := range resp.Rooms {
		for _, event := range events {
			// Only messages go on the bus. Reactions and stickers are logged
			// but not published: neither carries text a rule could match
			// (a sticker's body is only its name), and a sticker must not
			// start a session the way a written message can.
			if event.Type != "m.room.message" {
				continue
			}
			if event.Sender == self || matrix.SelfUserIDs[event.Sender] || matrix.IsBridgeBot(event.Sender) {
				continue
			}
			if w.sentBeforeWeJoined(roomID, event) {
				continue
			}
			inbound, err := w.envelope(roomID, event)
			if err != nil {
				return fmt.Errorf("event %s in %s: %w", event.EventID, roomID, err)
			}
			if err := w.Bus.JetPublish(messages.MessagingInboundSubject, inbound); err != nil {
				return fmt.Errorf("publish %s: %w", event.EventID, err)
			}
			log.Printf("inbound sync: published %s from %s (%s) in %s", event.EventID, inbound.SenderUserID, inbound.Platform, roomID)
		}
	}
	return nil
}

func (w *SyncWorker) envelope(roomID string, event matrix.TimelineEvent) (messages.MessagingInbound, error) {
	var content matrix.MessageContent
	if err := json.Unmarshal(event.Content, &content); err != nil {
		return messages.MessagingInbound{}, fmt.Errorf("content: %w", err)
	}
	facts, err := w.factsOf(roomID)
	if err != nil {
		return messages.MessagingInbound{}, err
	}
	displayName, err := w.Matrix.DisplayNameOf(event.Sender)
	if err != nil {
		return messages.MessagingInbound{}, err
	}
	return messages.MessagingInbound{
		EventID:           event.EventID,
		RoomID:            roomID,
		RoomName:          facts.Name,
		Platform:          matrix.PlatformOfUserID(event.Sender),
		SenderUserID:      event.Sender,
		SenderDisplayName: displayName,
		MessageType:       content.MsgType,
		Body:              content.Body,
		IsDirectMessage:   facts.IsDirectMessage,
		Timestamp:         time.UnixMilli(event.OriginServerTS).UTC(),
	}, nil
}

func (w *SyncWorker) factsOf(roomID string) (roomFacts, error) {
	if facts, ok := w.rooms[roomID]; ok {
		return facts, nil
	}
	name, err := w.Matrix.RoomName(roomID)
	if err != nil {
		return roomFacts{}, err
	}
	members, err := w.Matrix.JoinedMemberIDs(roomID)
	if err != nil {
		return roomFacts{}, err
	}
	people, err := w.Matrix.OtherPeopleInRoom(members)
	if err != nil {
		return roomFacts{}, err
	}
	facts := roomFacts{Name: name, IsDirectMessage: len(people) == 1}
	w.rooms[roomID] = facts
	return facts, nil
}

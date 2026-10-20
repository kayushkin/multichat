package inbound

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"

	"github.com/kayushkin/bus"
	"github.com/kayushkin/bus/messages"
	"github.com/kayushkin/multichat/internal/db"
)

// AutoReactorConsumerName is the auto-reactor's own JetStream durable on
// messaging.inbound, beside the rule dispatcher's, so each sees every message.
const AutoReactorConsumerName = "multichat-auto-reactor"

// ReactionSender posts a reaction as our Matrix account. *matrix.Client is one.
type ReactionSender interface {
	SendReaction(roomID, targetEventID, key string) (string, error)
}

// AutoReactor applies the auto_reactions table to messaging.inbound: for each
// message a person sent, it reacts with every enabled rule's emoji for that
// sender in that room. The sync worker logs the reaction when it comes back
// from Synapse, as it does any of ours.
type AutoReactor struct {
	Database *sql.DB
	Bus      *bus.Client
	Matrix   ReactionSender
}

// Run subscribes and blocks forever.
func (a *AutoReactor) Run() {
	err := a.Bus.JetSubscribe(messages.MessagingInboundSubject, AutoReactorConsumerName, func(subject string, data []byte) {
		var inbound messages.MessagingInbound
		if err := json.Unmarshal(data, &inbound); err != nil {
			log.Printf("auto-reactor: undecodable message on %s: %v", subject, err)
			return
		}
		if err := a.React(inbound); err != nil {
			log.Printf("auto-reactor: %s: %v", inbound.EventID, err)
		}
	})
	if err != nil {
		log.Fatalf("auto-reactor: subscribe %s: %v", messages.MessagingInboundSubject, err)
	}
	log.Printf("auto-reactor: consuming %s as %s", messages.MessagingInboundSubject, AutoReactorConsumerName)
	select {}
}

// React adds every matching rule's reaction to one message sent after the
// rule was made. A reaction the
// homeserver refuses is kept on its rule and does not stop the others.
func (a *AutoReactor) React(inbound messages.MessagingInbound) error {
	rules, err := db.EnabledAutoReactionsFor(a.Database, inbound.SenderUserID, inbound.RoomID)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		// A rule reacts only to what was sent after it was made. A new
		// durable consumer starts by replaying the whole stream, and a new
		// rule must not reach back into weeks of someone's messages.
		if inbound.Timestamp.Unix() < rule.CreatedAt {
			continue
		}
		if err := db.ClaimAutoReaction(a.Database, rule.ID, inbound.EventID); errors.Is(err, db.ErrAlreadyReacted) {
			continue
		} else if err != nil {
			return err
		}
		reactionEventID, reactionErr := a.Matrix.SendReaction(inbound.RoomID, inbound.EventID, rule.ReactionKey)
		if reactionErr != nil {
			log.Printf("auto-reactor: rule %d %s on %s in %s: %v", rule.ID, rule.ReactionKey, inbound.EventID, inbound.RoomID, reactionErr)
		} else {
			log.Printf("auto-reactor: rule %d reacted %s to %s from %s in %s", rule.ID, rule.ReactionKey, inbound.EventID, inbound.SenderUserID, inbound.RoomID)
			// Matrix took it; the bridge may still fail to deliver it, and
			// says so with a message send status that names this event.
			if err := db.RecordAutoReactionSent(a.Database, rule.ID, inbound.EventID, reactionEventID); err != nil {
				return err
			}
		}
		if err := db.RecordAutoReactionOutcome(a.Database, rule.ID, reactionErr); err != nil {
			return err
		}
	}
	return nil
}

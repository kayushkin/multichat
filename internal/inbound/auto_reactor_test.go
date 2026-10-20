package inbound

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kayushkin/bus/messages"
	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/matrix"
)

type sentReaction struct{ room, event, key string }

type fakeReactionSender struct {
	sent   []sentReaction
	refuse error
}

func (f *fakeReactionSender) SendReaction(roomID, targetEventID, key string) (string, error) {
	if f.refuse != nil {
		return "", f.refuse
	}
	f.sent = append(f.sent, sentReaction{roomID, targetEventID, key})
	return "$reaction-to-" + targetEventID, nil
}

const (
	lukasOnDiscord  = "@discord_920000000000000003:chat.example"
	lukasInMessages = "@gmessages_1.719:chat.example"
	groupChat        = "!group:chat.example"
)

func autoReactorForTest(t *testing.T) (*AutoReactor, *fakeReactionSender) {
	t.Helper()
	database, err := db.Initialize(filepath.Join(t.TempDir(), "multichat.db"))
	if err != nil {
		t.Fatal(err)
	}
	sender := &fakeReactionSender{}
	return &AutoReactor{Database: database, Matrix: sender}, sender
}

func message(eventID, roomID, sender string) messages.MessagingInbound {
	return messages.MessagingInbound{EventID: eventID, RoomID: roomID, SenderUserID: sender, Timestamp: time.Now().Add(time.Second)}
}

// TestAutoReactorReactsToThatPersonInTheRoomsTheRuleNames: an every-room rule
// follows Lukas into any channel, a one-room rule stays in its room, and
// nobody else gets a reaction.
func TestAutoReactorReactsToThatPersonInTheRoomsTheRuleNames(t *testing.T) {
	reactor, sender := autoReactorForTest(t)
	if _, err := db.CreateAutoReaction(reactor.Database, lukasOnDiscord, "", "🥛", true); err != nil {
		t.Fatal(err)
	}
	inGroup, err := db.CreateAutoReaction(reactor.Database, lukasInMessages, groupChat, "🥛", true)
	if err != nil {
		t.Fatal(err)
	}

	for _, m := range []messages.MessagingInbound{
		message("$1", "!general:chat.example", lukasOnDiscord),
		message("$2", "!memes:chat.example", lukasOnDiscord),
		message("$3", "!general:chat.example", "@discord_someone_else:chat.example"),
		message("$4", groupChat, lukasInMessages),
		message("$5", "!their-own-chat:chat.example", lukasInMessages),
	} {
		if err := reactor.React(m); err != nil {
			t.Fatal(err)
		}
	}
	want := []sentReaction{{"!general:chat.example", "$1", "🥛"}, {"!memes:chat.example", "$2", "🥛"}, {groupChat, "$4", "🥛"}}
	if len(sender.sent) != len(want) {
		t.Fatalf("sent %v, want %v", sender.sent, want)
	}
	for i := range want {
		if sender.sent[i] != want[i] {
			t.Errorf("reaction %d: %v, want %v", i, sender.sent[i], want[i])
		}
	}
	after, err := db.GetAutoReaction(reactor.Database, inGroup.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.ReactionCount != 1 || after.LastReactedAt == 0 {
		t.Errorf("group rule after one reaction: %+v", after)
	}
}

func TestAutoReactorReactsOnceToARedeliveredMessage(t *testing.T) {
	reactor, sender := autoReactorForTest(t)
	if _, err := db.CreateAutoReaction(reactor.Database, lukasOnDiscord, "", "🥛", true); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := reactor.React(message("$1", "!general:chat.example", lukasOnDiscord)); err != nil {
			t.Fatal(err)
		}
	}
	if len(sender.sent) != 1 {
		t.Errorf("reacted %d times to one message", len(sender.sent))
	}
}

func TestAutoReactorSkipsSwitchedOffRulesAndKeepsRefusals(t *testing.T) {
	reactor, sender := autoReactorForTest(t)
	rule, err := db.CreateAutoReaction(reactor.Database, lukasOnDiscord, "", "🥛", true)
	if err != nil {
		t.Fatal(err)
	}
	sender.refuse = errors.New("M_FORBIDDEN: not in room")
	if err := reactor.React(message("$1", "!general:chat.example", lukasOnDiscord)); err != nil {
		t.Fatal(err)
	}
	after, err := db.GetAutoReaction(reactor.Database, rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(after.LastError, "M_FORBIDDEN") || after.ReactionCount != 0 {
		t.Errorf("after a refusal: %+v", after)
	}

	sender.refuse = nil
	off := false
	if _, err := db.UpdateAutoReaction(reactor.Database, rule.ID, db.AutoReactionChange{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	if err := reactor.React(message("$2", "!general:chat.example", lukasOnDiscord)); err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 0 {
		t.Errorf("a switched-off rule reacted: %v", sender.sent)
	}
}

// TestAutoReactorLeavesMessagesFromBeforeTheRule: a new consumer replays the
// stream from its start, and that history must not get reactions.
func TestAutoReactorLeavesMessagesFromBeforeTheRule(t *testing.T) {
	reactor, sender := autoReactorForTest(t)
	if _, err := db.CreateAutoReaction(reactor.Database, lukasOnDiscord, "", "🥛", true); err != nil {
		t.Fatal(err)
	}
	old := message("$old", "!general:chat.example", lukasOnDiscord)
	old.Timestamp = time.Now().Add(-48 * time.Hour)
	if err := reactor.React(old); err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 0 {
		t.Errorf("reacted to a message from before the rule: %v", sender.sent)
	}
}

// TestABridgeRefusalLandsOnTheRule: Matrix takes the reaction and the rule
// counts it, then mautrix-discord reports that Discord refused it, as it did
// for every 🥛 on Lukas in the Riverside server from 2026-10-02 16:33 UTC. The
// refusal must show on the rule, not leave it looking healthy.
func TestABridgeRefusalLandsOnTheRule(t *testing.T) {
	reactor, _ := autoReactorForTest(t)
	rule, err := db.CreateAutoReaction(reactor.Database, lukasOnDiscord, "", "🥛", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"$1", "$2"} {
		if err := reactor.React(message(id, "!general:chat.example", lukasOnDiscord)); err != nil {
			t.Fatal(err)
		}
	}
	worker := &SyncWorker{Database: reactor.Database}
	status := func(id, reactionEventID, state string) matrix.TimelineEvent {
		content, _ := json.Marshal(map[string]any{
			"m.relates_to": map[string]string{"rel_type": "m.reference", "event_id": reactionEventID},
			"status":       state, "reason": "m.foreign_error",
			"error": `HTTP 403 Forbidden, {"message": "Reaction blocked", "code": 90001}`,
		})
		return matrix.TimelineEvent{EventID: id, Type: matrix.MessageSendStatusEventType, Sender: "@discordbot:chat.example", Content: content}
	}
	for _, e := range []matrix.TimelineEvent{
		status("$s1", "$reaction-to-$2", "SUCCESS"),                // delivered: nothing changes
		status("$s2", "$reaction-to-$1", "FAIL_PERMANENT"),         // refused
		status("$s3", "$reaction-to-$1", "FAIL_PERMANENT"),         // the same report again
		status("$s4", "$someone-elses-reaction", "FAIL_PERMANENT"), // not an auto-reaction
	} {
		if err := worker.logEvent("!general:chat.example", e); err != nil {
			t.Fatalf("%s: %v", e.EventID, err)
		}
	}
	after, err := db.GetAutoReaction(reactor.Database, rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.ReactionCount != 1 {
		t.Errorf("reaction_count = %d, want 1: two sent, one refused", after.ReactionCount)
	}
	if !strings.Contains(after.LastError, "Reaction blocked") || !strings.HasPrefix(after.LastError, "FAIL_PERMANENT") || after.LastErrorAt == 0 {
		t.Errorf("last_error = %q at %d", after.LastError, after.LastErrorAt)
	}
}

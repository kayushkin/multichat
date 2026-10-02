package inbound

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kayushkin/bus/messages"
	"github.com/kayushkin/multichat/internal/db"
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
	return "$reaction", nil
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
	return messages.MessagingInbound{EventID: eventID, RoomID: roomID, SenderUserID: sender}
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

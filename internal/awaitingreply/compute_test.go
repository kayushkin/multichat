package awaitingreply

import (
	"testing"
	"time"

	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/matrix"
)

var now = time.Date(2026, 10, 20, 18, 0, 0, 0, time.UTC)

const (
	me             = "@discord_me:chat.example"
	danaDiscord = "@discord_92000000000000001:chat.example"
	danaTexts   = "@gmessages_1.30:chat.example"
	priyaTexts   = "@gmessages_1.40:chat.example"
	priyaDiscord = "@discord_920000000000000002:chat.example"
	lukas         = "@gmessages_1.41:chat.example"
	stranger       = "@discord_1:chat.example"
	unlinkedTagged = "@meta_9100000008:chat.example"
)

func ago(d time.Duration) time.Time { return now.Add(-d) }

func message(room, sender string, sentAt time.Time, body string) db.LoggedMessage {
	return db.LoggedMessage{EventID: "$" + body, RoomID: room, Platform: "discord", SenderUserID: sender, SenderDisplayName: "name of " + sender, MessageType: "m.text", Body: body, SentAt: sentAt}
}

func baseInputs(messages ...db.LoggedMessage) Inputs {
	return Inputs{
		Settings: db.AwaitingReplySettings{TagIDs: []int64{6}, MaxGroupMemberCount: 5, MaxMessageAgeDays: 30, ClearWhenOpened: true, ClearWhenReplied: true},
		Conversations: map[string]matrix.Conversation{
			"!dana-dm":  {RoomID: "!dana-dm", Name: "D", Platform: "discord", MemberCount: 2, DirectMessagePartnerUserID: danaDiscord},
			"!dana-sms": {RoomID: "!dana-sms", Name: "Dana", Platform: "gmessages", MemberCount: 2, DirectMessagePartnerUserID: danaTexts},
			"!sms-group":   {RoomID: "!sms-group", Name: "Leah, Lukas, Priya", Platform: "gmessages", MemberCount: 4},
			"!big-group":   {RoomID: "!big-group", Name: "Dance team", Platform: "gmessages", MemberCount: 12},
			"!general":     {RoomID: "!general", Name: "#general", Platform: "discord", MemberCount: 3},
			"!tagged-room": {RoomID: "!tagged-room", Name: "Family", Platform: "whatsapp", MemberCount: 5},
			"!arthur-dm":   {RoomID: "!arthur-dm", Name: "Arthur", Platform: "meta", MemberCount: 2, DirectMessagePartnerUserID: unlinkedTagged},
			"!stranger-dm": {RoomID: "!stranger-dm", Name: "Stranger", Platform: "discord", MemberCount: 2, DirectMessagePartnerUserID: stranger},
		},
		TaggedAccounts:     map[string]bool{danaDiscord: true, priyaDiscord: true, unlinkedTagged: true},
		TaggedRooms:        map[string]bool{},
		AccountPersons:     map[string]string{danaDiscord: "person_000097", danaTexts: "person_000097", priyaDiscord: "person_000012", priyaTexts: "person_000012", lukas: "person_000050"},
		PersonNames:        map[string]string{"person_000097": "DanaT", "person_000012": "Priya", "person_000050": "Lukas"},
		Messages:           messages,
		NewestOwnMessageAt: map[string]time.Time{},
		OwnUserIDs:         map[string]bool{me: true},
		RoomClears:         map[string]db.AwaitingReplyRoomClear{},
		Now:                now,
	}
}

func waitingRooms(overview Overview) map[string]WaitingConversation {
	out := map[string]WaitingConversation{}
	for _, waiting := range overview.Waiting {
		out[waiting.RoomID] = waiting
	}
	return out
}

func clearedRooms(overview Overview) map[string]ClearedConversation {
	out := map[string]ClearedConversation{}
	for _, cleared := range overview.Cleared {
		out[cleared.RoomID] = cleared
	}
	return out
}

func TestATaggedAccountCountsEveryAccountOfItsPerson(t *testing.T) {
	counted := CountedAccounts(map[string]bool{danaDiscord: true, unlinkedTagged: true}, map[string]string{danaDiscord: "p1", danaTexts: "p1", lukas: "p2"})
	for _, account := range []string{danaDiscord, danaTexts, unlinkedTagged} {
		if !counted[account] {
			t.Errorf("%s should count", account)
		}
	}
	if counted[lukas] {
		t.Errorf("an untagged person's account counted")
	}
}

func TestATextFromATaggedPersonsOtherAccountWaitsUnderTheirName(t *testing.T) {
	overview := Compute(baseInputs(message("!dana-sms", danaTexts, ago(2*time.Hour), "are you free")))
	waiting, ok := waitingRooms(overview)["!dana-sms"]
	if !ok {
		t.Fatalf("the person's SMS did not wait: %+v", overview)
	}
	if got := waiting.Senders[0]; got.PersonID != "person_000097" || got.DisplayName != "DanaT" {
		t.Errorf("sender = %+v, want the person", got)
	}
	if !waiting.DirectMessage || waiting.UnansweredMessageCount != 1 || !waiting.NewestMessageAt.Equal(ago(2*time.Hour)) {
		t.Errorf("entry = %+v", waiting)
	}
}

func TestAnAccountNoPersonOwnsCountsOnItsOwnTagUnderItsOwnName(t *testing.T) {
	waiting := waitingRooms(Compute(baseInputs(message("!arthur-dm", unlinkedTagged, ago(time.Hour), "hi"))))["!arthur-dm"]
	if got := waiting.Senders; len(got) != 1 || got[0].PersonID != "" || got[0].DisplayName != "name of "+unlinkedTagged {
		t.Errorf("senders = %+v", got)
	}
}

func TestAReplyClearsWhatCameBeforeItAndNotWhatCameAfter(t *testing.T) {
	in := baseInputs(
		message("!dana-dm", danaDiscord, ago(5*time.Hour), "when are you out of office"),
		message("!dana-dm", danaDiscord, ago(time.Hour), "sounded awful lol"),
	)
	in.NewestOwnMessageAt["!dana-dm"] = ago(3 * time.Hour)
	waiting := waitingRooms(Compute(in))["!dana-dm"]
	if waiting.UnansweredMessageCount != 1 || waiting.Messages[0].Body != "sounded awful lol" || !waiting.WaitingSince.Equal(ago(time.Hour)) {
		t.Fatalf("waiting = %+v", waiting)
	}

	in.NewestOwnMessageAt["!dana-dm"] = ago(10 * time.Minute)
	overview := Compute(in)
	if len(overview.Waiting) != 0 {
		t.Fatalf("still waiting after the reply: %+v", overview.Waiting)
	}
	cleared := clearedRooms(overview)["!dana-dm"]
	if cleared.ClearedBy != ClearedByReply || !cleared.ClearedAt.Equal(ago(10*time.Minute)) || cleared.LastMessage.Body != "sounded awful lol" {
		t.Errorf("cleared = %+v", cleared)
	}
}

func TestWithClearWhenRepliedOffAReplyClearsNothing(t *testing.T) {
	in := baseInputs(message("!dana-dm", danaDiscord, ago(time.Hour), "sounded awful lol"))
	in.NewestOwnMessageAt["!dana-dm"] = ago(10 * time.Minute)
	in.Settings.ClearWhenReplied = false
	if _, ok := waitingRooms(Compute(in))["!dana-dm"]; !ok {
		t.Errorf("a reply cleared the room with clear_when_replied off")
	}
}

func TestADiscordServerChannelNeverCountsHoweverFewMembersItShows(t *testing.T) {
	inServer := message("!general", danaDiscord, ago(time.Hour), "Money_is_coming_your_way.mp4")
	inServer.NetworkID = "9300000000000000001"
	if overview := Compute(baseInputs(inServer)); len(overview.Waiting)+len(overview.Cleared) != 0 {
		t.Errorf("a server channel counted: %+v", overview)
	}
}

func TestANetworkOnAnotherPlatformIsNotAServerChannel(t *testing.T) {
	marketplace := message("!arthur-dm", unlinkedTagged, ago(time.Hour), "is it still available")
	marketplace.Platform, marketplace.NetworkID = "meta", "-12"
	if _, ok := waitingRooms(Compute(baseInputs(marketplace)))["!arthur-dm"]; !ok {
		t.Errorf("a Marketplace chat was left out as if it were a Discord server channel")
	}
}

func TestASmallGroupWaitsWhenATaggedPersonWroteAfterYouEvenIfSomeoneElseWroteLast(t *testing.T) {
	in := baseInputs(
		message("!sms-group", priyaTexts, ago(3*time.Hour), "some of the videos are too long to send"),
		message("!sms-group", lukas, ago(2*time.Hour), "are we still doing saturday?"),
	)
	waiting, ok := waitingRooms(Compute(in))["!sms-group"]
	if !ok {
		t.Fatal("the group did not wait")
	}
	if waiting.UnansweredMessageCount != 1 || waiting.Senders[0].DisplayName != "Priya" {
		t.Errorf("only the tagged person's message should count: %+v", waiting)
	}
}

func TestAGroupLargerThanTheLimitNeverCounts(t *testing.T) {
	in := baseInputs(message("!big-group", priyaTexts, ago(time.Hour), "see you there"))
	if overview := Compute(in); len(overview.Waiting) != 0 {
		t.Errorf("a 12-person group counted: %+v", overview.Waiting)
	}
	in.Settings.MaxGroupMemberCount = 12
	if _, ok := waitingRooms(Compute(in))["!big-group"]; !ok {
		t.Errorf("raising the limit to 12 did not let the group count")
	}
}

func TestOpeningClearsUpToWhatWasShownAndANewerMessageBringsTheRoomBack(t *testing.T) {
	in := baseInputs(message("!dana-dm", danaDiscord, ago(time.Hour), "sounded awful lol"))
	in.RoomClears["!dana-dm"] = db.AwaitingReplyRoomClear{RoomID: "!dana-dm", ClearedThrough: ago(time.Hour), ClearedAt: ago(30 * time.Minute), Reason: db.AwaitingReplyClearOpened}
	overview := Compute(in)
	if cleared := clearedRooms(overview)["!dana-dm"]; cleared.ClearedBy != ClearedByOpening || !cleared.ClearedAt.Equal(ago(30*time.Minute)) {
		t.Fatalf("cleared = %+v (waiting %+v)", cleared, overview.Waiting)
	}

	in.Messages = append(in.Messages, message("!dana-dm", danaDiscord, ago(5*time.Minute), "you there?"))
	waiting, ok := waitingRooms(Compute(in))["!dana-dm"]
	if !ok || waiting.UnansweredMessageCount != 1 || waiting.Messages[0].Body != "you there?" {
		t.Errorf("a message after the open did not bring the room back: %+v", waiting)
	}
}

func TestTheClearThatCameFirstIsTheOneReported(t *testing.T) {
	in := baseInputs(message("!dana-dm", danaDiscord, ago(time.Hour), "sounded awful lol"))
	in.RoomClears["!dana-dm"] = db.AwaitingReplyRoomClear{RoomID: "!dana-dm", ClearedThrough: ago(time.Hour), ClearedAt: ago(40 * time.Minute), Reason: db.AwaitingReplyClearDismissed}
	in.NewestOwnMessageAt["!dana-dm"] = ago(20 * time.Minute)
	if cleared := clearedRooms(Compute(in))["!dana-dm"]; cleared.ClearedBy != ClearedByDismissal {
		t.Errorf("dismissed 40 min ago then replied 20 min ago reported as %q", cleared.ClearedBy)
	}
	in.NewestOwnMessageAt["!dana-dm"] = ago(50 * time.Minute)
	if cleared := clearedRooms(Compute(in))["!dana-dm"]; cleared.ClearedBy != ClearedByReply {
		t.Errorf("replied 50 min ago then dismissed 40 min ago reported as %q", cleared.ClearedBy)
	}
}

func TestMessagesOlderThanTheAgeLimitAreLeftOut(t *testing.T) {
	in := baseInputs(message("!arthur-dm", unlinkedTagged, ago(31*24*time.Hour), "God damn, we are in a timeline"))
	if overview := Compute(in); len(overview.Waiting)+len(overview.Cleared) != 0 {
		t.Errorf("a 31-day-old message counted: %+v", overview)
	}
}

func TestATaggedRoomCountsEveryoneButYou(t *testing.T) {
	in := baseInputs(
		message("!tagged-room", stranger, ago(2*time.Hour), "dinner sunday?"),
		message("!tagged-room", me, ago(time.Hour), "my own words"),
	)
	in.TaggedRooms["!tagged-room"] = true
	waiting, ok := waitingRooms(Compute(in))["!tagged-room"]
	if !ok || !waiting.CountedBecauseRoomIsTagged || waiting.UnansweredMessageCount != 1 || waiting.Messages[0].Body != "dinner sunday?" {
		t.Errorf("waiting = %+v", waiting)
	}
	if _, ok := waitingRooms(Compute(baseInputs(message("!stranger-dm", stranger, ago(time.Hour), "hey"))))["!stranger-dm"]; ok {
		t.Errorf("an untagged stranger in an untagged room counted")
	}
}

func TestATaggedRoomCountsWhateverItsSizeAndEvenAsAServerChannel(t *testing.T) {
	inBigGroup := message("!big-group", stranger, ago(time.Hour), "who is bringing speakers")
	inServer := message("!general", stranger, ago(time.Hour), "event tonight")
	inServer.NetworkID = "9300000000000000001"
	in := baseInputs(inBigGroup, inServer)
	in.TaggedRooms["!big-group"], in.TaggedRooms["!general"] = true, true
	waiting := waitingRooms(Compute(in))
	if _, ok := waiting["!big-group"]; !ok {
		t.Errorf("a tagged 12-person group was dropped by the size rule")
	}
	if _, ok := waiting["!general"]; !ok {
		t.Errorf("a tagged server channel was dropped by the server channel rule")
	}
}

func TestARoomClearedMoreThanAWeekAgoLeavesTheClearedList(t *testing.T) {
	in := baseInputs(message("!dana-dm", danaDiscord, ago(10*24*time.Hour), "old"))
	in.NewestOwnMessageAt["!dana-dm"] = ago(8 * 24 * time.Hour)
	if overview := Compute(in); len(overview.Cleared) != 0 {
		t.Errorf("cleared 8 days ago still listed: %+v", overview.Cleared)
	}
}

func TestARoomMissingFromTheConversationListIsLeftOut(t *testing.T) {
	if overview := Compute(baseInputs(message("!left-room", danaDiscord, ago(time.Hour), "hi"))); len(overview.Waiting) != 0 {
		t.Errorf("a room we left counted: %+v", overview.Waiting)
	}
}

func TestPreviewKeepsTheNewestMessagesOldestFirst(t *testing.T) {
	var messages []db.LoggedMessage
	for i := 0; i < PreviewMessageLimit+3; i++ {
		messages = append(messages, message("!dana-dm", danaDiscord, ago(time.Duration(20-i)*time.Minute), string(rune('a'+i))))
	}
	waiting := waitingRooms(Compute(baseInputs(messages...)))["!dana-dm"]
	if waiting.UnansweredMessageCount != PreviewMessageLimit+3 || len(waiting.Messages) != PreviewMessageLimit || waiting.Messages[0].Body != "d" || waiting.Messages[PreviewMessageLimit-1].Body != "h" {
		t.Errorf("preview = %+v", waiting.Messages)
	}
}

func TestWaitingIsNewestFirst(t *testing.T) {
	overview := Compute(baseInputs(
		message("!dana-dm", danaDiscord, ago(3*time.Hour), "older"),
		message("!arthur-dm", unlinkedTagged, ago(time.Hour), "newer"),
	))
	if len(overview.Waiting) != 2 || overview.Waiting[0].RoomID != "!arthur-dm" {
		t.Errorf("order = %+v", overview.Waiting)
	}
}

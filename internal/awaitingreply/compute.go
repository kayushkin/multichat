// Package awaitingreply works out which conversations hold a message the
// operator has not answered from someone they tagged.
//
// It counts people, not accounts. In a direct message or a small group, a
// person (a people-store id) counts when the settings count everyone
// people-store knows, or when any of their chat accounts carries one of the
// chosen multichat tags; then every account of theirs counts. An account no
// person owns counts only on its own tag. In a bigger group only a person
// with one of the chosen people-store tags counts (work colleagues, say).
// A person with one of the never-count people-store tags never counts.
// A room carrying a chosen multichat tag counts every message in it but the
// operator's, whatever its size and even as a Discord server channel.
package awaitingreply

import (
	"sort"
	"time"

	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/matrix"
)

// ClearedListWindow is how far back the cleared list reaches.
const ClearedListWindow = 7 * 24 * time.Hour

// The reasons a room the operator opened or dismissed gives for clearing.
const (
	ClearedByOpening   = db.AwaitingReplyClearOpened
	ClearedByDismissal = db.AwaitingReplyClearDismissed
)

// Inputs is everything Compute reads, gathered by the caller from the stores
// that own each part.
type Inputs struct {
	Settings db.AwaitingReplySettings
	// Conversations is multichat's conversation list, keyed by room id.
	Conversations map[string]matrix.Conversation
	// TaggedAccounts carry a chosen tag; TaggedRooms carry one themselves.
	TaggedAccounts map[string]bool
	TaggedRooms    map[string]bool
	// AccountPersons maps an account to the person who owns it, for every
	// person who is not deleted.
	AccountPersons map[string]string
	// PersonTags are each person's people-store tags.
	PersonTags map[string][]string
	// PersonNames maps a person id to their display name.
	PersonNames map[string]string
	// Messages are the logged messages from counted accounts or in tagged
	// rooms, sent within the age limit, oldest first.
	Messages []db.LoggedMessage
	// NewestOwnMessageAt is when the operator last wrote in each room.
	NewestOwnMessageAt map[string]time.Time
	// OwnUserIDs are the operator's own accounts.
	OwnUserIDs map[string]bool
	// RoomClears are the operator's opened and dismissed marks.
	RoomClears map[string]db.AwaitingReplyRoomClear
	Now        time.Time
}

// CountedAccounts returns every account whose messages count: each tagged
// account, and every account of a person who owns a tagged one.
func CountedAccounts(taggedAccounts map[string]bool, accountPersons map[string]string) map[string]bool {
	countedPersons := map[string]bool{}
	for account := range taggedAccounts {
		if person := accountPersons[account]; person != "" {
			countedPersons[person] = true
		}
	}
	counted := map[string]bool{}
	for account := range taggedAccounts {
		counted[account] = true
	}
	for account, person := range accountPersons {
		if countedPersons[person] {
			counted[account] = true
		}
	}
	return counted
}

// CountedAccountSets returns the accounts whose messages count in a direct
// message or small group, and the ones that count in a group of any size.
func (in Inputs) CountedAccountSets() (smallRoomAccounts, anySizeAccounts map[string]bool) {
	smallRoomAccounts = CountedAccounts(in.TaggedAccounts, in.AccountPersons)
	anySizeTags := map[string]bool{}
	for _, tag := range in.Settings.AnySizePersonTags {
		anySizeTags[tag] = true
	}
	neverCountTags := map[string]bool{}
	for _, tag := range in.Settings.NeverCountPersonTags {
		neverCountTags[tag] = true
	}
	anySizeAccounts = map[string]bool{}
	for account, person := range in.AccountPersons {
		if in.Settings.CountEveryKnownPerson {
			smallRoomAccounts[account] = true
		}
		for _, tag := range in.PersonTags[person] {
			if anySizeTags[tag] {
				anySizeAccounts[account] = true
				smallRoomAccounts[account] = true
			}
		}
	}
	for account, person := range in.AccountPersons {
		for _, tag := range in.PersonTags[person] {
			if neverCountTags[tag] {
				delete(smallRoomAccounts, account)
				delete(anySizeAccounts, account)
			}
		}
	}
	return smallRoomAccounts, anySizeAccounts
}

// IsDiscordServerChannel reports whether a logged message came from a channel
// of a Discord server. Only there does the bridge set a network: a direct or
// group message on Discord has none. Other platforms set one for things that
// are not public channels (a WhatsApp community's groups, a Marketplace
// chat), so this is a Discord rule only.
func IsDiscordServerChannel(message db.LoggedMessage) bool {
	return message.Platform == "discord" && message.NetworkID != ""
}

// Compute works out the waiting and cleared lists.
func Compute(in Inputs) Overview {
	smallRoomAccounts, anySizeAccounts := in.CountedAccountSets()
	oldestCounted := in.Now.Add(-time.Duration(in.Settings.MaxMessageAgeDays) * 24 * time.Hour)

	serverChannels := map[string]bool{}
	for _, message := range in.Messages {
		if IsDiscordServerChannel(message) {
			serverChannels[message.RoomID] = true
		}
	}
	byRoom := map[string][]db.LoggedMessage{}
	for _, message := range in.Messages {
		if in.OwnUserIDs[message.SenderUserID] || !message.SentAt.After(oldestCounted) {
			continue
		}
		listed, ok := in.Conversations[message.RoomID]
		if !ok {
			continue
		}
		// A room the operator tagged always counts. The size and server
		// channel rules decide only where a person's messages count.
		counts := in.TaggedRooms[message.RoomID]
		if !counts && !serverChannels[message.RoomID] {
			if listed.MemberCount <= in.Settings.MaxGroupMemberCount {
				counts = smallRoomAccounts[message.SenderUserID]
			} else {
				counts = anySizeAccounts[message.SenderUserID]
			}
		}
		if counts {
			byRoom[message.RoomID] = append(byRoom[message.RoomID], message)
		}
	}

	overview := Overview{Settings: in.Settings, Waiting: []WaitingConversation{}, Cleared: []ClearedConversation{}, ComputedAt: in.Now}
	for roomID, messages := range byRoom {
		listed := in.Conversations[roomID]
		conversation := Conversation{
			RoomID:                     roomID,
			Name:                       listed.Name,
			Platform:                   listed.Platform,
			MemberCount:                listed.MemberCount,
			DirectMessage:              listed.DirectMessagePartnerUserID != "",
			CountedBecauseRoomIsTagged: in.TaggedRooms[roomID],
		}

		var answeredThrough time.Time
		if in.Settings.ClearWhenReplied {
			answeredThrough = in.NewestOwnMessageAt[roomID]
		}
		clear, hasClear := in.RoomClears[roomID]
		seenThrough := answeredThrough
		if hasClear && clear.ClearedThrough.After(seenThrough) {
			seenThrough = clear.ClearedThrough
		}

		var unanswered []db.LoggedMessage
		for _, message := range messages {
			if message.SentAt.After(seenThrough) {
				unanswered = append(unanswered, message)
			}
		}
		if len(unanswered) > 0 {
			overview.Waiting = append(overview.Waiting, in.waiting(conversation, unanswered))
			continue
		}

		last := messages[len(messages)-1]
		clearedBy, clearedAt := firstClearOf(last.SentAt, answeredThrough, clear, hasClear)
		if in.Now.Sub(clearedAt) > ClearedListWindow {
			continue
		}
		overview.Cleared = append(overview.Cleared, ClearedConversation{
			Conversation: conversation,
			LastMessage:  in.preview(last),
			ClearedBy:    clearedBy,
			ClearedAt:    clearedAt,
		})
	}
	sort.Slice(overview.Waiting, func(i, j int) bool {
		return overview.Waiting[i].NewestMessageAt.After(overview.Waiting[j].NewestMessageAt)
	})
	sort.Slice(overview.Cleared, func(i, j int) bool {
		return overview.Cleared[i].ClearedAt.After(overview.Cleared[j].ClearedAt)
	})
	return overview
}

// firstClearOf says what first covered a message sent at sentAt: the
// operator's reply at answeredThrough, or their open or dismissal of the
// room. Compute calls it only for a room that is not waiting, so one did.
func firstClearOf(sentAt, answeredThrough time.Time, clear db.AwaitingReplyRoomClear, hasClear bool) (string, time.Time) {
	repliedAfter := !answeredThrough.IsZero() && !answeredThrough.Before(sentAt)
	clearedAfter := hasClear && !clear.ClearedThrough.Before(sentAt)
	if clearedAfter && (!repliedAfter || clear.ClearedAt.Before(answeredThrough)) {
		return clear.Reason, clear.ClearedAt
	}
	return ClearedByReply, answeredThrough
}

func (in Inputs) waiting(conversation Conversation, unanswered []db.LoggedMessage) WaitingConversation {
	entry := WaitingConversation{
		Conversation:           conversation,
		Senders:                []Sender{},
		UnansweredMessageCount: len(unanswered),
		Messages:               []PreviewMessage{},
		WaitingSince:           unanswered[0].SentAt,
		NewestMessageAt:        unanswered[len(unanswered)-1].SentAt,
	}
	seen := map[string]bool{}
	for _, message := range unanswered {
		sender := in.sender(message)
		key := sender.PersonID
		if key == "" {
			key = sender.UserID
		}
		if !seen[key] {
			seen[key] = true
			entry.Senders = append(entry.Senders, sender)
		}
	}
	start := len(unanswered) - PreviewMessageLimit
	if start < 0 {
		start = 0
	}
	for _, message := range unanswered[start:] {
		entry.Messages = append(entry.Messages, in.preview(message))
	}
	return entry
}

func (in Inputs) sender(message db.LoggedMessage) Sender {
	sender := Sender{PersonID: in.AccountPersons[message.SenderUserID], UserID: message.SenderUserID}
	if sender.PersonID != "" {
		sender.DisplayName = in.PersonNames[sender.PersonID]
	} else {
		sender.DisplayName = message.SenderDisplayName
	}
	return sender
}

func (in Inputs) preview(message db.LoggedMessage) PreviewMessage {
	return PreviewMessage{
		EventID:       message.EventID,
		Sender:        in.sender(message),
		MessageType:   message.MessageType,
		Body:          message.Body,
		MediaURL:      message.MediaURL,
		MediaMimetype: message.MediaMimetype,
		SentAt:        message.SentAt,
	}
}

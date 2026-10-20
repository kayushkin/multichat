package awaitingreply

import (
	"time"

	"github.com/kayushkin/multichat/internal/db"
)

// PreviewMessageLimit is how many of a room's unanswered messages are sent
// with it, newest last.
const PreviewMessageLimit = 5

// ClearedByReply says the operator's own message cleared a room. A room the
// operator opened or dismissed says so with db.AwaitingReplyClearReasons.
const ClearedByReply = "replied"

// Overview is the answer of GET /awaiting-reply.
type Overview struct {
	Settings db.AwaitingReplySettings `json:"settings"`
	// Waiting is every room with an unanswered message, newest first.
	Waiting []WaitingConversation `json:"waiting"`
	// Cleared is every room that stopped waiting within ClearedListWindow,
	// newest first.
	Cleared []ClearedConversation `json:"cleared"`
	// ComputedAt is when the list was worked out.
	ComputedAt time.Time `json:"computed_at"`
}

// Sender is who wrote a counted message: the person who owns the account,
// or the account alone when no person owns it.
type Sender struct {
	// PersonID is the people-store id, "" when no person owns the account.
	PersonID string `json:"person_id"`
	// DisplayName is the person's name when a person owns the account, and
	// the account's own name in the room when none does.
	DisplayName string `json:"display_name"`
	UserID      string `json:"user_id"`
}

// PreviewMessage is one unanswered message.
type PreviewMessage struct {
	EventID       string    `json:"event_id"`
	Sender        Sender    `json:"sender"`
	MessageType   string    `json:"message_type"`
	Body          string    `json:"body"`
	MediaURL      string    `json:"media_url"`
	MediaMimetype string    `json:"media_mimetype"`
	SentAt        time.Time `json:"sent_at"`
}

// Conversation is the room a waiting or cleared entry is about.
type Conversation struct {
	RoomID      string `json:"room_id"`
	Name        string `json:"name"`
	Platform    string `json:"platform"`
	MemberCount int    `json:"member_count"`
	// DirectMessage is true in a one-to-one chat.
	DirectMessage bool `json:"direct_message"`
	// CountedBecauseRoomIsTagged is true when the room itself carries a
	// chosen tag, so everyone's messages count in it.
	CountedBecauseRoomIsTagged bool `json:"counted_because_room_is_tagged"`
}

// WaitingConversation is a room with messages the operator has not answered.
type WaitingConversation struct {
	Conversation `tstype:",extends"`
	// Senders are the counted people who wrote since the operator's last
	// word, in the order they first wrote.
	Senders []Sender `json:"senders"`
	// UnansweredMessageCount counts every unanswered counted message.
	UnansweredMessageCount int `json:"unanswered_message_count"`
	// Messages are the newest PreviewMessageLimit of them, oldest first.
	Messages []PreviewMessage `json:"messages"`
	// WaitingSince is when the oldest unanswered message was sent.
	WaitingSince time.Time `json:"waiting_since"`
	// NewestMessageAt is when the newest unanswered message was sent; a
	// client passes it back as cleared_through when the operator opens the room.
	NewestMessageAt time.Time `json:"newest_message_at"`
}

// ClearedConversation is a room that stopped waiting.
type ClearedConversation struct {
	Conversation `tstype:",extends"`
	// LastMessage is the newest counted message, the one that was cleared.
	LastMessage PreviewMessage `json:"last_message"`
	// ClearedBy is ClearedByReply or one of db.AwaitingReplyClearReasons.
	ClearedBy string    `json:"cleared_by"`
	ClearedAt time.Time `json:"cleared_at"`
}

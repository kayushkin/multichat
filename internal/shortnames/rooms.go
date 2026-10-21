package shortnames

import (
	"github.com/kayushkin/multichat/internal/matrix"
)

// Kinds of room, as the model is told them.
const (
	KindDirectMessage  = "direct_message"
	KindGroup          = "group"
	KindDiscordChannel = "discord_channel"
)

// maximumMemberNamesPerRoom caps the member names one room puts in the
// prompt. A Discord channel has hundreds; past a dozen they say nothing more
// about what the room is, and member_count still gives the size.
const maximumMemberNamesPerRoom = 15

// DiscordChannel is what the Discord bridge knows of a server channel's room.
type DiscordChannel struct {
	ServerID     string
	ServerName   string
	ChannelName  string
	CategoryName string
}

// Room is the facts about one Matrix room the model names from.
type Room struct {
	RoomID   string
	Name     string
	Platform string
	Kind     string
	// DirectMessagePartnerName is the other person's people-store name when
	// their account is linked to a person, else the name their account shows;
	// "" outside a direct message.
	DirectMessagePartnerName string
	MemberNames              []string
	MemberCount              int
	LastActivity             int64
	// Discord is set on a Discord server channel only.
	Discord *DiscordChannel
	// ExcludedFromTimeline is the bridge's own mark on a room that is no chat
	// (WhatsApp's status broadcast). Such a room is never named.
	ExcludedFromTimeline bool
}

// RoomsFromConversations turns GET /conversations rows into the facts the
// model sees. personNameByUserID is people-store's name for each bridge
// account linked to a person; discordChannelByRoomID is the Discord bridge's
// server channels keyed by Matrix room id.
//
// A room is a direct message when its bridge names a partner, or when it is
// not a server channel and holds one other person (Telegram does not mark its
// direct messages).
func RoomsFromConversations(conversations []matrix.Conversation, personNameByUserID map[string]string, discordChannelByRoomID map[string]DiscordChannel) []Room {
	nameOf := func(member matrix.ConversationMember) string {
		if name := personNameByUserID[member.UserID]; name != "" {
			return name
		}
		return member.DisplayName
	}
	rooms := make([]Room, 0, len(conversations))
	for _, conversation := range conversations {
		room := Room{
			RoomID:       conversation.RoomID,
			Name:         conversation.Name,
			Platform:     conversation.Platform,
			MemberCount:  conversation.MemberCount,
			LastActivity: conversation.LastActivity,
		}
		for _, member := range conversation.Members {
			if len(room.MemberNames) == maximumMemberNamesPerRoom {
				break
			}
			if name := nameOf(member); name != "" {
				room.MemberNames = append(room.MemberNames, name)
			}
		}
		if channel, isChannel := discordChannelByRoomID[conversation.RoomID]; isChannel {
			room.Kind = KindDiscordChannel
			room.Discord = &channel
		} else if conversation.DirectMessagePartnerUserID != "" || len(conversation.Members) == 1 {
			room.Kind = KindDirectMessage
			partnerUserID := conversation.DirectMessagePartnerUserID
			for _, member := range conversation.Members {
				if member.UserID == partnerUserID || (partnerUserID == "" && len(conversation.Members) == 1) {
					room.DirectMessagePartnerName = nameOf(member)
				}
			}
			if room.DirectMessagePartnerName == "" {
				room.DirectMessagePartnerName = personNameByUserID[partnerUserID]
			}
		} else {
			room.Kind = KindGroup
		}
		rooms = append(rooms, room)
	}
	return rooms
}

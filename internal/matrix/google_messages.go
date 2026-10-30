package matrix

import (
	"encoding/base64"
	"sort"
	"strings"
)

// Google Messages reaches multichat by two bridges: mautrix-gmessages
// (platform gmessages) and gmessages-web-bridge (platform gmessagesweb). Each
// makes its own room per Google conversation, so a conversation both carried
// is two rooms. Both name Google's conversation id in their m.bridge channel:
// mautrix as "<login prefix>.<conversation id>", the web bridge as the token
// in the web app's address, base64url protobuf whose field 2 is the id.

// GoogleMessagesConversationID is Google's conversation id behind a Google
// Messages room's m.bridge channel id, "" for any other platform or a channel
// that holds none.
func GoogleMessagesConversationID(platform, channelID string) string {
	switch platform {
	case "gmessages":
		if _, id, found := strings.Cut(channelID, "."); found {
			return id
		}
	case "gmessagesweb":
		return protobufField2(channelID)
	}
	return ""
}

// protobufField2 reads field 2 of a base64url protobuf message made of
// length-delimited fields shorter than 128 bytes, "" when the token is not one.
func protobufField2(token string) string {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(token, "="))
	if err != nil {
		return ""
	}
	field2 := ""
	for index := 0; index < len(raw); {
		key := raw[index]
		index++
		if key&7 != 2 || key&0x80 != 0 || index >= len(raw) || raw[index]&0x80 != 0 {
			return ""
		}
		length := int(raw[index])
		index++
		if index+length > len(raw) {
			return ""
		}
		if key>>3 == 2 {
			field2 = string(raw[index : index+length])
		}
		index += length
	}
	return field2
}

// GoogleMessagesTwins groups the rooms that carry one Google conversation, by
// its id, keeping only ids carried by two or more rooms. Each group's rooms
// are sorted by room id.
func GoogleMessagesTwins(conversations []Conversation) map[string][]string {
	rooms := map[string][]string{}
	for _, conversation := range conversations {
		if id := conversation.GoogleMessagesConversationID; id != "" {
			rooms[id] = append(rooms[id], conversation.RoomID)
		}
	}
	for id, roomIDs := range rooms {
		if len(roomIDs) < 2 {
			delete(rooms, id)
			continue
		}
		sort.Strings(roomIDs)
	}
	return rooms
}

// nameGoogleMessagesGroups names each Google Messages group after the other
// people in it when Google's own name is its generated list of names (it
// holds ", "). Google's list is of the contact names it shows for each
// participant, and it lists the user's own number among them whenever
// Google takes it for someone else's (a second participant id of the user's,
// which the bridge does not mark as theirs), so the name can show the user
// as a member and leave someone out. The members are read from whichever
// room carrying the conversation lists the most, since the web bridge's room
// holds only those who have written. A custom name ("Book club") is kept,
// and so is Google's when fewer than two other people are known.
func nameGoogleMessagesGroups(conversations []Conversation) {
	fullest := map[string]int{}
	for index, conversation := range conversations {
		id := conversation.GoogleMessagesConversationID
		if id == "" || conversation.DirectMessagePartnerUserID != "" {
			continue
		}
		best, seen := fullest[id]
		if !seen || len(conversation.Members) > len(conversations[best].Members) ||
			(len(conversation.Members) == len(conversations[best].Members) && conversation.RoomID < conversations[best].RoomID) {
			fullest[id] = index
		}
	}
	for index := range conversations {
		conversation := &conversations[index]
		best, known := fullest[conversation.GoogleMessagesConversationID]
		if !known || conversation.DirectMessagePartnerUserID != "" || !strings.Contains(conversation.Name, ", ") {
			continue
		}
		var names []string
		for _, member := range conversations[best].Members {
			if member.DisplayName != "" {
				names = append(names, member.DisplayName)
			}
		}
		if len(names) < 2 {
			continue
		}
		sort.Strings(names)
		conversation.Name = strings.Join(names, ", ")
		conversation.NamedAfterMembers = true
	}
}

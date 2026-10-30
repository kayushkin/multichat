package matrix

import (
	"encoding/base64"
	"reflect"
	"testing"
)

func TestGoogleMessagesConversationIDReadsBothBridges(t *testing.T) {
	webToken := base64.RawURLEncoding.EncodeToString([]byte{0x0a, 0x02, 'x', 'y', 0x12, 0x02, '3', '9'})
	cases := []struct{ platform, channel, want string }{
		{"gmessages", "1.39", "39"},
		{"gmessagesweb", webToken, "39"},
		{"gmessagesweb", "self-test", ""},
		{"discord", "1.39", ""},
	}
	for _, c := range cases {
		if got := GoogleMessagesConversationID(c.platform, c.channel); got != c.want {
			t.Errorf("%s %q: %q, want %q", c.platform, c.channel, got, c.want)
		}
	}
}

func TestGoogleMessagesTwinsGroupsRoomsOfOneConversation(t *testing.T) {
	twins := GoogleMessagesTwins([]Conversation{
		{RoomID: "!b:x", GoogleMessagesConversationID: "39"},
		{RoomID: "!a:x", GoogleMessagesConversationID: "39"},
		{RoomID: "!c:x", GoogleMessagesConversationID: "40"},
		{RoomID: "!d:x"},
	})
	if !reflect.DeepEqual(twins, map[string][]string{"39": {"!a:x", "!b:x"}}) {
		t.Fatalf("twins %v", twins)
	}
}

func TestNameGoogleMessagesGroupsListsTheOtherPeople(t *testing.T) {
	carol := ConversationMember{UserID: "@gmessages_1.5:x", DisplayName: "Carol"}
	dave := ConversationMember{UserID: "@gmessages_1.6:x", DisplayName: "Dave"}
	conversations := []Conversation{
		// Google's list names the user ("Me Self") and leaves Dave out.
		{RoomID: "!bridge:x", Name: "Carol, Me Self", GoogleMessagesConversationID: "22", Members: []ConversationMember{carol, dave}},
		// The web room knows only Carol, who wrote.
		{RoomID: "!web:x", Name: "Carol, Me Self", GoogleMessagesConversationID: "22", Members: []ConversationMember{{UserID: "@gmessagesweb_c:x", DisplayName: "Carol"}}},
		{RoomID: "!custom:x", Name: "Book club", GoogleMessagesConversationID: "23", Members: []ConversationMember{carol, dave}},
		{RoomID: "!lonely:x", Name: "Carol, Erin", GoogleMessagesConversationID: "24", Members: []ConversationMember{carol}},
		{RoomID: "!direct:x", Name: "Carol, Dave", GoogleMessagesConversationID: "25", Members: []ConversationMember{carol}, DirectMessagePartnerUserID: carol.UserID},
	}
	nameGoogleMessagesGroups(conversations)
	want := []string{"Carol, Dave", "Carol, Dave", "Book club", "Carol, Erin", "Carol, Dave"}
	for index, conversation := range conversations {
		if conversation.Name != want[index] {
			t.Errorf("%s named %q, want %q", conversation.RoomID, conversation.Name, want[index])
		}
	}
	if !conversations[1].NamedAfterMembers || conversations[2].NamedAfterMembers {
		t.Errorf("NamedAfterMembers: %v %v", conversations[1].NamedAfterMembers, conversations[2].NamedAfterMembers)
	}
}

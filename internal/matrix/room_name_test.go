package matrix

import "testing"

func TestRoomNameFromMembers(t *testing.T) {
	spacefan := ConversationMember{UserID: "@discord_4:chat.kayushkin.com", DisplayName: "Spacefan"}
	mara := ConversationMember{UserID: "@meta_1:chat.kayushkin.com", DisplayName: "Mara Rowan"}
	vera := ConversationMember{UserID: "@meta_2:chat.kayushkin.com", DisplayName: "Vera Ivanova"}
	nameless := ConversationMember{UserID: "@meta_3:chat.kayushkin.com"}
	cases := []struct {
		members []ConversationMember
		want    string
	}{
		{[]ConversationMember{spacefan}, "Spacefan"},
		{[]ConversationMember{mara, vera}, "Mara Rowan, Vera Ivanova"},
		{[]ConversationMember{nameless, vera}, "Vera Ivanova"},
		{nil, ""},
	}
	for _, c := range cases {
		if got := roomNameFromMembers(c.members); got != c.want {
			t.Errorf("roomNameFromMembers(%v) = %q, want %q", c.members, got, c.want)
		}
	}
}

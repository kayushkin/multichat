package matrix

import "testing"

func TestRoomNameFromMembers(t *testing.T) {
	displayNames := map[string]string{
		"@meta_1:chat.kayushkin.com":    "Mara Rowan (Meta)",
		"@meta_2:chat.kayushkin.com":    "Vera Ivanova",
		"@meta_3:chat.kayushkin.com":    "",
		"@discord_4:chat.kayushkin.com": "Spacefan",
	}
	cases := []struct {
		memberIDs []string
		want      string
	}{
		{[]string{"@discord_4:chat.kayushkin.com"}, "Spacefan"},
		{[]string{"@meta_1:chat.kayushkin.com", "@meta_2:chat.kayushkin.com"}, "Mara Rowan, Vera Ivanova"},
		{[]string{"@meta_3:chat.kayushkin.com", "@meta_2:chat.kayushkin.com"}, "Vera Ivanova"},
		{nil, ""},
	}
	for _, c := range cases {
		if got := roomNameFromMembers(c.memberIDs, displayNames); got != c.want {
			t.Errorf("roomNameFromMembers(%v) = %q, want %q", c.memberIDs, got, c.want)
		}
	}
}

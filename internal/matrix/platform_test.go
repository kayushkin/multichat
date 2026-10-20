package matrix

import "testing"

func TestPlatformOfUserIDReadsEveryBridgePuppetPrefix(t *testing.T) {
	for userID, want := range map[string]string{
		"@whatsapp_19545550100:chat.kayushkin.com":       "whatsapp",
		"@telegram_9000000001:chat.kayushkin.com":        "telegram",
		"@signal_19545550100:chat.kayushkin.com":         "signal",
		"@meta_910000000000006:chat.kayushkin.com":       "meta",
		"@discord_920000000000000004:chat.kayushkin.com": "discord",
		"@slack_t0123-u0456:chat.kayushkin.com":          "slack",
		"@gmessages_1.2:chat.kayushkin.com":              "gmessages",
		"@gmessagesweb_0a1b2c:chat.kayushkin.com":        "gmessagesweb",
		"@gmessageswebbot:chat.kayushkin.com":            "",
		"@gmessagesbot:chat.kayushkin.com":               "",
		"@discordbot:chat.kayushkin.com":                 "",
		"@slackbot:chat.kayushkin.com":                   "",
		"@admin:chat.kayushkin.com":                      "",
		"@multichat-test:chat.kayushkin.com":             "",
	} {
		if got := PlatformOfUserID(userID); got != want {
			t.Errorf("PlatformOfUserID(%q) = %q, want %q", userID, got, want)
		}
	}
}

func TestRedactedEventIDReadsBothRoomVersions(t *testing.T) {
	old := TimelineEvent{EventID: "$r", Redacts: "$target", Content: []byte(`{}`)}
	if got, err := old.RedactedEventID(); err != nil || got != "$target" {
		t.Fatalf("top-level redacts: %q %v", got, err)
	}
	v11 := TimelineEvent{EventID: "$r", Content: []byte(`{"redacts":"$target"}`)}
	if got, err := v11.RedactedEventID(); err != nil || got != "$target" {
		t.Fatalf("content redacts: %q %v", got, err)
	}
	if _, err := (TimelineEvent{EventID: "$r", Content: []byte(`{}`)}).RedactedEventID(); err == nil {
		t.Fatal("a redaction naming no event must be an error")
	}
}

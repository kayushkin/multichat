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

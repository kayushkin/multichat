package matrix

import "strings"

// KnownPlatforms is every platform a bridge puppet can come in through, in
// the order the mautrix bridges were added. It is the served vocabulary for
// anything that filters by platform (inbound rules, the MCP contact search),
// so nothing builds a filter from observed values.
var KnownPlatforms = []string{"whatsapp", "telegram", "signal", "meta", "discord", "slack"}

// PlatformOfUserID reports which bridge a Matrix user id belongs to, from the
// puppet prefix the bridge stamps on its ghosts (@whatsapp_…, @telegram_…).
// It returns "" for anything that is not a bridge puppet: the bridge bots
// themselves (@whatsappbot, @telegrambot), @admin, and any plain Matrix user.
func PlatformOfUserID(userID string) string {
	localpart := strings.SplitN(userID, ":", 2)[0]
	for _, platform := range KnownPlatforms {
		if strings.HasPrefix(localpart, "@"+platform+"_") {
			return platform
		}
	}
	return ""
}

// IsBridgeBot reports whether a Matrix user id is one of the bridge service
// bots (@whatsappbot:…, @telegrambot:…), which sit in every portal room and
// are never a person.
func IsBridgeBot(userID string) bool {
	localpart := strings.SplitN(userID, ":", 2)[0]
	return strings.HasSuffix(localpart, "bot")
}

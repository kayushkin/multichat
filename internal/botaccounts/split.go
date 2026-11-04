package botaccounts

import "strings"

// DiscordMessageLimit is the most characters Discord takes in one message.
const DiscordMessageLimit = 2000

// SplitForDiscord cuts body into messages of at most limit characters,
// breaking at a blank line where it can, then at a line end, then at a space,
// and only inside a word when a word is longer than the limit. Nothing is
// dropped except the whitespace at a break.
func SplitForDiscord(body string, limit int) []string {
	body = strings.TrimSpace(body)
	var parts []string
	for len([]rune(body)) > limit {
		runes := []rune(body)
		window := string(runes[:limit])
		cut := -1
		for _, separator := range []string{"\n\n", "\n", " "} {
			if index := strings.LastIndex(window, separator); index > 0 {
				cut = index
				break
			}
		}
		if cut < 0 {
			cut = len(window)
		}
		parts = append(parts, strings.TrimSpace(body[:cut]))
		body = strings.TrimSpace(body[cut:])
	}
	if body != "" {
		parts = append(parts, body)
	}
	return parts
}

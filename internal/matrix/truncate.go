package matrix

import "unicode/utf8"

// truncateAtRuneBoundary returns the longest prefix of s that is no longer than
// maxBytes and does not end part-way through a multi-byte UTF-8 sequence.
//
// Cutting a Go string at a fixed byte offset splits whatever rune straddles that
// offset, and the result is not valid UTF-8. Nothing reports it: encoding/json
// substitutes U+FFFD rather than failing, so the reader sees a replacement
// character and no error is raised anywhere along the way. Any code that cuts
// bridged chat text down to a byte budget has to walk the cut back to a
// boundary first — every emoji is four bytes, and emoji in chat is the normal
// case rather than an edge case.
//
// The walk-back costs at most three byte comparisons and allocates nothing,
// which is why it is preferred here over converting to []rune.
func truncateAtRuneBoundary(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	// s[cut] is the first byte past the prefix. While it is a continuation
	// byte, a rune straddles the cut, so move the cut earlier.
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// lastMessagePreviewBytes is the byte budget for a conversation's last-message
// preview, before the ellipsis is appended.
const lastMessagePreviewBytes = 100

// lastMessagePreview shortens a Matrix room message to the preview served as a
// conversation's last_message. The body arrives from Telegram, WhatsApp, Signal
// or Discord over a bridge, so it is the text in the fleet most likely to carry
// multi-byte runes.
func lastMessagePreview(body string) string {
	if len(body) <= lastMessagePreviewBytes {
		return body
	}
	return truncateAtRuneBoundary(body, lastMessagePreviewBytes) + "…"
}

package matrix

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// fourByteRune is deliberately four bytes wide. With a two-byte rune only one of
// the two straddling offsets is wrong, so a test that happens to pick the other
// one passes against the unfixed code and measures nothing.
const fourByteRune = "\U0001D11E" // 𝄞 MUSICAL SYMBOL G CLEF

// slideRuneAcross builds a body whose four-byte rune starts at byte offset
// runeStart, with enough trailing text that the body always overruns the preview
// budget.
func slideRuneAcross(runeStart int) string {
	return strings.Repeat("a", runeStart) + fourByteRune + strings.Repeat("b", 200)
}

// TestLastMessagePreviewNeverSplitsARune slides a four-byte rune across the cut.
// A single hand-picked string lands off the boundary and passes against the
// unfixed code, which is how this defect survived in twenty repos.
func TestLastMessagePreviewNeverSplitsARune(t *testing.T) {
	cases := []struct {
		name string
		// runeStart is where the four-byte rune begins. The cut is at
		// lastMessagePreviewBytes, so a rune starting at 97, 98 or 99 straddles
		// it; one starting at 96 or 100 does not.
		runeStart int
		// aligned marks the known-negative controls: offsets where the cut lands
		// on a rune boundary anyway. These must pass against the unfixed code
		// too, otherwise the test is only detecting non-ASCII input.
		aligned bool
	}{
		{name: "control/rune ends exactly at the cut", runeStart: 96, aligned: true},
		{name: "rune straddles the cut, 3 bytes before it", runeStart: 97},
		{name: "rune straddles the cut, 2 bytes before it", runeStart: 98},
		{name: "rune straddles the cut, 1 byte before it", runeStart: 99},
		{name: "control/rune starts exactly at the cut", runeStart: 100, aligned: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := slideRuneAcross(tc.runeStart)

			// Guard the instrument, not just the result. If the input stops
			// reaching the truncating branch, every assertion below goes green
			// against an untruncated string and measures nothing.
			if len(body) <= lastMessagePreviewBytes {
				t.Fatalf("input never reaches the cut: len(body)=%d, budget=%d", len(body), lastMessagePreviewBytes)
			}

			preview := lastMessagePreview(body)

			if !strings.HasSuffix(preview, "…") {
				t.Fatalf("preview of an overlong body must end in an ellipsis, got %q", preview)
			}
			kept := strings.TrimSuffix(preview, "…")

			if !utf8.ValidString(kept) {
				t.Errorf("preview is not valid UTF-8: %q", kept)
			}
			if strings.ContainsRune(kept, utf8.RuneError) {
				t.Errorf("preview contains U+FFFD, so a rune was split: %q", kept)
			}
			if !strings.HasPrefix(body, kept) {
				t.Errorf("preview is not a prefix of the body: %q", kept)
			}

			// Validity alone is not falsifiable: a helper that returns "" for
			// everything is valid UTF-8 within budget. Pin the length too — the
			// cut may lose at most the width of the rune straddling it.
			if len(kept) > lastMessagePreviewBytes {
				t.Errorf("preview overruns its budget: %d bytes, budget %d", len(kept), lastMessagePreviewBytes)
			}
			minKept := lastMessagePreviewBytes - (len(fourByteRune) - 1)
			if len(kept) < minKept {
				t.Errorf("preview lost more than a straddling rune's worth: %d bytes, want at least %d", len(kept), minKept)
			}

			if tc.aligned && len(kept) != lastMessagePreviewBytes {
				t.Errorf("the cut already lands on a boundary here, so nothing should be walked back: %d bytes, want %d", len(kept), lastMessagePreviewBytes)
			}
		})
	}
}

// TestLastMessagePreviewLeavesShortBodiesAlone pins the untruncated path: a body
// within budget is served whole, with no ellipsis, whatever runes it holds.
func TestLastMessagePreviewLeavesShortBodiesAlone(t *testing.T) {
	for _, body := range []string{
		"",
		"hello",
		strings.Repeat(fourByteRune, 25), // exactly 100 bytes, the budget
	} {
		if len(body) > lastMessagePreviewBytes {
			t.Fatalf("case is not a short body: len=%d, budget=%d", len(body), lastMessagePreviewBytes)
		}
		if got := lastMessagePreview(body); got != body {
			t.Errorf("lastMessagePreview(%q) = %q, want it returned unchanged", body, got)
		}
	}
}

// TestLastMessagePreviewBudgetIsAHundredBytes pins the budget with a literal
// rather than with lastMessagePreviewBytes. A test that reads the constant it
// is checking moves with that constant and cannot see it drift, so every
// assertion above — which does read it — is blind to the budget changing.
func TestLastMessagePreviewBudgetIsAHundredBytes(t *testing.T) {
	body := strings.Repeat("a", 250)
	want := strings.Repeat("a", 100) + "…"
	if got := lastMessagePreview(body); got != want {
		t.Errorf("preview kept %d bytes, want 100 followed by an ellipsis", len(strings.TrimSuffix(got, "…")))
	}
}

// TestTruncateAtRuneBoundary covers the helper directly, including the budgets
// the one call site never reaches.
func TestTruncateAtRuneBoundary(t *testing.T) {
	cases := []struct {
		name     string
		s        string
		maxBytes int
		want     string
	}{
		{name: "zero budget keeps nothing", s: "hello", maxBytes: 0, want: ""},
		{name: "negative budget keeps nothing", s: "hello", maxBytes: -1, want: ""},
		{name: "string within budget is untouched", s: "hello", maxBytes: 5, want: "hello"},
		{name: "ascii cut needs no walk-back", s: "hello", maxBytes: 3, want: "hel"},
		{name: "cut walks back off a straddled rune", s: "a" + fourByteRune, maxBytes: 3, want: "a"},
		{name: "cut on a boundary stays put", s: "a" + fourByteRune + "b", maxBytes: 5, want: "a" + fourByteRune},
		{name: "budget smaller than the leading rune keeps nothing", s: fourByteRune, maxBytes: 2, want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateAtRuneBoundary(tc.s, tc.maxBytes)
			if got != tc.want {
				t.Errorf("truncateAtRuneBoundary(%q, %d) = %q, want %q", tc.s, tc.maxBytes, got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("truncateAtRuneBoundary(%q, %d) returned invalid UTF-8: %q", tc.s, tc.maxBytes, got)
			}
		})
	}
}

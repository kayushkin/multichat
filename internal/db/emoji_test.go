package db

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestEmojiFavoritesKeepTheirOrderAndRefuseUnknownCustomEmoji(t *testing.T) {
	database, err := Initialize(filepath.Join(t.TempDir(), "multichat.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := ReplaceDiscordServerEmoji(database, "111", "Riverside", []DiscordCustomEmoji{{DiscordEmojiID: "1", Name: "partyParrot", Animated: true}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceEmojiFavorites(database, []string{"discord-emoji:1", "🔥", "👍"}); err != nil {
		t.Fatal(err)
	}
	keys, err := EmojiFavoriteKeys(database)
	if err != nil || len(keys) != 3 || keys[0] != "discord-emoji:1" || keys[2] != "👍" {
		t.Fatalf("keys = %v, %v", keys, err)
	}
	for _, bad := range [][]string{{"discord-emoji:999"}, {"🔥", "🔥"}, {":fire:"}, {""}} {
		if err := ReplaceEmojiFavorites(database, bad); !errors.Is(err, ErrInvalidEmojiKey) {
			t.Errorf("%q: err = %v", bad, err)
		}
	}
	if keys, _ := EmojiFavoriteKeys(database); len(keys) != 3 {
		t.Fatalf("a refused write changed the favourites: %v", keys)
	}
}

func TestEmojiSettingsDefaultAndValidate(t *testing.T) {
	database, err := Initialize(filepath.Join(t.TempDir(), "multichat.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if s, err := ReadEmojiSettings(database); err != nil || s != DefaultEmojiSettings {
		t.Fatalf("default = %+v, %v", s, err)
	}
	want := EmojiSettings{SkinTone: "🏽", QuickReactionCount: 8, OfferOtherServersEmoji: true}
	if err := SaveEmojiSettings(database, want); err != nil {
		t.Fatal(err)
	}
	if s, _ := ReadEmojiSettings(database); s != want {
		t.Fatalf("saved = %+v", s)
	}
	for _, bad := range []EmojiSettings{{SkinTone: "x"}, {QuickReactionCount: 13}, {QuickReactionCount: -1}} {
		if err := SaveEmojiSettings(database, bad); !errors.Is(err, ErrInvalidEmojiSettings) {
			t.Errorf("%+v: err = %v", bad, err)
		}
	}
}

// A refresh replaces a server's emoji whole and leaves other servers alone.
func TestReplaceDiscordServerEmojiDropsDeletedEmoji(t *testing.T) {
	database, err := Initialize(filepath.Join(t.TempDir(), "multichat.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ReplaceDiscordServerEmoji(database, "111", "Riverside", []DiscordCustomEmoji{{DiscordEmojiID: "1", Name: "a"}, {DiscordEmojiID: "2", Name: "b"}}, time.Now())
	ReplaceDiscordServerEmoji(database, "222", "Pretend", []DiscordCustomEmoji{{DiscordEmojiID: "3", Name: "c"}}, time.Now())
	ReplaceDiscordServerEmoji(database, "111", "Riverside", []DiscordCustomEmoji{{DiscordEmojiID: "2", Name: "b"}}, time.Now())
	list, _ := DiscordCustomEmojiList(database)
	if len(list) != 2 || list[0].Name != "c" || list[1].Name != "b" {
		t.Fatalf("list = %+v", list)
	}
	refreshes, _ := DiscordEmojiRefreshes(database)
	if len(refreshes) != 2 {
		t.Fatalf("refreshes = %+v", refreshes)
	}
}

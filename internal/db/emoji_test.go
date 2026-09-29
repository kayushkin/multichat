package db

import (
	"database/sql"
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
	if err := ReplaceEmojiFavorites(database, []string{"discord-emoji:1", "🔥", "👍"}, nil); err != nil {
		t.Fatal(err)
	}
	keys, err := EmojiFavoriteKeys(database)
	if err != nil || len(keys) != 3 || keys[0] != "discord-emoji:1" || keys[2] != "👍" {
		t.Fatalf("keys = %v, %v", keys, err)
	}
	for _, bad := range [][]string{{"discord-emoji:999"}, {"🔥", "🔥"}, {":fire:"}, {""}} {
		if err := ReplaceEmojiFavorites(database, bad, nil); !errors.Is(err, ErrInvalidEmojiKey) {
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
	want := EmojiSettings{SkinTone: "🏽", QuickReactionCount: 8}
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

// A seen emoji can be a favourite without being in the catalog.
func TestASeenEmojiCanBeAFavourite(t *testing.T) {
	database, err := Initialize(filepath.Join(t.TempDir(), "multichat.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := ReplaceEmojiFavorites(database, []string{"discord-emoji:77"}, map[string]bool{"77": true}); err != nil {
		t.Fatal(err)
	}
}

// The Nitro status is the latest check that answered, with the error of a
// later one that failed.
func TestDiscordNitroStatus(t *testing.T) {
	database, err := Initialize(filepath.Join(t.TempDir(), "multichat.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if nitro, lastError, err := DiscordNitroStatus(database); err != nil || nitro != nil || lastError != "" {
		t.Fatalf("before any check: %+v %q %v", nitro, lastError, err)
	}
	two := 2
	day := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	RecordDiscordNitroCheck(database, day, "42", &two, "")
	RecordDiscordNitroCheck(database, day.Add(24*time.Hour), "", nil, "discord: GET /users/@me: 401")
	nitro, lastError, err := DiscordNitroStatus(database)
	if err != nil || nitro == nil || !nitro.HasNitro || nitro.DiscordUserID != "42" || lastError != "discord: GET /users/@me: 401" {
		t.Fatalf("status: %+v %q %v", nitro, lastError, err)
	}
	zero := 0
	RecordDiscordNitroCheck(database, day.Add(48*time.Hour), "42", &zero, "")
	nitro, lastError, _ = DiscordNitroStatus(database)
	if nitro.HasNitro || lastError != "" {
		t.Fatalf("after a check without Nitro: %+v %q", nitro, lastError)
	}
	if last, _ := LastDiscordNitroCheckAt(database); !last.Equal(day.Add(48 * time.Hour)) {
		t.Fatalf("last check at %v", last)
	}
}

// A database from before the Nitro check loses the switch's column.
func TestTheOldSwitchColumnIsDropped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "multichat.db")
	old, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`CREATE TABLE emoji_settings (id INTEGER PRIMARY KEY CHECK (id = 1), skin_tone TEXT NOT NULL DEFAULT '',
		quick_reaction_count INTEGER NOT NULL DEFAULT 5, offer_other_servers_emoji INTEGER NOT NULL DEFAULT 0);
		INSERT INTO emoji_settings VALUES (1, '🏽', 7, 1);`); err != nil {
		t.Fatal(err)
	}
	old.Close()
	database, err := Initialize(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if s, err := ReadEmojiSettings(database); err != nil || s != (EmojiSettings{SkinTone: "🏽", QuickReactionCount: 7}) {
		t.Fatalf("settings kept: %+v %v", s, err)
	}
}

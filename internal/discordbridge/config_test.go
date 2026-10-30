package discordbridge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadBridgeConfigNamesTheBotAndItsPrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("homeserver:\n    domain: chat.example\nappservice:\n    bot:\n        username: discordbot\nbridge:\n    command_prefix: '!discord'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadBridgeConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := (BridgeConfig{BotUserID: "@discordbot:chat.example", CommandPrefix: "!discord"}); got != want {
		t.Fatalf("ReadBridgeConfig = %+v, want %+v", got, want)
	}
	if err := os.WriteFile(path, []byte("homeserver:\n    domain: chat.example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBridgeConfig(path); err == nil {
		t.Fatal("a config with no bot or prefix was accepted")
	}
}

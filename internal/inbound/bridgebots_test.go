package inbound

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/kayushkin/multichat/internal/matrix"
)

func TestTheWorkerInvitesADiscordBotMissingFromItsRoomAndLeavesOtherBridgesBotsOut(t *testing.T) {
	// Each room's state: the bot its m.bridge names, and that bot's membership.
	rooms := map[string]string{
		"!crab:x":   `[{"type":"m.bridge","state_key":"d","content":{"bridgebot":"@discordbot:x","protocol":{"id":"discordgo"}}},{"type":"m.room.member","state_key":"@discord_1:x","content":{"membership":"join"}}]`,
		"!left:x":   `[{"type":"m.bridge","state_key":"d","content":{"bridgebot":"@discordbot:x","protocol":{"id":"discordgo"}}},{"type":"m.room.member","state_key":"@discordbot:x","content":{"membership":"leave"}}]`,
		"!fine:x":   `[{"type":"m.bridge","state_key":"d","content":{"bridgebot":"@discordbot:x","protocol":{"id":"discordgo"}}},{"type":"m.room.member","state_key":"@discordbot:x","content":{"membership":"join"}}]`,
		"!asked:x":  `[{"type":"m.bridge","state_key":"d","content":{"bridgebot":"@discordbot:x","protocol":{"id":"discordgo"}}},{"type":"m.room.member","state_key":"@discordbot:x","content":{"membership":"invite"}}]`,
		"!saved:x":  `[{"type":"m.bridge","state_key":"t","content":{"bridgebot":"@telegrambot:x","protocol":{"id":"telegram"}}},{"type":"m.room.member","state_key":"@admin:x","content":{"membership":"join"}}]`,
		"!matrix:x": `[{"type":"m.room.member","state_key":"@admin:x","content":{"membership":"join"}}]`,
	}
	var invites []string
	rateLimitedOnce := false
	synapse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/_matrix/client/v3/rooms/")
		roomID, action, _ := strings.Cut(path, "/")
		switch {
		case r.Method == "GET" && action == "state":
			w.Write([]byte(rooms[roomID]))
		case r.Method == "POST" && action == "invite":
			if !rateLimitedOnce {
				rateLimitedOnce = true
				w.WriteHeader(429)
				w.Write([]byte(`{"errcode":"M_LIMIT_EXCEEDED","retry_after_ms":1}`))
				return
			}
			data, _ := io.ReadAll(r.Body)
			var body struct {
				UserID string `json:"user_id"`
			}
			if err := json.Unmarshal(data, &body); err != nil {
				t.Fatal(err)
			}
			invites = append(invites, body.UserID+" to "+roomID)
			w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer synapse.Close()
	worker := &SyncWorker{Matrix: matrix.NewClient(synapse.URL, "token")}

	for _, roomID := range []string{"!crab:x", "!left:x", "!fine:x", "!asked:x", "!saved:x", "!matrix:x"} {
		if err := worker.inviteMissingBridgeBots(roomID); err != nil {
			t.Fatalf("%s: %v", roomID, err)
		}
	}
	if want := []string{"@discordbot:x to !crab:x", "@discordbot:x to !left:x"}; !reflect.DeepEqual(invites, want) {
		t.Fatalf("invited %v, want %v (missing or departed Discord bots only, the rate-limited invite retried; never the Telegram bot)", invites, want)
	}
}

package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestDiscordStatusWithoutDiscordDBSaysSo(t *testing.T) {
	router := routerForSettingsTests(t) // no DISCORD_DB in its environment
	response := serve(router, http.MethodGet, "/discord/status", secretValues["MULTICHAT_TOKEN"])
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /discord/status with DISCORD_DB unset = %d, want 503", response.Code)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || !strings.Contains(body.Error, "DISCORD_DB") {
		t.Errorf("error = %q (%v), want it to name DISCORD_DB", body.Error, err)
	}
	if got := serve(router, http.MethodPost, "/discord/status", secretValues["MULTICHAT_TOKEN"]).Code; got != http.StatusMethodNotAllowed {
		t.Errorf("POST /discord/status = %d, want 405", got)
	}
}

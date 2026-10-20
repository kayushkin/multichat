package api

import (
	"strings"
	"testing"
)

func TestAssigningShortNamesSaysWhatSettingsItLacksAndRefusesABadDryRun(t *testing.T) {
	router := routerForSettingsTests(t)
	bearer := secretValues["MULTICHAT_TOKEN"]
	if got := serve(router, "GET", "/short-names/assign", bearer); got.Code != 405 {
		t.Errorf("GET: %d", got.Code)
	}
	if got := serve(router, "POST", "/short-names/assign?dry_run=yes", bearer); got.Code != 400 {
		t.Errorf("dry_run=yes: %d %s", got.Code, got.Body)
	}
	got := serve(router, "POST", "/short-names/assign?dry_run=true", bearer)
	if got.Code != 503 || !strings.Contains(got.Body.String(), "SHORT_NAME_STORE_URL") || !strings.Contains(got.Body.String(), "DISCORD_DB") {
		t.Errorf("unconfigured: %d %s", got.Code, got.Body)
	}
	if got := serve(router, "POST", "/short-names/assign?dry_run=true", ""); got.Code != 401 {
		t.Errorf("no bearer: %d", got.Code)
	}
}

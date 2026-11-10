package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTheBotsReactionRulesPassThroughAndNothingElse(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.EscapedPath()+"?"+r.URL.RawQuery+" "+string(body))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		io.WriteString(w, `{"id":7}`)
	}))
	t.Cleanup(server.Close)
	router := routerWithoutAuth(t)
	router.discordSignupStoreURL = server.URL

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("POST", "/discord-signup-store/auto-reactions", strings.NewReader(`{"emoji":"🥛"}`)))
	if recorder.Code != 201 || recorder.Body.String() != `{"id":7}` || seen[0] != `POST /api/auto-reactions? {"emoji":"🥛"}` {
		t.Fatalf("%d %s, store saw %v", recorder.Code, recorder.Body, seen)
	}
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/discord-signup-store/guilds/123/members/search?q=ab%20c", nil))
	if recorder.Code != 201 || seen[1] != "GET /api/guilds/123/members/search?q=ab%20c " {
		t.Fatalf("%d, store saw %v", recorder.Code, seen)
	}
	for _, request := range []*http.Request{
		httptest.NewRequest("GET", "/discord-signup-store/events", nil),
		httptest.NewRequest("DELETE", "/discord-signup-store/auto-reactions", nil),
		httptest.NewRequest("GET", "/discord-signup-store/guilds/1/roster", nil),
	} {
		recorder = httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != 404 {
			t.Fatalf("%s %s: %d", request.Method, request.URL.Path, recorder.Code)
		}
	}
	if len(seen) != 2 {
		t.Fatalf("the store was called for a refused route: %v", seen)
	}
}

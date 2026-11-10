package api

// The Event-Manager bot's own reaction rules, through multichat, for the
// Messages pages' Auto-reactions page. discord-signup-store owns the bot and
// its rules, and serves them under /api; these routes pass a fixed set of its
// routes through unchanged — method, path, query, body, status and answer —
// so the web front end calls multichat alone:
//
//	GET, POST      /discord-signup-store/auto-reactions
//	PATCH, DELETE  /discord-signup-store/auto-reactions/{id}
//	GET            /discord-signup-store/guilds
//	GET            /discord-signup-store/guilds/{guild_id}/members/search
//
// Its rosters and events are not carried.

import (
	"fmt"
	"net/http"
	"strings"
)

// discordSignupStoreMount is where these routes hang, under the /api prefix main strips.
const discordSignupStoreMount = "/discord-signup-store/"

// discordSignupStoreRoutes are the routes carried, by method; a "*" is
// exactly one path segment.
var discordSignupStoreRoutes = map[string][]string{
	http.MethodGet:    {"auto-reactions", "guilds", "guilds/*/members/search"},
	http.MethodPost:   {"auto-reactions"},
	http.MethodPatch:  {"auto-reactions/*"},
	http.MethodDelete: {"auto-reactions/*"},
}

func (r *Router) discordSignupStoreHandler(w http.ResponseWriter, req *http.Request) {
	if r.discordSignupStoreURL == "" {
		r.writeError(w, 503, "multichat does not reach discord-signup-store: DISCORD_SIGNUP_STORE_URL is not set")
		return
	}
	// The escaped path, so a segment keeps its own encoding on the way through.
	rest := strings.Trim(strings.TrimPrefix(req.URL.EscapedPath(), "/discord-signup-store"), "/")
	if !pathMatchesAny(discordSignupStoreRoutes[req.Method], strings.Split(rest, "/")) {
		r.writeError(w, 404, fmt.Sprintf("%s %s%s is not a discord-signup-store route multichat carries", req.Method, discordSignupStoreMount, rest))
		return
	}
	address := r.discordSignupStoreURL + "/api/" + rest
	if req.URL.RawQuery != "" {
		address += "?" + req.URL.RawQuery
	}
	forward, err := http.NewRequestWithContext(req.Context(), req.Method, address, req.Body)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	if contentType := req.Header.Get("Content-Type"); contentType != "" {
		forward.Header.Set("Content-Type", contentType)
	}
	response, err := r.httpClient.Do(forward)
	if err != nil {
		r.writeError(w, 502, "discord-signup-store: "+err.Error())
		return
	}
	r.passAnswerThrough(w, response, "discord-signup-store "+req.Method+" /"+rest)
}

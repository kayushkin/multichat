package api

import (
	"net/http"

	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/discordbridge"
)

// discordStatusHandler serves GET /discord/status: whether the Discord
// bridge's account is logged in, the servers it bridges with each channel's
// room, whether @admin has joined that room, when the bridge last saw a
// message there, and what the message log holds for it. Read-only; it reads
// the bridge's database, Synapse's joined_rooms and multichat's own log, and
// never calls Discord.
func (r *Router) discordStatusHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "GET" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	if r.discordDBPath == "" {
		r.writeError(w, 503, "DISCORD_DB is not set, so the Discord bridge's database cannot be read")
		return
	}
	if r.matrix == nil {
		r.writeError(w, 503, "Matrix client not configured (SYNAPSE_URL and MATRIX_TOKEN), so joined rooms cannot be read")
		return
	}
	bridge, err := discordbridge.Open(r.discordDBPath)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	defer bridge.Close()
	status, err := discordbridge.ReadStatus(bridge)
	if err != nil {
		r.writeError(w, 500, "DISCORD_DB "+r.discordDBPath+": "+err.Error())
		return
	}
	joinedRoomIDs, err := r.matrix.JoinedRoomIDs()
	if err != nil {
		r.writeError(w, 502, err.Error())
		return
	}
	counts, err := db.MessageLogCountsByChannel(r.db, "discord")
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	status.Annotate(joinedRoomIDs, counts)
	r.writeJSON(w, status)
}

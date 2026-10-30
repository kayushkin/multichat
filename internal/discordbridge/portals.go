package discordbridge

import (
	"database/sql"

	"github.com/kayushkin/multichat/internal/matrix"
)

// PortalsByRoomID returns what the bridge database says about every room it
// has made: the Discord channel the room carries, and whether that channel is
// a one-to-one DM (portal type 1). mautrix-discord marks neither in a room's
// m.bridge state, so the bridge database is the only place that says a room
// is a DM rather than a server channel or a group DM (type 3), and the only
// place that joins a room to the Discord channel id other stores record.
func PortalsByRoomID(bridge *sql.DB) (map[string]matrix.DiscordPortal, error) {
	rows, err := bridge.Query(`SELECT mxid, dcid, type FROM portal WHERE mxid IS NOT NULL AND mxid != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	portals := map[string]matrix.DiscordPortal{}
	for rows.Next() {
		var roomID, channelID string
		var channelType int
		if err := rows.Scan(&roomID, &channelID, &channelType); err != nil {
			return nil, err
		}
		portals[roomID] = matrix.DiscordPortal{ChannelID: channelID, DirectMessage: channelType == 1}
	}
	return portals, rows.Err()
}

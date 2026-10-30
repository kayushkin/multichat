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

// DirectMessagePortal is the bridge's record of a one-to-one DM with one
// Discord user: its channel, and its room, "" until the bridge makes one.
type DirectMessagePortal struct {
	ChannelID string
	RoomID    string
}

// DirectMessagePortalWith returns the DM with a Discord user, by their
// Discord id. found is false when the bridge knows no DM with them: the
// account has never messaged them.
func DirectMessagePortalWith(bridge *sql.DB, discordUserID string) (portal DirectMessagePortal, found bool, err error) {
	var roomID sql.NullString
	err = bridge.QueryRow(`SELECT dcid, mxid FROM portal WHERE type = 1 AND other_user_id = ?`, discordUserID).Scan(&portal.ChannelID, &roomID)
	if err == sql.ErrNoRows {
		return DirectMessagePortal{}, false, nil
	}
	if err != nil {
		return DirectMessagePortal{}, false, err
	}
	portal.RoomID = roomID.String
	return portal, true, nil
}

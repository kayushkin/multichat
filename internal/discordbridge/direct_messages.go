package discordbridge

import "database/sql"

// DirectMessageRoomIDs returns the Matrix room of every one-to-one Discord DM
// the bridge has made a room for (portal type 1). mautrix-discord does not
// mark these rooms in their m.bridge state, so the bridge database is the
// only place that says a room is a DM rather than a server channel or a group
// DM (type 3).
func DirectMessageRoomIDs(bridge *sql.DB) (map[string]bool, error) {
	rows, err := bridge.Query(`SELECT mxid FROM portal WHERE type = 1 AND mxid IS NOT NULL AND mxid != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roomIDs := map[string]bool{}
	for rows.Next() {
		var roomID string
		if err := rows.Scan(&roomID); err != nil {
			return nil, err
		}
		roomIDs[roomID] = true
	}
	return roomIDs, rows.Err()
}

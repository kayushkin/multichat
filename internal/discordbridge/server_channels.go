package discordbridge

import (
	"database/sql"
	"fmt"
)

// discordCategoryChannelType is Discord's channel type for a category, which
// only groups channels and holds no messages of its own.
const discordCategoryChannelType = 4

// ServerChannelRoom is the Matrix room of one Discord server channel: its
// server and its names as the bridge stores them.
type ServerChannelRoom struct {
	MatrixRoomID     string
	DiscordServerID  string
	ServerName       string
	DiscordChannelID string
	ChannelName      string
	ChannelType      int
	CategoryName     string
}

// ServerChannelRooms returns every server channel the bridge has made a
// Matrix room for, keyed by room id. Unlike ReadStatus it keeps every kind of
// channel but a category, voice channels included: Discord gives a voice
// channel a text chat, and the bridge makes a room for it.
func ServerChannelRooms(bridge *sql.DB) (map[string]ServerChannelRoom, error) {
	rows, err := bridge.Query(`SELECT p.mxid, p.dc_guild_id, g.name, p.dcid, p.name, p.type, COALESCE(category.name, '')
		FROM portal p
		JOIN guild g ON g.dcid = p.dc_guild_id
		LEFT JOIN portal category ON category.dcid = p.dc_parent_id AND category.receiver = p.dc_parent_receiver
		WHERE p.dc_guild_id != '' AND p.mxid IS NOT NULL AND p.mxid != '' AND p.type != ?`, discordCategoryChannelType)
	if err != nil {
		return nil, fmt.Errorf("read server channel rooms: %w", err)
	}
	defer rows.Close()
	rooms := map[string]ServerChannelRoom{}
	for rows.Next() {
		var room ServerChannelRoom
		if err := rows.Scan(&room.MatrixRoomID, &room.DiscordServerID, &room.ServerName, &room.DiscordChannelID,
			&room.ChannelName, &room.ChannelType, &room.CategoryName); err != nil {
			return nil, fmt.Errorf("scan server channel room: %w", err)
		}
		rooms[room.MatrixRoomID] = room
	}
	return rooms, rows.Err()
}

// Package discordbridge reads the mautrix-discord bridge's own SQLite
// database (DISCORD_DB) to report what the bridge is doing: whether its
// account is logged in, which Discord servers it bridges, and each bridged
// channel's Matrix room. It opens the file read-only and never calls Discord.
//
// The bridge owns every id here: Discord ids (dcid) come from Discord, and
// Matrix room ids (mxid) from Synapse when the bridge made the room.
package discordbridge

import (
	"database/sql"
	"fmt"
	"slices"
	"time"

	"github.com/kayushkin/multichat/internal/db"
	_ "github.com/mattn/go-sqlite3"
)

// Open opens the bridge database read-only. The bridge runs in WAL mode as
// root; SQLite reads a world-readable WAL database without writing to it.
func Open(path string) (*sql.DB, error) {
	database, err := sql.Open("sqlite3", "file:"+path+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open DISCORD_DB %s: %w", path, err)
	}
	return database, nil
}

// Discord channel types that carry messages, from Discord's API
// (discordgo.ChannelType): 0 is a text channel, 5 an announcement channel.
// Categories (4), voice channels (2) and forums (15) have no messages of
// their own in a portal.
var messageChannelTypes = []int{0, 5}

// Bridging modes, from mautrix-discord's database/guild.go
// (GuildBridgingMode). Zero means the server is not bridged.
var bridgingModeNames = map[int]string{
	0: "none",
	1: "if-portal-exists",
	2: "create-on-message",
	3: "everything",
}

// DiscordBridgeAccount is one Matrix user logged in (or once logged in) to the bridge.
// The Discord token is never read: LoggedIn comes from a SQL IS NOT NULL
// test, so its value never leaves the bridge database.
type DiscordBridgeAccount struct {
	MatrixUserID     string `json:"matrix_user_id"`
	DiscordUserID    string `json:"discord_user_id"`
	LoggedIn         bool   `json:"logged_in"`
	ManagementRoomID string `json:"management_room_id"`
}

// DiscordBridgedChannel is one message-carrying channel of a bridged server.
type DiscordBridgedChannel struct {
	DiscordChannelID string `json:"discord_channel_id"`
	Name             string `json:"name"`
	ChannelType      int    `json:"channel_type"`
	CategoryName     string `json:"category_name"`
	// MatrixRoomID is empty when the bridge has not made a room for it yet.
	MatrixRoomID string `json:"matrix_room_id"`
	// AdminJoined says whether @admin has joined the room. Until it has,
	// multichat can neither read nor log the channel.
	AdminJoined bool `json:"admin_joined"`
	// LastBridgedMessageAt is the newest message the bridge recorded in the
	// channel, nil when it has recorded none.
	LastBridgedMessageAt *time.Time `json:"last_bridged_message_at"`
	// From multichat's message log: messages (not edits) logged in the
	// channel, how many of those were deleted, and the newest one's time.
	LoggedMessageCount  int        `json:"logged_message_count"`
	DeletedMessageCount int        `json:"deleted_message_count"`
	LastLoggedMessageAt *time.Time `json:"last_logged_message_at"`
}

// DiscordBridgedServer is one Discord server the bridge bridges.
type DiscordBridgedServer struct {
	DiscordServerID  string                  `json:"discord_server_id"`
	Name             string                  `json:"name"`
	BridgingMode     int                     `json:"bridging_mode"`
	BridgingModeName string                  `json:"bridging_mode_name"`
	MatrixSpaceID    string                  `json:"matrix_space_id"`
	Channels         []DiscordBridgedChannel `json:"channels"`
}

// DiscordBridgeStatus is what GET /discord/status answers.
type DiscordBridgeStatus struct {
	Accounts       []DiscordBridgeAccount `json:"accounts"`
	BridgedServers []DiscordBridgedServer `json:"bridged_servers"`
	// UnbridgedServerCount counts servers a logged-in account is a member of
	// that the bridge does not bridge.
	UnbridgedServerCount int `json:"unbridged_server_count"`
}

// ReadStatus reads accounts, bridged servers and their channels from the
// bridge database. AdminJoined and the message-log fields are left zero:
// the bridge database does not know them, and Annotate fills them in.
func ReadStatus(bridge *sql.DB) (DiscordBridgeStatus, error) {
	status := DiscordBridgeStatus{Accounts: []DiscordBridgeAccount{}, BridgedServers: []DiscordBridgedServer{}}

	accountRows, err := bridge.Query(`SELECT mxid, COALESCE(dcid, ''), discord_token IS NOT NULL,
		COALESCE(management_room, '') FROM "user" ORDER BY mxid`)
	if err != nil {
		return DiscordBridgeStatus{}, fmt.Errorf("read bridge accounts: %w", err)
	}
	for accountRows.Next() {
		var account DiscordBridgeAccount
		if err := accountRows.Scan(&account.MatrixUserID, &account.DiscordUserID, &account.LoggedIn,
			&account.ManagementRoomID); err != nil {
			accountRows.Close()
			return DiscordBridgeStatus{}, fmt.Errorf("scan bridge account: %w", err)
		}
		status.Accounts = append(status.Accounts, account)
	}
	accountRows.Close()
	if err := accountRows.Err(); err != nil {
		return DiscordBridgeStatus{}, fmt.Errorf("read bridge accounts: %w", err)
	}

	serverRows, err := bridge.Query(`SELECT dcid, name, bridging_mode, COALESCE(mxid, '') FROM guild
		WHERE bridging_mode != 0 ORDER BY name, dcid`)
	if err != nil {
		return DiscordBridgeStatus{}, fmt.Errorf("read bridged servers: %w", err)
	}
	for serverRows.Next() {
		server := DiscordBridgedServer{Channels: []DiscordBridgedChannel{}}
		if err := serverRows.Scan(&server.DiscordServerID, &server.Name, &server.BridgingMode,
			&server.MatrixSpaceID); err != nil {
			serverRows.Close()
			return DiscordBridgeStatus{}, fmt.Errorf("scan bridged server: %w", err)
		}
		server.BridgingModeName = bridgingModeNames[server.BridgingMode]
		status.BridgedServers = append(status.BridgedServers, server)
	}
	serverRows.Close()
	if err := serverRows.Err(); err != nil {
		return DiscordBridgeStatus{}, fmt.Errorf("read bridged servers: %w", err)
	}

	for i := range status.BridgedServers {
		channels, err := readChannels(bridge, status.BridgedServers[i].DiscordServerID)
		if err != nil {
			return DiscordBridgeStatus{}, err
		}
		status.BridgedServers[i].Channels = channels
	}

	// A server a logged-in account belongs to shows up in user_portal with
	// type 'guild'.
	if err := bridge.QueryRow(`SELECT count(DISTINCT g.dcid) FROM user_portal up
		JOIN "user" u ON u.mxid = up.user_mxid AND u.discord_token IS NOT NULL
		JOIN guild g ON g.dcid = up.discord_id
		WHERE up.type = 'guild' AND g.bridging_mode = 0`).Scan(&status.UnbridgedServerCount); err != nil {
		return DiscordBridgeStatus{}, fmt.Errorf("count unbridged servers: %w", err)
	}
	return status, nil
}

func readChannels(bridge *sql.DB, serverID string) ([]DiscordBridgedChannel, error) {
	placeholders := ""
	args := []any{serverID}
	for i, channelType := range messageChannelTypes {
		if i > 0 {
			placeholders += ", "
		}
		placeholders += "?"
		args = append(args, channelType)
	}
	rows, err := bridge.Query(`SELECT p.dcid, p.name, p.type, COALESCE(category.name, ''), COALESCE(p.mxid, ''),
		(SELECT max(m.timestamp) FROM message m WHERE m.dc_chan_id = p.dcid AND m.dc_chan_receiver = p.receiver)
		FROM portal p
		LEFT JOIN portal category ON category.dcid = p.dc_parent_id AND category.receiver = p.dc_parent_receiver
		WHERE p.dc_guild_id = ? AND p.type IN (`+placeholders+`)
		ORDER BY COALESCE(category.name, ''), p.name, p.dcid`, args...)
	if err != nil {
		return nil, fmt.Errorf("read channels of server %s: %w", serverID, err)
	}
	defer rows.Close()
	channels := []DiscordBridgedChannel{}
	for rows.Next() {
		var channel DiscordBridgedChannel
		var lastMilliseconds sql.NullInt64
		if err := rows.Scan(&channel.DiscordChannelID, &channel.Name, &channel.ChannelType, &channel.CategoryName,
			&channel.MatrixRoomID, &lastMilliseconds); err != nil {
			return nil, fmt.Errorf("scan channel of server %s: %w", serverID, err)
		}
		if lastMilliseconds.Valid {
			t := time.UnixMilli(lastMilliseconds.Int64).UTC()
			channel.LastBridgedMessageAt = &t
		}
		channels = append(channels, channel)
	}
	return channels, rows.Err()
}

// Annotate fills in what the bridge database cannot know: whether @admin has
// joined each channel's room, and the message-log counts keyed by Discord
// channel id.
func (status *DiscordBridgeStatus) Annotate(joinedRoomIDs []string, countsByChannelID map[string]db.ChannelLogCounts) {
	for i := range status.BridgedServers {
		for j := range status.BridgedServers[i].Channels {
			channel := &status.BridgedServers[i].Channels[j]
			channel.AdminJoined = channel.MatrixRoomID != "" && slices.Contains(joinedRoomIDs, channel.MatrixRoomID)
			counts := countsByChannelID[channel.DiscordChannelID]
			channel.LoggedMessageCount = counts.LoggedMessageCount
			channel.DeletedMessageCount = counts.DeletedMessageCount
			channel.LastLoggedMessageAt = counts.LastLoggedMessageAt
		}
	}
}

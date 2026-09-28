package discordbridge

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kayushkin/multichat/internal/db"
)

// bridgeSchema is the part of mautrix-discord's schema ReadStatus reads,
// copied from the live bridge database (sqlite3 .schema) on 2026-09-28.
const bridgeSchema = `
CREATE TABLE "user" (
    mxid TEXT PRIMARY KEY,
    dcid TEXT UNIQUE,
    discord_token   TEXT,
    management_room TEXT,
    space_room      TEXT,
    dm_space_room   TEXT,
    read_state_version INTEGER NOT NULL DEFAULT 0,
    heartbeat_session jsonb
);
CREATE TABLE guild (
    dcid       TEXT PRIMARY KEY,
    mxid       TEXT UNIQUE,
    plain_name TEXT NOT NULL,
    name       TEXT NOT NULL,
    name_set   BOOLEAN NOT NULL,
    avatar     TEXT NOT NULL,
    avatar_url TEXT NOT NULL,
    avatar_set BOOLEAN NOT NULL,
    bridging_mode INTEGER NOT NULL
);
CREATE TABLE portal (
    dcid          TEXT,
    receiver      TEXT,
    other_user_id TEXT,
    type          INTEGER NOT NULL,
    dc_guild_id  TEXT,
    dc_parent_id TEXT,
    dc_parent_receiver TEXT NOT NULL DEFAULT '',
    mxid       TEXT UNIQUE,
    plain_name TEXT NOT NULL,
    name       TEXT NOT NULL,
    name_set   BOOLEAN NOT NULL,
    friend_nick BOOLEAN NOT NULL,
    topic      TEXT NOT NULL,
    topic_set  BOOLEAN NOT NULL,
    avatar     TEXT NOT NULL,
    avatar_url TEXT NOT NULL,
    avatar_set BOOLEAN NOT NULL,
    encrypted  BOOLEAN NOT NULL,
    in_space   TEXT NOT NULL,
    first_event_id TEXT NOT NULL,
    relay_webhook_id     TEXT,
    relay_webhook_secret TEXT,
    PRIMARY KEY (dcid, receiver)
);
CREATE TABLE message (
    dcid              TEXT,
    dc_attachment_id  TEXT,
    dc_chan_id        TEXT,
    dc_chan_receiver  TEXT,
    dc_sender         TEXT   NOT NULL,
    timestamp         BIGINT NOT NULL,
    dc_edit_timestamp BIGINT NOT NULL,
    dc_thread_id      TEXT   NOT NULL,
    mxid        TEXT NOT NULL UNIQUE,
    sender_mxid TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (dcid, dc_attachment_id, dc_chan_id, dc_chan_receiver)
);
CREATE TABLE user_portal (
    discord_id TEXT,
    user_mxid  TEXT,
    type       TEXT NOT NULL,
    in_space   BOOLEAN NOT NULL,
    timestamp  BIGINT NOT NULL,
    PRIMARY KEY (discord_id, user_mxid)
);
`

const secretToken = "the-discord-token-under-test"

// throwawayBridge writes a bridge database with one logged-in account, one
// bridged server (a category, two text channels, one announcement channel,
// a voice channel), and two servers the account is in but that are not
// bridged, then reopens it read-only the way the route does.
func throwawayBridge(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mautrix-discord.db")
	writer, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{bridgeSchema,
		`INSERT INTO "user" (mxid, dcid, discord_token, management_room) VALUES
			('@admin:x', '110', '` + secretToken + `', '!management:x'),
			('@someone-else:x', NULL, NULL, NULL)`,
		`INSERT INTO guild (dcid, mxid, plain_name, name, name_set, avatar, avatar_url, avatar_set, bridging_mode) VALUES
			('900', '!space:x', 'Riverside', 'Riverside', 1, '', '', 0, 3),
			('901', NULL, 'Other', 'Other', 1, '', '', 0, 0),
			('902', NULL, 'Another', 'Another', 1, '', '', 0, 0),
			('903', NULL, 'Left long ago', 'Left long ago', 1, '', '', 0, 0)`,
		`INSERT INTO user_portal (discord_id, user_mxid, type, in_space, timestamp) VALUES
			('900', '@admin:x', 'guild', 1, 0), ('901', '@admin:x', 'guild', 1, 0),
			('902', '@admin:x', 'guild', 1, 0), ('555', '@admin:x', 'dm', 1, 0)`,
	}
	portal := func(dcid string, channelType int, guild, parent, mxid, name string) string {
		mxidValue := "NULL"
		if mxid != "" {
			mxidValue = "'" + mxid + "'"
		}
		return `INSERT INTO portal (dcid, receiver, type, dc_guild_id, dc_parent_id, mxid, plain_name, name, name_set,
			friend_nick, topic, topic_set, avatar, avatar_url, avatar_set, encrypted, in_space, first_event_id)
			VALUES ('` + dcid + `', '', ` + strconv.Itoa(channelType) + `, '` + guild + `', '` + parent + `', ` + mxidValue + `, '` + name +
			`', '` + name + `', 1, 0, '', 0, '', '', 0, 0, '', '')`
	}
	statements = append(statements,
		portal("1000", 4, "900", "", "!category:x", "Text Channels"),
		portal("1001", 0, "900", "1000", "!general:x", "#general"),
		portal("1002", 0, "900", "1000", "!quiet:x", "#quiet"),
		portal("1003", 5, "900", "", "!news:x", "#announcements"),
		portal("1004", 2, "900", "", "!voice:x", "Voice"),
		portal("1005", 0, "900", "", "", "#not-yet-a-room"),
		portal("2001", 0, "901", "", "!other:x", "#elsewhere"),
		`INSERT INTO message (dcid, dc_attachment_id, dc_chan_id, dc_chan_receiver, dc_sender, timestamp, dc_edit_timestamp, dc_thread_id, mxid) VALUES
			('m1', '', '1001', '', '7', 1790000000000, 0, '', '$m1'),
			('m2', '', '1001', '', '7', 1790000060000, 0, '', '$m2')`,
	)
	for _, statement := range statements {
		if _, err := writer.Exec(statement); err != nil {
			t.Fatalf("%v\n%s", err, statement)
		}
	}
	writer.Close()
	bridge, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { bridge.Close() })
	return bridge
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestReadStatusReportsLoginServersAndMessageChannels(t *testing.T) {
	status, err := ReadStatus(throwawayBridge(t))
	if err != nil {
		t.Fatal(err)
	}

	if len(status.Accounts) != 2 {
		t.Fatalf("accounts = %+v, want two", status.Accounts)
	}
	admin := status.Accounts[0]
	if admin.MatrixUserID != "@admin:x" || admin.DiscordUserID != "110" || !admin.LoggedIn || admin.ManagementRoomID != "!management:x" {
		t.Errorf("admin account = %+v", admin)
	}
	if other := status.Accounts[1]; other.LoggedIn || other.DiscordUserID != "" {
		t.Errorf("account with no token = %+v, want logged out with no Discord id", other)
	}

	if len(status.BridgedServers) != 1 {
		t.Fatalf("bridged servers = %+v, want only Riverside", status.BridgedServers)
	}
	server := status.BridgedServers[0]
	if server.DiscordServerID != "900" || server.BridgingModeName != "everything" || server.MatrixSpaceID != "!space:x" {
		t.Errorf("server = %+v", server)
	}
	var names []string
	for _, channel := range server.Channels {
		names = append(names, channel.Name)
	}
	// Ordered by category then name; the category and the voice channel are
	// left out, the announcement channel and the channel with no room kept.
	if got, want := strings.Join(names, ","), "#announcements,#not-yet-a-room,#general,#quiet"; got != want {
		t.Errorf("channels = %s, want %s", got, want)
	}
	general := server.Channels[2]
	if general.CategoryName != "Text Channels" || general.MatrixRoomID != "!general:x" {
		t.Errorf("#general = %+v", general)
	}
	if general.LastBridgedMessageAt == nil || !general.LastBridgedMessageAt.Equal(time.UnixMilli(1790000060000)) {
		t.Errorf("#general last bridged message = %v, want the newer of its two", general.LastBridgedMessageAt)
	}
	if quiet := server.Channels[3]; quiet.LastBridgedMessageAt != nil {
		t.Errorf("#quiet has no messages but last bridged message = %v", quiet.LastBridgedMessageAt)
	}

	// 901 and 902 hold the account; 903 is known to the bridge but the
	// account is not in it; 555 is a DM.
	if status.UnbridgedServerCount != 2 {
		t.Errorf("unbridged servers = %d, want 2", status.UnbridgedServerCount)
	}

	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secretToken) {
		t.Fatal("the Discord token reached the status")
	}
}

func TestAnnotateMarksJoinedRoomsAndLogCounts(t *testing.T) {
	status, err := ReadStatus(throwawayBridge(t))
	if err != nil {
		t.Fatal(err)
	}
	last := time.Date(2026, 9, 28, 7, 0, 0, 0, time.UTC)
	status.Annotate([]string{"!general:x", "!news:x"}, map[string]db.ChannelLogCounts{
		"1001": {LoggedMessageCount: 5, DeletedMessageCount: 2, LastLoggedMessageAt: &last},
	})
	byName := map[string]Channel{}
	for _, channel := range status.BridgedServers[0].Channels {
		byName[channel.Name] = channel
	}
	if c := byName["#general"]; !c.AdminJoined || c.LoggedMessageCount != 5 || c.DeletedMessageCount != 2 || c.LastLoggedMessageAt != &last {
		t.Errorf("#general = %+v", c)
	}
	if c := byName["#quiet"]; c.AdminJoined || c.LoggedMessageCount != 0 || c.LastLoggedMessageAt != nil {
		t.Errorf("#quiet = %+v, want not joined and nothing logged", c)
	}
	if c := byName["#not-yet-a-room"]; c.AdminJoined {
		t.Errorf("a channel with no room cannot be joined: %+v", c)
	}
}

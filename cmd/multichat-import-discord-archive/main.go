// Command multichat-import-discord-archive adds messages from a Discord
// history fetch to multichat's message log, so the log reaches back before the
// live sync began.
//
// The input is JSONL: one raw Discord API message object per line. Each
// message sent before message_log_started_at becomes a row with source
// "discord-archive", its Discord message id, body = the Discord content
// (format "discord-markdown") and content_json = the whole Discord object.
// Messages from that moment on are skipped, because the live log already
// has them. Running it twice inserts nothing the second time.
//
// The Matrix room, server and channel come from the mautrix-discord bridge
// database, looked up by the Discord channel id.
//
//	multichat-import-discord-archive -db ~/repos/multichat/multichat.db \
//	  -discord-db ~/multichat/discord-data/mautrix-discord.db -archive messages.raw.jsonl
package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/discordbridge"
	"github.com/kayushkin/multichat/internal/matrix"
)

// discordMessage is the part of a Discord API message object the import reads.
type discordMessage struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
	Author    struct {
		ID         string  `json:"id"`
		Username   string  `json:"username"`
		GlobalName *string `json:"global_name"`
	} `json:"author"`
}

// bridgedChannel is a Discord channel's portal in the bridge database.
type bridgedChannel struct {
	RoomID      string
	ChannelName string
	ServerID    string
	ServerName  string
}

func main() {
	databasePath := flag.String("db", "", "multichat's SQLite database (the unit's MULTICHAT_DB)")
	discordDatabasePath := flag.String("discord-db", "", "the mautrix-discord bridge database (the unit's DISCORD_DB)")
	archivePath := flag.String("archive", "", "JSONL file of raw Discord message objects")
	flag.Parse()
	if *databasePath == "" || *discordDatabasePath == "" || *archivePath == "" {
		flag.Usage()
		os.Exit(2)
	}
	for _, path := range []string{*databasePath, *discordDatabasePath, *archivePath} {
		// SQLite creates a missing file; an import into a new empty
		// database would look like success.
		if _, err := os.Stat(path); err != nil {
			log.Fatal(err)
		}
	}
	database, err := db.Initialize(*databasePath)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()
	bridgeDatabase, err := discordbridge.Open(*discordDatabasePath)
	if err != nil {
		log.Fatal(err)
	}
	defer bridgeDatabase.Close()

	result, err := importArchive(database, bridgeDatabase, *archivePath)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("read %d messages: inserted %d, already in the log %d, sent after the live log began %d\n",
		result.Read, result.Inserted, result.AlreadyLogged, result.AfterLiveLog)
}

type importResult struct {
	Read, Inserted, AlreadyLogged, AfterLiveLog int
}

func importArchive(database, bridgeDatabase *sql.DB, archivePath string) (importResult, error) {
	var result importResult
	liveLogStartedAt, err := db.RecordedMessageLogStartedAt(database)
	if err != nil {
		return result, err
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return result, err
	}
	defer file.Close()
	type archivedRow struct {
		line int
		row  db.LoggedMessage
	}
	channels := map[string]bridgedChannel{}
	var rows []archivedRow
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 64<<20)
	line := 0
	for scanner.Scan() {
		line++
		raw := scanner.Bytes()
		if len(strings.TrimSpace(string(raw))) == 0 {
			continue
		}
		var message discordMessage
		if err := json.Unmarshal(raw, &message); err != nil {
			return result, fmt.Errorf("line %d: %w", line, err)
		}
		result.Read++
		// The scanner reuses its buffer; the row keeps its own copy.
		row, err := archiveRow(bridgeDatabase, channels, message, append([]byte(nil), raw...))
		if err != nil {
			return result, fmt.Errorf("line %d (message %s): %w", line, message.ID, err)
		}
		if !row.SentAt.Before(liveLogStartedAt) {
			result.AfterLiveLog++
			continue
		}
		rows = append(rows, archivedRow{line, row})
	}
	if err := scanner.Err(); err != nil {
		return result, fmt.Errorf("read %s: %w", archivePath, err)
	}
	// Oldest first, so row ids follow the order messages were sent (a
	// history fetch lists newest first).
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].row.SentAt.Before(rows[j].row.SentAt) })
	for _, r := range rows {
		inserted, err := db.LogArchivedDiscordMessage(database, r.row)
		if err != nil {
			return result, fmt.Errorf("line %d: %w", r.line, err)
		}
		if inserted {
			result.Inserted++
		} else {
			result.AlreadyLogged++
		}
	}
	return result, nil
}

func archiveRow(bridgeDatabase *sql.DB, channels map[string]bridgedChannel, message discordMessage, raw []byte) (db.LoggedMessage, error) {
	if message.ID == "" || message.ChannelID == "" || message.Author.ID == "" {
		return db.LoggedMessage{}, fmt.Errorf("a Discord message needs id, channel_id and author.id")
	}
	sentAt, err := time.Parse(time.RFC3339Nano, message.Timestamp)
	if err != nil {
		return db.LoggedMessage{}, fmt.Errorf("timestamp: %w", err)
	}
	channel, ok := channels[message.ChannelID]
	if !ok {
		channel, err = lookUpChannel(bridgeDatabase, message.ChannelID)
		if err != nil {
			return db.LoggedMessage{}, err
		}
		channels[message.ChannelID] = channel
	}
	// The puppet the bridge makes for a Discord user, on the homeserver
	// that holds the portal room.
	homeserver := channel.RoomID[strings.Index(channel.RoomID, ":")+1:]
	senderUserID := "@discord_" + message.Author.ID + ":" + homeserver
	platform := matrix.PlatformOfUserID(senderUserID)
	if platform == "" {
		return db.LoggedMessage{}, fmt.Errorf("%s is not a bridge puppet id", senderUserID)
	}
	displayName := message.Author.Username
	if message.Author.GlobalName != nil && *message.Author.GlobalName != "" {
		displayName = *message.Author.GlobalName
	}
	return db.LoggedMessage{
		DiscordMessageID:  message.ID,
		RoomID:            channel.RoomID,
		RoomName:          channel.ChannelName,
		Platform:          platform,
		NetworkID:         channel.ServerID,
		NetworkName:       channel.ServerName,
		ChannelID:         message.ChannelID,
		ChannelName:       channel.ChannelName,
		SenderUserID:      senderUserID,
		SenderDisplayName: displayName,
		Body:              message.Content,
		Format:            db.MessageFormatDiscordMarkdown,
		ContentJSON:       string(raw),
		SentAt:            sentAt,
	}, nil
}

// lookUpChannel finds a server channel's portal by its Discord id. A server
// channel's portal has no receiver (only a DM's does).
func lookUpChannel(bridgeDatabase *sql.DB, channelID string) (bridgedChannel, error) {
	var channel bridgedChannel
	var roomID sql.NullString
	err := bridgeDatabase.QueryRow(`SELECT portal.mxid, portal.name, guild.dcid, guild.name
		FROM portal JOIN guild ON guild.dcid = portal.dc_guild_id
		WHERE portal.dcid = ? AND portal.receiver = ''`, channelID).
		Scan(&roomID, &channel.ChannelName, &channel.ServerID, &channel.ServerName)
	if err == sql.ErrNoRows {
		return channel, fmt.Errorf("Discord channel %s has no server portal in the bridge database", channelID)
	}
	if err != nil {
		return channel, fmt.Errorf("look up Discord channel %s: %w", channelID, err)
	}
	if !roomID.Valid || !strings.Contains(roomID.String, ":") {
		return channel, fmt.Errorf("Discord channel %s has no Matrix room yet", channelID)
	}
	channel.RoomID = roomID.String
	return channel, nil
}

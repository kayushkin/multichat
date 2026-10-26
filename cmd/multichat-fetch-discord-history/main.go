// Command multichat-fetch-discord-history reads the history of a bridged
// Discord server's text channels through the bridge's logged-in account, at a
// walking pace, and writes it for cmd/multichat-import-discord-archive:
//
//	<out>/<server id>/<channel id>.jsonl   one raw Discord message per line
//	<out>/<server id>/images/…             each image attachment, with -images
//	<out>/<server id>/images.jsonl         which message each image came from
//
// -direct-message fetches a DM or group DM the account is in, by channel id,
// into <out>/direct-messages/<channel id>.jsonl (and its images beside it);
// it needs no server and no bridged room.
//
// -channel limits the fetch to named channel ids. A channel whose history is complete gets <channel id>.done and is skipped on
// the next run. The account's login is read from the bridge database, held in
// memory, and sent only to discord.com.
//
//	multichat-fetch-discord-history -discord-db ~/multichat/discord-data/mautrix-discord.db \
//	  -server 9300000000000000003 -out ~/.local/share/life-archive/discord -images
package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/kayushkin/multichat/internal/discordbridge"
	"github.com/kayushkin/multichat/internal/discordemoji"
	"github.com/kayushkin/multichat/internal/discordhistory"
)

type discordIDList []string

func (s *discordIDList) String() string      { return strings.Join(*s, ",") }
func (s *discordIDList) Set(id string) error { *s = append(*s, id); return nil }

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func main() {
	discordDatabasePath := flag.String("discord-db", "", "the mautrix-discord bridge database (the unit's DISCORD_DB)")
	var servers discordIDList
	flag.Var(&servers, "server", "a bridged Discord server id; repeat for more")
	var onlyChannels discordIDList
	var directMessages discordIDList
	flag.Var(&directMessages, "direct-message", "a DM or group DM channel id to fetch; repeat for more")
	flag.Var(&onlyChannels, "channel", "fetch only this channel id of the servers; repeat for more (default: every text channel)")
	out := flag.String("out", "", "where to write the history")
	pause := flag.Duration("pause", 3*time.Second, "wait before every request to Discord")
	images := flag.Bool("images", false, "also save each image attachment")
	flag.Parse()
	if *discordDatabasePath == "" || len(servers)+len(directMessages) == 0 || *out == "" || *pause < time.Second {
		flag.Usage()
		os.Exit(2)
	}
	if _, err := os.Stat(*discordDatabasePath); err != nil {
		log.Fatal(err)
	}
	bridgeDatabase, err := discordbridge.Open(*discordDatabasePath)
	if err != nil {
		log.Fatal(err)
	}
	defer bridgeDatabase.Close()
	_, token, err := discordemoji.ReadDiscordLogin(bridgeDatabase)
	if err != nil {
		log.Fatal(err)
	}
	fetcher := &discordhistory.Fetcher{APIBaseURL: discordemoji.DiscordAPIBaseURL, Token: token,
		Client: &http.Client{Timeout: time.Minute}, Pause: *pause, MaximumRateLimitWaits: 10}
	for _, server := range servers {
		if err := fetchServer(bridgeDatabase, fetcher, server, *out, *images, onlyChannels); err != nil {
			log.Fatalf("server %s: %v", server, err)
		}
	}
	if len(directMessages) > 0 {
		var channels []channel
		for _, id := range directMessages {
			channels = append(channels, channel{id: id, name: id})
		}
		if err := fetchChannels(fetcher, directMessagesFolder, channels, *out, *images); err != nil {
			log.Fatalf("direct messages: %v", err)
		}
	}
}

// directMessagesFolder is where DMs and group DMs go under -out, in place of
// a server id.
const directMessagesFolder = "direct-messages"

type channel struct{ id, name string }

func fetchServer(bridgeDatabase *sql.DB, fetcher *discordhistory.Fetcher, server, out string, images bool, onlyChannels []string) error {
	rows, err := bridgeDatabase.Query(`SELECT dcid, name FROM portal WHERE dc_guild_id = ? AND type IN (0, 5) ORDER BY name`, server)
	if err != nil {
		return err
	}
	var channels []channel
	for rows.Next() {
		var c channel
		if err := rows.Scan(&c.id, &c.name); err != nil {
			rows.Close()
			return err
		}
		if len(onlyChannels) > 0 && !slices.Contains(onlyChannels, c.id) {
			continue
		}
		channels = append(channels, c)
	}
	rows.Close()
	if len(channels) == 0 {
		return fmt.Errorf("the bridge has no matching text channels for this server; bridge it first (guilds bridge %s --entire), and check -channel", server)
	}
	return fetchChannels(fetcher, server, channels, out, images)
}

// fetchChannels writes each channel's history to <out>/<folder>/<channel id>.jsonl,
// skipping a channel already marked done. folder is a server id, or
// directMessagesFolder.
func fetchChannels(fetcher *discordhistory.Fetcher, folder string, channels []channel, out string, images bool) error {
	server := folder
	directory := filepath.Join(out, folder)
	if err := os.MkdirAll(filepath.Join(directory, "images"), 0o700); err != nil {
		return err
	}
	manifest, err := os.OpenFile(filepath.Join(directory, "images.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer manifest.Close()
	for _, c := range channels {
		done := filepath.Join(directory, c.id+".done")
		if _, err := os.Stat(done); err == nil {
			fmt.Printf("%s #%s: already complete\n", server, c.name)
			continue
		}
		file, err := os.OpenFile(filepath.Join(directory, c.id+".jsonl"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		writer := bufio.NewWriter(file)
		saved := 0
		onMessage := func(messageID string, attachments []discordhistory.Attachment) error {
			if !images {
				return nil
			}
			for _, attachment := range attachments {
				if !strings.HasPrefix(attachment.ContentType, "image/") {
					continue
				}
				content, err := fetcher.Download(attachment)
				if errors.Is(err, discordhistory.ErrNoAccess) {
					fmt.Printf("    image %s of message %s is gone; skipped\n", attachment.ID, messageID)
					continue
				}
				if err != nil {
					return err
				}
				name := messageID + "_" + attachment.ID + "_" + unsafeName.ReplaceAllString(attachment.Filename, "_")
				if err := os.WriteFile(filepath.Join(directory, "images", name), content, 0o600); err != nil {
					return err
				}
				line, _ := json.Marshal(map[string]string{"server_id": server, "channel_id": c.id, "message_id": messageID,
					"attachment_id": attachment.ID, "filename": attachment.Filename, "content_type": attachment.ContentType, "path": "images/" + name})
				if _, err := manifest.Write(append(line, '\n')); err != nil {
					return err
				}
				saved++
			}
			return nil
		}
		result, err := fetcher.FetchChannel(c.id, "", writer, onMessage)
		if flushErr := writer.Flush(); err == nil {
			err = flushErr
		}
		file.Close()
		if errors.Is(err, discordhistory.ErrNoAccess) {
			fmt.Printf("%s #%s: the account cannot read it; skipped\n", server, c.name)
			continue
		}
		if err != nil {
			return fmt.Errorf("#%s: %w (its file holds the %d messages read so far)", c.name, err, result.Messages)
		}
		if err := os.WriteFile(done, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
			return err
		}
		fmt.Printf("%s #%s: %d messages in %d pages, %d images\n", server, c.name, result.Messages, result.Pages, saved)
	}
	return nil
}

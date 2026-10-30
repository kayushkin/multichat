package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/kayushkin/multichat/internal/discordbridge"
	"github.com/kayushkin/multichat/internal/matrix"
)

// errDiscordDirectMessageHasNoRoom says a send cannot reach a Discord DM:
// the bridge knows no DM with the person, or no bridge config is set to ask
// it for a room. It answers 409, since nothing was sent.
var errDiscordDirectMessageHasNoRoom = errors.New("discord direct message has no room")

// discordBridgeWait is how long the bridge bot gets to join a new room, and
// then to take it.
const discordBridgeWait = 15 * time.Second

// discordDirectMessageRoom returns the room of the one-to-one Discord DM with
// a Discord puppet, by the bridge's own record of it, never by guessing from
// a room's members: a room the bridge does not carry would take the message
// and Discord would never see it. When the bridge knows the DM but has made
// no room for it — it makes rooms for few DMs at start — it opens one: a new
// room with the person and the bot, and the bot's bridge command naming the
// DM's channel. opened says whether it did.
func (r *Router) discordDirectMessageRoom(puppetUserID string) (roomID string, opened bool, err error) {
	discordUserID := strings.TrimPrefix(strings.SplitN(puppetUserID, ":", 2)[0], "@discord_")
	bridge, err := discordbridge.Open(r.discordDBPath)
	if err != nil {
		return "", false, err
	}
	defer bridge.Close()
	portal, found, err := discordbridge.DirectMessagePortalWith(bridge, discordUserID)
	if err != nil {
		return "", false, fmt.Errorf("read the Discord DM with %s from DISCORD_DB: %w", puppetUserID, err)
	}
	if !found {
		return "", false, fmt.Errorf("%w: the Discord bridge knows no DM with %s; send them the first message from Discord", errDiscordDirectMessageHasNoRoom, puppetUserID)
	}
	if portal.RoomID != "" {
		return portal.RoomID, false, nil
	}
	if r.discordConfigPath == "" {
		return "", false, fmt.Errorf("%w: the Discord DM with %s has no room, and DISCORD_CONFIG is unset, so the bridge bot cannot be asked for one", errDiscordDirectMessageHasNoRoom, puppetUserID)
	}
	bridgeConfig, err := discordbridge.ReadBridgeConfig(r.discordConfigPath)
	if err != nil {
		return "", false, err
	}
	roomID, err = r.matrix.CreateDM(puppetUserID)
	if err != nil {
		return "", false, fmt.Errorf("create a room for the Discord DM with %s: %w", puppetUserID, err)
	}
	if err := r.matrix.InviteUser(roomID, bridgeConfig.BotUserID); err != nil {
		return "", false, fmt.Errorf("invite %s to %s: %w", bridgeConfig.BotUserID, roomID, err)
	}
	if err := waitFor(func() (bool, error) {
		members, err := r.matrix.JoinedMemberIDs(roomID)
		return slices.Contains(members, bridgeConfig.BotUserID), err
	}); err != nil {
		return "", false, fmt.Errorf("%s did not join %s: %w", bridgeConfig.BotUserID, roomID, err)
	}
	commandEventID, err := r.matrix.SendMessage(roomID, bridgeConfig.CommandPrefix+" bridge "+portal.ChannelID)
	if err != nil {
		return "", false, fmt.Errorf("ask %s to bridge %s to Discord channel %s: %w", bridgeConfig.BotUserID, roomID, portal.ChannelID, err)
	}
	if err := waitFor(func() (bool, error) {
		bridge, err := discordbridge.Open(r.discordDBPath)
		if err != nil {
			return false, err
		}
		defer bridge.Close()
		now, _, err := discordbridge.DirectMessagePortalWith(bridge, discordUserID)
		return now.RoomID == roomID, err
	}); err != nil {
		return "", false, fmt.Errorf("the Discord bridge did not take %s for channel %s: %w", roomID, portal.ChannelID, err)
	}
	// The command and the bot's answer to it are not messages to the person;
	// they go from the thread, so the chat opens on what was said. Both came
	// before the room carried the DM, so Discord never saw them.
	page, err := r.matrix.GetMessagePage(roomID, 20, "")
	if err != nil {
		return "", false, fmt.Errorf("read the bridge's answer in %s: %w", roomID, err)
	}
	for _, eventID := range bridgeCommandAndAnswer(page.Messages, commandEventID, bridgeConfig.BotUserID) {
		if _, err := r.matrix.Redact(roomID, eventID); err != nil {
			return "", false, fmt.Errorf("redact %s in %s: %w", eventID, roomID, err)
		}
	}
	return roomID, true, nil
}

// bridgeCommandAndAnswer returns the bridge command and every message the bot
// sent after it, from a room's messages oldest first.
func bridgeCommandAndAnswer(messages []matrix.Message, commandEventID, botUserID string) []string {
	eventIDs := []string{}
	afterCommand := false
	for _, message := range messages {
		if message.EventID == commandEventID {
			afterCommand = true
			eventIDs = append(eventIDs, message.EventID)
			continue
		}
		if afterCommand && message.Sender == botUserID {
			eventIDs = append(eventIDs, message.EventID)
		}
	}
	if !afterCommand {
		eventIDs = append(eventIDs, commandEventID)
	}
	return eventIDs
}

// waitFor asks done every half second until it says yes, fails, or
// discordBridgeWait passes.
func waitFor(done func() (bool, error)) error {
	deadline := time.Now().Add(discordBridgeWait)
	for {
		ok, err := done()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("gave up after %s", discordBridgeWait)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// isDiscordPuppet reports whether a user id is a Discord bridge puppet.
func isDiscordPuppet(userID string) bool {
	return matrix.PlatformOfUserID(userID) == "discord"
}

// openDiscordDirectMessageHandler serves POST /discord/direct-messages/open
// {"user_id":"@discord_…"}: the room of the one-to-one Discord DM with that
// puppet, opened through the bridge bot when the bridge has none, and whether
// it was opened. Nothing is sent to the person. A DM the bridge does not know
// is a 409.
func (r *Router) openDiscordDirectMessageHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	if r.matrix == nil || r.discordDBPath == "" {
		r.writeError(w, 503, "Matrix client or DISCORD_DB not configured")
		return
	}
	var body struct {
		UserID string `json:"user_id"`
	}
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		r.writeError(w, 400, `body must be JSON {"user_id":"@discord_…"}: `+err.Error())
		return
	}
	if !isDiscordPuppet(body.UserID) {
		r.writeError(w, 400, body.UserID+" is not a Discord puppet")
		return
	}
	roomID, opened, err := r.discordDirectMessageRoom(body.UserID)
	if errors.Is(err, errDiscordDirectMessageHasNoRoom) {
		r.writeError(w, 409, err.Error())
		return
	}
	if err != nil {
		r.writeError(w, 502, err.Error())
		return
	}
	r.writeJSON(w, map[string]any{"room_id": roomID, "opened": opened})
}

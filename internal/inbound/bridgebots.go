package inbound

import (
	"errors"
	"log"
	"time"

	"github.com/kayushkin/multichat/internal/matrix"
)

// A bridge relays what we send from the platform's own app (not from here)
// through a puppet of our own platform account. mautrix-discord has its bot
// invite that puppet into the room before it posts, and creates some
// direct-message rooms without the bot; there the invite is refused and the
// bridge drops the message: on 2026-09-29 seven of the operator's Discord
// messages to one person were lost this way. So the worker checks each room
// it is in and invites the bot where a bridge in
// bridgeProtocolsThatDropOurMessagesWithoutTheirBot is missing it.

// bridgeProtocolsThatDropOurMessagesWithoutTheirBot holds the m.bridge
// protocol ids of the bridges seen dropping our messages in a room their bot
// is not in. Only these get their bot invited. The pinned Python
// mautrix-telegram, for one, leaves its bot out of some rooms with no harm,
// and treats a room holding only the bot and us as a room for bridge
// commands: inviting it there on 2026-10-20 turned the operator's Telegram
// Saved Messages room into one. Add a protocol only on evidence of a drop
// ("All parts of message failed to send to Matrix" in that bridge's log).
var bridgeProtocolsThatDropOurMessagesWithoutTheirBot = map[string]bool{
	"discordgo": true,
}

// bridgeBotQueueSize bounds the rooms waiting for a check. Startup queues
// every joined room; a full queue blocks whoever queues the next one.
const bridgeBotQueueSize = 4096

// queueBridgeBotCheck asks the bridge bot checker to look at a room.
func (w *SyncWorker) queueBridgeBotCheck(roomID string) {
	w.bridgeBotChecks <- roomID
}

// checkBridgeBots runs forever, checking each queued room in turn. It has a
// goroutine of its own because Synapse lets a user receive only a few
// invites at once, and waiting that out must not hold up the sync.
func (w *SyncWorker) checkBridgeBots() {
	for roomID := range w.bridgeBotChecks {
		if err := w.inviteMissingBridgeBots(roomID); err != nil {
			log.Printf("inbound sync: WARNING bridge bots of %s: %v", roomID, err)
		}
	}
}

// inviteMissingBridgeBots invites each bridge bot a room's m.bridge state
// names that has no membership in the room or has left it, when its bridge is
// in bridgeProtocolsThatDropOurMessagesWithoutTheirBot. A bot already invited
// is reported and left: inviting it again would not make it join.
func (w *SyncWorker) inviteMissingBridgeBots(roomID string) error {
	bots, err := w.Matrix.RoomBridgeBots(roomID)
	if err != nil {
		return err
	}
	for _, bot := range bots {
		if !bridgeProtocolsThatDropOurMessagesWithoutTheirBot[bot.ProtocolID] {
			continue
		}
		switch bot.Membership {
		case "join":
			continue
		case "invite":
			log.Printf("inbound sync: WARNING %s is invited to %s but has not joined", bot.BotUserID, roomID)
			continue
		case "ban":
			log.Printf("inbound sync: WARNING %s is banned from %s; not inviting it", bot.BotUserID, roomID)
			continue
		}
		err := w.Matrix.InviteUser(roomID, bot.BotUserID)
		var limited *matrix.RateLimitedError
		for errors.As(err, &limited) {
			time.Sleep(limited.RetryAfter)
			err = w.Matrix.InviteUser(roomID, bot.BotUserID)
		}
		if err != nil {
			return err
		}
		log.Printf("inbound sync: invited %s to %s, which it was missing from", bot.BotUserID, roomID)
	}
	return nil
}

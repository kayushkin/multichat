package matrix

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Reading the conversation list from Synapse takes three or four requests per
// joined room, which for a few hundred rooms is many seconds. So the list is
// read once and then kept current by /sync: a room with a new message, name,
// member or bridge state is read again, and a room we left is dropped.

// conversationReadWorkers is how many rooms are read from Synapse at once.
const conversationReadWorkers = 8

// conversationSyncTimeout is how long Synapse holds each /sync open.
const conversationSyncTimeout = 30 * time.Second

// conversationChangeTypes are the events that change what the list shows of
// a room: its last message (a new one, an edit, a deletion), its name, its
// members, and its bridge state.
var conversationChangeTypes = []string{"m.room.message", "m.room.redaction", "m.room.name", "m.room.member", "m.bridge"}

// conversationSyncFilter asks only whether a room had any of those events;
// one event is enough to know it must be read again.
var conversationSyncFilter = func() string {
	types, _ := json.Marshal(conversationChangeTypes)
	return url.QueryEscape(`{"room":{"timeline":{"types":` + string(types) + `,"limit":1},"state":{"types":` + string(types) + `},"ephemeral":{"types":[]},"account_data":{"types":[]}},"presence":{"types":[]},"account_data":{"types":[]}}`)
}()

// noRoomsSyncFilter returns only a token: where the next /sync starts.
var noRoomsSyncFilter = url.QueryEscape(`{"room":{"rooms":[]},"presence":{"types":[]},"account_data":{"types":[]}}`)

type conversationCache struct {
	startOnce sync.Once
	// ready closes when every joined room has been read once.
	ready chan struct{}

	mutex sync.Mutex
	rooms map[string]listedRoom
	// failed holds each room the last read of failed, with why; every pass
	// of the sync loop reads them again.
	failed map[string]error
}

// ListConversations returns the rooms the user has joined, newest activity
// first, from the cache that /sync keeps current. The first call waits for
// the first read of every room. discordPortals, from the Discord bridge's
// database, gives each Discord room its channel id and names its DMs, which
// that bridge does not mark in m.bridge; the mautrix-go bridges mark theirs
// with com.beeper.room_type "dm".
func (c *Client) ListConversations(metaPlatforms MetaPlatformMap, discordPortals map[string]DiscordPortal) ([]Conversation, error) {
	c.KeepConversationsCurrent()
	cache := &c.conversations
	<-cache.ready

	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if len(cache.failed) > 0 {
		return nil, cache.failedError()
	}
	convos := make([]Conversation, 0, len(cache.rooms))
	for _, room := range cache.rooms {
		convos = append(convos, room.finish(metaPlatforms, discordPortals))
	}
	nameGoogleMessagesGroups(convos)
	sort.Slice(convos, func(i, j int) bool {
		return convos[i].LastActivity > convos[j].LastActivity
	})
	return convos, nil
}

// KeepConversationsCurrent starts, once per client, the read of every room
// and the /sync loop that keeps the conversation list current. It returns at
// once; ListConversations waits for the first read.
func (c *Client) KeepConversationsCurrent() {
	cache := &c.conversations
	cache.startOnce.Do(func() {
		cache.ready = make(chan struct{})
		cache.rooms = map[string]listedRoom{}
		cache.failed = map[string]error{}
		go c.runConversationCache()
	})
}

func (cache *conversationCache) failedError() error {
	roomIDs := make([]string, 0, len(cache.failed))
	for roomID := range cache.failed {
		roomIDs = append(roomIDs, roomID)
	}
	sort.Strings(roomIDs)
	reasons := make([]string, 0, len(roomIDs))
	for _, roomID := range roomIDs {
		reasons = append(reasons, cache.failed[roomID].Error())
	}
	return fmt.Errorf("%d rooms could not be read from Synapse (retried on every sync): %s", len(roomIDs), strings.Join(reasons, "; "))
}

func (c *Client) runConversationCache() {
	cache := &c.conversations
	// Take the token before the first read, so a change made while the rooms
	// are being read is still seen by the first /sync.
	since := ""
	for {
		var err error
		since, err = c.syncToken()
		if err == nil {
			break
		}
		log.Printf("conversation cache: %v (retrying in 5s)", err)
		time.Sleep(5 * time.Second)
	}
	for {
		joined, err := c.joinedRoomIDs()
		if err == nil {
			started := time.Now()
			c.readConversations(joined)
			log.Printf("conversation cache: read %d joined rooms in %s", len(joined), time.Since(started).Round(time.Millisecond))
			break
		}
		log.Printf("conversation cache: %v (retrying in 5s)", err)
		time.Sleep(5 * time.Second)
	}
	close(cache.ready)

	for {
		changed, left, next, err := c.syncConversationChanges(since)
		if err != nil {
			log.Printf("conversation cache: %v (retrying in 5s)", err)
			time.Sleep(5 * time.Second)
			continue
		}
		since = next
		cache.mutex.Lock()
		for _, roomID := range left {
			delete(cache.rooms, roomID)
			delete(cache.failed, roomID)
		}
		for roomID := range cache.failed {
			changed = append(changed, roomID)
		}
		cache.mutex.Unlock()
		if len(changed) > 0 {
			c.readConversations(changed)
		}
	}
}

// readConversations reads each room from Synapse into the cache.
func (c *Client) readConversations(roomIDs []string) {
	cache := &c.conversations
	work := make(chan string)
	var wait sync.WaitGroup
	for range conversationReadWorkers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for roomID := range work {
				room, listed, err := c.conversationOf(roomID)
				cache.mutex.Lock()
				switch {
				case err != nil:
					log.Printf("conversation cache: read %s: %v", roomID, err)
					cache.failed[roomID] = err
				case listed:
					cache.rooms[roomID] = room
					delete(cache.failed, roomID)
				default:
					delete(cache.rooms, roomID)
					delete(cache.failed, roomID)
				}
				cache.mutex.Unlock()
			}
		}()
	}
	for _, roomID := range uniqueStrings(roomIDs) {
		work <- roomID
	}
	close(work)
	wait.Wait()
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func (c *Client) syncToken() (string, error) {
	data, err := c.request("GET", "/_matrix/client/v3/sync?timeout=0&filter="+noRoomsSyncFilter, nil)
	if err != nil {
		return "", err
	}
	var resp struct {
		NextBatch string `json:"next_batch"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("sync token: %w", err)
	}
	if resp.NextBatch == "" {
		return "", fmt.Errorf("sync token: empty next_batch")
	}
	return resp.NextBatch, nil
}

func (c *Client) joinedRoomIDs() ([]string, error) {
	data, err := c.request("GET", "/_matrix/client/v3/joined_rooms", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		JoinedRooms []string `json:"joined_rooms"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("joined_rooms: %w", err)
	}
	return resp.JoinedRooms, nil
}

// syncConversationChanges long-polls /sync from since and returns the joined
// rooms with any event in conversationChangeTypes, and the rooms we left.
func (c *Client) syncConversationChanges(since string) (changed, left []string, next string, err error) {
	path := "/_matrix/client/v3/sync?filter=" + conversationSyncFilter +
		"&timeout=" + strconv.Itoa(int(conversationSyncTimeout/time.Millisecond)) + "&since=" + url.QueryEscape(since)
	data, err := c.request("GET", path, nil)
	if err != nil {
		return nil, nil, "", err
	}
	var resp struct {
		NextBatch string `json:"next_batch"`
		Rooms     struct {
			Join map[string]struct {
				Timeline struct {
					Events []json.RawMessage `json:"events"`
				} `json:"timeline"`
				State struct {
					Events []json.RawMessage `json:"events"`
				} `json:"state"`
			} `json:"join"`
			Leave map[string]json.RawMessage `json:"leave"`
		} `json:"rooms"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, nil, "", fmt.Errorf("conversation sync: %w", err)
	}
	if resp.NextBatch == "" {
		return nil, nil, "", fmt.Errorf("conversation sync: empty next_batch")
	}
	for roomID, room := range resp.Rooms.Join {
		if len(room.Timeline.Events) > 0 || len(room.State.Events) > 0 {
			changed = append(changed, roomID)
		}
	}
	for roomID := range resp.Rooms.Leave {
		left = append(left, roomID)
	}
	return changed, left, resp.NextBatch, nil
}

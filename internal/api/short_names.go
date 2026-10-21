package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/discordbridge"
	"github.com/kayushkin/multichat/internal/shortnames"
)

// shortNamerMaxTokens caps one model reply: 40 names of a path and a few
// words each come to about 3,000 tokens.
const shortNamerMaxTokens = 12000

// assignShortNamesHandler serves POST /short-names/assign[?dry_run=true]: it
// names every conversation with no current name in short-name-store's
// namespace messages, and answers what it named, refused and skipped. A dry
// run writes nothing. A second call while one runs is a 409.
func (r *Router) assignShortNamesHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	dryRun := false
	switch req.URL.Query().Get("dry_run") {
	case "", "false":
	case "true":
		dryRun = true
	default:
		r.writeError(w, 400, "dry_run must be true or false")
		return
	}
	var missing []string
	if r.matrix == nil {
		missing = append(missing, "SYNAPSE_URL and MATRIX_TOKEN")
	}
	if r.shortNameStoreURL == "" {
		missing = append(missing, "SHORT_NAME_STORE_URL")
	}
	if r.discordDBPath == "" {
		missing = append(missing, "DISCORD_DB")
	}
	if !r.people.Configured() {
		missing = append(missing, "PEOPLE_STORE_URL")
	}
	if r.llmBridgeURL == "" {
		missing = append(missing, "LLM_BRIDGE_URL")
	}
	if len(missing) > 0 {
		r.writeError(w, 503, "short names need "+strings.Join(missing, ", ")+", which are not set")
		return
	}

	inputs, err := r.shortNameInputs()
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	result, err := r.shortNameAssigner.Run(req.Context(), inputs, dryRun)
	if errors.Is(err, shortnames.ErrRunInProgress) {
		r.writeError(w, 409, err.Error())
		return
	}
	if err != nil {
		// What the run did before it stopped is part of the answer: names
		// written before the failure are in short-name-store for good.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(502)
		r.writeJSON(w, map[string]any{"error": err.Error(), "result": result})
		return
	}
	r.writeJSON(w, result)
}

// shortNameInputs gathers the facts the pass names from: the conversations,
// their links, their message counts, the Discord bridge's server channels and
// people-store's names for linked accounts.
func (r *Router) shortNameInputs() (shortnames.Inputs, error) {
	conversations, err := r.listConversations()
	if err != nil {
		return shortnames.Inputs{}, err
	}
	links, err := db.ListConversationLinks(r.db)
	if err != nil {
		return shortnames.Inputs{}, err
	}
	counts, err := db.MessageCountsByRoom(r.db)
	if err != nil {
		return shortnames.Inputs{}, err
	}

	bridge, err := discordbridge.Open(r.discordDBPath)
	if err != nil {
		return shortnames.Inputs{}, err
	}
	channelRooms, err := discordbridge.ServerChannelRooms(bridge)
	bridge.Close()
	if err != nil {
		return shortnames.Inputs{}, err
	}
	discordChannelByRoomID := map[string]shortnames.DiscordChannel{}
	for roomID, channel := range channelRooms {
		discordChannelByRoomID[roomID] = shortnames.DiscordChannel{ServerID: channel.DiscordServerID,
			ServerName: channel.ServerName, ChannelName: channel.ChannelName, CategoryName: channel.CategoryName}
	}

	identities, err := r.people.MatrixUserIdentities()
	if err != nil {
		return shortnames.Inputs{}, err
	}
	persons, err := r.people.Persons()
	if err != nil {
		return shortnames.Inputs{}, err
	}
	personNameByUserID := map[string]string{}
	for userID, identity := range identities {
		if person, ok := persons[identity.PersonID]; ok && person.DeletedAt == 0 {
			personNameByUserID[userID] = person.DisplayName
		}
	}

	inputs := shortnames.Inputs{
		Rooms:         shortnames.RoomsFromConversations(conversations, personNameByUserID, discordChannelByRoomID),
		MessageCounts: counts,
	}
	// The pass skips a room with no logged messages before it looks at this
	// mark, so only the rooms it may name cost a Synapse request.
	for i := range inputs.Rooms {
		if counts[inputs.Rooms[i].RoomID] == 0 {
			continue
		}
		excluded, err := r.matrix.ExcludedFromTimeline(inputs.Rooms[i].RoomID)
		if err != nil {
			return shortnames.Inputs{}, err
		}
		inputs.Rooms[i].ExcludedFromTimeline = excluded
	}
	for _, link := range links {
		inputs.Links = append(inputs.Links, link.RoomIDs)
	}
	return inputs, nil
}

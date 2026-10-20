// Package shortnames chooses short names for chats, the typeable paths under
// dash's /messages page (messages/kim, messages/nyc, messages/social/general),
// and writes them to short-name-store, which owns them.
//
// multichat owns the facts about rooms, so the pass lives here: it gathers the
// rooms with no current name, most messaged first, asks a model for a path for
// each, checks every proposal in Go, and writes the ones that pass. A refused
// room is reported and waits for the next run; Go never makes up a name of its
// own. A room that has a current name is never renamed.
package shortnames

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// BatchSize is how many rooms one model call names.
const BatchSize = 40

// ErrRunInProgress is a second run asked for while one is going.
var ErrRunInProgress = errors.New("a short-name run is already going")

// Inputs are the facts a run names from.
type Inputs struct {
	Rooms []Room
	// Links are the conversation links, each the room ids of one link.
	Links [][]string
	// MessageCounts is the logged messages of each room, edits left out.
	MessageCounts map[string]int
}

// Named is one name the run chose: written, or in a dry run only proposed.
type Named struct {
	Path          string   `json:"path"`
	EntityID      string   `json:"entity_id"`
	LinkedRoomIDs []string `json:"linked_room_ids,omitempty"`
	RoomName      string   `json:"room_name"`
	Platform      string   `json:"platform"`
	Kind          string   `json:"kind"`
	MessageCount  int      `json:"message_count"`
	Reason        string   `json:"reason"`
	// ShortNameID is short-name-store's id; "" in a dry run.
	ShortNameID string `json:"short_name_id,omitempty"`
}

// Refusal is a room the run left unnamed, and why.
type Refusal struct {
	EntityID     string `json:"entity_id"`
	RoomName     string `json:"room_name"`
	Platform     string `json:"platform"`
	Kind         string `json:"kind"`
	MessageCount int    `json:"message_count"`
	ProposedPath string `json:"proposed_path"`
	Why          string `json:"why"`
}

// ServerShortName is the first segment of every name of one Discord server's
// channels.
type ServerShortName struct {
	ServerID   string `json:"server_id"`
	ServerName string `json:"server_name"`
	ShortName  string `json:"short_name"`
	// Source is "existing" when a named channel already carries it, and
	// "proposed" when this run chose it.
	Source string `json:"source"`
}

// Result is what a run did.
type Result struct {
	DryRun           bool              `json:"dry_run"`
	ModelRole        string            `json:"model_role"`
	ModelsUsed       []string          `json:"models_used"`
	CandidateCount   int               `json:"candidate_count"`
	ServerShortNames []ServerShortName `json:"server_short_names"`
	Named            []Named           `json:"named"`
	Refused          []Refusal         `json:"refused"`
	// SkippedAlreadyNamed counts rooms with a current name; a link with one
	// counts once, under SkippedLinksAlreadyNamed.
	SkippedAlreadyNamed      int `json:"skipped_already_named"`
	SkippedLinksAlreadyNamed int `json:"skipped_links_already_named"`
	NamedCount               int `json:"named_count"`
	RefusedCount             int `json:"refused_count"`
	SkippedCount             int `json:"skipped_count"`
}

func (result *Result) count() {
	result.NamedCount = len(result.Named)
	result.RefusedCount = len(result.Refused)
	result.SkippedCount = result.SkippedAlreadyNamed + result.SkippedLinksAlreadyNamed
}

// Assigner runs the pass. One run at a time.
type Assigner struct {
	Store     Store
	Model     Model
	ModelRole string

	running sync.Mutex
}

// candidate is one thing to name: a room, or a link's rooms under the link's
// most messaged room.
type candidate struct {
	room          Room
	linkedRooms   []Room
	linkedRoomIDs []string
	messageCount  int
}

func (c candidate) refusal(proposedPath, why string) Refusal {
	return Refusal{EntityID: c.room.RoomID, RoomName: c.room.Name, Platform: c.room.Platform, Kind: c.room.Kind,
		MessageCount: c.messageCount, ProposedPath: proposedPath, Why: why}
}

// Run names every room in inputs that has no current name. With dryRun it
// writes nothing and answers what it would have written. An error stops the
// run; the Result still holds what it did before the error.
func (a *Assigner) Run(ctx context.Context, inputs Inputs, dryRun bool) (Result, error) {
	if !a.running.TryLock() {
		return Result{}, ErrRunInProgress
	}
	defer a.running.Unlock()
	result := Result{}
	err := a.run(ctx, inputs, dryRun, &result)
	result.count()
	return result, err
}

func (a *Assigner) run(ctx context.Context, inputs Inputs, dryRun bool, out *Result) error {
	*out = Result{DryRun: dryRun, ModelRole: a.ModelRole, ModelsUsed: []string{}, ServerShortNames: []ServerShortName{},
		Named: []Named{}, Refused: []Refusal{}}
	result := out

	namespace, err := a.Store.Namespace(ctx, NamespaceMessages)
	if err != nil {
		return err
	}
	existing, err := a.Store.AllShortNames(ctx, NamespaceMessages)
	if err != nil {
		return err
	}
	run := newRunState(namespace, existing, inputs.Rooms)
	candidates := run.candidates(inputs, result)
	result.CandidateCount = len(candidates)
	result.ServerShortNames = append(result.ServerShortNames, run.existingServerShortNames()...)

	usedModel := func(model string) {
		for _, seen := range result.ModelsUsed {
			if seen == model {
				return
			}
		}
		result.ModelsUsed = append(result.ModelsUsed, model)
	}

	refusedServers, err := a.nameServers(ctx, run, candidates, result, usedModel)
	if err != nil {
		return err
	}

	for start := 0; start < len(candidates); start += BatchSize {
		batch := candidates[start:min(start+BatchSize, len(candidates))]
		var askable []candidate
		for _, c := range batch {
			if c.room.Discord != nil {
				if why, refused := refusedServers[c.room.Discord.ServerID]; refused {
					result.Refused = append(result.Refused, c.refusal("", why))
					continue
				}
			}
			askable = append(askable, c)
		}
		if len(askable) == 0 {
			continue
		}
		var reply roomNamesReply
		answeredBy, err := a.Model.Propose(ctx, roomSystemPrompt, run.roomPrompt(askable, start), roomNamesSchema, &reply)
		if err != nil {
			return fmt.Errorf("model call for candidates %d–%d: %w", start+1, start+len(batch), err)
		}
		usedModel(answeredBy)
		proposals := map[string]proposal{}
		for _, p := range reply.Names {
			if _, seen := proposals[p.EntityID]; !seen {
				proposals[p.EntityID] = p
			}
		}
		for _, c := range askable {
			p, proposed := proposals[c.room.RoomID]
			if !proposed {
				result.Refused = append(result.Refused, c.refusal("", "the model proposed no name for it"))
				continue
			}
			if p.Path == "" {
				result.Refused = append(result.Refused, c.refusal("", "the model gave no name: "+p.Reason))
				continue
			}
			if why := run.check(c, p.Path); why != "" {
				result.Refused = append(result.Refused, c.refusal(p.Path, why))
				continue
			}
			named := Named{Path: p.Path, EntityID: c.room.RoomID, LinkedRoomIDs: c.linkedRoomIDs, RoomName: c.room.Name,
				Platform: c.room.Platform, Kind: c.room.Kind, MessageCount: c.messageCount,
				Reason: fmt.Sprintf("%s (%d messages)", strings.TrimSpace(p.Reason), c.messageCount)}
			if !dryRun {
				written, err := a.Store.CreateShortName(ctx, NewShortName{Namespace: NamespaceMessages, Path: p.Path,
					EntityType: EntityTypeMultichatRoom, EntityID: c.room.RoomID, CreatedBy: CreatedBy, Reason: named.Reason})
				if errors.Is(err, ErrPathTaken) {
					result.Refused = append(result.Refused, c.refusal(p.Path, err.Error()))
					run.takenPaths[p.Path] = true
					continue
				}
				if err != nil {
					return err
				}
				named.ShortNameID = written.ID
			}
			run.take(c, p.Path)
			result.Named = append(result.Named, named)
		}
	}
	return nil
}

// nameServers fixes the first segment for every Discord server that has
// channels to name and no named channel yet, in one model call. It answers
// why each server it could not name was refused, keyed by server id.
func (a *Assigner) nameServers(ctx context.Context, run *runState, candidates []candidate, result *Result, usedModel func(string)) (map[string]string, error) {
	refused := map[string]string{}
	var servers []serverFacts
	listed := map[string]int{}
	for _, c := range candidates {
		channel := c.room.Discord
		if channel == nil || run.serverShortName[channel.ServerID] != "" {
			continue
		}
		index, seen := listed[channel.ServerID]
		if !seen {
			index = len(servers)
			listed[channel.ServerID] = index
			servers = append(servers, serverFacts{ServerID: channel.ServerID, ServerName: channel.ServerName})
		}
		servers[index].ChannelNames = append(servers[index].ChannelNames, channel.ChannelName)
	}
	if len(servers) == 0 {
		return refused, nil
	}
	var reply serverNamesReply
	answeredBy, err := a.Model.Propose(ctx, serverSystemPrompt, run.serverPrompt(servers), serverNamesSchema, &reply)
	if err != nil {
		return refused, fmt.Errorf("model call for Discord server short names: %w", err)
	}
	usedModel(answeredBy)
	proposals := map[string]serverProposal{}
	for _, p := range reply.Servers {
		if _, seen := proposals[p.ServerID]; !seen {
			proposals[p.ServerID] = p
		}
	}
	for _, server := range servers {
		p, proposed := proposals[server.ServerID]
		why := ""
		switch {
		case !proposed:
			why = "the model proposed no short name for it"
		default:
			why = run.checkServerShortName(server.ServerID, p.ShortName)
		}
		if why != "" {
			refused[server.ServerID] = fmt.Sprintf("Discord server %q got no short name (%q: %s)", server.ServerName, p.ShortName, why)
			continue
		}
		run.serverShortName[server.ServerID] = p.ShortName
		run.serverName[server.ServerID] = server.ServerName
		run.firstSegmentOwner[p.ShortName] = serverOwner(server.ServerID)
		result.ServerShortNames = append(result.ServerShortNames, ServerShortName{ServerID: server.ServerID,
			ServerName: server.ServerName, ShortName: p.ShortName, Source: "proposed"})
	}
	return refused, nil
}

// runState is what one run knows about the namespace, kept current as the
// run accepts names.
type runState struct {
	reserved map[string]bool
	// takenPaths holds every path in the namespace, superseded and of any
	// entity type, since a path is never reused, plus those accepted this run.
	takenPaths map[string]bool
	// currentRoomPath is the current name of each named room.
	currentRoomPath map[string]string
	// serverShortName is the first segment of each Discord server's names.
	serverShortName map[string]string
	serverName      map[string]string
	// firstSegmentOwner says who uses a first segment: a server
	// ("server:<id>"), or "path" for any other name.
	firstSegmentOwner map[string]string
}

func serverOwner(serverID string) string { return "server:" + serverID }

const otherNameOwner = "path"

func newRunState(namespace Namespace, existing []ShortName, rooms []Room) *runState {
	run := &runState{reserved: map[string]bool{}, takenPaths: map[string]bool{}, currentRoomPath: map[string]string{},
		serverShortName: map[string]string{}, serverName: map[string]string{}, firstSegmentOwner: map[string]string{}}
	for _, segment := range namespace.ReservedSegments {
		run.reserved[segment] = true
	}
	discordByRoomID := map[string]*DiscordChannel{}
	for _, room := range rooms {
		if room.Discord != nil {
			discordByRoomID[room.RoomID] = room.Discord
		}
	}
	// Oldest first, so when a server's channels disagree on a first segment
	// the first one chosen stands.
	sorted := append([]ShortName(nil), existing...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].CreatedAt < sorted[j].CreatedAt })
	for _, name := range sorted {
		run.takenPaths[name.Path] = true
		first := firstSegment(name.Path)
		owner := otherNameOwner
		if name.EntityType == EntityTypeMultichatRoom {
			if name.Current() {
				run.currentRoomPath[name.EntityID] = name.Path
			}
			if channel := discordByRoomID[name.EntityID]; channel != nil && strings.Contains(name.Path, "/") {
				owner = serverOwner(channel.ServerID)
				if run.serverShortName[channel.ServerID] == "" {
					run.serverShortName[channel.ServerID] = first
					run.serverName[channel.ServerID] = channel.ServerName
				}
			}
		}
		if _, claimed := run.firstSegmentOwner[first]; !claimed {
			run.firstSegmentOwner[first] = owner
		}
	}
	return run
}

func (run *runState) existingServerShortNames() []ServerShortName {
	var names []ServerShortName
	for serverID, shortName := range run.serverShortName {
		names = append(names, ServerShortName{ServerID: serverID, ServerName: run.serverName[serverID], ShortName: shortName, Source: "existing"})
	}
	sort.Slice(names, func(i, j int) bool { return names[i].ShortName < names[j].ShortName })
	return names
}

// candidates are the rooms and links with no current name, most messaged
// first. A link is one candidate under its most messaged room, and is skipped
// whole when any of its rooms has a current name.
func (run *runState) candidates(inputs Inputs, result *Result) []candidate {
	roomByID := map[string]Room{}
	for _, room := range inputs.Rooms {
		roomByID[room.RoomID] = room
	}
	inLink := map[string]bool{}
	var candidates []candidate
	for _, link := range inputs.Links {
		linked := append([]string(nil), link...)
		sort.Strings(linked)
		named := false
		var listed []Room
		count := 0
		for _, roomID := range linked {
			inLink[roomID] = true
			if run.currentRoomPath[roomID] != "" {
				named = true
			}
			count += inputs.MessageCounts[roomID]
			if room, ok := roomByID[roomID]; ok {
				listed = append(listed, room)
			}
		}
		if named {
			result.SkippedLinksAlreadyNamed++
			continue
		}
		if len(listed) == 0 {
			continue
		}
		// Most messaged first; equal counts keep room id order.
		sort.SliceStable(listed, func(i, j int) bool {
			return inputs.MessageCounts[listed[i].RoomID] > inputs.MessageCounts[listed[j].RoomID]
		})
		candidates = append(candidates, candidate{room: listed[0], linkedRooms: listed[1:], linkedRoomIDs: linked, messageCount: count})
	}
	for _, room := range inputs.Rooms {
		if inLink[room.RoomID] {
			continue
		}
		if run.currentRoomPath[room.RoomID] != "" {
			result.SkippedAlreadyNamed++
			continue
		}
		candidates = append(candidates, candidate{room: room, messageCount: inputs.MessageCounts[room.RoomID]})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].messageCount != candidates[j].messageCount {
			return candidates[i].messageCount > candidates[j].messageCount
		}
		if candidates[i].room.LastActivity != candidates[j].room.LastActivity {
			return candidates[i].room.LastActivity > candidates[j].room.LastActivity
		}
		return candidates[i].room.RoomID < candidates[j].room.RoomID
	})
	return candidates
}

// checkServerShortName says why shortName cannot be serverID's first segment,
// or "" when it can.
func (run *runState) checkServerShortName(serverID, shortName string) string {
	if why := checkSegment(shortName); why != "" {
		return why
	}
	if run.reserved[shortName] {
		return "it is a reserved segment of namespace " + NamespaceMessages
	}
	if owner, used := run.firstSegmentOwner[shortName]; used && owner != serverOwner(serverID) {
		return "another name already starts with it"
	}
	return ""
}

// check says why path cannot name c, or "" when it can.
func (run *runState) check(c candidate, path string) string {
	if why := checkPath(path); why != "" {
		return why
	}
	segments := strings.Split(path, "/")
	first := segments[0]
	if run.reserved[first] {
		return fmt.Sprintf("%q is a reserved segment of namespace %s", first, NamespaceMessages)
	}
	if c.room.Discord != nil {
		serverShortName := run.serverShortName[c.room.Discord.ServerID]
		if len(segments) != 2 || first != serverShortName {
			return fmt.Sprintf("a Discord channel's path is %s/<channel>", serverShortName)
		}
	} else {
		if len(segments) != 1 {
			return "a direct message or group's path is one segment"
		}
		if owner, used := run.firstSegmentOwner[first]; used && owner != otherNameOwner {
			return "a Discord server's channels start with it"
		}
	}
	if run.takenPaths[path] {
		return "the path is taken: by a name in short-name-store, or by a more popular room in this run"
	}
	return ""
}

func (run *runState) take(c candidate, path string) {
	run.takenPaths[path] = true
	first := firstSegment(path)
	if _, claimed := run.firstSegmentOwner[first]; !claimed {
		run.firstSegmentOwner[first] = otherNameOwner
	}
	for _, roomID := range append([]string{c.room.RoomID}, c.linkedRoomIDs...) {
		run.currentRoomPath[roomID] = path
	}
}

func (run *runState) sortedTakenPaths() []string {
	paths := make([]string, 0, len(run.takenPaths))
	for path := range run.takenPaths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func (run *runState) sortedReserved() []string {
	segments := make([]string, 0, len(run.reserved))
	for segment := range run.reserved {
		segments = append(segments, segment)
	}
	sort.Strings(segments)
	return segments
}

func mustJSON(value any) string {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		panic(err) // only plain structs, maps and slices are encoded
	}
	return string(encoded)
}

package shortnames

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// fakeStore keeps names in memory the way short-name-store does: a path is
// never reused, and a 409 refuses a path another record holds.
type fakeStore struct {
	mu        sync.Mutex
	namespace Namespace
	names     []ShortName
	created   []NewShortName
	// refuse makes a write of these paths a 409, as if another writer had
	// taken them since the run read the store.
	refuse map[string]bool
}

func (s *fakeStore) Namespace(ctx context.Context, name string) (Namespace, error) {
	if name != s.namespace.Name {
		return Namespace{}, ErrNamespaceMissing
	}
	return s.namespace, nil
}

func (s *fakeStore) AllShortNames(ctx context.Context, namespace string) ([]ShortName, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ShortName(nil), s.names...), nil
}

func (s *fakeStore) CreateShortName(ctx context.Context, name NewShortName) (ShortName, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refuse[name.Path] {
		return ShortName{}, fmt.Errorf("%w: short-name-store POST /short-names: 409 path %s belongs to another record", ErrPathTaken, name.Path)
	}
	for _, existing := range s.names {
		if existing.Path == name.Path {
			return ShortName{}, fmt.Errorf("%w: path %s", ErrPathTaken, name.Path)
		}
	}
	s.created = append(s.created, name)
	written := ShortName{ID: fmt.Sprintf("short_name_%06d", len(s.names)+1), Namespace: name.Namespace, Path: name.Path,
		EntityType: name.EntityType, EntityID: name.EntityID, CreatedBy: name.CreatedBy, Reason: name.Reason}
	s.names = append(s.names, written)
	return written, nil
}

// fakeModel answers with name(facts) for each candidate and server(facts)
// for each server, reading the facts back out of the prompt as the model
// would.
type fakeModel struct {
	room   func(candidateFacts) string
	server func(serverFacts) string
	// roomCalls holds the entity ids of each room call, in prompt order.
	roomCalls   [][]string
	serverCalls int
	block       chan struct{}
	entered     chan struct{}
}

func jsonAfter(t *testing.T, prompt, marker string, into any) {
	t.Helper()
	_, rest, found := strings.Cut(prompt, marker)
	if !found {
		t.Fatalf("prompt has no %q", marker)
	}
	if err := json.Unmarshal([]byte(rest), into); err != nil {
		t.Fatalf("facts after %q: %v", marker, err)
	}
}

func (m *fakeModel) propose(t *testing.T) func(ctx context.Context, systemPrompt, prompt string, schema map[string]any, out any) (string, error) {
	return func(ctx context.Context, systemPrompt, prompt string, schema map[string]any, out any) (string, error) {
		if m.block != nil {
			m.entered <- struct{}{}
			<-m.block
		}
		if strings.Contains(prompt, "Servers to name:\n") {
			m.serverCalls++
			var servers []serverFacts
			jsonAfter(t, prompt, "Servers to name:\n", &servers)
			reply := out.(*serverNamesReply)
			for _, server := range servers {
				reply.Servers = append(reply.Servers, serverProposal{ServerID: server.ServerID, ShortName: m.server(server), Reason: "short"})
			}
			return "fake-model", nil
		}
		var candidates []candidateFacts
		jsonAfter(t, prompt, "Candidates, most messages first:\n", &candidates)
		var ids []string
		reply := out.(*roomNamesReply)
		for _, c := range candidates {
			ids = append(ids, c.EntityID)
			if path := m.room(c); path != "-" {
				reply.Names = append(reply.Names, proposal{EntityID: c.EntityID, Path: path, Reason: "first name"})
			}
		}
		m.roomCalls = append(m.roomCalls, ids)
		return "fake-model", nil
	}
}

type modelFunc func(ctx context.Context, systemPrompt, prompt string, schema map[string]any, out any) (string, error)

func (f modelFunc) Propose(ctx context.Context, systemPrompt, prompt string, schema map[string]any, out any) (string, error) {
	return f(ctx, systemPrompt, prompt, schema, out)
}

func newAssigner(t *testing.T, store *fakeStore, model *fakeModel) *Assigner {
	return &Assigner{Store: store, Model: modelFunc(model.propose(t)), ModelRole: "balanced"}
}

func messagesNamespace() Namespace {
	return Namespace{Name: NamespaceMessages, ReservedSegments: []string{"search", "contacts", "discord", "emoji", "auto-reactions"}}
}

func directMessage(roomID, partner string) Room {
	return Room{RoomID: roomID, Name: partner, Platform: "whatsapp", Kind: KindDirectMessage, DirectMessagePartnerName: partner, MemberCount: 2}
}

func lowercaseFirstName(c candidateFacts) string {
	first, _, _ := strings.Cut(c.DirectMessagePartner, " ")
	return strings.ToLower(first)
}

func TestTheBusiestKatGetsTheBareNameAndTheNextIsRefusedForTheModelToTryAgain(t *testing.T) {
	store := &fakeStore{namespace: messagesNamespace()}
	model := &fakeModel{room: lowercaseFirstName}
	inputs := Inputs{
		Rooms:         []Room{directMessage("!quiet:x", "Kim Moss"), directMessage("!busy:x", "Kim Park")},
		MessageCounts: map[string]int{"!quiet:x": 5, "!busy:x": 90},
	}
	result, err := newAssigner(t, store, model).Run(context.Background(), inputs, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := model.roomCalls; len(got) != 1 || got[0][0] != "!busy:x" {
		t.Fatalf("the model saw %v; want the busier Kim first", got)
	}
	if len(result.Named) != 1 || result.Named[0].EntityID != "!busy:x" || result.Named[0].Path != "kim" {
		t.Fatalf("named %+v", result.Named)
	}
	if result.Named[0].Reason != "first name (90 messages)" || result.Named[0].ShortNameID == "" {
		t.Errorf("written %+v", result.Named[0])
	}
	if len(result.Refused) != 1 || result.Refused[0].EntityID != "!quiet:x" || !strings.Contains(result.Refused[0].Why, "taken") {
		t.Fatalf("refused %+v", result.Refused)
	}
	if len(store.created) != 1 || store.created[0].CreatedBy != CreatedBy || store.created[0].EntityType != EntityTypeMultichatRoom {
		t.Fatalf("store got %+v", store.created)
	}
	if result.NamedCount != 1 || result.RefusedCount != 1 {
		t.Errorf("counts %+v", result)
	}
}

func TestTheModelSeesEveryTakenPathSupersededOnesIncluded(t *testing.T) {
	store := &fakeStore{namespace: messagesNamespace(), names: []ShortName{
		{ID: "short_name_000001", Path: "kim", EntityType: EntityTypeMultichatRoom, EntityID: "!old:x", SupersededAt: 10, SupersededBy: "short_name_000002"},
		{ID: "short_name_000002", Path: "kim-park", EntityType: EntityTypeMultichatRoom, EntityID: "!old:x"},
	}}
	var sawPrompt string
	model := modelFunc(func(ctx context.Context, systemPrompt, prompt string, schema map[string]any, out any) (string, error) {
		sawPrompt = prompt
		out.(*roomNamesReply).Names = []proposal{{EntityID: "!new:x", Path: "kim", Reason: "first name"}}
		return "fake-model", nil
	})
	assigner := &Assigner{Store: store, Model: model, ModelRole: "balanced"}
	result, err := assigner.Run(context.Background(), Inputs{Rooms: []Room{directMessage("!new:x", "Kim Moss")}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sawPrompt, `"kim"`) || !strings.Contains(sawPrompt, `"kim-park"`) || !strings.Contains(sawPrompt, `"search"`) {
		t.Errorf("prompt lacks a taken path or a reserved segment:\n%s", sawPrompt)
	}
	if len(result.Named) != 0 || len(result.Refused) != 1 {
		t.Fatalf("a superseded path was reused: %+v", result)
	}
}

func TestANamedRoomIsNeverAskedAboutOrRenamed(t *testing.T) {
	store := &fakeStore{namespace: messagesNamespace(), names: []ShortName{
		{ID: "short_name_000001", Path: "kim", EntityType: EntityTypeMultichatRoom, EntityID: "!kim:x"},
	}}
	model := &fakeModel{room: func(c candidateFacts) string { t.Errorf("asked about %s", c.EntityID); return "-" }}
	result, err := newAssigner(t, store, model).Run(context.Background(), Inputs{Rooms: []Room{directMessage("!kim:x", "Kim Park")}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.SkippedAlreadyNamed != 1 || result.SkippedCount != 1 || len(store.created) != 0 || len(model.roomCalls) != 0 {
		t.Fatalf("%+v; store got %+v", result, store.created)
	}
}

func TestALinkIsOneCandidateUnderItsBusiestRoomWithTheCountsSummed(t *testing.T) {
	store := &fakeStore{namespace: messagesNamespace()}
	var counts []int
	model := &fakeModel{room: func(c candidateFacts) string {
		counts = append(counts, c.MessageCount)
		if len(c.SameConversationOnApps) != 1 {
			t.Errorf("the link's other room is not in the facts: %+v", c)
		}
		return "nyc"
	}}
	inputs := Inputs{
		Rooms: []Room{
			{RoomID: "!sms:x", Name: "NYC crew", Platform: "gmessages", Kind: KindGroup, MemberCount: 4},
			{RoomID: "!wa:x", Name: "NYC crew", Platform: "whatsapp", Kind: KindGroup, MemberCount: 4},
		},
		Links:         [][]string{{"!sms:x", "!wa:x"}},
		MessageCounts: map[string]int{"!sms:x": 10, "!wa:x": 30},
	}
	result, err := newAssigner(t, store, model).Run(context.Background(), inputs, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.CandidateCount != 1 || len(result.Named) != 1 || result.Named[0].EntityID != "!wa:x" || result.Named[0].MessageCount != 40 {
		t.Fatalf("%+v", result)
	}
	if len(counts) != 1 || counts[0] != 40 {
		t.Errorf("the model saw counts %v", counts)
	}
}

func TestALinkWithANamedRoomIsSkippedWhole(t *testing.T) {
	store := &fakeStore{namespace: messagesNamespace(), names: []ShortName{
		{ID: "short_name_000001", Path: "nyc", EntityType: EntityTypeMultichatRoom, EntityID: "!sms:x"},
	}}
	model := &fakeModel{room: func(c candidateFacts) string { t.Errorf("asked about %s", c.EntityID); return "-" }}
	inputs := Inputs{
		Rooms:         []Room{{RoomID: "!sms:x", Kind: KindGroup}, {RoomID: "!wa:x", Kind: KindGroup}},
		Links:         [][]string{{"!sms:x", "!wa:x"}},
		MessageCounts: map[string]int{"!wa:x": 30},
	}
	result, err := newAssigner(t, store, model).Run(context.Background(), inputs, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.SkippedLinksAlreadyNamed != 1 || result.CandidateCount != 0 || len(store.created) != 0 {
		t.Fatalf("%+v", result)
	}
}

func discordChannel(roomID, serverID, serverName, channelName string) Room {
	return Room{RoomID: roomID, Name: "#" + channelName, Platform: "discord", Kind: KindDiscordChannel,
		Discord: &DiscordChannel{ServerID: serverID, ServerName: serverName, ChannelName: channelName}}
}

func TestDiscordChannelsShareTheirServersShortName(t *testing.T) {
	store := &fakeStore{namespace: messagesNamespace(), names: []ShortName{
		{ID: "short_name_000001", Path: "social/general", EntityType: EntityTypeMultichatRoom, EntityID: "!general:x"},
	}}
	model := &fakeModel{
		server: func(s serverFacts) string {
			if s.ServerID != "pretend-id" {
				t.Errorf("asked to name server %s, which already has a short name", s.ServerID)
			}
			return "pretend"
		},
		room: func(c candidateFacts) string {
			switch c.EntityID {
			case "!events:x":
				if c.DiscordServerShortName != "social" {
					t.Errorf("the model was not told the server's existing short name: %+v", c)
				}
				return "social/events"
			case "!wrong:x":
				return "reno/hiking"
			default:
				return c.DiscordServerShortName + "/general"
			}
		},
	}
	inputs := Inputs{
		Rooms: []Room{
			discordChannel("!general:x", "reno-id", "Riverside social group", "20s-30s-general-chat💫"),
			discordChannel("!events:x", "reno-id", "Riverside social group", "events"),
			discordChannel("!wrong:x", "reno-id", "Riverside social group", "hiking"),
			discordChannel("!pretend-general:x", "pretend-id", "Pretend", "general"),
		},
		MessageCounts: map[string]int{"!events:x": 9, "!wrong:x": 8, "!pretend-general:x": 7},
	}
	result, err := newAssigner(t, store, model).Run(context.Background(), inputs, false)
	if err != nil {
		t.Fatal(err)
	}
	if model.serverCalls != 1 {
		t.Errorf("%d server calls", model.serverCalls)
	}
	paths := map[string]string{}
	for _, named := range result.Named {
		paths[named.EntityID] = named.Path
	}
	if paths["!events:x"] != "social/events" || paths["!pretend-general:x"] != "pretend/general" || len(paths) != 2 {
		t.Fatalf("named %v", paths)
	}
	if len(result.Refused) != 1 || result.Refused[0].EntityID != "!wrong:x" || !strings.Contains(result.Refused[0].Why, "social/<channel>") {
		t.Fatalf("refused %+v", result.Refused)
	}
	sources := map[string]string{}
	for _, server := range result.ServerShortNames {
		sources[server.ShortName] = server.Source
	}
	if sources["social"] != "existing" || sources["pretend"] != "proposed" {
		t.Errorf("server short names %+v", result.ServerShortNames)
	}
}

func TestAServerShortNameMustNotBeAnotherNamesFirstSegment(t *testing.T) {
	store := &fakeStore{namespace: messagesNamespace(), names: []ShortName{
		{ID: "short_name_000001", Path: "pretend", EntityType: EntityTypeMultichatRoom, EntityID: "!someone:x"},
	}}
	model := &fakeModel{server: func(serverFacts) string { return "pretend" },
		room: func(c candidateFacts) string { t.Errorf("asked about %s", c.EntityID); return "-" }}
	inputs := Inputs{Rooms: []Room{discordChannel("!c:x", "pretend-id", "Pretend", "general")}}
	result, err := newAssigner(t, store, model).Run(context.Background(), inputs, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Refused) != 1 || !strings.Contains(result.Refused[0].Why, "another name already starts with it") {
		t.Fatalf("%+v", result.Refused)
	}
}

func TestEveryProposalIsCheckedBeforeItIsWritten(t *testing.T) {
	store := &fakeStore{namespace: messagesNamespace()}
	proposals := map[string]string{
		"!upper:x":    "Kim",
		"!reserved:x": "search",
		"!two:x":      "kim/park",
		"!long:x":     strings.Repeat("a", 41),
		"!hyphens:x":  "kim--park",
		"!silent:x":   "-",
		"!declined:x": "",
		"!good:x":     "maria",
	}
	model := &fakeModel{room: func(c candidateFacts) string { return proposals[c.EntityID] }}
	var rooms []Room
	for roomID := range proposals {
		rooms = append(rooms, directMessage(roomID, "Someone"))
	}
	result, err := newAssigner(t, store, model).Run(context.Background(), Inputs{Rooms: rooms}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Named) != 1 || result.Named[0].Path != "maria" {
		t.Fatalf("named %+v", result.Named)
	}
	whys := map[string]string{}
	for _, refusal := range result.Refused {
		whys[refusal.EntityID] = refusal.Why
	}
	for roomID, want := range map[string]string{
		"!upper:x":    "not lowercase",
		"!reserved:x": "reserved",
		"!two:x":      "one segment",
		"!long:x":     "longer than 40",
		"!hyphens:x":  "single hyphens",
		"!silent:x":   "proposed no name",
		"!declined:x": "gave no name: first name",
	} {
		if !strings.Contains(whys[roomID], want) {
			t.Errorf("%s refused for %q, want %q", roomID, whys[roomID], want)
		}
	}
	if len(store.created) != 1 {
		t.Errorf("store got %+v", store.created)
	}
}

func TestADryRunWritesNothingAndStillRefusesDuplicates(t *testing.T) {
	store := &fakeStore{namespace: messagesNamespace()}
	model := &fakeModel{room: lowercaseFirstName}
	inputs := Inputs{Rooms: []Room{directMessage("!a:x", "Kim A"), directMessage("!b:x", "Kim B")}, MessageCounts: map[string]int{"!a:x": 2, "!b:x": 1}}
	result, err := newAssigner(t, store, model).Run(context.Background(), inputs, true)
	if err != nil {
		t.Fatal(err)
	}
	if !result.DryRun || len(store.created) != 0 || len(result.Named) != 1 || result.Named[0].ShortNameID != "" || len(result.Refused) != 1 {
		t.Fatalf("%+v; store got %+v", result, store.created)
	}
}

func TestAConflictIsReportedAndTheRunGoesOn(t *testing.T) {
	store := &fakeStore{namespace: messagesNamespace(), refuse: map[string]bool{"kim": true}}
	model := &fakeModel{room: lowercaseFirstName}
	inputs := Inputs{Rooms: []Room{directMessage("!a:x", "Kim A"), directMessage("!b:x", "Maria B")}, MessageCounts: map[string]int{"!a:x": 2}}
	result, err := newAssigner(t, store, model).Run(context.Background(), inputs, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Refused) != 1 || !strings.Contains(result.Refused[0].Why, "409") || len(result.Named) != 1 || result.Named[0].Path != "maria" {
		t.Fatalf("%+v", result)
	}
}

func TestCandidatesGoToTheModelInBatchesInPopularityOrder(t *testing.T) {
	store := &fakeStore{namespace: messagesNamespace()}
	model := &fakeModel{room: func(c candidateFacts) string { return fmt.Sprintf("person-%d", c.PopularityRank) }}
	inputs := Inputs{MessageCounts: map[string]int{}}
	for i := 0; i < BatchSize+5; i++ {
		roomID := fmt.Sprintf("!room%02d:x", i)
		inputs.Rooms = append(inputs.Rooms, directMessage(roomID, "Someone"))
		inputs.MessageCounts[roomID] = i
	}
	result, err := newAssigner(t, store, model).Run(context.Background(), inputs, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(model.roomCalls) != 2 || len(model.roomCalls[0]) != BatchSize || len(model.roomCalls[1]) != 5 {
		t.Fatalf("calls %v", model.roomCalls)
	}
	if model.roomCalls[0][0] != fmt.Sprintf("!room%02d:x", BatchSize+4) || model.roomCalls[1][4] != "!room00:x" {
		t.Errorf("calls not in popularity order: %v", model.roomCalls)
	}
	if result.NamedCount != BatchSize+5 {
		t.Errorf("named %d", result.NamedCount)
	}
}

func TestASecondRunWhileOneIsGoingIsRefused(t *testing.T) {
	store := &fakeStore{namespace: messagesNamespace()}
	model := &fakeModel{room: lowercaseFirstName, block: make(chan struct{}), entered: make(chan struct{})}
	assigner := newAssigner(t, store, model)
	inputs := Inputs{Rooms: []Room{directMessage("!a:x", "Kim A")}}
	done := make(chan error)
	go func() {
		_, err := assigner.Run(context.Background(), inputs, true)
		done <- err
	}()
	<-model.entered
	if _, err := assigner.Run(context.Background(), inputs, true); err != ErrRunInProgress {
		t.Fatalf("second run: %v, want ErrRunInProgress", err)
	}
	close(model.block)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

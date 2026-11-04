package botaccounts

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/kayushkin/bus/messages"
)

var testBot = Account{ID: "message_account_000002", Platform: "discord", Handle: "900000000000000001", DisplayName: "Helper Bot", Kind: "bot", CredentialID: "cred_test"}

func TestSplitForDiscordBreaksAtParagraphsThenLinesThenSpaces(t *testing.T) {
	if got := SplitForDiscord("  short  ", 10); len(got) != 1 || got[0] != "short" {
		t.Fatalf("short body: %q", got)
	}
	got := SplitForDiscord("aaaa bbbb\n\ncccc dddd", 12)
	if len(got) != 2 || got[0] != "aaaa bbbb" || got[1] != "cccc dddd" {
		t.Fatalf("paragraph break: %q", got)
	}
	got = SplitForDiscord("aaaa\nbbbb cccc", 10)
	if len(got) != 2 || got[0] != "aaaa" || got[1] != "bbbb cccc" {
		t.Fatalf("line break: %q", got)
	}
	got = SplitForDiscord("abcdefghijklmnop", 5)
	if strings.Join(got, "") != "abcdefghijklmnop" {
		t.Fatalf("long word lost characters: %q", got)
	}
	for _, part := range SplitForDiscord(strings.Repeat("word ", 1000), DiscordMessageLimit) {
		if len([]rune(part)) > DiscordMessageLimit {
			t.Fatalf("part of %d characters", len([]rune(part)))
		}
	}
}

func TestInboundOfKeepsOnlyAPersonsDirectMessage(t *testing.T) {
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	person := &discordgo.User{ID: "700000000000000007", Username: "carol", GlobalName: "Carol"}
	message := &discordgo.Message{ID: "123", ChannelID: "456", Author: person, Content: "hi", Timestamp: when,
		Attachments: []*discordgo.MessageAttachment{{URL: "https://cdn.example/a.png", Filename: "a.png", ContentType: "image/png", Size: 10}}}
	inbound, ok := InboundOf(testBot, message)
	if !ok {
		t.Fatal("a person's DM was dropped")
	}
	if inbound.EventID != "discord:123" || inbound.ReceivingAccountID != testBot.ID || inbound.PlatformChannelID != "456" ||
		inbound.SenderPlatformUserID != person.ID || inbound.SenderDisplayName != "Carol" || inbound.Body != "hi" ||
		!inbound.IsDirectMessage || inbound.RoomID != "" || inbound.SenderUserID != "" || len(inbound.Attachments) != 1 || !inbound.Timestamp.Equal(when) {
		t.Fatalf("envelope: %+v", inbound)
	}
	for name, dropped := range map[string]*discordgo.Message{
		"server message": {ID: "1", GuildID: "9", Author: person, Content: "x"},
		"another bot":    {ID: "2", Author: &discordgo.User{ID: "5", Bot: true}, Content: "x"},
		"ourselves":      {ID: "3", Author: &discordgo.User{ID: testBot.Handle}, Content: "x"},
		"no author":      {ID: "4", Content: "x"},
	} {
		if _, ok := InboundOf(testBot, dropped); ok {
			t.Errorf("%s was published", name)
		}
	}
}

type fakeGateway struct {
	mu        sync.Mutex
	sent      []string
	channels  []string
	openError error
	sendError error
}

func (g *fakeGateway) Open() error  { return g.openError }
func (g *fakeGateway) Close() error { return nil }
func (g *fakeGateway) UserChannelCreate(id string, _ ...discordgo.RequestOption) (*discordgo.Channel, error) {
	return &discordgo.Channel{ID: "dm-with-" + id}, nil
}
func (g *fakeGateway) ChannelMessageSendComplex(channel string, data *discordgo.MessageSend, _ ...discordgo.RequestOption) (*discordgo.Message, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.sendError != nil {
		return nil, g.sendError
	}
	if data.AllowedMentions == nil || len(data.AllowedMentions.Parse) != 0 {
		panic("a send must mention nobody")
	}
	g.sent = append(g.sent, data.Content)
	g.channels = append(g.channels, channel)
	return &discordgo.Message{ID: "m" + string(rune('0'+len(g.sent)))}, nil
}

type fakeAccounts struct{ accounts []Account }

func (f fakeAccounts) DiscordBotAccounts() ([]Account, error) { return f.accounts, nil }

type fakeTokens struct{}

func (fakeTokens) BotToken(string, string) (string, error) { return "token", nil }

type fakePublisher struct {
	mu        sync.Mutex
	published []messages.MessagingInbound
	failures  int
}

func (p *fakePublisher) JetPublish(subject string, data any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failures > 0 {
		p.failures--
		return errors.New("bus down")
	}
	p.published = append(p.published, data.(messages.MessagingInbound))
	return nil
}

func newTestManager(gateway *fakeGateway, publisher Publisher) (*Manager, *func(*discordgo.Message)) {
	var deliver func(*discordgo.Message)
	m := &Manager{
		Accounts: fakeAccounts{[]Account{testBot}}, Tokens: fakeTokens{}, Publisher: publisher,
		NewGateway: func(_ string, onMessage func(*discordgo.Message), onState func(string, string)) (Gateway, error) {
			deliver = onMessage
			onState(StateConnected, "ready")
			return gateway, nil
		},
		RefreshEvery: time.Hour, RetryEvery: time.Millisecond, RetryAtMost: time.Millisecond, PublishAttempts: 3,
		Sleep: func(time.Duration) {}, Now: time.Now,
		connections: map[string]*connection{testBot.ID: {state: AccountState{Account: testBot, State: StateConnecting}}},
	}
	m.keepConnected(testBot)
	return m, &deliver
}

func TestManagerPublishesRetriesAndSends(t *testing.T) {
	gateway := &fakeGateway{}
	publisher := &fakePublisher{failures: 2}
	m, deliver := newTestManager(gateway, publisher)
	(*deliver)(&discordgo.Message{ID: "77", ChannelID: "dm", Author: &discordgo.User{ID: "700"}, Content: "hello"})
	if len(publisher.published) != 1 || publisher.published[0].EventID != "discord:77" {
		t.Fatalf("published after two failures: %+v", publisher.published)
	}
	states := m.States()
	if len(states) != 1 || states[0].State != StateConnected || states[0].MessagesReceived != 1 || states[0].PublishFailures != 0 {
		t.Fatalf("states: %+v", states)
	}

	result, err := m.Send(testBot.ID, "700", "", strings.Repeat("x ", 1500))
	if err != nil {
		t.Fatal(err)
	}
	if result.ChannelID != "dm-with-700" || len(result.MessageIDs) != 2 || len(gateway.sent) != 2 {
		t.Fatalf("send: %+v, sent %d", result, len(gateway.sent))
	}
	if _, err := m.Send(testBot.ID, "", "chan-9", "hi"); err != nil || gateway.channels[2] != "chan-9" {
		t.Fatalf("send by channel: %v %v", err, gateway.channels)
	}
	if _, err := m.Send("message_account_000099", "", "c", "hi"); !errors.Is(err, ErrUnknownAccount) {
		t.Fatalf("unknown account: %v", err)
	}
}

func TestManagerCountsAMessageTheBusNeverTook(t *testing.T) {
	publisher := &fakePublisher{failures: 10}
	m, deliver := newTestManager(&fakeGateway{}, publisher)
	(*deliver)(&discordgo.Message{ID: "78", ChannelID: "dm", Author: &discordgo.User{ID: "700"}, Content: "hello"})
	if states := m.States(); states[0].PublishFailures != 1 {
		t.Fatalf("lost message not counted: %+v", states)
	}
}

func TestMessageStoreAccountsKeepsDiscordBotsAndRefusesOneWithoutACredential(t *testing.T) {
	body := `[{"id":"message_account_000001","platform":"discord","handle":"1","display_name":"Me","kind":"user"},
	{"id":"message_account_000002","platform":"discord","handle":"2","display_name":"Helper Bot","kind":"bot","credential_id":"cred_x"}]`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
	defer server.Close()
	bots, err := MessageStoreAccounts{BaseURL: server.URL, HTTPClient: server.Client()}.DiscordBotAccounts()
	if err != nil || len(bots) != 1 || bots[0].ID != "message_account_000002" || bots[0].CredentialID != "cred_x" {
		t.Fatalf("bots %+v err %v", bots, err)
	}
	body = `[{"id":"message_account_000003","platform":"discord","kind":"bot"}]`
	if _, err := (MessageStoreAccounts{BaseURL: server.URL, HTTPClient: server.Client()}).DiscordBotAccounts(); err == nil {
		t.Fatal("a bot with no credential was accepted")
	}
}

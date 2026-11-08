package botaccounts

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/kayushkin/bus/messages"
)

// Connection states, as GET /accounts reports them.
const (
	StateConnecting   = "connecting"
	StateConnected    = "connected"
	StateDisconnected = "disconnected"
	StateFailed       = "failed"
)

// AccountSource answers the bot accounts to connect.
type AccountSource interface {
	DiscordBotAccounts() ([]Account, error)
}

// TokenSource answers a bot's token.
type TokenSource interface {
	BotToken(credentialID, reason string) (string, error)
}

// Publisher puts a message on the bus.
type Publisher interface {
	JetPublish(subject string, data any) error
}

// DiscordREST is the part of a discordgo session that sends.
type DiscordREST interface {
	UserChannelCreate(recipientID string, options ...discordgo.RequestOption) (*discordgo.Channel, error)
	ChannelMessageSendComplex(channelID string, data *discordgo.MessageSend, options ...discordgo.RequestOption) (*discordgo.Message, error)
}

// Gateway is one bot's connection: a discordgo session in production.
type Gateway interface {
	DiscordREST
	Open() error
	Close() error
}

// GatewayFactory makes a bot's connection; onMessage gets every message the
// gateway delivers, and onState every change of state.
type GatewayFactory func(token string, onMessage func(*discordgo.Message), onState func(state, detail string)) (Gateway, error)

// AccountState is one bot account and its connection, for GET /accounts.
type AccountState struct {
	Account
	State            string    `json:"state"`
	StateSince       time.Time `json:"state_since"`
	LastError        string    `json:"last_error,omitempty"`
	MessagesReceived int       `json:"messages_received"`
	LastReceivedAt   time.Time `json:"last_received_at,omitzero"`
	// PublishFailures counts messages that never reached the bus after every
	// retry; each is also logged with its id.
	PublishFailures int `json:"publish_failures"`
}

// Manager keeps one connection per bot account.
type Manager struct {
	Accounts AccountSource
	Tokens   TokenSource
	// Publisher is nil when NATS is not configured: the bots still send, and
	// what people send them is logged as not published.
	Publisher  Publisher
	NewGateway GatewayFactory
	// RefreshEvery is how often message-store is asked again for new bots.
	RefreshEvery time.Duration
	// RetryEvery is the first wait before reconnecting a bot whose connection
	// could not open; it doubles to RetryAtMost.
	RetryEvery, RetryAtMost time.Duration
	// PublishAttempts is how many times one message is offered to the bus.
	PublishAttempts int
	Sleep           func(time.Duration)
	Now             func() time.Time

	mu          sync.Mutex
	connections map[string]*connection
}

type connection struct {
	state   AccountState
	gateway Gateway
}

// ErrUnknownAccount is a send naming an account that is not one of our bots.
var ErrUnknownAccount = errors.New("not a bot account multichat holds")

// ErrNotConnected is a send through a bot whose connection never opened.
var ErrNotConnected = errors.New("bot account is not connected")

// NewDiscordGateway is the production GatewayFactory: a discordgo session
// with the direct-messages intent, which reconnects by itself after a drop.
func NewDiscordGateway(token string, onMessage func(*discordgo.Message), onState func(state, detail string)) (Gateway, error) {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, err
	}
	session.Identify.Intents = discordgo.IntentDirectMessages
	session.AddHandler(func(_ *discordgo.Session, ready *discordgo.Ready) {
		onState(StateConnected, "ready as "+ready.User.Username)
	})
	session.AddHandler(func(_ *discordgo.Session, _ *discordgo.Resumed) { onState(StateConnected, "resumed") })
	session.AddHandler(func(_ *discordgo.Session, _ *discordgo.Disconnect) {
		onState(StateDisconnected, "gateway closed; discordgo reconnects")
	})
	session.AddHandler(func(_ *discordgo.Session, created *discordgo.MessageCreate) { onMessage(created.Message) })
	return session, nil
}

// Run connects every bot message-store lists, and asks again every
// RefreshEvery for bots added since. It does not return.
func (m *Manager) Run() {
	for {
		accounts, err := m.Accounts.DiscordBotAccounts()
		if err != nil {
			log.Printf("bot accounts: %v; asking again in %s", err, m.RetryEvery)
			m.Sleep(m.RetryEvery)
			continue
		}
		for _, account := range accounts {
			m.mu.Lock()
			if m.connections == nil {
				m.connections = map[string]*connection{}
			}
			_, known := m.connections[account.ID]
			if !known {
				m.connections[account.ID] = &connection{state: AccountState{Account: account, State: StateConnecting, StateSince: m.Now()}}
			}
			m.mu.Unlock()
			if !known {
				go m.keepConnected(account)
			}
		}
		m.Sleep(m.RefreshEvery)
	}
}

// keepConnected opens one bot's connection, retrying with a growing wait
// until it opens; after that discordgo reconnects by itself.
func (m *Manager) keepConnected(account Account) {
	wait := m.RetryEvery
	for {
		err := m.connect(account)
		if err == nil {
			return
		}
		m.setState(account.ID, StateFailed, err.Error())
		log.Printf("bot accounts: %s (%s) could not connect: %v; trying again in %s", account.ID, account.DisplayName, err, wait)
		m.Sleep(wait)
		wait *= 2
		if wait > m.RetryAtMost {
			wait = m.RetryAtMost
		}
	}
}

func (m *Manager) connect(account Account) error {
	token, err := m.Tokens.BotToken(account.CredentialID, "connect the "+account.DisplayName+" bot to Discord for its direct messages")
	if err != nil {
		return err
	}
	gateway, err := m.NewGateway(token,
		func(message *discordgo.Message) { m.received(account, message) },
		func(state, detail string) {
			m.setState(account.ID, state, detail)
			log.Printf("bot accounts: %s (%s) %s: %s", account.ID, account.DisplayName, state, detail)
		})
	if err != nil {
		return err
	}
	if err := gateway.Open(); err != nil {
		return fmt.Errorf("open the gateway: %w", err)
	}
	m.mu.Lock()
	m.connections[account.ID].gateway = gateway
	m.mu.Unlock()
	return nil
}

func (m *Manager) setState(accountID, state, detail string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.connections[accountID]
	if c == nil {
		return
	}
	if c.state.State != state {
		c.state.State = state
		c.state.StateSince = m.Now()
	}
	if state == StateFailed || state == StateDisconnected {
		c.state.LastError = detail
	}
}

// InboundOf turns a message the bot's gateway delivered into the bus
// envelope, or answers false for one that is not a person's direct message
// to the bot: a server message, or one from a bot (ourselves included).
func InboundOf(account Account, message *discordgo.Message) (messages.MessagingInbound, bool) {
	if message.GuildID != "" || message.Author == nil || message.Author.Bot || message.Author.ID == account.Handle {
		return messages.MessagingInbound{}, false
	}
	name := message.Author.GlobalName
	if name == "" {
		name = message.Author.Username
	}
	var attachments []messages.MessagingAttachment
	for _, attachment := range message.Attachments {
		attachments = append(attachments, messages.MessagingAttachment{
			URL: attachment.URL, Filename: attachment.Filename, ContentType: attachment.ContentType, SizeBytes: attachment.Size,
		})
	}
	messageType := "m.text"
	if message.Content == "" && len(attachments) > 0 {
		messageType = "m.file"
	}
	return messages.MessagingInbound{
		EventID:              account.Platform + ":" + message.ID,
		Platform:             account.Platform,
		SenderDisplayName:    name,
		MessageType:          messageType,
		Body:                 message.Content,
		IsDirectMessage:      true,
		Timestamp:            message.Timestamp.UTC(),
		ReceivingAccountID:   account.ID,
		ReceivingAccountName: account.DisplayName,
		PlatformChannelID:    message.ChannelID,
		PlatformMessageID:    message.ID,
		SenderPlatformUserID: message.Author.ID,
		Attachments:          attachments,
	}, true
}

func (m *Manager) received(account Account, message *discordgo.Message) {
	inbound, ok := InboundOf(account, message)
	if !ok {
		return
	}
	m.mu.Lock()
	if c := m.connections[account.ID]; c != nil {
		c.state.MessagesReceived++
		c.state.LastReceivedAt = m.Now()
	}
	m.mu.Unlock()
	if m.Publisher == nil {
		log.Printf("bot accounts: %s got %s from %s, NOT published: NATS_URL is not set", account.ID, inbound.EventID, inbound.SenderPlatformUserID)
		return
	}
	wait := time.Second
	var err error
	for attempt := 1; attempt <= m.PublishAttempts; attempt++ {
		if err = m.Publisher.JetPublish(messages.MessagingInboundSubject, inbound); err == nil {
			log.Printf("bot accounts: published %s from %s to %s (%s)", inbound.EventID, inbound.SenderPlatformUserID, account.ID, account.DisplayName)
			return
		}
		log.Printf("bot accounts: publish %s, attempt %d of %d: %v", inbound.EventID, attempt, m.PublishAttempts, err)
		m.Sleep(wait)
		wait *= 2
	}
	m.mu.Lock()
	if c := m.connections[account.ID]; c != nil {
		c.state.PublishFailures++
	}
	m.mu.Unlock()
	log.Printf("bot accounts: LOST %s from %s to %s from the bus after %d attempts: %v (message-store's bot importer still files it)", inbound.EventID, inbound.SenderPlatformUserID, account.ID, m.PublishAttempts, err)
}

// States answers every bot account and its connection, by account id.
func (m *Manager) States() []AccountState {
	m.mu.Lock()
	defer m.mu.Unlock()
	states := make([]AccountState, 0, len(m.connections))
	for _, c := range m.connections {
		states = append(states, c.state)
	}
	sort.Slice(states, func(i, j int) bool { return states[i].ID < states[j].ID })
	return states
}

// SendResult is what a send as a bot posted.
// File is one file a bot sends with a message.
type File struct {
	Name        string
	ContentType string
	Data        []byte
}

// Discord's own limits on what one bot message carries.
const (
	MaximumFilesPerMessage = 10
	MaximumFileBytes       = 10 << 20
)

type SendResult struct {
	AccountID  string   `json:"account_id"`
	ChannelID  string   `json:"channel_id"`
	MessageIDs []string `json:"message_ids"`
}

// Send posts body as the bot, to the DM channel with userID or to
// channelID (exactly one is set), split at Discord's limit. It mentions
// nobody: a reply is text, not a ping.
func (m *Manager) Send(accountID, userID, channelID, body string, files []File) (SendResult, error) {
	if len(files) > MaximumFilesPerMessage {
		return SendResult{}, fmt.Errorf("%d files: a message carries at most %d", len(files), MaximumFilesPerMessage)
	}
	for _, file := range files {
		if file.Name == "" || len(file.Data) == 0 {
			return SendResult{}, fmt.Errorf("a file needs a name and some bytes")
		}
		if len(file.Data) > MaximumFileBytes {
			return SendResult{}, fmt.Errorf("%s is %d bytes: a bot's file is at most %d", file.Name, len(file.Data), MaximumFileBytes)
		}
	}
	m.mu.Lock()
	c := m.connections[accountID]
	var gateway Gateway
	if c != nil {
		gateway = c.gateway
	}
	m.mu.Unlock()
	if c == nil {
		return SendResult{}, fmt.Errorf("%s: %w", accountID, ErrUnknownAccount)
	}
	if gateway == nil {
		return SendResult{}, fmt.Errorf("%s: %w (%s)", accountID, ErrNotConnected, c.state.LastError)
	}
	if channelID == "" {
		channel, err := gateway.UserChannelCreate(userID)
		if err != nil {
			return SendResult{}, fmt.Errorf("open the direct message with %s: %w", userID, err)
		}
		channelID = channel.ID
	}
	result := SendResult{AccountID: accountID, ChannelID: channelID}
	parts := SplitForDiscord(body, DiscordMessageLimit)
	if len(parts) == 0 {
		// Files alone: one message with no text.
		parts = []string{""}
	}
	for index, part := range parts {
		send := &discordgo.MessageSend{
			Content:         part,
			AllowedMentions: &discordgo.MessageAllowedMentions{Parse: []discordgo.AllowedMentionType{}},
		}
		// The files go with the last part, after all the text.
		if index == len(parts)-1 {
			for _, file := range files {
				send.Files = append(send.Files, &discordgo.File{Name: file.Name, ContentType: file.ContentType, Reader: bytes.NewReader(file.Data)})
			}
		}
		sent, err := gateway.ChannelMessageSendComplex(channelID, send)
		if err != nil {
			return result, fmt.Errorf("send part %d of the message to channel %s (parts before it were sent: %v): %w", len(result.MessageIDs)+1, channelID, result.MessageIDs, err)
		}
		result.MessageIDs = append(result.MessageIDs, sent.ID)
	}
	return result, nil
}

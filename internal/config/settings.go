// Package config declares every environment variable multichat's three
// binaries read, and reads them through llm-bridge's servicesettings registry.
//
// The server serves its own declarations at GET /api/settings. multichat-send
// and multichat-mcp are clients that start in other processes' environments
// (scheduler jobs, tool-store's MCP spawns), so each has a registry of its own
// that owns no prefix and serves nothing: it exists so the source-scan test
// holds their reads to a declaration too.
package config

import (
	"fmt"

	"github.com/kayushkin/llm-bridge/msg"
	"github.com/kayushkin/llm-bridge/servicesettings"
)

// ServiceName is the server's name in its own settings description, as
// healthcheck and the repo know it.
const ServiceName = "multichat"

// OwnedEnvironmentVariablePrefixes are the prefixes of the variables that are
// the server's alone. A set variable carrying one that SettingDefinitions does
// not declare stops the server from starting: it is a misspelling or a
// leftover, and either way someone believes it does something.
//
// Each is a full variable name, not "MULTICHAT_". MULTICHAT_URL is how
// kanban-store and multichat-send find this service, so one shared environment
// file carrying it would stop the server over a variable that was never meant
// for it. So MULTICHAT_DB_PATH and MATRIX_TOKENS are refused, and
// MULTICHAT_URL is not. PORT, NATS_URL, LLM_BRIDGE_URL and the API keys are
// declared and not owned: other services read the same names.
var OwnedEnvironmentVariablePrefixes = []string{
	"MULTICHAT_LISTEN",
	"MULTICHAT_DB",
	"MULTICHAT_TOKEN",
	"SYNAPSE_URL",
	"MATRIX_TOKEN",
	"TELEGRAM_DB",
	"META_DB",
	"DISCORD_DB",
	"WHATSAPP_DB",
	"GMESSAGES_DB",
	"SIGNAL_DB",
	"DISCORD_EMOJI_BOT_CREDENTIAL_ID",
	"SHORT_NAMER_MODEL_ROLE",
}

// Keys of the server's settings, as GET /api/settings names them.
const (
	SettingListenHost                       = "listen_host"
	SettingListenPort                       = "listen_port"
	SettingDatabasePath                     = "database_path"
	SettingAPIToken                         = "api_token"
	SettingSynapseURL                       = "synapse_url"
	SettingMatrixToken                      = "matrix_token"
	SettingTelegramBridgeDatabasePath       = "telegram_bridge_database_path"
	SettingMetaBridgeDatabasePath           = "meta_bridge_database_path"
	SettingDiscordBridgeDatabasePath        = "discord_bridge_database_path"
	SettingWhatsAppBridgeDatabasePath       = "whatsapp_bridge_database_path"
	SettingGoogleMessagesBridgeDatabasePath = "google_messages_bridge_database_path"
	SettingSignalBridgeDatabasePath         = "signal_bridge_database_path"
	SettingPeopleStoreURL                   = "people_store_url"
	SettingDiscordEmojiBotCredential        = "discord_emoji_bot_credential_id"
	SettingAuthStoreURL                     = "auth_store_url"
	SettingAuthStoreToken                   = "auth_store_token"
	SettingNATSURL                          = "nats_url"
	SettingLLMBridgeServerURL               = "llm_bridge_server_url"
	SettingLLMBridgeServiceToken            = "llm_bridge_service_token"
	SettingOpenAIAPIKey                     = "openai_api_key"
	SettingAnthropicAPIKey                  = "anthropic_api_key"
	SettingShortNameStoreURL                = "short_name_store_url"
	SettingShortNamerModelRole              = "short_namer_model_role"
)

// The values in force with nothing set. The shipped unit sets the port and an
// absolute database path; these defaults exist so a bare run from the checkout
// behaves as it always did.
const (
	// DefaultListenHost is loopback: nginx, dash's /api/multichat proxy and
	// every MULTICHAT_URL reach it on this host.
	DefaultListenHost   = "127.0.0.1"
	DefaultListenPort   = 8402
	DefaultDatabasePath = "multichat.db"
	// DefaultShortNamerModelRole is model-store's "balanced" role. Naming a
	// chat is a short judgement over a page of facts, and the pass runs
	// hourly, so the strongest and dearest role buys little.
	DefaultShortNamerModelRole = "balanced"
)

// SettingDefinitions declares every environment variable the server reads.
//
// Nothing here is Editable: the server stores no setting of its own, so only
// GET /api/settings is mounted.
//
// Every secret stays a string. servicesettings.New quotes a value it cannot
// parse, so a secret of any other type could reach a log through its own error.
func SettingDefinitions() []servicesettings.Definition {
	return []servicesettings.Definition{
		{Key: SettingListenHost, EnvironmentVariable: "MULTICHAT_LISTEN_HOST", Kind: msg.ServiceSettingKindWiring, ValueType: msg.ServiceSettingValueTypeString, Default: DefaultListenHost,
			Description: "The interface the HTTP server listens on. The default is loopback, because nginx, dash and every MULTICHAT_URL are on this host; an empty value or 0.0.0.0 listens on every interface."},
		{Key: SettingListenPort, EnvironmentVariable: "PORT", Kind: msg.ServiceSettingKindWiring, ValueType: msg.ServiceSettingValueTypeInteger, Default: fmt.Sprint(DefaultListenPort),
			Description: "The port the HTTP server listens on, on the interface MULTICHAT_LISTEN_HOST names. Changing it moves the service: nginx's chat.kayushkin.com vhost, dash's /api/multichat proxy and every MULTICHAT_URL must be told the new address."},
		{Key: SettingDatabasePath, EnvironmentVariable: "MULTICHAT_DB", Kind: msg.ServiceSettingKindPath, ValueType: msg.ServiceSettingValueTypeString, Default: DefaultDatabasePath,
			Description: "The SQLite file that holds contacts, tags, loads, inbound rules and dispatches. The default is relative to the working directory, so the unit pins it absolutely. Changing it starts the service on whatever database is there, or a new empty one that answers every route as if nothing were lost."},
		{Key: SettingAPIToken, EnvironmentVariable: "MULTICHAT_TOKEN", Kind: msg.ServiceSettingKindSecret, ValueType: msg.ServiceSettingValueTypeString,
			Description: "The bearer token every route except /auth/login and /health asks for. Unset turns authentication OFF on an API published at chat.kayushkin.com; deploy.sh refuses to deploy without it and asserts a 401 afterwards."},
		{Key: SettingSynapseURL, EnvironmentVariable: "SYNAPSE_URL", Kind: msg.ServiceSettingKindWiring, ValueType: msg.ServiceSettingValueTypeString,
			Description: "Where the Synapse homeserver's client API answers. With MATRIX_TOKEN it configures the Matrix client; without either, every conversation, contact and send route has nothing to talk to and inbound cannot start."},
		{Key: SettingMatrixToken, EnvironmentVariable: "MATRIX_TOKEN", Kind: msg.ServiceSettingKindSecret, ValueType: msg.ServiceSettingValueTypeString,
			Description: "The Matrix access token the service reads and sends as. Changing it changes who every message is sent from."},
		{Key: SettingTelegramBridgeDatabasePath, EnvironmentVariable: "TELEGRAM_DB", Kind: msg.ServiceSettingKindPath, ValueType: msg.ServiceSettingValueTypeString,
			Description: "The mautrix-telegram bridge's SQLite file, opened read-only for phone-number lookups and to find our own Telegram puppet. Unset leaves both out."},
		{Key: SettingMetaBridgeDatabasePath, EnvironmentVariable: "META_DB", Kind: msg.ServiceSettingKindPath, ValueType: msg.ServiceSettingValueTypeString,
			Description: "The mautrix-meta bridge's SQLite file, opened read-only to tell Facebook from Instagram rooms and to find our own Meta logins. Unset leaves both out."},
		{Key: SettingDiscordBridgeDatabasePath, EnvironmentVariable: "DISCORD_DB", Kind: msg.ServiceSettingKindPath, ValueType: msg.ServiceSettingValueTypeString,
			Description: "The mautrix-discord bridge's SQLite file. Read to find our own Discord puppet, so our own Discord messages are not taken for someone else's, to answer GET /discord/status and to list the bridged servers whose emoji GET /emoji offers. Written in one place: reacting with a custom emoji the bridge has not seen adds it to the bridge's discord_file table, so the service needs write access to the file. The daily Nitro check reads the account's Discord login from its user table for one GET /users/@me. Unset leaves the puppet out and makes /discord/status and the emoji routes a 503."},
		{Key: SettingWhatsAppBridgeDatabasePath, EnvironmentVariable: "WHATSAPP_DB", Kind: msg.ServiceSettingKindPath, ValueType: msg.ServiceSettingValueTypeString,
			Description: "The mautrix-whatsapp bridge's SQLite file, opened read-only at start to find our own WhatsApp LID puppet (@whatsapp_lid-…), which sits in every WhatsApp room beside the other people. Unset leaves it out, so every WhatsApp room counts us as one of its members and no WhatsApp DM names its partner."},
		{Key: SettingGoogleMessagesBridgeDatabasePath, EnvironmentVariable: "GMESSAGES_DB", Kind: msg.ServiceSettingKindPath, ValueType: msg.ServiceSettingValueTypeString,
			Description: "The mautrix-gmessages bridge's SQLite file, opened read-only at start to find our own Google Messages puppets (@gmessages_<id_prefix>.<self participant id>), one per phone login, so the texts we send from the phone are not taken for someone else's. Unset leaves them out."},
		{Key: SettingSignalBridgeDatabasePath, EnvironmentVariable: "SIGNAL_DB", Kind: msg.ServiceSettingKindPath, ValueType: msg.ServiceSettingValueTypeString,
			Description: "The mautrix-signal bridge's SQLite file, opened read-only by the phone-number matcher for each Signal contact's number. Unset leaves Signal out of phone matching."},
		{Key: SettingPeopleStoreURL, EnvironmentVariable: "PEOPLE_STORE_URL", Kind: msg.ServiceSettingKindWiring, ValueType: msg.ServiceSettingValueTypeString,
			Description: "Where people-store answers. It owns every person (person_000001) and every bridge account attached to one, with who linked it and why; GET /contacts/unified groups by its person ids and names, and every link, unlink and phone-number match writes there. Unset makes the link routes and /contacts/unified a 503 and turns phone matching off."},
		{Key: SettingDiscordEmojiBotCredential, EnvironmentVariable: "DISCORD_EMOJI_BOT_CREDENTIAL_ID", Kind: msg.ServiceSettingKindWiring, ValueType: msg.ServiceSettingValueTypeString,
			Description: "The auth-store credential id whose api_key is a Discord bot token. POST /emoji/discord/refresh lists each bridged server's custom emoji with it, so the bot must be a member of every bridged server. Unset makes that refresh a 503."},
		{Key: SettingAuthStoreURL, EnvironmentVariable: "AUTH_STORE_URL", Kind: msg.ServiceSettingKindWiring, ValueType: msg.ServiceSettingValueTypeString,
			Description: "Where auth-store answers. The emoji refresh resolves the Discord bot token there. Unset makes the refresh a 503."},
		{Key: SettingAuthStoreToken, EnvironmentVariable: "AUTH_STORE_TOKEN", Kind: msg.ServiceSettingKindSecret, ValueType: msg.ServiceSettingValueTypeString,
			Description: "The bearer auth-store asks of the emoji refresh. Unset makes the refresh a 503."},
		{Key: SettingNATSURL, EnvironmentVariable: "NATS_URL", Kind: msg.ServiceSettingKindWiring, ValueType: msg.ServiceSettingValueTypeString,
			Description: "Where NATS answers. Set, it turns inbound on: the sync worker publishes messaging.inbound and the dispatcher applies the inbound rules. It needs the Matrix client and LLM_BRIDGE_URL beside it, and the service refuses to start without them. Unset, inbound is off."},
		{Key: SettingLLMBridgeServerURL, EnvironmentVariable: "LLM_BRIDGE_URL", Kind: msg.ServiceSettingKindWiring, ValueType: msg.ServiceSettingValueTypeString,
			Description: "Where llm-bridge-server answers. An inbound rule's instance is checked there when the rule is written and again when it fires."},
		{Key: SettingLLMBridgeServiceToken, EnvironmentVariable: "LLMBRIDGE_SERVICE_TOKEN", Kind: msg.ServiceSettingKindSecret, ValueType: msg.ServiceSettingValueTypeString,
			Description: "The token llm-bridge-server asks of every caller. Without it an instance check is a 401, which reads as an unknown instance to whoever is saving a rule."},
		{Key: SettingOpenAIAPIKey, EnvironmentVariable: "OPENAI_API_KEY", Kind: msg.ServiceSettingKindSecret, ValueType: msg.ServiceSettingValueTypeString,
			Description: "The OpenAI key the /ai/summarize, /ai/escalation-scan and /ai/auto-tag routes spend. Unset, those routes answer 500 \"No AI API key configured\", except summarize, which then tries ANTHROPIC_API_KEY."},
		{Key: SettingAnthropicAPIKey, EnvironmentVariable: "ANTHROPIC_API_KEY", Kind: msg.ServiceSettingKindSecret, ValueType: msg.ServiceSettingValueTypeString,
			Description: "The Anthropic key /ai/summarize uses when OPENAI_API_KEY is unset. Nothing else reads it."},
		{Key: SettingShortNameStoreURL, EnvironmentVariable: "SHORT_NAME_STORE_URL", Kind: msg.ServiceSettingKindWiring, ValueType: msg.ServiceSettingValueTypeString,
			Description: "Where short-name-store answers. POST /short-names/assign reads the names in namespace messages there and writes the new ones there. Unset makes that route a 503."},
		{Key: SettingShortNamerModelRole, EnvironmentVariable: "SHORT_NAMER_MODEL_ROLE", Kind: msg.ServiceSettingKindBehaviour, ValueType: msg.ServiceSettingValueTypeString, Default: DefaultShortNamerModelRole,
			Description: "The model-store role POST /short-names/assign asks llm-bridge-server's POST /oneshot for when it has a model propose short names. Changing it changes which model picks names, and names are permanent once written."},
	}
}

// NewSettingsRegistry reads the server's settings from environment. It fails
// on a set variable under an owned prefix that nobody declared, and on a port
// that is not a whole number.
func NewSettingsRegistry(environment servicesettings.Environment) (*servicesettings.Registry, error) {
	return servicesettings.New(ServiceName, OwnedEnvironmentVariablePrefixes, SettingDefinitions(), environment)
}

// Config is what the server starts with.
type Config struct {
	ListenHost   string
	ListenPort   int
	DatabasePath string

	// APIToken is empty when authentication is off.
	APIToken string

	// SynapseURL and MatrixToken configure the Matrix client only together.
	SynapseURL  string
	MatrixToken string

	TelegramBridgeDatabasePath       string
	MetaBridgeDatabasePath           string
	DiscordBridgeDatabasePath        string
	WhatsAppBridgeDatabasePath       string
	GoogleMessagesBridgeDatabasePath string
	SignalBridgeDatabasePath         string
	PeopleStoreURL                   string

	// DiscordEmojiBotCredentialID, AuthStoreURL and AuthStoreToken let the
	// emoji refresh fetch the Discord bot token from auth-store.
	DiscordEmojiBotCredentialID string
	AuthStoreURL                string
	AuthStoreToken              string

	// NATSURL is empty when inbound is off. When it is set, the Matrix client
	// is configured and LLMBridgeServerURL is set too.
	NATSURL               string
	LLMBridgeServerURL    string
	LLMBridgeServiceToken string

	OpenAIAPIKey    string
	AnthropicAPIKey string

	// ShortNameStoreURL is empty when the short-name pass cannot run.
	ShortNameStoreURL   string
	ShortNamerModelRole string
}

// MatrixConfigured reports whether both halves of the Matrix client are set.
func (c Config) MatrixConfigured() bool {
	return c.SynapseURL != "" && c.MatrixToken != ""
}

// Load reads the server's configuration from registry. It fails when NATS_URL
// is set without what inbound needs beside it, as main did before 2026-09-21;
// the check lives here so the live-environment test deploy.sh runs asks it too.
// An error names the variable and never its value.
func Load(registry *servicesettings.Registry) (Config, error) {
	loaded := Config{
		ListenHost:                       registry.String(SettingListenHost),
		ListenPort:                       registry.Integer(SettingListenPort),
		DatabasePath:                     registry.String(SettingDatabasePath),
		APIToken:                         registry.String(SettingAPIToken),
		SynapseURL:                       registry.String(SettingSynapseURL),
		MatrixToken:                      registry.String(SettingMatrixToken),
		TelegramBridgeDatabasePath:       registry.String(SettingTelegramBridgeDatabasePath),
		MetaBridgeDatabasePath:           registry.String(SettingMetaBridgeDatabasePath),
		DiscordBridgeDatabasePath:        registry.String(SettingDiscordBridgeDatabasePath),
		WhatsAppBridgeDatabasePath:       registry.String(SettingWhatsAppBridgeDatabasePath),
		GoogleMessagesBridgeDatabasePath: registry.String(SettingGoogleMessagesBridgeDatabasePath),
		SignalBridgeDatabasePath:         registry.String(SettingSignalBridgeDatabasePath),
		PeopleStoreURL:                   registry.String(SettingPeopleStoreURL),
		DiscordEmojiBotCredentialID:      registry.String(SettingDiscordEmojiBotCredential),
		AuthStoreURL:                     registry.String(SettingAuthStoreURL),
		AuthStoreToken:                   registry.String(SettingAuthStoreToken),
		NATSURL:                          registry.String(SettingNATSURL),
		LLMBridgeServerURL:               registry.String(SettingLLMBridgeServerURL),
		LLMBridgeServiceToken:            registry.String(SettingLLMBridgeServiceToken),
		OpenAIAPIKey:                     registry.String(SettingOpenAIAPIKey),
		AnthropicAPIKey:                  registry.String(SettingAnthropicAPIKey),
		ShortNameStoreURL:                registry.String(SettingShortNameStoreURL),
		ShortNamerModelRole:              registry.String(SettingShortNamerModelRole),
	}
	if loaded.NATSURL != "" && !loaded.MatrixConfigured() {
		return Config{}, fmt.Errorf("NATS_URL is set but the Matrix client is not configured (SYNAPSE_URL and MATRIX_TOKEN); inbound needs both")
	}
	if loaded.NATSURL != "" && loaded.LLMBridgeServerURL == "" {
		return Config{}, fmt.Errorf("NATS_URL is set but LLM_BRIDGE_URL is not; inbound rules dispatch to llm-bridge-server instances")
	}
	return loaded, nil
}

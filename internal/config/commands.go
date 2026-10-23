package config

import (
	"github.com/kayushkin/llm-bridge/msg"
	"github.com/kayushkin/llm-bridge/servicesettings"
)

// The two client binaries start in environments that are not the server's:
// multichat-send in a scheduler job's or an agent's shell, multichat-mcp in the
// process tool-store spawns for a session. Both environments carry many other
// services' variables, so neither registry owns a prefix. Each fails only on
// what the command cannot run without.

// SendCommandName is multichat-send's name in its settings errors.
const SendCommandName = "multichat-send"

// Keys of multichat-send's settings.
const (
	SendSettingMultichatURL   = "multichat_url"
	SendSettingAuthStoreURL   = "auth_store_url"
	SendSettingAuthStoreToken = "auth_store_token"
)

// SendCommandSettingDefinitions declares every environment variable
// multichat-send reads.
func SendCommandSettingDefinitions() []servicesettings.Definition {
	return []servicesettings.Definition{
		{Key: SendSettingMultichatURL, EnvironmentVariable: "MULTICHAT_URL", Kind: msg.ServiceSettingKindWiring, ValueType: msg.ServiceSettingValueTypeString, Required: true,
			Description: "Where the multichat server answers; the message is sent through its POST /api/messages/send."},
		{Key: SendSettingAuthStoreURL, EnvironmentVariable: "AUTH_STORE_URL", Kind: msg.ServiceSettingKindWiring, ValueType: msg.ServiceSettingValueTypeString, Required: true,
			Description: "Where auth-store answers. multichat's API token is resolved there from provider \"multichat\" on every send."},
		{Key: SendSettingAuthStoreToken, EnvironmentVariable: "AUTH_STORE_TOKEN", Kind: msg.ServiceSettingKindSecret, ValueType: msg.ServiceSettingValueTypeString, Required: true,
			Description: "The bearer token auth-store takes."},
	}
}

// NewSendCommandSettingsRegistry reads multichat-send's settings from
// environment. It owns no prefix, so only CheckRequired can refuse.
func NewSendCommandSettingsRegistry(environment servicesettings.Environment) (*servicesettings.Registry, error) {
	return servicesettings.New(SendCommandName, nil, SendCommandSettingDefinitions(), environment)
}

// MCPServerName is multichat-mcp's name in its settings errors.
const MCPServerName = "multichat-mcp"

// Keys of multichat-mcp's settings.
const MCPSettingAPIToken = "api_token"

// MCPServerSettingDefinitions declares every environment variable multichat-mcp
// reads. The server's address is its -url flag, which the tool-store row
// passes; it is not a setting.
func MCPServerSettingDefinitions() []servicesettings.Definition {
	return []servicesettings.Definition{
		{Key: MCPSettingAPIToken, EnvironmentVariable: "MULTICHAT_TOKEN", Kind: msg.ServiceSettingKindSecret, ValueType: msg.ServiceSettingValueTypeString, Required: true,
			Description: "The multichat server's bearer token. tool-store provisions it from auth-store provider \"multichat\" when it spawns the MCP server."},
	}
}

// NewMCPServerSettingsRegistry reads multichat-mcp's settings from environment.
// It owns no prefix, so only CheckRequired can refuse.
func NewMCPServerSettingsRegistry(environment servicesettings.Environment) (*servicesettings.Registry, error) {
	return servicesettings.New(MCPServerName, nil, MCPServerSettingDefinitions(), environment)
}

// ReactionBackfillCommandName is multichat-backfill-reactions' name in its
// settings errors.
const ReactionBackfillCommandName = "multichat-backfill-reactions"

// Keys of multichat-backfill-reactions' settings.
const (
	ReactionBackfillSettingSynapseURL  = "synapse_url"
	ReactionBackfillSettingMatrixToken = "matrix_token"
)

// ReactionBackfillCommandSettingDefinitions declares every environment
// variable multichat-backfill-reactions reads: the same Synapse address and
// token the server uses, from the server's environment file. The database is
// its -db flag.
func ReactionBackfillCommandSettingDefinitions() []servicesettings.Definition {
	return []servicesettings.Definition{
		{Key: ReactionBackfillSettingSynapseURL, EnvironmentVariable: "SYNAPSE_URL", Kind: msg.ServiceSettingKindWiring, ValueType: msg.ServiceSettingValueTypeString, Required: true,
			Description: "The Synapse homeserver the reactions are read from."},
		{Key: ReactionBackfillSettingMatrixToken, EnvironmentVariable: "MATRIX_TOKEN", Kind: msg.ServiceSettingKindSecret, ValueType: msg.ServiceSettingValueTypeString, Required: true,
			Description: "The access token of the Matrix account multichat reads as (@admin)."},
	}
}

// NewReactionBackfillCommandSettingsRegistry reads the command's settings
// from environment. It owns no prefix, so only CheckRequired can refuse.
func NewReactionBackfillCommandSettingsRegistry(environment servicesettings.Environment) (*servicesettings.Registry, error) {
	return servicesettings.New(ReactionBackfillCommandName, nil, ReactionBackfillCommandSettingDefinitions(), environment)
}

// RoomBackfillCommandName is multichat-backfill-room's name in its settings
// errors.
const RoomBackfillCommandName = "multichat-backfill-room"

// Keys of multichat-backfill-room's settings.
const (
	RoomBackfillSettingSynapseURL  = "synapse_url"
	RoomBackfillSettingMatrixToken = "matrix_token"
)

// RoomBackfillCommandSettingDefinitions declares every environment variable
// multichat-backfill-room reads: the server's Synapse address and token, from
// the server's environment file. The database and rooms are flags.
func RoomBackfillCommandSettingDefinitions() []servicesettings.Definition {
	return []servicesettings.Definition{
		{Key: RoomBackfillSettingSynapseURL, EnvironmentVariable: "SYNAPSE_URL", Kind: msg.ServiceSettingKindWiring, ValueType: msg.ServiceSettingValueTypeString, Required: true,
			Description: "The Synapse homeserver the rooms' history is read from."},
		{Key: RoomBackfillSettingMatrixToken, EnvironmentVariable: "MATRIX_TOKEN", Kind: msg.ServiceSettingKindSecret, ValueType: msg.ServiceSettingValueTypeString, Required: true,
			Description: "The access token of the Matrix account multichat reads as (@admin)."},
	}
}

// NewRoomBackfillCommandSettingsRegistry reads the command's settings from
// environment. It owns no prefix, so only CheckRequired can refuse.
func NewRoomBackfillCommandSettingsRegistry(environment servicesettings.Environment) (*servicesettings.Registry, error) {
	return servicesettings.New(RoomBackfillCommandName, nil, RoomBackfillCommandSettingDefinitions(), environment)
}

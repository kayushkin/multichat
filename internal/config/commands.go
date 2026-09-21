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

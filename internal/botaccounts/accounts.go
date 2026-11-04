// Package botaccounts connects multichat to the bot accounts we run, beside
// the operator's own accounts that reach it through Matrix.
//
// message-store owns which accounts are ours: an account of kind "bot"
// carries its platform, its user id there (handle) and the auth-store
// credential that holds its token. multichat keeps no list of its own. For
// each Discord bot it opens a gateway connection of its own (a bot may hold
// several, and each receives the bot's direct messages, so the service that
// owns the bot keeps its connection), publishes every direct message a person
// sends the bot on messaging.inbound, and sends as the bot through Discord's
// REST API.
package botaccounts

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Account is one bot account of ours, as message-store records it.
type Account struct {
	// ID is message-store's account id (message_account_000001): the key
	// every caller names the bot by.
	ID          string `json:"id"`
	Platform    string `json:"platform"`
	Handle      string `json:"handle"`
	DisplayName string `json:"display_name"`
	Kind        string `json:"kind"`
	// CredentialID is the auth-store credential whose api_key is the bot's
	// token.
	CredentialID string `json:"credential_id"`
}

// AccountKindBot is message-store's account kind for a bot we run.
const AccountKindBot = "bot"

// PlatformDiscord is the one platform whose bots multichat connects today.
const PlatformDiscord = "discord"

// MessageStoreAccounts lists accounts from message-store's GET /accounts.
type MessageStoreAccounts struct {
	BaseURL    string
	HTTPClient *http.Client
}

// DiscordBotAccounts answers every Discord bot account message-store holds.
func (m MessageStoreAccounts) DiscordBotAccounts() ([]Account, error) {
	response, err := m.HTTPClient.Get(strings.TrimRight(m.BaseURL, "/") + "/accounts")
	if err != nil {
		return nil, fmt.Errorf("message-store: list accounts: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("message-store: list accounts: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("message-store: list accounts: %d %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	var all []Account
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, fmt.Errorf("message-store: list accounts: %w", err)
	}
	var bots []Account
	for _, account := range all {
		if account.Kind != AccountKindBot || account.Platform != PlatformDiscord {
			continue
		}
		if account.CredentialID == "" {
			return nil, fmt.Errorf("message-store: bot account %s (%s) names no credential_id, so its token cannot be found", account.ID, account.DisplayName)
		}
		bots = append(bots, account)
	}
	return bots, nil
}

// AuthStoreTokens resolves a bot's token from auth-store at use.
type AuthStoreTokens struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

// BotToken answers the api_key of one credential.
func (a AuthStoreTokens) BotToken(credentialID, reason string) (string, error) {
	request, err := http.NewRequest(http.MethodGet, strings.TrimRight(a.BaseURL, "/")+"/api/credentials/"+url.PathEscape(credentialID)+"/resolve", nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+a.Token)
	request.Header.Set("X-Auth-App", "multichat")
	request.Header.Set("X-Auth-Reason", reason)
	response, err := a.HTTPClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("auth-store: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("auth-store: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("auth-store: resolve credential %s: %d %s", credentialID, response.StatusCode, strings.TrimSpace(string(data)))
	}
	var out struct {
		AuthType string `json:"auth_type"`
		APIKey   string `json:"api_key"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("auth-store: credential %s: %w", credentialID, err)
	}
	if out.AuthType != "api_key" || out.APIKey == "" {
		return "", fmt.Errorf("auth-store: credential %s is %q with no api_key; expected a bot token", credentialID, out.AuthType)
	}
	return out.APIKey, nil
}

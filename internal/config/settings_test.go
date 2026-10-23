package config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/kayushkin/llm-bridge/msg"
	"github.com/kayushkin/llm-bridge/servicesettings"
)

func loadFrom(variables map[string]string) (Config, error) {
	registry, err := NewSettingsRegistry(servicesettings.MapEnvironment(variables))
	if err != nil {
		return Config{}, err
	}
	if err := registry.CheckRequired(); err != nil {
		return Config{}, err
	}
	return Load(registry)
}

func mustLoadFrom(t *testing.T, variables map[string]string) Config {
	t.Helper()
	loaded, err := loadFrom(variables)
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

// The registry gives the server what os.Getenv gave it before 2026-09-21: the
// same defaults with nothing set, and the operator's values when they are.
func TestLoadReadsTheSameValuesItAlwaysDid(t *testing.T) {
	want := Config{ListenHost: "127.0.0.1", ListenPort: 8402, DatabasePath: "multichat.db", ShortNamerModelRole: "balanced"}
	if got := mustLoadFrom(t, nil); got != want {
		t.Errorf("Load with nothing set = %+v, want %+v", got, want)
	}

	want = Config{
		ListenHost:                       "192.0.2.1",
		ListenPort:                       9402,
		DatabasePath:                     "/srv/multichat.db",
		APIToken:                         "an-api-token",
		SynapseURL:                       "http://synapse.example:1",
		MatrixToken:                      "a-matrix-token",
		TelegramBridgeDatabasePath:       "/srv/telegram.db",
		MetaBridgeDatabasePath:           "/srv/meta.db",
		DiscordBridgeDatabasePath:        "/srv/discord.db",
		WhatsAppBridgeDatabasePath:       "/srv/whatsapp.db",
		GoogleMessagesBridgeDatabasePath: "/srv/gmessages.db",
		SignalBridgeDatabasePath:         "/srv/signal.db",
		PeopleStoreURL:                   "http://people-store.example:1",
		DiscordEmojiBotCredentialID:      "cred_discord_bot",
		AuthStoreURL:                     "http://auth-store.example:1",
		AuthStoreToken:                   "an-auth-store-token",
		NATSURL:                          "nats://nats.example:1",
		LLMBridgeServerURL:               "http://bridge.example:1",
		LLMBridgeServiceToken:            "a-bridge-service-token",
		OpenAIAPIKey:                     "an-openai-key",
		AnthropicAPIKey:                  "an-anthropic-key",
		ShortNameStoreURL:                "http://short-name-store.example:1",
		ShortNamerModelRole:              "efficient",
	}
	got := mustLoadFrom(t, map[string]string{
		"MULTICHAT_LISTEN_HOST":           "192.0.2.1",
		"PORT":                            "9402",
		"MULTICHAT_DB":                    "/srv/multichat.db",
		"MULTICHAT_TOKEN":                 "an-api-token",
		"SYNAPSE_URL":                     "http://synapse.example:1",
		"MATRIX_TOKEN":                    "a-matrix-token",
		"TELEGRAM_DB":                     "/srv/telegram.db",
		"META_DB":                         "/srv/meta.db",
		"DISCORD_DB":                      "/srv/discord.db",
		"WHATSAPP_DB":                     "/srv/whatsapp.db",
		"GMESSAGES_DB":                    "/srv/gmessages.db",
		"SIGNAL_DB":                       "/srv/signal.db",
		"PEOPLE_STORE_URL":                "http://people-store.example:1",
		"DISCORD_EMOJI_BOT_CREDENTIAL_ID": "cred_discord_bot",
		"AUTH_STORE_URL":                  "http://auth-store.example:1",
		"AUTH_STORE_TOKEN":                "an-auth-store-token",
		"NATS_URL":                        "nats://nats.example:1",
		"LLM_BRIDGE_URL":                  "http://bridge.example:1",
		"LLMBRIDGE_SERVICE_TOKEN":         "a-bridge-service-token",
		"OPENAI_API_KEY":                  "an-openai-key",
		"ANTHROPIC_API_KEY":               "an-anthropic-key",
		"SHORT_NAME_STORE_URL":            "http://short-name-store.example:1",
		"SHORT_NAMER_MODEL_ROLE":          "efficient",
	})
	if got != want {
		t.Errorf("Load = %+v, want %+v", got, want)
	}

	// A variable set to the empty string is the same as unset, as it was when
	// main compared os.Getenv to "".
	got = mustLoadFrom(t, map[string]string{"PORT": "", "MULTICHAT_DB": "", "MULTICHAT_TOKEN": ""})
	if got.ListenPort != 8402 || got.DatabasePath != "multichat.db" || got.APIToken != "" {
		t.Errorf("empty variables: %+v", got)
	}
}

// Every declared variable is set to a value of its own above. If a setting is
// added and that test is not told, this fails.
func TestTheSameValuesTestCoversEveryDeclaredSetting(t *testing.T) {
	if got, want := len(SettingDefinitions()), 23; got != want {
		t.Fatalf("%d settings are declared and TestLoadReadsTheSameValuesItAlwaysDid sets %d: add the new one there, then change this number", got, want)
	}
}

// Before 2026-09-21 PORT=eighty reached http.ListenAndServe as ":eighty" and
// failed there, after the database was open and the bus connected. Now the
// registry refuses it.
func TestAPortThatIsNotAWholeNumberIsRefused(t *testing.T) {
	_, err := loadFrom(map[string]string{"PORT": "eighty"})
	if err == nil || !strings.Contains(err.Error(), "PORT") {
		t.Fatalf("PORT=eighty = %v, want a refusal naming it", err)
	}
}

// NATS_URL turns inbound on, which needs the Matrix client and llm-bridge-server
// beside it. main refused the start without them before 2026-09-21; Load does
// now, so deploy.sh's live-environment check asks it before the stop.
func TestInboundWithoutWhatItNeedsIsRefused(t *testing.T) {
	complete := map[string]string{
		"NATS_URL":       "nats://nats.example:1",
		"SYNAPSE_URL":    "http://synapse.example:1",
		"MATRIX_TOKEN":   "a-matrix-token",
		"LLM_BRIDGE_URL": "http://bridge.example:1",
	}
	if _, err := loadFrom(complete); err != nil {
		t.Fatalf("inbound with everything it needs: %v", err)
	}
	for _, missing := range []string{"SYNAPSE_URL", "MATRIX_TOKEN", "LLM_BRIDGE_URL"} {
		variables := map[string]string{}
		for name, value := range complete {
			if name != missing {
				variables[name] = value
			}
		}
		_, err := loadFrom(variables)
		if err == nil || !strings.Contains(err.Error(), "NATS_URL is set") {
			t.Errorf("NATS_URL without %s = %v, want a refusal", missing, err)
			continue
		}
		if strings.Contains(err.Error(), "a-matrix-token") {
			t.Errorf("the refusal quotes the Matrix token: %v", err)
		}
	}
	// Without NATS_URL, inbound is off and nothing else is needed.
	if _, err := loadFrom(map[string]string{"SYNAPSE_URL": "http://synapse.example:1"}); err != nil {
		t.Errorf("a half-configured Matrix client with inbound off was refused: %v", err)
	}
}

// MULTICHAT_URL is how other services find this one, and is not the server's to
// refuse; a misspelling of a declared name is.
func TestTheRegistryRefusesAMisspellingAndNotAVariableMeantForSomethingElse(t *testing.T) {
	for _, misspelled := range []string{"MULTICHAT_DB_PATH", "MULTICHAT_TOKENS", "SYNAPSE_URLS", "MATRIX_TOKEN_FILE", "TELEGRAM_DB_PATH", "META_DB_PATH", "DISCORD_DB_PATH"} {
		_, err := NewSettingsRegistry(servicesettings.MapEnvironment(map[string]string{misspelled: "x"}))
		if err == nil || !strings.Contains(err.Error(), misspelled+" is set and multichat declares no such setting") {
			t.Errorf("NewSettingsRegistry with %s = %v, want a refusal naming it", misspelled, err)
		}
	}
	// What the live unit carries beside its own settings (measured 2026-09-21
	// in /proc/<pid>/environ), and what other services call this one.
	notOurs := map[string]string{
		"MULTICHAT_URL":                    "http://localhost:8402",
		"KANBAN_STORE_SERVICE_TOKEN":       "x",
		"GRANT_STORE_SERVICE_TOKEN":        "x",
		"LLMBRIDGE_DEMO_LOGIN_SIGNING_KEY": "x",
		"META_PATH":                        "x",
		"SYNAPSE_SERVER_NAME":              "x",
		"PATH":                             "/bin",
	}
	if _, err := NewSettingsRegistry(servicesettings.MapEnvironment(notOurs)); err != nil {
		t.Errorf("variables meant for other services were refused: %v", err)
	}
}

// Every owned prefix is the start of a declared variable. An owned prefix that
// starts nothing guards nothing and reads as if it did.
func TestEveryOwnedPrefixStartsADeclaredVariable(t *testing.T) {
	for _, prefix := range OwnedEnvironmentVariablePrefixes {
		found := false
		for _, definition := range SettingDefinitions() {
			found = found || strings.HasPrefix(definition.EnvironmentVariable, prefix)
		}
		if !found {
			t.Errorf("owned prefix %s starts no declared variable", prefix)
		}
	}
}

// New quotes a value it cannot parse. A secret must be a string, which always
// parses, or its value could reach the log through its own refusal.
func TestEverySecretIsAString(t *testing.T) {
	definitionLists := map[string][]servicesettings.Definition{
		ServiceName:     SettingDefinitions(),
		SendCommandName: SendCommandSettingDefinitions(),
		MCPServerName:   MCPServerSettingDefinitions(),
	}
	wantSecrets := map[string]string{
		ServiceName:     "MULTICHAT_TOKEN MATRIX_TOKEN AUTH_STORE_TOKEN LLMBRIDGE_SERVICE_TOKEN OPENAI_API_KEY ANTHROPIC_API_KEY",
		SendCommandName: "AUTH_STORE_TOKEN",
		MCPServerName:   "MULTICHAT_TOKEN",
	}
	for name, definitions := range definitionLists {
		secrets := []string{}
		for _, definition := range definitions {
			if definition.Kind != msg.ServiceSettingKindSecret {
				continue
			}
			secrets = append(secrets, definition.EnvironmentVariable)
			if definition.ValueType != msg.ServiceSettingValueTypeString {
				t.Errorf("%s: %s is a secret declared %s", name, definition.EnvironmentVariable, definition.ValueType)
			}
			if definition.Default != "" {
				t.Errorf("%s: %s is a secret with a default", name, definition.EnvironmentVariable)
			}
		}
		if got := strings.Join(secrets, " "); got != wantSecrets[name] {
			t.Errorf("%s: secrets declared: %s, want %s", name, got, wantSecrets[name])
		}
	}
}

// Only GET /settings is mounted, so a setting declared Editable would promise
// a write nothing serves.
func TestNoSettingIsEditable(t *testing.T) {
	for _, definition := range SettingDefinitions() {
		if definition.Editable {
			t.Errorf("%s is declared Editable, and the server stores no setting and mounts no PUT /settings/{key}", definition.Key)
		}
	}
}

// The two commands refuse to run without what they need, naming the variable,
// as requireEnv and the MCP server's own check did before 2026-09-21.
func TestTheCommandsRefuseToRunWithoutTheirSettings(t *testing.T) {
	send, err := NewSendCommandSettingsRegistry(servicesettings.MapEnvironment(map[string]string{"MULTICHAT_URL": "http://localhost:8402"}))
	if err != nil {
		t.Fatal(err)
	}
	err = send.CheckRequired()
	if err == nil || !strings.Contains(err.Error(), "AUTH_STORE_URL is unset") || !strings.Contains(err.Error(), "AUTH_STORE_TOKEN is unset") || strings.Contains(err.Error(), "MULTICHAT_URL is unset") {
		t.Errorf("multichat-send with only MULTICHAT_URL = %v, want both auth-store variables named and nothing else", err)
	}

	mcpServer, err := NewMCPServerSettingsRegistry(servicesettings.MapEnvironment(nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := mcpServer.CheckRequired(); err == nil || !strings.Contains(err.Error(), "MULTICHAT_TOKEN is unset") || !strings.Contains(err.Error(), "tool-store provisions it") {
		t.Errorf("multichat-mcp with nothing set = %v, want MULTICHAT_TOKEN named with where it comes from", err)
	}

	// Both start in environments full of other services' variables, so neither
	// owns a prefix and neither refuses a stranger.
	crowded := servicesettings.MapEnvironment(map[string]string{
		"MULTICHAT_URL": "http://localhost:8402", "AUTH_STORE_URL": "http://localhost:8303", "AUTH_STORE_TOKEN": "t",
		"MULTICHAT_TOKEN": "t", "MULTICHAT_DB": "/srv/multichat.db", "KANBAN_STORE_URL": "x",
	})
	if _, err := NewSendCommandSettingsRegistry(crowded); err != nil {
		t.Errorf("multichat-send refused a crowded environment: %v", err)
	}
	if _, err := NewMCPServerSettingsRegistry(crowded); err != nil {
		t.Errorf("multichat-mcp refused a crowded environment: %v", err)
	}
}

// Every environment variable each binary's code reads by name is declared for
// that binary. A read that is not declared is invisible on the settings page
// and escapes the startup check. The two commands are held to their own
// declarations, and everything else to the server's.
func TestEveryEnvironmentVariableTheCodeReadsIsDeclared(t *testing.T) {
	declaredFor := func(definitions []servicesettings.Definition) map[string]bool {
		declared := map[string]bool{}
		for _, definition := range definitions {
			declared[definition.EnvironmentVariable] = true
		}
		return declared
	}
	server := declaredFor(SettingDefinitions())
	commands := map[string]map[string]bool{
		filepath.Join("cmd", "multichat-send"):               declaredFor(SendCommandSettingDefinitions()),
		filepath.Join("cmd", "multichat-mcp"):                declaredFor(MCPServerSettingDefinitions()),
		filepath.Join("cmd", "multichat-backfill-reactions"): declaredFor(ReactionBackfillCommandSettingDefinitions()),
		filepath.Join("cmd", "multichat-backfill-room"):      declaredFor(RoomBackfillCommandSettingDefinitions()),
	}
	const repositoryRoot = "../.."

	filesRead := 0
	// Only cmd/ and internal/: the root also holds untracked build output and
	// .openclaw/, whose Go is not this module's.
	for _, top := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(repositoryRoot, top), func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			declared := server
			relative, _ := filepath.Rel(repositoryRoot, path)
			for directory, commandDeclared := range commands {
				if filepath.Dir(relative) == directory {
					declared = commandDeclared
				}
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			filesRead++
			ast.Inspect(file, func(node ast.Node) bool {
				call, isCall := node.(*ast.CallExpr)
				if !isCall {
					return true
				}
				selector, isSelector := call.Fun.(*ast.SelectorExpr)
				if !isSelector {
					return true
				}
				packageName, isIdentifier := selector.X.(*ast.Ident)
				if !isIdentifier || packageName.Name != "os" {
					return true
				}
				switch selector.Sel.Name {
				case "Environ":
					t.Errorf("%s reads the whole environment, which no declaration can be held to", path)
				case "Getenv", "LookupEnv":
					literal, isLiteral := call.Args[0].(*ast.BasicLit)
					if !isLiteral {
						t.Errorf("%s reads an environment variable whose name is computed, which no declaration can be held to", path)
						return true
					}
					name, _ := strconv.Unquote(literal.Value)
					if !declared[name] {
						t.Errorf("%s reads %s, which its binary's definitions do not declare", path, name)
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// The walk starts two directories above this package, which is the
	// repository root. If the package moves, the walk would read the wrong tree
	// and pass.
	for _, command := range []string{"server", "multichat-send", "multichat-mcp"} {
		if _, err := os.Stat(filepath.Join(repositoryRoot, "cmd", command, "main.go")); err != nil {
			t.Fatalf("the scan starts somewhere that is not the repository root: %v", err)
		}
	}
	if filesRead < 10 {
		t.Fatalf("the scan read %d files; it is not looking at the service", filesRead)
	}
}

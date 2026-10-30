package discordbridge

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// BridgeConfig is what multichat needs from mautrix-discord's config.yaml to
// give its bot a command: the bot's Matrix user id and the command prefix.
type BridgeConfig struct {
	BotUserID     string
	CommandPrefix string
}

// ReadBridgeConfig reads the bot's user id (appservice.bot.username on
// homeserver.domain) and bridge.command_prefix from the bridge's config file.
// A file that lacks any of them is an error, not a guess.
func ReadBridgeConfig(path string) (BridgeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return BridgeConfig{}, err
	}
	var file struct {
		Homeserver struct {
			Domain string `yaml:"domain"`
		} `yaml:"homeserver"`
		Appservice struct {
			Bot struct {
				Username string `yaml:"username"`
			} `yaml:"bot"`
		} `yaml:"appservice"`
		Bridge struct {
			CommandPrefix string `yaml:"command_prefix"`
		} `yaml:"bridge"`
	}
	if err := yaml.Unmarshal(data, &file); err != nil {
		return BridgeConfig{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if file.Homeserver.Domain == "" || file.Appservice.Bot.Username == "" || file.Bridge.CommandPrefix == "" {
		return BridgeConfig{}, fmt.Errorf("%s names no homeserver.domain, appservice.bot.username or bridge.command_prefix", path)
	}
	return BridgeConfig{
		BotUserID:     "@" + file.Appservice.Bot.Username + ":" + file.Homeserver.Domain,
		CommandPrefix: file.Bridge.CommandPrefix,
	}, nil
}

package shortnames

import (
	"fmt"
	"sort"
	"strings"
)

// proposal is the model's name for one room.
type proposal struct {
	EntityID string `json:"entity_id"`
	Path     string `json:"path"`
	Reason   string `json:"reason"`
}

type roomNamesReply struct {
	Names []proposal `json:"names"`
}

type serverProposal struct {
	ServerID  string `json:"server_id"`
	ShortName string `json:"short_name"`
	Reason    string `json:"reason"`
}

type serverNamesReply struct {
	Servers []serverProposal `json:"servers"`
}

var roomNamesSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"names": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"entity_id": map[string]any{"type": "string", "description": "the candidate's entity_id, verbatim"},
					"path":      map[string]any{"type": "string", "description": "the short name"},
					"reason":    map[string]any{"type": "string", "description": "why this name, in a few words"},
				},
				"required":             []string{"entity_id", "path", "reason"},
				"additionalProperties": false,
			},
		},
	},
	"required":             []string{"names"},
	"additionalProperties": false,
}

var serverNamesSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"servers": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"server_id":  map[string]any{"type": "string", "description": "the server's server_id, verbatim"},
					"short_name": map[string]any{"type": "string", "description": "one path segment"},
					"reason":     map[string]any{"type": "string", "description": "why this short name, in a few words"},
				},
				"required":             []string{"server_id", "short_name", "reason"},
				"additionalProperties": false,
			},
		},
	},
	"required":             []string{"servers"},
	"additionalProperties": false,
}

const segmentRule = `A segment is lowercase ASCII letters and digits, in words joined by single hyphens, 1 to 40 characters: "kim", "kim-moss", "social". No spaces, underscores, dots, emoji or accents.`

var serverSystemPrompt = `You choose short names for Discord servers. Each becomes the first part of a URL the operator types to reach that server's channels on their chat inbox: messages/<server>/<channel>. Make it short, easy to type and plainly that server: an initialism, the distinctive word or a squashed form of the name ("Riverside social group" -> "social", "Seattle Board Game Club" -> "boardgames"). ` + segmentRule + ` Never use a reserved segment or a segment listed as in use. Give each server a different short name. Answer once for every server listed.`

var roomSystemPrompt = `You choose short names for the operator's chats. Each becomes a URL the operator types to open that chat on their inbox: messages/<path>. Names are permanent once written, so choose ones that will still fit next year.

Rules:
- A direct message (kind direct_message): the other person's first name, lowercase ("kim"). If that path is taken, first name and surname ("kim-moss"); with no surname known, the first name plus one word that tells this person apart (the platform, a nickname, a place). Use the person's name from direct_message_partner, else the room name. A phone number with no name is named for what the messages are, if room_name tells you; else the digits.
- A group (kind group): one or two short words for what the group is ("nyc" for a group about New York, "climbing", "family"). One segment. Never a person's bare first name, even when the group is named after one person: a plain first name is kept for that person's direct message.
- A Discord server channel (kind discord_channel): exactly "<discord_server_short_name>/<channel>", using the discord_server_short_name given, unchanged. <channel> is the channel's name without emoji and filler words: "#general-chat" -> "general", "🎉-events-and-plans" -> "events".
- A business or an SMS short code: the business's name ("chase", "uber"), one segment.
- ` + segmentRule + ` Paths have one segment, except Discord channels, which have two.
- Never use a path listed as taken, nor a reserved segment as the first segment, nor a discord server short name as the whole path of something that is not that server's channel.
- The candidates are in popularity order, most messages first. When two want the same name, the earlier one gets the plain name and the later one the longer form. Give every candidate a different path.
- If nothing tells you who or what a chat is (a placeholder such as "Facebook user"), answer an empty path and say why: the chat waits for a later run, when more may be known.
- Answer once for every candidate, with its entity_id verbatim, and say why in a few words.`

type serverFacts struct {
	ServerID     string   `json:"server_id"`
	ServerName   string   `json:"server_name"`
	ChannelNames []string `json:"channel_names"`
}

func (run *runState) inUseFirstSegments() []string {
	segments := make([]string, 0, len(run.firstSegmentOwner))
	for segment := range run.firstSegmentOwner {
		segments = append(segments, segment)
	}
	return sortedStrings(segments)
}

func (run *runState) serverPrompt(servers []serverFacts) string {
	return fmt.Sprintf("Reserved segments: %s\n\nSegments in use, which no new server may take:\n%s\n\nServers to name:\n%s\n",
		mustJSON(run.sortedReserved()), mustJSON(run.inUseFirstSegments()), mustJSON(servers))
}

// candidateFacts is one candidate as the model sees it.
type candidateFacts struct {
	EntityID               string        `json:"entity_id"`
	PopularityRank         int           `json:"popularity_rank"`
	Kind                   string        `json:"kind"`
	Platform               string        `json:"platform"`
	RoomName               string        `json:"room_name"`
	MessageCount           int           `json:"message_count"`
	DirectMessagePartner   string        `json:"direct_message_partner,omitempty"`
	MemberCount            int           `json:"member_count"`
	MemberNames            []string      `json:"member_names,omitempty"`
	DiscordServerName      string        `json:"discord_server_name,omitempty"`
	DiscordServerShortName string        `json:"discord_server_short_name,omitempty"`
	DiscordChannelName     string        `json:"discord_channel_name,omitempty"`
	DiscordChannelCategory string        `json:"discord_channel_category,omitempty"`
	SameConversationOnApps []linkedFacts `json:"same_conversation_on_other_apps,omitempty"`
}

type linkedFacts struct {
	Platform string `json:"platform"`
	RoomName string `json:"room_name"`
}

func (run *runState) roomPrompt(candidates []candidate, firstRankMinusOne int) string {
	facts := make([]candidateFacts, 0, len(candidates))
	for i, c := range candidates {
		f := candidateFacts{EntityID: c.room.RoomID, PopularityRank: firstRankMinusOne + i + 1, Kind: c.room.Kind,
			Platform: c.room.Platform, RoomName: c.room.Name, MessageCount: c.messageCount,
			DirectMessagePartner: c.room.DirectMessagePartnerName, MemberCount: c.room.MemberCount, MemberNames: c.room.MemberNames}
		if channel := c.room.Discord; channel != nil {
			f.DiscordServerName = channel.ServerName
			f.DiscordServerShortName = run.serverShortName[channel.ServerID]
			f.DiscordChannelName = channel.ChannelName
			f.DiscordChannelCategory = channel.CategoryName
		}
		for _, linked := range c.linkedRooms {
			f.SameConversationOnApps = append(f.SameConversationOnApps, linkedFacts{Platform: linked.Platform, RoomName: linked.Name})
		}
		facts = append(facts, f)
	}
	var serverShortNames []string
	for serverID, shortName := range run.serverShortName {
		serverShortNames = append(serverShortNames, shortName+" ("+run.serverName[serverID]+")")
	}
	var prompt strings.Builder
	fmt.Fprintf(&prompt, "Reserved segments: %s\n\n", mustJSON(run.sortedReserved()))
	fmt.Fprintf(&prompt, "Discord server short names: %s\n\n", mustJSON(sortedStrings(serverShortNames)))
	fmt.Fprintf(&prompt, "Taken paths:\n%s\n\n", mustJSON(run.sortedTakenPaths()))
	fmt.Fprintf(&prompt, "Candidates, most messages first:\n%s\n", mustJSON(facts))
	return prompt.String()
}

func sortedStrings(values []string) []string {
	sorted := append([]string{}, values...)
	sort.Strings(sorted)
	return sorted
}

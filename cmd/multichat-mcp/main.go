// multichat-mcp is a stdio MCP server over multichat's HTTP API, so an agent
// session can read and send messages on WhatsApp, Telegram, Signal and Meta.
//
// Registered in tool-store as "multichat" (kind mcp). tool-store provisions
// MULTICHAT_TOKEN from auth-store at spawn; the multichat URL is an argument
// on the tool row. Tool names reach the harness as mcp__multichat__<name>,
// which is what a permission-store rule matches on.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/kayushkin/llm-bridge/servicesettings"
	"github.com/kayushkin/multichat/internal/client"
	"github.com/kayushkin/multichat/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	multichatURL := flag.String("url", "", "multichat base URL, e.g. http://localhost:8402 (required)")
	flag.Parse()
	if *multichatURL == "" {
		log.Fatal("multichat-mcp: -url is required")
	}
	settings, err := config.NewMCPServerSettingsRegistry(servicesettings.ProcessEnvironment())
	if err != nil {
		log.Fatalf("multichat-mcp: %v", err)
	}
	// The error names MULTICHAT_TOKEN and says tool-store provisions it.
	if err := settings.CheckRequired(); err != nil {
		log.Fatalf("multichat-mcp: %v", err)
	}
	api := client.New(*multichatURL, settings.String(config.MCPSettingAPIToken))

	server := mcp.NewServer(&mcp.Implementation{Name: "multichat", Version: "1"}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_contacts",
		Description: "People reachable through multichat. A person linked across platforms carries a people-store person_id (person_000001, the id a journal entry or note uses for them) and every identity linked to it, each under the name that platform shows (a Discord handle, the phone's contact name for texts); an identity linked to no one is listed alone. Filter by a person_id, by a case-insensitive substring of the person's or any identity's name, and/or by a platform (whatsapp, telegram, signal, meta, discord, slack, gmessages for SMS). The user_id of an identity is what send_message takes.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in listContactsInput) (*mcp.CallToolResult, listContactsOutput, error) {
		contacts, err := api.UnifiedContacts()
		if err != nil {
			return nil, listContactsOutput{}, err
		}
		out := listContactsOutput{Contacts: []client.UnifiedContact{}}
		for _, contact := range contacts {
			if in.PersonID != "" && contact.PersonID != in.PersonID {
				continue
			}
			if in.Query != "" && !contactNameContains(contact, in.Query) {
				continue
			}
			if in.Platform != "" {
				var identities []client.Identity
				for _, identity := range contact.Identities {
					if identity.Platform == in.Platform {
						identities = append(identities, identity)
					}
				}
				if len(identities) == 0 {
					continue
				}
				contact.Identities = identities
			}
			out.Contacts = append(out.Contacts, contact)
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_conversations",
		Description: "Every joined conversation: direct messages and groups, with room_id, name, platform, member ids and the last message.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, listConversationsOutput, error) {
		conversations, err := api.Conversations()
		if err != nil {
			return nil, listConversationsOutput{}, err
		}
		return nil, listConversationsOutput{Conversations: conversations}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "read_messages",
		Description: "Recent messages in one conversation, oldest first. Pass the `end` token back as `from` to page further back.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in readMessagesInput) (*mcp.CallToolResult, client.MessagePage, error) {
		limit := in.Limit
		if limit == 0 {
			limit = 50
		}
		page, err := api.Messages(in.RoomID, limit, in.From)
		if err != nil {
			return nil, client.MessagePage{}, err
		}
		return nil, *page, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "send_message",
		Description: "Send a text message to a real person or group. Pass user_id (an identity from list_contacts) to message one person in the direct-message room shared with them, or room_id (from list_conversations) to post into a known room. Exactly one of the two.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPointer(false), IdempotentHint: false, OpenWorldHint: boolPointer(true)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in sendMessageInput) (*mcp.CallToolResult, client.SendResult, error) {
		if (in.UserID == "") == (in.RoomID == "") {
			return nil, client.SendResult{}, fmt.Errorf("pass exactly one of user_id or room_id")
		}
		if strings.TrimSpace(in.Body) == "" {
			return nil, client.SendResult{}, fmt.Errorf("body is empty")
		}
		var result *client.SendResult
		var err error
		if in.UserID != "" {
			result, err = api.SendDirectMessage(in.UserID, in.Body)
		} else {
			result, err = api.SendToRoom(in.RoomID, in.Body)
		}
		if err != nil {
			return nil, client.SendResult{}, err
		}
		return nil, *result, nil
	})

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("multichat-mcp: %v", err)
	}
}

type listContactsInput struct {
	PersonID string `json:"person_id,omitempty" jsonschema:"people-store person id, e.g. person_000012"`
	Query    string `json:"query,omitempty" jsonschema:"case-insensitive substring of the display name"`
	Platform string `json:"platform,omitempty" jsonschema:"whatsapp | telegram | signal | meta | discord | slack"`
}

type listContactsOutput struct {
	Contacts []client.UnifiedContact `json:"contacts"`
}

type listConversationsOutput struct {
	Conversations []client.Conversation `json:"conversations"`
}

type readMessagesInput struct {
	RoomID string `json:"room_id" jsonschema:"the conversation's room_id"`
	Limit  int    `json:"limit,omitempty" jsonschema:"messages per page, 1-200, default 50"`
	From   string `json:"from,omitempty" jsonschema:"paging token from a previous page's end"`
}

type sendMessageInput struct {
	UserID string `json:"user_id,omitempty" jsonschema:"identity user_id of the person to message"`
	RoomID string `json:"room_id,omitempty" jsonschema:"room to post into"`
	Body   string `json:"body" jsonschema:"the message text"`
}

func boolPointer(b bool) *bool { return &b }

// contactNameContains reports whether the person's name or any identity's own
// name contains query, case-insensitively.
func contactNameContains(contact client.UnifiedContact, query string) bool {
	query = strings.ToLower(query)
	if strings.Contains(strings.ToLower(contact.DisplayName), query) {
		return true
	}
	for _, identity := range contact.Identities {
		if strings.Contains(strings.ToLower(identity.DisplayName), query) {
			return true
		}
	}
	return false
}

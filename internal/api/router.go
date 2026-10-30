package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/kayushkin/multichat/internal/contactlinks"
	"github.com/kayushkin/multichat/internal/people"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kayushkin/llm-bridge/servicesettings"
	"github.com/kayushkin/multichat/internal/config"
	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/discordbridge"
	"github.com/kayushkin/multichat/internal/discordemoji"
	"github.com/kayushkin/multichat/internal/matrix"
	"github.com/kayushkin/multichat/internal/shortnames"
)

type Router struct {
	db         *sql.DB
	matrix     *matrix.Client
	mux        *http.ServeMux
	tgDBPath   string // path to mautrix-telegram SQLite DB (for phone lookups)
	metaDBPath string // path to mautrix-meta SQLite DB (for FB vs IG distinction)
	// discordDBPath is the mautrix-discord SQLite file GET /discord/status
	// reads; empty makes that route a 503 naming DISCORD_DB.
	discordDBPath string
	// discordConfigPath is the bridge's config.yaml, read for its bot's id and
	// command prefix when a send has to open a room for a Discord DM.
	discordConfigPath string
	// people reaches people-store, which owns every person and the accounts
	// attached to them; bridgeDatabases are where the phone-number matcher
	// reads numbers.
	people          people.Client
	bridgeDatabases contactlinks.BridgeDatabases
	// discordEmojiRefresher fetches the bridged servers' custom emoji;
	// discordEmojiTeacher adds one to the bridge before we react with it.
	discordEmojiRefresher discordemoji.Refresher
	discordEmojiTeacher   discordemoji.Teacher
	discordNitroChecker   discordemoji.NitroChecker
	discordServerSweeper  *discordemoji.ServerSweeper

	// llmBridgeURL is where the short-name pass asks a model to name chats.
	// Empty makes POST /short-names/assign a 503 naming the missing setting.
	llmBridgeURL string
	httpClient   *http.Client

	// apiToken is the bearer every route but /auth/login and /health asks
	// for. Empty turns authentication off: see ServeHTTP.
	apiToken        string
	openAIAPIKey    string
	anthropicAPIKey string

	// shortNameAssigner names chats in short-name-store at shortNameStoreURL.
	shortNameAssigner *shortnames.Assigner
	shortNameStoreURL string

	// settings is what GET /settings describes: every environment variable
	// the server reads.
	settings *servicesettings.Registry
}

// SettingsPath is where servicesettings.Handler is mounted, under the /api
// prefix main strips.
const SettingsPath = "/settings"

// NewRouter panics on a nil settings registry, at boot: the registry is an
// argument rather than an option so that no caller can build a router without
// GET /settings.
func NewRouter(db *sql.DB, matrixClient *matrix.Client, loaded config.Config, settings *servicesettings.Registry) *Router {
	if settings == nil {
		panic("multichat: a settings registry is required; GET /settings describes the service from it")
	}
	r := &Router{
		db:                db,
		matrix:            matrixClient,
		mux:               http.NewServeMux(),
		tgDBPath:          loaded.TelegramBridgeDatabasePath,
		metaDBPath:        loaded.MetaBridgeDatabasePath,
		discordDBPath:     loaded.DiscordBridgeDatabasePath,
		discordConfigPath: loaded.DiscordBridgeConfigPath,
		people:            people.Client{BaseURL: loaded.PeopleStoreURL, HTTPClient: &http.Client{Timeout: 10 * time.Second}},
		bridgeDatabases: contactlinks.BridgeDatabases{
			GoogleMessages: loaded.GoogleMessagesBridgeDatabasePath,
			WhatsApp:       loaded.WhatsAppBridgeDatabasePath,
			Signal:         loaded.SignalBridgeDatabasePath,
			Telegram:       loaded.TelegramBridgeDatabasePath,
		},
		llmBridgeURL:    strings.TrimRight(loaded.LLMBridgeServerURL, "/"),
		httpClient:      &http.Client{Timeout: 10 * time.Second},
		apiToken:        loaded.APIToken,
		openAIAPIKey:    loaded.OpenAIAPIKey,
		anthropicAPIKey: loaded.AnthropicAPIKey,
		settings:        settings,
	}
	r.discordEmojiRefresher = discordemoji.Refresher{
		HTTPClient:        r.httpClient,
		AuthStoreURL:      loaded.AuthStoreURL,
		AuthStoreToken:    loaded.AuthStoreToken,
		BotCredentialID:   loaded.DiscordEmojiBotCredentialID,
		DiscordAPIBaseURL: discordemoji.DiscordAPIBaseURL,
	}
	r.discordServerSweeper = &discordemoji.ServerSweeper{
		HTTPClient: r.httpClient, DiscordAPIBaseURL: discordemoji.DiscordAPIBaseURL,
		Interval: discordemoji.ServerSweepInterval, StaleAfter: discordemoji.ServerSweepStaleAfter,
		Now: time.Now, Sleep: time.Sleep,
	}
	r.discordNitroChecker = discordemoji.NitroChecker{HTTPClient: r.httpClient, DiscordAPIBaseURL: discordemoji.DiscordAPIBaseURL}
	r.discordEmojiTeacher = discordemoji.Teacher{HTTPClient: r.httpClient, CDNBaseURL: discordemoji.DiscordCDNBaseURL, Now: time.Now}
	r.shortNameStoreURL = loaded.ShortNameStoreURL
	r.shortNameAssigner = &shortnames.Assigner{
		Store: shortnames.StoreClient{BaseURL: loaded.ShortNameStoreURL, HTTPClient: r.httpClient},
		Model: shortnames.OneShotModel{
			BridgeURL:          r.llmBridgeURL,
			ServiceTokenHeader: shortnames.BridgeServiceTokenHeader,
			ServiceToken:       loaded.LLMBridgeServiceToken,
			ModelRole:          loaded.ShortNamerModelRole,
			MaxTokens:          shortNamerMaxTokens,
			// One call names 40 rooms, and the bridge may try a second model.
			HTTPClient: &http.Client{Timeout: 5 * time.Minute},
		},
		ModelRole: loaded.ShortNamerModelRole,
	}
	r.setupRoutes()
	return r
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	if req.Method == "OPTIONS" {
		return
	}

	// Token auth — skip for login and health
	if req.URL.Path != "/auth/login" && req.URL.Path != "/health" {
		authToken := r.apiToken
		if authToken != "" {
			token := ""
			if auth := req.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
				token = strings.TrimPrefix(auth, "Bearer ")
			}
			if token == "" {
				if c, err := req.Cookie("multichat_token"); err == nil {
					token = c.Value
				}
			}
			if token != authToken {
				r.writeError(w, 401, "Unauthorized")
				return
			}
		}
	}

	r.mux.ServeHTTP(w, req)
}

func (r *Router) setupRoutes() {
	r.mux.HandleFunc("/auth/login", r.loginHandler)
	r.mux.HandleFunc("/health", r.healthHandler)
	r.mux.HandleFunc("/schema", r.schemaHandler)
	r.mux.HandleFunc("/contacts", r.contactsHandler)
	r.mux.HandleFunc("/contacts/add", r.addContactHandler)
	r.mux.HandleFunc("/stats", r.statsHandler)
	r.mux.HandleFunc("/search", r.searchHandler)
	r.mux.HandleFunc("/alerts", r.alertsHandler)
	r.mux.HandleFunc("/ai/summarize", r.summarizeHandler)
	r.mux.HandleFunc("/ai/escalation-scan", r.escalationScanHandler)
	r.mux.HandleFunc("/tags", r.tagsHandler)
	r.mux.HandleFunc("/contacts/tags", r.contactTagsHandler)
	r.mux.HandleFunc("/contacts/tags/bulk", r.bulkContactTagsHandler)
	r.mux.HandleFunc("/ai/auto-tag", r.autoTagHandler)
	r.mux.HandleFunc("/loads", r.loadsHandler)
	r.mux.HandleFunc("/loads/search", r.loadsSearchHandler)
	r.mux.HandleFunc("/conversations", r.conversationsHandler)
	r.mux.HandleFunc("/conversations/new", r.newConversationHandler)
	r.mux.HandleFunc("/conversations/tags", r.conversationTagsHandler)
	r.mux.HandleFunc("/conversations/tags/bulk", r.bulkConversationTagsHandler)
	r.mux.HandleFunc("/conversations/visibility-settings", r.conversationVisibilitySettingsHandler)
	r.mux.HandleFunc("/conversations/links", r.conversationLinksHandler)
	r.mux.HandleFunc("/conversations/links/remove", r.unlinkConversationHandler)
	r.mux.HandleFunc("/conversations/", r.conversationSubHandler)
	r.mux.HandleFunc("/messages/send", r.sendDirectMessageHandler)
	r.mux.HandleFunc("/awaiting-reply", r.awaitingReplyHandler)
	r.mux.HandleFunc("/awaiting-reply/settings", r.awaitingReplySettingsHandler)
	r.mux.HandleFunc("/awaiting-reply/clears", r.awaitingReplyClearsHandler)
	r.mux.HandleFunc("/auto-reactions", r.autoReactionsHandler)
	r.mux.HandleFunc("/auto-reactions/", r.autoReactionHandler)
	r.mux.HandleFunc("/inbound-platforms", r.inboundPlatformsHandler)
	r.mux.HandleFunc("/message-log", r.messageLogHandler)
	r.mux.HandleFunc("/message-log/", r.loggedMessageHandler)
	r.mux.HandleFunc("/discord/status", r.discordStatusHandler)
	r.mux.HandleFunc("/discord/direct-messages/open", r.openDiscordDirectMessageHandler)
	r.mux.HandleFunc("/emoji", r.emojiHandler)
	r.mux.HandleFunc("/emoji/discord/refresh", r.refreshDiscordEmojiHandler)
	r.mux.HandleFunc("/emoji/favorites", r.emojiFavoritesHandler)
	r.mux.HandleFunc("/emoji/settings", r.emojiSettingsHandler)
	r.mux.HandleFunc("/emoji/discord/nitro-check", r.discordNitroCheckHandler)
	r.mux.HandleFunc("/emoji/discord/server-sweep", r.discordServerSweepHandler)
	r.mux.HandleFunc("/media/", r.mediaHandler)
	r.mux.HandleFunc("/short-names/assign", r.assignShortNamesHandler)
	r.setupContactLinkRoutes()
	// Behind the same bearer as every other route. Nothing is Editable, so
	// PUT /settings/{key} is not mounted and answers 404.
	r.mux.Handle(SettingsPath, servicesettings.Handler(r.settings, SettingsPath))
}

func (r *Router) healthHandler(w http.ResponseWriter, req *http.Request) {
	r.writeJSON(w, map[string]string{"status": "ok"})
}

func (r *Router) schemaHandler(w http.ResponseWriter, req *http.Request) {
	rows, err := r.db.Query(`SELECT name, type, sql FROM sqlite_master WHERE sql IS NOT NULL ORDER BY type, name`)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	type SchemaItem struct {
		Name string `json:"name"`
		Type string `json:"type"`
		SQL  string `json:"sql"`
	}
	var items []SchemaItem
	for rows.Next() {
		var item SchemaItem
		if err := rows.Scan(&item.Name, &item.Type, &item.SQL); err != nil {
			r.writeError(w, 500, err.Error())
			return
		}
		items = append(items, item)
	}
	r.writeJSON(w, items)
}

func (r *Router) contactsHandler(w http.ResponseWriter, req *http.Request) {
	if r.matrix == nil {
		r.writeError(w, 503, "Matrix client not configured")
		return
	}
	contacts, err := r.matrix.ListContacts()
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}

	// Load Telegram phone numbers for merging
	tgPhones := map[string]string{}    // phone -> telegram display name
	tgIDToPhone := map[string]string{} // telegram ID -> phone
	if r.tgDBPath != "" {
		if tgDB, err := sql.Open("sqlite3", r.tgDBPath+"?mode=ro"); err == nil {
			rows, _ := tgDB.Query("SELECT id, displayname, phone FROM puppet WHERE phone IS NOT NULL AND phone != ''")
			if rows != nil {
				for rows.Next() {
					var id int64
					var name, phone string
					rows.Scan(&id, &name, &phone)
					name = strings.TrimSuffix(name, " (Telegram)")
					tgPhones[phone] = name
					tgIDToPhone[strconv.FormatInt(id, 10)] = phone
				}
				rows.Close()
			}
			tgDB.Close()
		}
	}

	// Index telegram contacts by phone
	tgByPhone := map[string]*matrix.Contact{}
	for i := range contacts {
		if contacts[i].Platform == "telegram" {
			if phone, ok := tgIDToPhone[contacts[i].TelegramID]; ok {
				contacts[i].Phone = phone
				tgByPhone[phone] = &contacts[i]
			}
		}
	}

	// Merge: for WhatsApp contacts whose phone matches a Telegram contact, merge them
	var merged []matrix.Contact
	waPhonesSeen := map[string]bool{}
	for _, c := range contacts {
		if c.Platform == "whatsapp" {
			phone := c.TelegramID // WhatsApp ID is the phone number
			if tgContact, ok := tgByPhone[phone]; ok {
				// Already have this person from Telegram with a better name
				tgContact.Platforms = append(tgContact.Platforms, "whatsapp")
				waPhonesSeen[phone] = true
				continue
			}
			waPhonesSeen[phone] = true
			// Use telegram phone book name if available
			if name, ok := tgPhones[phone]; ok && name != "" {
				c.DisplayName = name
			}
			c.Phone = phone
		}
		merged = append(merged, c)
	}

	// Resolve meta contacts to facebook/instagram using meta bridge DB
	if r.metaDBPath != "" {
		if metaDB, err := sql.Open("sqlite3", r.metaDBPath+"?mode=ro"); err == nil {
			// Map login ID → platform
			loginPlatform := map[string]string{}
			rows, _ := metaDB.Query(`SELECT id, json_extract(metadata, '$.platform') FROM user_login`)
			if rows != nil {
				for rows.Next() {
					var id, platform string
					rows.Scan(&id, &platform)
					loginPlatform[id] = platform
				}
				rows.Close()
			}
			// Build map: other_user_id → platform (via portal → receiver → login)
			ghostPlatform := map[string]string{}
			rows, _ = metaDB.Query(`SELECT p.other_user_id, json_extract(ul.metadata, '$.platform') FROM portal p JOIN user_login ul ON p.receiver = ul.id WHERE p.other_user_id IS NOT NULL`)
			if rows != nil {
				for rows.Next() {
					var ghostID, platform string
					rows.Scan(&ghostID, &platform)
					ghostPlatform[ghostID] = platform
				}
				rows.Close()
			}
			metaDB.Close()

			for i := range merged {
				if merged[i].Platform == "meta" {
					id := merged[i].TelegramID
					if p, ok := loginPlatform[id]; ok {
						merged[i].Platform = p
					} else if p, ok := ghostPlatform[id]; ok {
						merged[i].Platform = p
					} else {
						// Default unresolved meta contacts to facebook (primary account)
						merged[i].Platform = "facebook"
					}
				}
			}
		}
	}

	// Set platforms for non-merged contacts
	for i := range merged {
		if len(merged[i].Platforms) == 0 {
			merged[i].Platforms = []string{merged[i].Platform}
		}
	}

	// Add local contacts from the database
	rows, err := r.db.Query(`SELECT id, display_name, platform, identifier FROM local_contacts ORDER BY display_name`)
	if err == nil {
		for rows.Next() {
			var id int64
			var displayName, platform, identifier string
			if err := rows.Scan(&id, &displayName, &platform, &identifier); err == nil {
				localContact := matrix.Contact{
					UserID:      fmt.Sprintf("local_%d", id),
					DisplayName: displayName,
					Platform:    platform,
					TelegramID:  identifier, // Using TelegramID field to store the identifier
					Platforms:   []string{platform},
				}
				merged = append(merged, localContact)
			}
		}
		rows.Close()
	}

	r.writeJSON(w, merged)
}

func (r *Router) addContactHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		r.writeError(w, 405, "Method not allowed")
		return
	}

	var body struct {
		DisplayName string `json:"display_name"`
		Platform    string `json:"platform"`
		Identifier  string `json:"identifier"`
	}

	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		r.writeError(w, 400, "Invalid JSON")
		return
	}

	if body.DisplayName == "" || body.Platform == "" || body.Identifier == "" {
		r.writeError(w, 400, "display_name, platform, and identifier are required")
		return
	}

	// For now, store it in the local SQLite DB only
	_, err := r.db.Exec(`
		CREATE TABLE IF NOT EXISTS local_contacts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			display_name TEXT NOT NULL,
			platform TEXT NOT NULL,
			identifier TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(platform, identifier)
		);
		CREATE TABLE IF NOT EXISTS tags (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			color TEXT NOT NULL DEFAULT '#f5d40c',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS contact_tags (
			contact_user_id TEXT NOT NULL,
			tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
			PRIMARY KEY (contact_user_id, tag_id)
		);
	`)
	if err != nil {
		r.writeError(w, 500, "Failed to create contacts table")
		return
	}

	// Insert the contact
	result, err := r.db.Exec(`
		INSERT OR REPLACE INTO local_contacts (display_name, platform, identifier)
		VALUES (?, ?, ?)
	`, body.DisplayName, body.Platform, body.Identifier)
	if err != nil {
		r.writeError(w, 500, "Failed to add contact")
		return
	}

	id, _ := result.LastInsertId()

	// Return the new contact
	contact := map[string]interface{}{
		"id":           id,
		"display_name": body.DisplayName,
		"platform":     body.Platform,
		"identifier":   body.Identifier,
		"user_id":      fmt.Sprintf("local_%d", id), // Create a synthetic user_id for consistency
	}

	r.writeJSON(w, contact)
}

func (r *Router) conversationsHandler(w http.ResponseWriter, req *http.Request) {
	if r.matrix == nil {
		r.writeError(w, 503, "Matrix client not configured")
		return
	}
	convos, err := r.listConversations()
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	if err := r.markHiddenConversations(convos); err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	r.writeJSON(w, convos)
}

// listConversations reads the conversation list with what the bridge
// databases say now: which Meta rooms are Facebook or Instagram, and which
// Discord rooms are direct messages.
func (r *Router) listConversations() ([]matrix.Conversation, error) {
	// Build meta platform map from bridge DB
	var metaPlatforms matrix.MetaPlatformMap
	if r.metaDBPath != "" {
		if metaDB, err := sql.Open("sqlite3", r.metaDBPath+"?mode=ro"); err == nil {
			metaPlatforms = make(matrix.MetaPlatformMap)
			// Map login ID → platform
			loginPlatform := map[string]string{}
			rows, _ := metaDB.Query(`SELECT id, json_extract(metadata, '$.platform') FROM user_login`)
			if rows != nil {
				for rows.Next() {
					var id, platform string
					rows.Scan(&id, &platform)
					loginPlatform[id] = platform
				}
				rows.Close()
			}
			// Map portal mxid → platform via receiver (DMs)
			rows, _ = metaDB.Query(`SELECT mxid, receiver FROM portal WHERE mxid IS NOT NULL AND receiver != ''`)
			if rows != nil {
				for rows.Next() {
					var mxid, receiver string
					rows.Scan(&mxid, &receiver)
					if p, ok := loginPlatform[receiver]; ok {
						metaPlatforms[mxid] = p
					}
				}
				rows.Close()
			}
			// Fallback for groups (empty receiver) — use user_portal login_id
			rows, _ = metaDB.Query(`SELECT p.mxid, up.login_id FROM portal p JOIN user_portal up ON p.id = up.portal_id AND p.receiver = up.portal_receiver WHERE p.mxid IS NOT NULL AND p.receiver = ''`)
			if rows != nil {
				for rows.Next() {
					var mxid, loginID string
					rows.Scan(&mxid, &loginID)
					if _, exists := metaPlatforms[mxid]; !exists {
						if p, ok := loginPlatform[loginID]; ok {
							metaPlatforms[mxid] = p
						}
					}
				}
				rows.Close()
			}
			metaDB.Close()
		}
	}

	discordPortals, err := r.discordPortals()
	if err != nil {
		return nil, err
	}

	return r.matrix.ListConversations(metaPlatforms, discordPortals)
}

// discordPortals reads the Discord bridge's rooms from DISCORD_DB: each
// room's channel, and which are DMs. nil when no bridge database is set.
func (r *Router) discordPortals() (map[string]matrix.DiscordPortal, error) {
	if r.discordDBPath == "" {
		return nil, nil
	}
	bridge, err := discordbridge.Open(r.discordDBPath)
	if err != nil {
		return nil, err
	}
	defer bridge.Close()
	portals, err := discordbridge.PortalsByRoomID(bridge)
	if err != nil {
		return nil, fmt.Errorf("read Discord rooms from DISCORD_DB: %w", err)
	}
	return portals, nil
}

func (r *Router) newConversationHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	if r.matrix == nil {
		r.writeError(w, 503, "Matrix client not configured")
		return
	}
	var body struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.UserID == "" {
		r.writeError(w, 400, "Missing user_id")
		return
	}
	roomID, err := r.matrix.CreateDM(body.UserID)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	r.writeJSON(w, map[string]string{"room_id": roomID})
}

func (r *Router) conversationSubHandler(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/conversations/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 {
		r.writeError(w, 404, "Not found")
		return
	}
	roomID := parts[0]
	action := parts[1]

	switch {
	case action == "messages":
		r.messagesHandler(w, req, roomID)
	case action == "members":
		r.membersHandler(w, req, roomID)
	case action == "send":
		r.sendHandler(w, req, roomID)
	case action == "reactions":
		r.postReactionHandler(w, req, roomID)
	case strings.HasPrefix(action, "reactions/"):
		r.takeBackReactionHandler(w, req, roomID, strings.TrimPrefix(action, "reactions/"))
	case action == "send-file":
		r.sendFileHandler(w, req, roomID)
	case action == "visibility":
		r.conversationVisibilityHandler(w, req, roomID)
	case action == "loads":
		r.conversationLoadsHandler(w, req, roomID)
	case strings.HasPrefix(action, "loads/"):
		loadID := strings.TrimPrefix(action, "loads/")
		r.conversationLoadDeleteHandler(w, req, roomID, loadID)
	default:
		r.writeError(w, 404, "Not found")
	}
}

func (r *Router) membersHandler(w http.ResponseWriter, req *http.Request, roomID string) {
	if r.matrix == nil {
		r.writeError(w, 503, "Matrix client not configured")
		return
	}
	members, err := r.matrix.GetRoomMembers(roomID)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	r.writeJSON(w, members)
}

func (r *Router) sendHandler(w http.ResponseWriter, req *http.Request, roomID string) {
	if req.Method != "POST" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	if r.matrix == nil {
		r.writeError(w, 503, "Matrix client not configured")
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Body == "" {
		r.writeError(w, 400, "Missing body")
		return
	}
	eventID, err := r.matrix.SendMessage(roomID, body.Body)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	r.writeJSON(w, map[string]string{"event_id": eventID})
}

func (r *Router) messagesHandler(w http.ResponseWriter, req *http.Request, roomID string) {
	if r.matrix == nil {
		r.writeError(w, 503, "Matrix client not configured")
		return
	}

	limit := 50
	if l := req.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	from := req.URL.Query().Get("from")

	page, err := r.matrix.GetMessagePage(roomID, limit, from)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	eventIDs := make([]string, len(page.Messages))
	for i, m := range page.Messages {
		eventIDs[i] = m.EventID
	}
	own, err := r.ownSenders()
	if err != nil {
		r.writeError(w, 502, err.Error())
		return
	}
	reactions, err := db.ReactionsTo(r.db, eventIDs, own)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	out := conversationMessagePage{End: page.End, HasMore: page.HasMore}
	if page.Messages != nil {
		out.Messages = make([]conversationMessage, len(page.Messages))
		for i, m := range page.Messages {
			groups := reactions[m.EventID]
			if groups == nil {
				groups = []db.MessageReactionGroup{}
			}
			out.Messages[i] = conversationMessage{Message: m, Reactions: groups}
		}
	}
	r.writeJSON(w, out)
}

// conversationMessage is one message of GET /conversations/{room_id}/messages:
// the Matrix event and its current reactions from the reaction log (only
// those logged since the log began).
type conversationMessage struct {
	matrix.Message
	Reactions []db.MessageReactionGroup `json:"reactions"`
}

// conversationMessagePage is matrix.MessagePage with each message's reactions.
type conversationMessagePage struct {
	Messages []conversationMessage `json:"messages"`
	End      string                `json:"end,omitempty"`
	HasMore  bool                  `json:"has_more"`
}

func (r *Router) writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (r *Router) writeError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// loadMetaPlatforms builds a map of Matrix room IDs to platform names from the meta bridge DB
func (r *Router) loadMetaPlatforms() matrix.MetaPlatformMap {
	if r.metaDBPath == "" {
		return nil
	}
	metaDB, err := sql.Open("sqlite3", r.metaDBPath+"?mode=ro")
	if err != nil {
		return nil
	}
	defer metaDB.Close()
	metaPlatforms := make(matrix.MetaPlatformMap)
	loginPlatform := map[string]string{}
	rows, _ := metaDB.Query(`SELECT id, json_extract(metadata, '$.platform') FROM user_login`)
	if rows != nil {
		for rows.Next() {
			var id, platform string
			rows.Scan(&id, &platform)
			loginPlatform[id] = platform
		}
		rows.Close()
	}
	rows, _ = metaDB.Query(`SELECT mxid, receiver FROM portal WHERE mxid IS NOT NULL AND receiver != ''`)
	if rows != nil {
		for rows.Next() {
			var mxid, receiver string
			rows.Scan(&mxid, &receiver)
			if p, ok := loginPlatform[receiver]; ok {
				metaPlatforms[mxid] = p
			}
		}
		rows.Close()
	}
	rows, _ = metaDB.Query(`SELECT p.mxid, up.login_id FROM portal p JOIN user_portal up ON p.id = up.portal_id AND p.receiver = up.portal_receiver WHERE p.mxid IS NOT NULL AND p.receiver = ''`)
	if rows != nil {
		for rows.Next() {
			var mxid, loginID string
			rows.Scan(&mxid, &loginID)
			if _, exists := metaPlatforms[mxid]; !exists {
				if p, ok := loginPlatform[loginID]; ok {
					metaPlatforms[mxid] = p
				}
			}
		}
		rows.Close()
	}
	return metaPlatforms
}

// --- New features: stats, search, alerts, AI summary, unified contacts, escalation ---

func (r *Router) statsHandler(w http.ResponseWriter, req *http.Request) {
	convos, err := r.matrix.ListConversations(r.loadMetaPlatforms(), nil)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	platformCounts := map[string]int{}
	activeToday := 0
	todayStart := time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day(), 0, 0, 0, 0, time.UTC).UnixMilli()
	for _, c := range convos {
		platformCounts[c.Platform]++
		if c.LastActivity >= todayStart {
			activeToday++
		}
	}
	total := len(convos)
	var breakdown []map[string]interface{}
	for p, count := range platformCounts {
		pct := 0
		if total > 0 {
			pct = count * 100 / total
		}
		breakdown = append(breakdown, map[string]interface{}{"platform": p, "count": count, "percentage": pct})
	}
	sort.Slice(breakdown, func(i, j int) bool { return breakdown[i]["count"].(int) > breakdown[j]["count"].(int) })
	var mostActive []map[string]interface{}
	for i, c := range convos {
		if i >= 5 || c.LastActivity == 0 {
			break
		}
		mostActive = append(mostActive, map[string]interface{}{"name": c.Name, "platform": c.Platform, "room_id": c.RoomID})
	}
	r.writeJSON(w, map[string]interface{}{
		"total_conversations": total, "active_today": activeToday,
		"platforms_connected": len(platformCounts), "platform_breakdown": breakdown, "most_active": mostActive,
	})
}

func (r *Router) summarizeHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	var body struct {
		RoomID string `json:"room_id"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.RoomID == "" {
		r.writeError(w, 400, "room_id required")
		return
	}
	msgs, err := r.matrix.GetMessages(body.RoomID, 50)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	var convo strings.Builder
	for _, m := range msgs {
		if m.Body != "" {
			convo.WriteString(fmt.Sprintf("%s: %s\n", m.SenderName, m.Body))
		}
	}
	if convo.Len() == 0 {
		r.writeJSON(w, map[string]interface{}{"summary": "No messages found.", "topics": []string{}, "sentiment": "neutral"})
		return
	}
	openaiKey := r.openAIAPIKey
	anthropicKey := r.anthropicAPIKey
	var result map[string]interface{}
	if openaiKey != "" {
		result, err = callOpenAI(openaiKey, convo.String(), "Analyze the following conversation. Provide: 1) concise summary (2-3 sentences) 2) key topics as tags 3) sentiment (positive/negative/neutral/urgent). Respond ONLY with JSON: {\"summary\": \"...\", \"topics\": [\"...\"], \"sentiment\": \"...\"}")
	} else if anthropicKey != "" {
		result, err = callClaude(anthropicKey, convo.String(), "Analyze the following conversation. Provide: 1) concise summary (2-3 sentences) 2) key topics as tags 3) sentiment (positive/negative/neutral/urgent). Respond ONLY with JSON: {\"summary\": \"...\", \"topics\": [\"...\"], \"sentiment\": \"...\"}")
	} else {
		r.writeError(w, 500, "No AI API key configured")
		return
	}
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	r.writeJSON(w, result)
}

func (r *Router) searchHandler(w http.ResponseWriter, req *http.Request) {
	q := strings.ToLower(strings.TrimSpace(req.URL.Query().Get("q")))
	if q == "" {
		r.writeError(w, 400, "q parameter required")
		return
	}
	convos, _ := r.matrix.ListConversations(r.loadMetaPlatforms(), nil)
	type Result struct {
		RoomID   string `json:"room_id"`
		RoomName string `json:"room_name"`
		Platform string `json:"platform"`
		Sender   string `json:"sender_name"`
		Body     string `json:"body"`
		Time     int64  `json:"timestamp"`
		EventID  string `json:"event_id"`
	}
	type Group struct {
		RoomID   string   `json:"room_id"`
		RoomName string   `json:"room_name"`
		Platform string   `json:"platform"`
		Results  []Result `json:"results"`
	}
	groupMap := map[string]*Group{}
	for _, c := range convos {
		msgs, err := r.matrix.GetMessages(c.RoomID, 50)
		if err != nil {
			continue
		}
		for _, m := range msgs {
			if strings.Contains(strings.ToLower(m.Body), q) {
				if _, ok := groupMap[c.RoomID]; !ok {
					groupMap[c.RoomID] = &Group{RoomID: c.RoomID, RoomName: c.Name, Platform: c.Platform}
				}
				groupMap[c.RoomID].Results = append(groupMap[c.RoomID].Results, Result{
					RoomID: c.RoomID, RoomName: c.Name, Platform: c.Platform,
					Sender: m.SenderName, Body: m.Body, Time: m.Timestamp, EventID: m.EventID,
				})
			}
		}
	}
	// Also search loads by reference number
	type LoadResult struct {
		Load              Load     `json:"load"`
		ConversationIDs   []string `json:"conversation_ids"`
		ConversationNames []string `json:"conversation_names"`
	}
	var loadResults []LoadResult
	loadRows, err := r.db.Query(`SELECT id, reference_number, shipper, consignee, origin_city, origin_state, destination_city, destination_state, status, rate, weight, pickup_date, delivery_date, equipment_type, created_at FROM loads WHERE LOWER(reference_number) LIKE ? OR LOWER(shipper) LIKE ? OR LOWER(consignee) LIKE ?`, "%"+q+"%", "%"+q+"%", "%"+q+"%")
	if err == nil {
		for loadRows.Next() {
			var l Load
			loadRows.Scan(&l.ID, &l.ReferenceNumber, &l.Shipper, &l.Consignee, &l.OriginCity, &l.OriginState, &l.DestinationCity, &l.DestinationState, &l.Status, &l.Rate, &l.Weight, &l.PickupDate, &l.DeliveryDate, &l.EquipmentType, &l.CreatedAt)
			lr := LoadResult{Load: l}
			// Find conversations this load is attached to
			clRows, _ := r.db.Query(`SELECT conversation_id FROM conversation_loads WHERE load_id = ?`, l.ID)
			if clRows != nil {
				for clRows.Next() {
					var cid string
					clRows.Scan(&cid)
					lr.ConversationIDs = append(lr.ConversationIDs, cid)
					// Find conversation name
					for _, c := range convos {
						if c.RoomID == cid {
							lr.ConversationNames = append(lr.ConversationNames, c.Name)
							break
						}
					}
				}
				clRows.Close()
			}
			loadResults = append(loadResults, lr)
		}
		loadRows.Close()
	}

	groups := make([]Group, 0, len(groupMap))
	total := 0
	for _, g := range groupMap {
		groups = append(groups, *g)
		total += len(g.Results)
	}
	r.writeJSON(w, map[string]interface{}{"groups": groups, "total": total, "query": q, "load_results": loadResults})
}

func (r *Router) alertsHandler(w http.ResponseWriter, req *http.Request) {
	// POST to add/remove custom keywords
	if req.Method == "POST" {
		var body struct {
			Action   string `json:"action"` // "add", "remove", or "set-severity"
			Keyword  string `json:"keyword"`
			Severity string `json:"severity"` // "high", "medium", "low"
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Keyword == "" {
			r.writeError(w, 400, "keyword required")
			return
		}
		r.db.Exec(`CREATE TABLE IF NOT EXISTS alert_keywords (keyword TEXT NOT NULL UNIQUE, severity TEXT NOT NULL DEFAULT 'low')`)
		r.db.Exec(`ALTER TABLE alert_keywords ADD COLUMN severity TEXT NOT NULL DEFAULT 'low'`) // migrate old schema
		r.db.Exec(`ALTER TABLE alert_keywords ADD COLUMN severity TEXT NOT NULL DEFAULT 'low'`)
		switch body.Action {
		case "remove":
			r.db.Exec(`DELETE FROM alert_keywords WHERE keyword = ?`, strings.ToLower(body.Keyword))
		case "set-severity":
			sev := body.Severity
			if sev != "high" && sev != "medium" && sev != "low" {
				sev = "low"
			}
			r.db.Exec(`INSERT INTO alert_keywords (keyword, severity) VALUES (?, ?) ON CONFLICT(keyword) DO UPDATE SET severity = ?`, strings.ToLower(body.Keyword), sev, sev)
		default: // "add"
			sev := body.Severity
			if sev == "" {
				sev = "low"
			}
			r.db.Exec(`INSERT INTO alert_keywords (keyword, severity) VALUES (?, ?) ON CONFLICT(keyword) DO UPDATE SET severity = ?`, strings.ToLower(body.Keyword), sev, sev)
		}
		r.writeJSON(w, map[string]string{"status": "ok"})
		return
	}

	hours := 24
	if h := req.URL.Query().Get("hours"); h != "" {
		if v, err := strconv.Atoi(h); err == nil {
			hours = v
		}
	}
	// Default keywords + custom from DB
	keywords := []string{"complaint", "urgent", "late", "damaged", "delay", "escalate", "cancel", "problem", "issue", "wrong", "broken", "failed", "missing", "angry", "refund"}
	// Keyword → severity map
	kwSeverity := map[string]string{
		"urgent": "high", "escalate": "high", "angry": "high",
		"complaint": "medium", "failed": "medium", "refund": "medium",
	}
	r.db.Exec(`CREATE TABLE IF NOT EXISTS alert_keywords (keyword TEXT NOT NULL UNIQUE, severity TEXT NOT NULL DEFAULT 'low')`)
	r.db.Exec(`ALTER TABLE alert_keywords ADD COLUMN severity TEXT NOT NULL DEFAULT 'low'`) // migrate old schema
	r.db.Exec(`ALTER TABLE alert_keywords ADD COLUMN severity TEXT NOT NULL DEFAULT 'low'`)
	rows, err := r.db.Query(`SELECT keyword, severity FROM alert_keywords`)
	if err == nil {
		for rows.Next() {
			var kw, sev string
			rows.Scan(&kw, &sev)
			kwSeverity[kw] = sev
			found := false
			for _, existing := range keywords {
				if existing == kw {
					found = true
					break
				}
			}
			if !found {
				keywords = append(keywords, kw)
			}
		}
		rows.Close()
	}
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour).UnixMilli()
	convos, _ := r.matrix.ListConversations(r.loadMetaPlatforms(), nil)
	type Alert struct {
		RoomID   string `json:"room_id"`
		RoomName string `json:"room_name"`
		Platform string `json:"platform"`
		Keyword  string `json:"keyword"`
		Body     string `json:"body"`
		Sender   string `json:"sender_name"`
		Time     int64  `json:"timestamp"`
		Severity string `json:"severity"`
	}
	var alerts []Alert
	for _, c := range convos {
		msgs, err := r.matrix.GetMessages(c.RoomID, 30)
		if err != nil {
			continue
		}
		for _, m := range msgs {
			if m.Timestamp < cutoff {
				continue
			}
			lower := strings.ToLower(m.Body)
			for _, kw := range keywords {
				if strings.Contains(lower, kw) {
					sev := "low"
					if s, ok := kwSeverity[kw]; ok {
						sev = s
					}
					alerts = append(alerts, Alert{RoomID: c.RoomID, RoomName: c.Name, Platform: c.Platform, Keyword: kw, Body: m.Body, Sender: m.SenderName, Time: m.Timestamp, Severity: sev})
					break
				}
			}
		}
	}
	sevCounts := map[string]int{"high": 0, "medium": 0, "low": 0}
	for _, a := range alerts {
		sevCounts[a.Severity]++
	}
	if alerts == nil {
		alerts = []Alert{}
	}
	// Build keyword list with severities
	type KWInfo struct {
		Keyword  string `json:"keyword"`
		Severity string `json:"severity"`
	}
	var kwList []KWInfo
	for _, kw := range keywords {
		sev := "low"
		if s, ok := kwSeverity[kw]; ok {
			sev = s
		}
		kwList = append(kwList, KWInfo{Keyword: kw, Severity: sev})
	}
	r.writeJSON(w, map[string]interface{}{"alerts": alerts, "total": len(alerts), "hours_scanned": hours, "keywords_used": keywords, "keywords_detail": kwList, "cutoff_time": cutoff, "severity_counts": sevCounts})
}

func (r *Router) escalationScanHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	openaiKey := r.openAIAPIKey
	if openaiKey == "" {
		r.writeError(w, 500, "No AI API key configured")
		return
	}
	convos, _ := r.matrix.ListConversations(r.loadMetaPlatforms(), nil)
	limit := 10
	if len(convos) < limit {
		limit = len(convos)
	}
	var escalations []map[string]interface{}
	for _, c := range convos[:limit] {
		msgs, err := r.matrix.GetMessages(c.RoomID, 20)
		if err != nil || len(msgs) == 0 {
			continue
		}
		var convo strings.Builder
		for _, m := range msgs {
			if m.Body != "" {
				convo.WriteString(fmt.Sprintf("%s: %s\n", m.SenderName, m.Body))
			}
		}
		if convo.Len() == 0 {
			continue
		}
		result, err := callOpenAI(openaiKey, convo.String(), "You are analyzing business conversations for a logistics company. Does this conversation need manager attention? Look for: angry customers, missed deadlines, risky promises, compliance issues. Respond ONLY with JSON: {\"needs_attention\": true/false, \"reason\": \"...\", \"severity\": \"high/medium/low\"}")
		if err != nil {
			continue
		}
		if needs, ok := result["needs_attention"].(bool); ok && needs {
			result["room_id"] = c.RoomID
			result["room_name"] = c.Name
			result["platform"] = c.Platform
			escalations = append(escalations, result)
		}
	}
	r.writeJSON(w, map[string]interface{}{"escalations": escalations, "total": len(escalations), "rooms_scanned": limit})
}

// --- AI API helpers ---

func callOpenAI(apiKey, conversation, systemPrompt string) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"model": "gpt-4o-mini", "max_tokens": 500, "temperature": 0.3,
		"messages": []map[string]interface{}{{"role": "user", "content": systemPrompt + "\n\nConversation:\n" + conversation}},
	}
	jsonData, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OpenAI returned %d: %s", resp.StatusCode, string(b))
	}
	var oai struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	json.NewDecoder(resp.Body).Decode(&oai)
	if len(oai.Choices) == 0 {
		return nil, fmt.Errorf("no choices")
	}
	text := strings.TrimSpace(oai.Choices[0].Message.Content)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return map[string]interface{}{"summary": text, "topics": []string{}, "sentiment": "neutral"}, nil
	}
	return result, nil
}

func callClaude(apiKey, conversation, systemPrompt string) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"model": "claude-sonnet-4-20250514", "max_tokens": 500,
		"messages": []map[string]interface{}{{"role": "user", "content": systemPrompt + "\n\nConversation:\n" + conversation}},
	}
	jsonData, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Claude returned %d: %s", resp.StatusCode, string(b))
	}
	var cr struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	json.NewDecoder(resp.Body).Decode(&cr)
	if len(cr.Content) == 0 {
		return nil, fmt.Errorf("no content")
	}
	text := strings.TrimSpace(cr.Content[0].Text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return map[string]interface{}{"summary": text, "topics": []string{}, "sentiment": "neutral"}, nil
	}
	return result, nil
}

// --- Tags ---

// GET /tags — list all tags
// POST /tags — create a tag { name, color? }
// DELETE /tags?id=N — delete a tag
func (r *Router) tagsHandler(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
		rows, err := r.db.Query(`SELECT id, name, color FROM tags ORDER BY name`)
		if err != nil {
			r.writeError(w, 500, err.Error())
			return
		}
		defer rows.Close()
		type Tag struct {
			ID    int64  `json:"id"`
			Name  string `json:"name"`
			Color string `json:"color"`
		}
		var tags []Tag
		for rows.Next() {
			var t Tag
			rows.Scan(&t.ID, &t.Name, &t.Color)
			tags = append(tags, t)
		}
		if tags == nil {
			tags = []Tag{}
		}
		r.writeJSON(w, tags)

	case "POST":
		var body struct {
			Name  string `json:"name"`
			Color string `json:"color"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Name == "" {
			r.writeError(w, 400, "name required")
			return
		}
		if body.Color == "" {
			body.Color = "#f5d40c"
		}
		result, err := r.db.Exec(`INSERT INTO tags (name, color) VALUES (?, ?)`, body.Name, body.Color)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				r.writeError(w, 409, "Tag already exists")
				return
			}
			r.writeError(w, 500, err.Error())
			return
		}
		id, _ := result.LastInsertId()
		r.writeJSON(w, map[string]interface{}{"id": id, "name": body.Name, "color": body.Color})

	case "DELETE":
		id := req.URL.Query().Get("id")
		if id == "" {
			r.writeError(w, 400, "id required")
			return
		}
		tagID, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			r.writeError(w, 400, "id must be a number")
			return
		}
		if err := db.DeleteTagAndAssignments(r.db, tagID); err != nil {
			r.writeError(w, 500, err.Error())
			return
		}
		r.writeJSON(w, map[string]string{"status": "deleted"})

	default:
		r.writeError(w, 405, "Method not allowed")
	}
}

// GET /contacts/tags?contact=USER_ID — get tags for a contact
// POST /contacts/tags — assign tag { contact_user_id, tag_id }
// DELETE /contacts/tags?contact=USER_ID&tag=TAG_ID — remove tag from contact
func (r *Router) contactTagsHandler(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
		contactID := req.URL.Query().Get("contact")
		if contactID == "" {
			r.writeError(w, 400, "contact required")
			return
		}
		rows, err := r.db.Query(`SELECT t.id, t.name, t.color FROM tags t JOIN contact_tags ct ON t.id = ct.tag_id WHERE ct.contact_user_id = ? ORDER BY t.name`, contactID)
		if err != nil {
			r.writeError(w, 500, err.Error())
			return
		}
		defer rows.Close()
		type Tag struct {
			ID    int64  `json:"id"`
			Name  string `json:"name"`
			Color string `json:"color"`
		}
		var tags []Tag
		for rows.Next() {
			var t Tag
			rows.Scan(&t.ID, &t.Name, &t.Color)
			tags = append(tags, t)
		}
		if tags == nil {
			tags = []Tag{}
		}
		r.writeJSON(w, tags)

	case "POST":
		var body struct {
			ContactUserID string `json:"contact_user_id"`
			TagID         int64  `json:"tag_id"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.ContactUserID == "" || body.TagID == 0 {
			r.writeError(w, 400, "contact_user_id and tag_id required")
			return
		}
		_, err := r.db.Exec(`INSERT OR IGNORE INTO contact_tags (contact_user_id, tag_id) VALUES (?, ?)`, body.ContactUserID, body.TagID)
		if err != nil {
			r.writeError(w, 500, err.Error())
			return
		}
		r.writeJSON(w, map[string]string{"status": "assigned"})

	case "DELETE":
		contactID := req.URL.Query().Get("contact")
		tagID := req.URL.Query().Get("tag")
		if contactID == "" || tagID == "" {
			r.writeError(w, 400, "contact and tag required")
			return
		}
		r.db.Exec(`DELETE FROM contact_tags WHERE contact_user_id = ? AND tag_id = ?`, contactID, tagID)
		r.writeJSON(w, map[string]string{"status": "removed"})

	default:
		r.writeError(w, 405, "Method not allowed")
	}
}

// loginHandler validates token and sets a cookie
func (r *Router) loginHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		r.writeError(w, 400, "Invalid body")
		return
	}
	authToken := r.apiToken
	if authToken == "" || body.Token != authToken {
		r.writeError(w, 401, "Invalid token")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "multichat_token",
		Value:    body.Token,
		Path:     "/",
		MaxAge:   86400 * 30, // 30 days
		HttpOnly: false,      // frontend needs to read it
		SameSite: http.SameSiteLaxMode,
		Secure:   true,
	})
	r.writeJSON(w, map[string]string{"status": "ok"})
}

// autoTagHandler analyzes a conversation and suggests tags
func (r *Router) autoTagHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		r.writeError(w, 405, "Method not allowed")
		return
	}

	var body struct {
		RoomID string `json:"room_id"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.RoomID == "" {
		r.writeError(w, 400, "room_id required")
		return
	}

	openaiKey := r.openAIAPIKey
	if openaiKey == "" {
		r.writeError(w, 500, "No AI API key configured")
		return
	}

	// Get messages
	msgs, err := r.matrix.GetMessages(body.RoomID, 50)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}

	var convo strings.Builder
	for _, m := range msgs {
		if m.Body != "" {
			convo.WriteString(fmt.Sprintf("%s: %s\n", m.SenderName, m.Body))
		}
	}
	if convo.Len() == 0 {
		r.writeJSON(w, map[string]interface{}{"suggested_tags": []string{}, "new_tags": []string{}})
		return
	}

	// Get existing tags
	rows, err := r.db.Query(`SELECT name FROM tags ORDER BY name`)
	var existingTags []string
	if err == nil {
		for rows.Next() {
			var name string
			rows.Scan(&name)
			existingTags = append(existingTags, name)
		}
		rows.Close()
	}

	tagList := "none yet"
	if len(existingTags) > 0 {
		tagList = strings.Join(existingTags, ", ")
	}

	prompt := fmt.Sprintf(`You are analyzing a business conversation from a logistics/freight brokerage company.

Existing tags in the system: [%s]

Based on the conversation below, suggest which existing tags apply AND suggest new tags if the conversation covers topics not covered by existing tags.

Focus on logistics-relevant categories like: freight type (LTL, FTL, Partial), equipment (Refrigerated, Flatbed, Dry Van, Power Only), service (Expedited, White Glove, Last Mile), status (Active, Pending, Closed), priority (Urgent, Routine), or any other relevant business category.

Respond ONLY with JSON: {"existing_tags": ["tag1", "tag2"], "new_tags": ["suggested new tag 1"]}

If no tags apply, return empty arrays.

Conversation:
%s`, tagList, convo.String())

	result, err := callOpenAI(openaiKey, convo.String(), prompt)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}

	r.writeJSON(w, result)
}

// bulkContactTagsHandler returns ALL contact→tag mappings in one call
// GET /contacts/tags/bulk → { "contact_user_id": [tag1, tag2], ... }
func (r *Router) bulkContactTagsHandler(w http.ResponseWriter, req *http.Request) {
	rows, err := r.db.Query(`SELECT ct.contact_user_id, t.id, t.name, t.color FROM contact_tags ct JOIN tags t ON ct.tag_id = t.id ORDER BY ct.contact_user_id, t.name`)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	type Tag struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	result := map[string][]Tag{}
	for rows.Next() {
		var contactID string
		var t Tag
		rows.Scan(&contactID, &t.ID, &t.Name, &t.Color)
		result[contactID] = append(result[contactID], t)
	}
	r.writeJSON(w, result)
}

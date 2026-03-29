package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/kayushkin/multichat/internal/matrix"
)

type Router struct {
	db         *sql.DB
	matrix     *matrix.Client
	mux        *http.ServeMux
	tgDBPath   string // path to mautrix-telegram SQLite DB (for phone lookups)
	metaDBPath string // path to mautrix-meta SQLite DB (for FB vs IG distinction)
}

func NewRouter(db *sql.DB, matrixClient *matrix.Client, tgDBPath, metaDBPath string) *Router {
	r := &Router{
		db:         db,
		matrix:     matrixClient,
		mux:        http.NewServeMux(),
		tgDBPath:   tgDBPath,
		metaDBPath: metaDBPath,
	}
	r.setupRoutes()
	return r
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// CORS for development
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	if req.Method == "OPTIONS" {
		return
	}
	r.mux.ServeHTTP(w, req)
}

func (r *Router) setupRoutes() {
	r.mux.HandleFunc("/health", r.healthHandler)
	r.mux.HandleFunc("/schema", r.schemaHandler)
	r.mux.HandleFunc("/contacts", r.contactsHandler)
	r.mux.HandleFunc("/conversations", r.conversationsHandler)
	r.mux.HandleFunc("/conversations/new", r.newConversationHandler)
	r.mux.HandleFunc("/conversations/", r.conversationSubHandler)
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
	tgPhones := map[string]string{}   // phone -> telegram display name
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
			// Map ghost (other_user_id in portal) → platform via receiver
			ghostPlatform := map[string]string{}
			rows, _ = metaDB.Query(`SELECT other_user_id, receiver FROM portal WHERE other_user_id IS NOT NULL AND receiver != ''`)
			if rows != nil {
				for rows.Next() {
					var ghostID, receiver string
					rows.Scan(&ghostID, &receiver)
					if p, ok := loginPlatform[receiver]; ok {
						ghostPlatform[ghostID] = p
					}
				}
				rows.Close()
			}
			metaDB.Close()

			for i := range merged {
				if merged[i].Platform == "meta" {
					id := merged[i].TelegramID
					if p, ok := ghostPlatform[id]; ok {
						merged[i].Platform = p
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

	r.writeJSON(w, merged)
}

func (r *Router) conversationsHandler(w http.ResponseWriter, req *http.Request) {
	if r.matrix == nil {
		r.writeError(w, 503, "Matrix client not configured")
		return
	}

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

	convos, err := r.matrix.ListConversations(metaPlatforms)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	r.writeJSON(w, convos)
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

	switch action {
	case "messages":
		r.messagesHandler(w, req, roomID)
	case "members":
		r.membersHandler(w, req, roomID)
	case "send":
		r.sendHandler(w, req, roomID)
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

	messages, err := r.matrix.GetMessages(roomID, limit)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	r.writeJSON(w, messages)
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

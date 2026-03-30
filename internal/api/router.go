package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

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
	r.mux.HandleFunc("/contacts/add", r.addContactHandler)
	r.mux.HandleFunc("/contacts/unified", r.unifiedContactsHandler)
	r.mux.HandleFunc("/stats", r.statsHandler)
	r.mux.HandleFunc("/search", r.searchHandler)
	r.mux.HandleFunc("/alerts", r.alertsHandler)
	r.mux.HandleFunc("/ai/summarize", r.summarizeHandler)
	r.mux.HandleFunc("/ai/escalation-scan", r.escalationScanHandler)
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
	// We'll create a simple contacts table for local storage
	_, err := r.db.Exec(`
		CREATE TABLE IF NOT EXISTS local_contacts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			display_name TEXT NOT NULL,
			platform TEXT NOT NULL,
			identifier TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(platform, identifier)
		)
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

// loadMetaPlatforms builds a map of Matrix room IDs to platform names from the meta bridge DB
func (r *Router) loadMetaPlatforms() matrix.MetaPlatformMap {
	if r.metaDBPath == "" { return nil }
	metaDB, err := sql.Open("sqlite3", r.metaDBPath+"?mode=ro")
	if err != nil { return nil }
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
			if p, ok := loginPlatform[receiver]; ok { metaPlatforms[mxid] = p }
		}
		rows.Close()
	}
	rows, _ = metaDB.Query(`SELECT p.mxid, up.login_id FROM portal p JOIN user_portal up ON p.id = up.portal_id AND p.receiver = up.portal_receiver WHERE p.mxid IS NOT NULL AND p.receiver = ''`)
	if rows != nil {
		for rows.Next() {
			var mxid, loginID string
			rows.Scan(&mxid, &loginID)
			if _, exists := metaPlatforms[mxid]; !exists {
				if p, ok := loginPlatform[loginID]; ok { metaPlatforms[mxid] = p }
			}
		}
		rows.Close()
	}
	return metaPlatforms
}

// --- New features: stats, search, alerts, AI summary, unified contacts, escalation ---

func (r *Router) statsHandler(w http.ResponseWriter, req *http.Request) {
	convos, err := r.matrix.ListConversations(r.loadMetaPlatforms())
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
		if total > 0 { pct = count * 100 / total }
		breakdown = append(breakdown, map[string]interface{}{"platform": p, "count": count, "percentage": pct})
	}
	sort.Slice(breakdown, func(i, j int) bool { return breakdown[i]["count"].(int) > breakdown[j]["count"].(int) })
	var mostActive []map[string]interface{}
	for i, c := range convos {
		if i >= 5 || c.LastActivity == 0 { break }
		mostActive = append(mostActive, map[string]interface{}{"name": c.Name, "platform": c.Platform, "room_id": c.RoomID})
	}
	r.writeJSON(w, map[string]interface{}{
		"total_conversations": total, "active_today": activeToday,
		"platforms_connected": len(platformCounts), "platform_breakdown": breakdown, "most_active": mostActive,
	})
}

func (r *Router) summarizeHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" { r.writeError(w, 405, "Method not allowed"); return }
	var body struct{ RoomID string `json:"room_id"` }
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.RoomID == "" {
		r.writeError(w, 400, "room_id required"); return
	}
	msgs, err := r.matrix.GetMessages(body.RoomID, 50)
	if err != nil { r.writeError(w, 500, err.Error()); return }
	var convo strings.Builder
	for _, m := range msgs {
		if m.Body != "" { convo.WriteString(fmt.Sprintf("%s: %s\n", m.SenderName, m.Body)) }
	}
	if convo.Len() == 0 {
		r.writeJSON(w, map[string]interface{}{"summary": "No messages found.", "topics": []string{}, "sentiment": "neutral"})
		return
	}
	openaiKey := os.Getenv("OPENAI_API_KEY")
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	var result map[string]interface{}
	if openaiKey != "" {
		result, err = callOpenAI(openaiKey, convo.String(), "Analyze the following conversation. Provide: 1) concise summary (2-3 sentences) 2) key topics as tags 3) sentiment (positive/negative/neutral/urgent). Respond ONLY with JSON: {\"summary\": \"...\", \"topics\": [\"...\"], \"sentiment\": \"...\"}")
	} else if anthropicKey != "" {
		result, err = callClaude(anthropicKey, convo.String(), "Analyze the following conversation. Provide: 1) concise summary (2-3 sentences) 2) key topics as tags 3) sentiment (positive/negative/neutral/urgent). Respond ONLY with JSON: {\"summary\": \"...\", \"topics\": [\"...\"], \"sentiment\": \"...\"}")
	} else {
		r.writeError(w, 500, "No AI API key configured"); return
	}
	if err != nil { r.writeError(w, 500, err.Error()); return }
	r.writeJSON(w, result)
}

func (r *Router) unifiedContactsHandler(w http.ResponseWriter, req *http.Request) {
	contacts, err := r.matrix.ListContacts()
	if err != nil { r.writeError(w, 500, err.Error()); return }
	type Identity struct {
		UserID   string `json:"user_id"`
		Platform string `json:"platform"`
		Avatar   string `json:"avatar_url,omitempty"`
	}
	type Unified struct {
		DisplayName string     `json:"display_name"`
		Identities  []Identity `json:"identities"`
		Platforms   []string   `json:"platforms"`
	}
	groups := map[string]*Unified{}
	for _, c := range contacts {
		key := strings.ToLower(strings.TrimSpace(c.DisplayName))
		if key == "" { continue }
		if _, ok := groups[key]; !ok {
			groups[key] = &Unified{DisplayName: c.DisplayName}
		}
		found := false
		for _, id := range groups[key].Identities {
			if id.UserID == c.UserID { found = true; break }
		}
		if !found {
			groups[key].Identities = append(groups[key].Identities, Identity{UserID: c.UserID, Platform: c.Platform, Avatar: c.AvatarURL})
		}
	}
	result := make([]Unified, 0, len(groups))
	for _, g := range groups {
		platSet := map[string]bool{}
		for _, id := range g.Identities { platSet[id.Platform] = true }
		for p := range platSet { g.Platforms = append(g.Platforms, p) }
		result = append(result, *g)
	}
	sort.Slice(result, func(i, j int) bool {
		if len(result[i].Platforms) != len(result[j].Platforms) { return len(result[i].Platforms) > len(result[j].Platforms) }
		return result[i].DisplayName < result[j].DisplayName
	})
	r.writeJSON(w, result)
}

func (r *Router) searchHandler(w http.ResponseWriter, req *http.Request) {
	q := strings.ToLower(strings.TrimSpace(req.URL.Query().Get("q")))
	if q == "" { r.writeError(w, 400, "q parameter required"); return }
	convos, _ := r.matrix.ListConversations(r.loadMetaPlatforms())
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
		if err != nil { continue }
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
	groups := make([]Group, 0, len(groupMap))
	total := 0
	for _, g := range groupMap { groups = append(groups, *g); total += len(g.Results) }
	r.writeJSON(w, map[string]interface{}{"groups": groups, "total": total, "query": q})
}

func (r *Router) alertsHandler(w http.ResponseWriter, req *http.Request) {
	hours := 24
	if h := req.URL.Query().Get("hours"); h != "" {
		if v, err := strconv.Atoi(h); err == nil { hours = v }
	}
	keywords := []string{"complaint", "urgent", "late", "damaged", "delay", "escalate", "cancel", "problem", "issue", "wrong", "broken", "failed", "missing", "angry", "refund"}
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour).UnixMilli()
	convos, _ := r.matrix.ListConversations(r.loadMetaPlatforms())
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
		if err != nil { continue }
		for _, m := range msgs {
			if m.Timestamp < cutoff { continue }
			lower := strings.ToLower(m.Body)
			for _, kw := range keywords {
				if strings.Contains(lower, kw) {
					sev := "low"
					if kw == "urgent" || kw == "escalate" || kw == "angry" { sev = "high" }
					if kw == "complaint" || kw == "failed" || kw == "refund" { sev = "medium" }
					alerts = append(alerts, Alert{RoomID: c.RoomID, RoomName: c.Name, Platform: c.Platform, Keyword: kw, Body: m.Body, Sender: m.SenderName, Time: m.Timestamp, Severity: sev})
					break
				}
			}
		}
	}
	sevCounts := map[string]int{"high": 0, "medium": 0, "low": 0}
	for _, a := range alerts { sevCounts[a.Severity]++ }
	if alerts == nil { alerts = []Alert{} }
	r.writeJSON(w, map[string]interface{}{"alerts": alerts, "total": len(alerts), "hours_scanned": hours, "keywords_used": keywords, "cutoff_time": cutoff, "severity_counts": sevCounts})
}

func (r *Router) escalationScanHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" { r.writeError(w, 405, "Method not allowed"); return }
	openaiKey := os.Getenv("OPENAI_API_KEY")
	if openaiKey == "" { r.writeError(w, 500, "No AI API key configured"); return }
	convos, _ := r.matrix.ListConversations(r.loadMetaPlatforms())
	limit := 10
	if len(convos) < limit { limit = len(convos) }
	var escalations []map[string]interface{}
	for _, c := range convos[:limit] {
		msgs, err := r.matrix.GetMessages(c.RoomID, 20)
		if err != nil || len(msgs) == 0 { continue }
		var convo strings.Builder
		for _, m := range msgs { if m.Body != "" { convo.WriteString(fmt.Sprintf("%s: %s\n", m.SenderName, m.Body)) } }
		if convo.Len() == 0 { continue }
		result, err := callOpenAI(openaiKey, convo.String(), "You are analyzing business conversations for a logistics company. Does this conversation need manager attention? Look for: angry customers, missed deadlines, risky promises, compliance issues. Respond ONLY with JSON: {\"needs_attention\": true/false, \"reason\": \"...\", \"severity\": \"high/medium/low\"}")
		if err != nil { continue }
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
	if err != nil { return nil, err }
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OpenAI returned %d: %s", resp.StatusCode, string(b))
	}
	var oai struct { Choices []struct { Message struct { Content string `json:"content"` } `json:"message"` } `json:"choices"` }
	json.NewDecoder(resp.Body).Decode(&oai)
	if len(oai.Choices) == 0 { return nil, fmt.Errorf("no choices") }
	text := strings.TrimSpace(oai.Choices[0].Message.Content)
	text = strings.TrimPrefix(text, "```json"); text = strings.TrimPrefix(text, "```"); text = strings.TrimSuffix(text, "```"); text = strings.TrimSpace(text)
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
	if err != nil { return nil, err }
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Claude returned %d: %s", resp.StatusCode, string(b))
	}
	var cr struct { Content []struct { Text string `json:"text"` } `json:"content"` }
	json.NewDecoder(resp.Body).Decode(&cr)
	if len(cr.Content) == 0 { return nil, fmt.Errorf("no content") }
	text := strings.TrimSpace(cr.Content[0].Text)
	text = strings.TrimPrefix(text, "```json"); text = strings.TrimPrefix(text, "```"); text = strings.TrimSuffix(text, "```"); text = strings.TrimSpace(text)
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return map[string]interface{}{"summary": text, "topics": []string{}, "sentiment": "neutral"}, nil
	}
	return result, nil
}

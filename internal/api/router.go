package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

type Router struct {
	db         *sql.DB
	mux        *http.ServeMux
	synapseURL string
	matrixToken string
}

func NewRouter(db *sql.DB) *Router {
	r := &Router{
		db:          db,
		mux:         http.NewServeMux(),
		synapseURL:  getEnvDefault("SYNAPSE_URL", "http://localhost:8008"),
		matrixToken: os.Getenv("MATRIX_TOKEN"),
	}
	r.setupRoutes()
	return r
}

func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if req.Method == "OPTIONS" {
		w.WriteHeader(200)
		return
	}
	r.mux.ServeHTTP(w, req)
}

func (r *Router) setupRoutes() {
	r.mux.HandleFunc("/health", r.healthHandler)
	r.mux.HandleFunc("/schema", r.schemaHandler)
	r.mux.HandleFunc("/conversations", r.conversationsHandler)
	r.mux.HandleFunc("/conversations/", r.conversationMessagesHandler)
	r.mux.HandleFunc("/contacts", r.contactsHandler)
	r.mux.HandleFunc("/contacts/unified", r.unifiedContactsHandler)
	r.mux.HandleFunc("/stats", r.statsHandler)
	r.mux.HandleFunc("/ai/summarize", r.summarizeHandler)
}

// --- Matrix helpers ---

func (r *Router) matrixGet(path string) ([]byte, error) {
	req, err := http.NewRequest("GET", r.synapseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+r.matrixToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

type MatrixRoom struct {
	RoomID       string `json:"room_id"`
	Name         string `json:"name"`
	MemberCount  int    `json:"member_count"`
	Platform     string `json:"platform"`
	LastActivity int64  `json:"last_activity"`
	LastMessage  string `json:"last_message,omitempty"`
}

func detectPlatform(members []string) string {
	for _, m := range members {
		if strings.Contains(m, "@telegram_") { return "telegram" }
		if strings.Contains(m, "@whatsapp_") { return "whatsapp" }
		if strings.Contains(m, "@meta_") { return "facebook" }
		if strings.Contains(m, "@signal_") { return "signal" }
		if strings.Contains(m, "@instagram_") { return "instagram" }
	}
	// Check room aliases or name patterns
	return "unknown"
}

func (r *Router) getJoinedRooms() ([]MatrixRoom, error) {
	data, err := r.matrixGet("/_matrix/client/v3/joined_rooms")
	if err != nil {
		return nil, err
	}
	var result struct {
		JoinedRooms []string `json:"joined_rooms"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	var rooms []MatrixRoom
	for _, roomID := range result.JoinedRooms {
		room := MatrixRoom{RoomID: roomID}

		// Get room state for name, members, etc.
		stateData, err := r.matrixGet("/_matrix/client/v3/rooms/" + url.PathEscape(roomID) + "/state")
		if err != nil {
			continue
		}
		var events []map[string]interface{}
		if err := json.Unmarshal(stateData, &events); err != nil {
			continue
		}

		var members []string
		for _, ev := range events {
			evType, _ := ev["type"].(string)
			content, _ := ev["content"].(map[string]interface{})
			switch evType {
			case "m.room.name":
				if name, ok := content["name"].(string); ok {
					room.Name = name
				}
			case "m.room.member":
				stateKey, _ := ev["state_key"].(string)
				membership, _ := content["membership"].(string)
				if membership == "join" {
					members = append(members, stateKey)
					if room.Name == "" {
						if dn, ok := content["displayname"].(string); ok && !strings.HasPrefix(stateKey, "@admin") {
							room.Name = dn
						}
					}
				}
			}
		}
		room.MemberCount = len(members)
		room.Platform = detectPlatform(members)

		// Also detect platform from member IDs in the room
		for _, m := range members {
			if strings.Contains(m, "@meta_") {
				// Could be facebook or instagram - check bridge type
				if strings.Contains(strings.ToLower(room.Name), "instagram") || strings.Contains(m, "instagram") {
					room.Platform = "instagram"
				} else if room.Platform == "unknown" {
					room.Platform = "facebook"
				}
			}
		}

		// Get last message
		msgsData, err := r.matrixGet("/_matrix/client/v3/rooms/" + url.PathEscape(roomID) + "/messages?dir=b&limit=1")
		if err == nil {
			var msgsResult struct {
				Chunk []struct {
					Content         map[string]interface{} `json:"content"`
					OriginServerTS  int64                  `json:"origin_server_ts"`
				} `json:"chunk"`
			}
			if json.Unmarshal(msgsData, &msgsResult) == nil && len(msgsResult.Chunk) > 0 {
				room.LastActivity = msgsResult.Chunk[0].OriginServerTS
				if body, ok := msgsResult.Chunk[0].Content["body"].(string); ok {
					room.LastMessage = body
				}
			}
		}

		if room.Name == "" {
			room.Name = "Unnamed Room"
		}
		rooms = append(rooms, room)
	}

	// Sort by last activity
	sort.Slice(rooms, func(i, j int) bool {
		return rooms[i].LastActivity > rooms[j].LastActivity
	})

	return rooms, nil
}

// --- Handlers ---

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

func (r *Router) conversationsHandler(w http.ResponseWriter, req *http.Request) {
	rooms, err := r.getJoinedRooms()
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	r.writeJSON(w, rooms)
}

func (r *Router) conversationMessagesHandler(w http.ResponseWriter, req *http.Request) {
	// Extract room ID from path: /conversations/{roomId}/messages
	path := strings.TrimPrefix(req.URL.Path, "/conversations/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		r.writeError(w, 400, "room_id required")
		return
	}
	roomID := parts[0]

	limit := "50"
	if l := req.URL.Query().Get("limit"); l != "" {
		limit = l
	}

	data, err := r.matrixGet("/_matrix/client/v3/rooms/" + url.PathEscape(roomID) + "/messages?dir=b&limit=" + limit)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}

	var msgsResult struct {
		Chunk []struct {
			EventID        string                 `json:"event_id"`
			Sender         string                 `json:"sender"`
			Content        map[string]interface{} `json:"content"`
			OriginServerTS int64                  `json:"origin_server_ts"`
		} `json:"chunk"`
	}
	if err := json.Unmarshal(data, &msgsResult); err != nil {
		r.writeError(w, 500, err.Error())
		return
	}

	// Determine "me" user IDs (telegram + whatsapp + meta bridges for the owner)
	myUserID := "@admin:chat.kayushkin.com"
	myTelegram := "@telegram_9000000001:chat.kayushkin.com"
	myWhatsapp := "@whatsapp_15550100001:chat.kayushkin.com"
	myMeta := "@meta_910000000000001:chat.kayushkin.com"

	type Message struct {
		EventID    string `json:"event_id"`
		Sender     string `json:"sender"`
		SenderName string `json:"sender_name"`
		Body       string `json:"body"`
		MsgType    string `json:"msg_type"`
		Timestamp  int64  `json:"timestamp"`
		IsMe       bool   `json:"is_me"`
	}

	var messages []Message
	for _, ev := range msgsResult.Chunk {
		body, _ := ev.Content["body"].(string)
		msgType, _ := ev.Content["msgtype"].(string)
		if msgType == "" || body == "" {
			continue
		}

		// Get display name from sender
		senderName := ev.Sender
		if idx := strings.Index(ev.Sender, ":"); idx > 0 {
			senderName = strings.TrimPrefix(ev.Sender[:idx], "@")
			// Clean up bridge prefixes
			for _, prefix := range []string{"telegram_", "whatsapp_", "meta_", "signal_", "instagram_"} {
				senderName = strings.TrimPrefix(senderName, prefix)
			}
		}

		isMe := ev.Sender == myUserID || ev.Sender == myTelegram || ev.Sender == myWhatsapp || ev.Sender == myMeta

		messages = append(messages, Message{
			EventID:    ev.EventID,
			Sender:     ev.Sender,
			SenderName: senderName,
			Body:       body,
			MsgType:    msgType,
			Timestamp:  ev.OriginServerTS,
			IsMe:       isMe,
		})
	}

	// Reverse to chronological order
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	r.writeJSON(w, messages)
}

type Contact struct {
	UserID      string   `json:"user_id"`
	DisplayName string   `json:"display_name"`
	AvatarURL   string   `json:"avatar_url,omitempty"`
	Phone       string   `json:"phone,omitempty"`
	Platform    string   `json:"platform"`
	Platforms   []string `json:"platforms"`
}

func (r *Router) contactsHandler(w http.ResponseWriter, req *http.Request) {
	data, err := r.matrixGet("/_matrix/client/v3/joined_rooms")
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	var result struct {
		JoinedRooms []string `json:"joined_rooms"`
	}
	json.Unmarshal(data, &result)

	contactMap := map[string]*Contact{}
	for _, roomID := range result.JoinedRooms {
		stateData, err := r.matrixGet("/_matrix/client/v3/rooms/" + url.PathEscape(roomID) + "/state")
		if err != nil {
			continue
		}
		var events []map[string]interface{}
		json.Unmarshal(stateData, &events)

		for _, ev := range events {
			evType, _ := ev["type"].(string)
			if evType != "m.room.member" {
				continue
			}
			stateKey, _ := ev["state_key"].(string)
			content, _ := ev["content"].(map[string]interface{})
			membership, _ := content["membership"].(string)
			if membership != "join" || strings.HasPrefix(stateKey, "@admin") {
				continue
			}

			if _, exists := contactMap[stateKey]; !exists {
				dn, _ := content["displayname"].(string)
				avatar, _ := content["avatar_url"].(string)
				platform := "unknown"
				if strings.Contains(stateKey, "@telegram_") {
					platform = "telegram"
				} else if strings.Contains(stateKey, "@whatsapp_") {
					platform = "whatsapp"
				} else if strings.Contains(stateKey, "@meta_") {
					platform = "facebook"
				} else if strings.Contains(stateKey, "@signal_") {
					platform = "signal"
				}
				contactMap[stateKey] = &Contact{
					UserID:      stateKey,
					DisplayName: dn,
					AvatarURL:   avatar,
					Platform:    platform,
					Platforms:   []string{platform},
				}
			}
		}
	}

	contacts := make([]Contact, 0, len(contactMap))
	for _, c := range contactMap {
		contacts = append(contacts, *c)
	}
	sort.Slice(contacts, func(i, j int) bool {
		return contacts[i].DisplayName < contacts[j].DisplayName
	})
	r.writeJSON(w, contacts)
}

func (r *Router) statsHandler(w http.ResponseWriter, req *http.Request) {
	rooms, err := r.getJoinedRooms()
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}

	platformCounts := map[string]int{}
	activeToday := 0
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).UnixMilli()

	for _, room := range rooms {
		platformCounts[room.Platform]++
		if room.LastActivity >= todayStart {
			activeToday++
		}
	}

	total := len(rooms)
	var breakdown []map[string]interface{}
	for platform, count := range platformCounts {
		pct := 0
		if total > 0 {
			pct = count * 100 / total
		}
		breakdown = append(breakdown, map[string]interface{}{
			"platform":   platform,
			"count":      count,
			"percentage": pct,
		})
	}
	sort.Slice(breakdown, func(i, j int) bool {
		return breakdown[i]["count"].(int) > breakdown[j]["count"].(int)
	})

	// Top 5 most active
	var mostActive []map[string]interface{}
	for i, room := range rooms {
		if i >= 5 {
			break
		}
		if room.LastActivity == 0 {
			break
		}
		mostActive = append(mostActive, map[string]interface{}{
			"name":     room.Name,
			"platform": room.Platform,
			"room_id":  room.RoomID,
		})
	}

	r.writeJSON(w, map[string]interface{}{
		"total_conversations": total,
		"active_today":        activeToday,
		"platforms_connected":  len(platformCounts),
		"platform_breakdown":   breakdown,
		"most_active":          mostActive,
	})
}

func (r *Router) summarizeHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		r.writeError(w, 405, "Method not allowed")
		return
	}

	var reqBody struct {
		RoomID string `json:"room_id"`
	}
	if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
		r.writeError(w, 400, "Invalid JSON body")
		return
	}
	if reqBody.RoomID == "" {
		r.writeError(w, 400, "room_id is required")
		return
	}

	// Fetch REAL messages from Matrix
	data, err := r.matrixGet("/_matrix/client/v3/rooms/" + url.PathEscape(reqBody.RoomID) + "/messages?dir=b&limit=50")
	if err != nil {
		r.writeError(w, 500, fmt.Sprintf("Error fetching messages: %v", err))
		return
	}

	var msgsResult struct {
		Chunk []struct {
			Sender         string                 `json:"sender"`
			Content        map[string]interface{} `json:"content"`
			OriginServerTS int64                  `json:"origin_server_ts"`
		} `json:"chunk"`
	}
	if err := json.Unmarshal(data, &msgsResult); err != nil {
		r.writeError(w, 500, "Failed to parse messages")
		return
	}

	// Build conversation text
	var convo strings.Builder
	for i := len(msgsResult.Chunk) - 1; i >= 0; i-- {
		ev := msgsResult.Chunk[i]
		body, _ := ev.Content["body"].(string)
		if body == "" {
			continue
		}
		sender := ev.Sender
		if idx := strings.Index(sender, ":"); idx > 0 {
			sender = strings.TrimPrefix(sender[:idx], "@")
			for _, p := range []string{"telegram_", "whatsapp_", "meta_", "signal_"} {
				sender = strings.TrimPrefix(sender, p)
			}
		}
		convo.WriteString(fmt.Sprintf("%s: %s\n", sender, body))
	}

	if convo.Len() == 0 {
		r.writeJSON(w, map[string]interface{}{
			"summary":   "No messages found in this conversation.",
			"topics":    []string{},
			"sentiment": "neutral",
		})
		return
	}

	// Try OpenAI first, fall back to returning a basic summary
	openaiKey := os.Getenv("OPENAI_API_KEY")
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")

	var summary map[string]interface{}

	if openaiKey != "" {
		summary, err = r.callOpenAI(openaiKey, convo.String())
	} else if anthropicKey != "" {
		summary, err = r.callClaude(anthropicKey, convo.String())
	} else {
		r.writeError(w, 500, "No AI API key configured (set OPENAI_API_KEY or ANTHROPIC_API_KEY)")
		return
	}

	if err != nil {
		r.writeError(w, 500, fmt.Sprintf("AI API error: %v", err))
		return
	}

	r.writeJSON(w, summary)
}

func (r *Router) callOpenAI(apiKey, conversation string) (map[string]interface{}, error) {
	prompt := fmt.Sprintf(`Analyze the following conversation from a business messaging platform. Provide:
1. A concise summary (2-3 sentences)
2. Key topics discussed (as short tags)
3. Overall sentiment (positive, negative, neutral, or urgent)

Respond ONLY with valid JSON: {"summary": "...", "topics": ["..."], "sentiment": "..."}

Conversation:
%s`, conversation)

	body := map[string]interface{}{
		"model": "gpt-4o-mini",
		"messages": []map[string]interface{}{
			{"role": "user", "content": prompt},
		},
		"max_tokens":  500,
		"temperature": 0.3,
	}
	jsonData, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OpenAI API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var oaiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&oaiResp); err != nil {
		return nil, err
	}
	if len(oaiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	text := oaiResp.Choices[0].Message.Content
	// Strip markdown code fences if present
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return map[string]interface{}{
			"summary":   text,
			"topics":    []string{},
			"sentiment": "neutral",
		}, nil
	}
	return result, nil
}

func (r *Router) callClaude(apiKey, conversation string) (map[string]interface{}, error) {
	prompt := fmt.Sprintf(`Analyze the following conversation from a business messaging platform. Provide:
1. A concise summary (2-3 sentences)
2. Key topics discussed (as short tags)
3. Overall sentiment (positive, negative, neutral, or urgent)

Respond ONLY with valid JSON: {"summary": "...", "topics": ["..."], "sentiment": "..."}

Conversation:
%s`, conversation)

	body := map[string]interface{}{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 500,
		"messages":   []map[string]interface{}{{"role": "user", "content": prompt}},
	}
	jsonData, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Claude API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var claudeResp struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	json.NewDecoder(resp.Body).Decode(&claudeResp)
	if len(claudeResp.Content) == 0 {
		return nil, fmt.Errorf("no content in response")
	}

	text := claudeResp.Content[0].Text
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return map[string]interface{}{
			"summary":   text,
			"topics":    []string{},
			"sentiment": "neutral",
		}, nil
	}
	return result, nil
}

func (r *Router) unifiedContactsHandler(w http.ResponseWriter, req *http.Request) {
	// Fetch all contacts from Matrix
	data, err := r.matrixGet("/_matrix/client/v3/joined_rooms")
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	var result struct {
		JoinedRooms []string `json:"joined_rooms"`
	}
	json.Unmarshal(data, &result)

	type PlatformIdentity struct {
		UserID    string `json:"user_id"`
		Platform  string `json:"platform"`
		AvatarURL string `json:"avatar_url,omitempty"`
		Phone     string `json:"phone,omitempty"`
	}

	// Collect all contacts
	contactMap := map[string]*struct {
		DisplayName string             `json:"display_name"`
		Identities  []PlatformIdentity `json:"identities"`
	}{}

	for _, roomID := range result.JoinedRooms {
		stateData, err := r.matrixGet("/_matrix/client/v3/rooms/" + url.PathEscape(roomID) + "/state")
		if err != nil {
			continue
		}
		var events []map[string]interface{}
		json.Unmarshal(stateData, &events)

		for _, ev := range events {
			evType, _ := ev["type"].(string)
			if evType != "m.room.member" {
				continue
			}
			stateKey, _ := ev["state_key"].(string)
			content, _ := ev["content"].(map[string]interface{})
			membership, _ := content["membership"].(string)
			if membership != "join" || strings.HasPrefix(stateKey, "@admin") {
				continue
			}

			dn, _ := content["displayname"].(string)
			avatar, _ := content["avatar_url"].(string)
			if dn == "" {
				continue
			}

			platform := "unknown"
			if strings.Contains(stateKey, "@telegram_") {
				platform = "telegram"
			} else if strings.Contains(stateKey, "@whatsapp_") {
				platform = "whatsapp"
			} else if strings.Contains(stateKey, "@meta_") {
				platform = "facebook"
			} else if strings.Contains(stateKey, "@signal_") {
				platform = "signal"
			}

			// Group by normalized display name
			key := strings.ToLower(strings.TrimSpace(dn))

			if _, exists := contactMap[key]; !exists {
				contactMap[key] = &struct {
					DisplayName string             `json:"display_name"`
					Identities  []PlatformIdentity `json:"identities"`
				}{
					DisplayName: dn,
				}
			}

			// Check if this exact user_id already added
			found := false
			for _, id := range contactMap[key].Identities {
				if id.UserID == stateKey {
					found = true
					break
				}
			}
			if !found {
				contactMap[key].Identities = append(contactMap[key].Identities, PlatformIdentity{
					UserID:    stateKey,
					Platform:  platform,
					AvatarURL: avatar,
				})
			}
		}
	}

	// Convert to sorted slice, multi-platform contacts first
	type UnifiedContact struct {
		DisplayName string             `json:"display_name"`
		Identities  []PlatformIdentity `json:"identities"`
		Platforms   []string           `json:"platforms"`
	}

	contacts := make([]UnifiedContact, 0, len(contactMap))
	for _, c := range contactMap {
		platSet := map[string]bool{}
		for _, id := range c.Identities {
			platSet[id.Platform] = true
		}
		plats := make([]string, 0, len(platSet))
		for p := range platSet {
			plats = append(plats, p)
		}
		contacts = append(contacts, UnifiedContact{
			DisplayName: c.DisplayName,
			Identities:  c.Identities,
			Platforms:    plats,
		})
	}

	// Sort: multi-platform first, then alphabetical
	sort.Slice(contacts, func(i, j int) bool {
		if len(contacts[i].Platforms) != len(contacts[j].Platforms) {
			return len(contacts[i].Platforms) > len(contacts[j].Platforms)
		}
		return contacts[i].DisplayName < contacts[j].DisplayName
	})

	r.writeJSON(w, contacts)
}

func (r *Router) writeJSON(w http.ResponseWriter, data interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(data)
}

func (r *Router) writeError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

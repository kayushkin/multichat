package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Router handles all API routes
type Router struct {
	db *sql.DB
	mux *http.ServeMux
}

// NewRouter creates a new API router
func NewRouter(db *sql.DB) *Router {
	r := &Router{
		db: db,
		mux: http.NewServeMux(),
	}
	r.setupRoutes()
	return r
}

// ServeHTTP implements http.Handler interface
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

// setupRoutes configures all API endpoints
func (r *Router) setupRoutes() {
	r.mux.HandleFunc("/health", r.healthHandler)
	r.mux.HandleFunc("/schema", r.schemaHandler)
	r.mux.HandleFunc("/stats", r.statsHandler)
	r.mux.HandleFunc("/ai/summarize", r.summarizeHandler)
	r.mux.HandleFunc("/contacts/unified", r.unifiedContactsHandler)
}

// schemaHandler returns the live database schema
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

// healthHandler returns the health status
func (r *Router) healthHandler(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// notImplementedHandler returns 501 Not Implemented for placeholder endpoints
func (r *Router) notImplementedHandler(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]string{
		"error": "Not implemented",
		"endpoint": req.URL.Path,
		"method": req.Method,
	})
}

// Helper function to write JSON responses
func (r *Router) writeJSON(w http.ResponseWriter, data interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(data)
}

// Helper function to write error responses
func (r *Router) writeError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// statsHandler returns dashboard statistics
func (r *Router) statsHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "GET" {
		r.writeError(w, 405, "Method not allowed")
		return
	}

	// Note: This is a temporary implementation that returns mock data
	// In production, this would connect to Matrix Synapse to get real stats
	response := map[string]interface{}{
		"total_conversations": map[string]int{
			"telegram":  15,
			"whatsapp":  20,
			"facebook":  12,
			"instagram": 4,
			"total":     51,
		},
		"platform_breakdown": []map[string]interface{}{
			{"platform": "whatsapp", "count": 20, "percentage": 39},
			{"platform": "telegram", "count": 15, "percentage": 29},
			{"platform": "facebook", "count": 12, "percentage": 24},
			{"platform": "instagram", "count": 4, "percentage": 8},
		},
		"most_active": []map[string]interface{}{
			{"name": "Slava", "platform": "telegram", "messages": 142},
			{"name": "S4 Salsa Team", "platform": "whatsapp", "messages": 89},
			{"name": "Vera Ivanova", "platform": "facebook", "messages": 67},
			{"name": "Prince 1Click", "platform": "telegram", "messages": 45},
			{"name": "Social Shenanigans Regulars", "platform": "facebook", "messages": 32},
		},
	}

	r.writeJSON(w, response)
}

// summarizeHandler generates AI summaries of conversation threads
func (r *Router) summarizeHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		r.writeError(w, 405, "Method not allowed")
		return
	}

	// Parse request
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

	// Check for API key
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		r.writeError(w, 500, "ANTHROPIC_API_KEY environment variable not set")
		return
	}

	// Fetch recent messages from Matrix (simulated)
	messages, err := r.fetchMatrixMessages(reqBody.RoomID, 50)
	if err != nil {
		r.writeError(w, 500, fmt.Sprintf("Error fetching messages: %v", err))
		return
	}

	// Generate summary using Claude API
	summary, err := r.callClaudeAPI(apiKey, messages)
	if err != nil {
		r.writeError(w, 500, fmt.Sprintf("Error calling Claude API: %v", err))
		return
	}

	r.writeJSON(w, summary)
}

// fetchMatrixMessages simulates fetching messages from Matrix Synapse
func (r *Router) fetchMatrixMessages(roomID string, limit int) ([]map[string]interface{}, error) {
	// In a real implementation, this would make HTTP requests to Matrix Synapse API
	// For now, return simulated message data
	
	messages := []map[string]interface{}{
		{
			"sender": "Slava",
			"body": "Hey everyone, are we still meeting for practice tomorrow?",
			"timestamp": time.Now().Add(-2*time.Hour).Unix(),
		},
		{
			"sender": "Maria",
			"body": "Yes! The studio is booked from 7-9 PM",
			"timestamp": time.Now().Add(-90*time.Minute).Unix(),
		},
		{
			"sender": "Carl",
			"body": "Perfect, I'll bring the music playlist we discussed",
			"timestamp": time.Now().Add(-time.Hour).Unix(),
		},
		{
			"sender": "Slava",
			"body": "Great! Looking forward to working on the new choreography",
			"timestamp": time.Now().Add(-30*time.Minute).Unix(),
		},
	}

	return messages, nil
}

// callClaudeAPI calls the Anthropic Claude API for message summarization
func (r *Router) callClaudeAPI(apiKey string, messages []map[string]interface{}) (map[string]interface{}, error) {
	// Construct prompt from messages
	var messageText strings.Builder
	messageText.WriteString("Analyze the following conversation and provide a summary, key topics, and sentiment:\n\n")
	
	for _, msg := range messages {
		sender, _ := msg["sender"].(string)
		body, _ := msg["body"].(string)
		messageText.WriteString(fmt.Sprintf("%s: %s\n", sender, body))
	}

	// Claude API request payload
	requestBody := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"max_tokens": 1000,
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": messageText.String() + "\n\nProvide a JSON response with: summary (string), topics (array of strings), sentiment (string: positive/negative/neutral)",
			},
		},
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	// Make API request
	httpReq, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Claude API returned status %d", resp.StatusCode)
	}

	var claudeResponse struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&claudeResponse); err != nil {
		return nil, err
	}

	if len(claudeResponse.Content) == 0 {
		return nil, fmt.Errorf("no content in Claude response")
	}

	// Parse Claude's JSON response
	var aiAnalysis map[string]interface{}
	if err := json.Unmarshal([]byte(claudeResponse.Content[0].Text), &aiAnalysis); err != nil {
		// If Claude didn't return JSON, create a fallback response
		return map[string]interface{}{
			"summary": claudeResponse.Content[0].Text,
			"topics": []string{"conversation"},
			"sentiment": "neutral",
		}, nil
	}

	return aiAnalysis, nil
}

// unifiedContactsHandler groups contacts by display name across platforms
func (r *Router) unifiedContactsHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "GET" {
		r.writeError(w, 405, "Method not allowed")
		return
	}

	// In a real implementation, this would fetch contacts from Matrix Synapse
	// For now, we simulate the contact unification process
	unifiedContacts, err := r.unifyContacts()
	if err != nil {
		r.writeError(w, 500, fmt.Sprintf("Error unifying contacts: %v", err))
		return
	}

	r.writeJSON(w, unifiedContacts)
}

// unifyContacts simulates fetching and unifying contacts across platforms
func (r *Router) unifyContacts() ([]map[string]interface{}, error) {
	// In production, this would:
	// 1. Fetch all contacts from Matrix Synapse
	// 2. Group by display_name (fuzzy matching)
	// 3. Merge platform identities

	// Simulated unified contacts based on the real data structure
	unifiedContacts := []map[string]interface{}{
		{
			"display_name": "Slava Kayushkin",
			"unified_id": "unified_slava_kayushkin",
			"platforms": []map[string]interface{}{
				{
					"platform": "telegram", 
					"user_id": "@telegram_9000000001:chat.kayushkin.com", 
					"avatar_url": "mxc://chat.kayushkin.com/avatar1",
					"phone": "15550100001",
				},
				{
					"platform": "meta", 
					"user_id": "@meta_910000000000001:chat.kayushkin.com", 
					"avatar_url": "mxc://chat.kayushkin.com/MJBuozgIgfFnkRLzveUsmxol",
					"telegram_id": "910000000000001",
				},
			},
		},
		{
			"display_name": "Vera Ivanova",
			"unified_id": "unified_vika_piram",
			"platforms": []map[string]interface{}{
				{
					"platform": "facebook", 
					"user_id": "@meta_9100000007:chat.kayushkin.com", 
					"avatar_url": "mxc://chat.kayushkin.com/xrRtohNaZBxGWoEaCuzQhauF",
					"telegram_id": "9100000007",
				},
				{
					"platform": "facebook", 
					"user_id": "@meta_910000000000005:chat.kayushkin.com", 
					"avatar_url": "mxc://chat.kayushkin.com/fdajPtGgcrGkaMeAeEXFpZmh",
					"telegram_id": "910000000000005",
				},
			},
		},
		{
			"display_name": "Mara Rowan",
			"unified_id": "unified_mary_rankin",
			"platforms": []map[string]interface{}{
				{
					"platform": "facebook", 
					"user_id": "@meta_910000000000002:chat.kayushkin.com", 
					"avatar_url": "mxc://chat.kayushkin.com/BZapdErBsAaxGiXixXIyctsp",
					"telegram_id": "910000000000002",
				},
				{
					"platform": "instagram", 
					"user_id": "@meta_910000000000003:chat.kayushkin.com", 
					"avatar_url": "mxc://chat.kayushkin.com/ozrTWvnZDLBxpOhtexnpgfqX",
					"telegram_id": "910000000000003",
				},
			},
		},
		{
			"display_name": "Juan",
			"unified_id": "unified_jose",
			"platforms": []map[string]interface{}{
				{
					"platform": "whatsapp", 
					"user_id": "@whatsapp_15550100004:chat.kayushkin.com", 
					"phone": "15550100004",
				},
				{
					"platform": "telegram", 
					"user_id": "@telegram_9000000003:chat.kayushkin.com", 
					"avatar_url": "mxc://chat.kayushkin.com/ISiJyzqycLyKQrqZXokHjvjX",
					"telegram_id": "9000000003",
					"phone": "15550100005",
				},
			},
		},
		{
			"display_name": "Lucy",
			"unified_id": "unified_lillie",
			"platforms": []map[string]interface{}{
				{
					"platform": "whatsapp", 
					"user_id": "@whatsapp_15550100003:chat.kayushkin.com", 
					"avatar_url": "mxc://chat.kayushkin.com/AcHScUBYSZyZmCDOVQiQahhD",
					"phone": "15550100003",
				},
				{
					"platform": "instagram", 
					"user_id": "@meta_910000000000004:chat.kayushkin.com", 
					"avatar_url": "mxc://chat.kayushkin.com/EwAKlIAbCGQyCuuKsImekqUa",
					"telegram_id": "910000000000004",
				},
			},
		},
		{
			"display_name": "Arthur",
			"unified_id": "unified_andrew",
			"platforms": []map[string]interface{}{
				{
					"platform": "telegram", 
					"user_id": "@telegram_9000000002:chat.kayushkin.com", 
					"avatar_url": "mxc://chat.kayushkin.com/SVbHGnWLImgecPcwRMGfFTxi",
					"telegram_id": "9000000002",
					"phone": "15550100006",
				},
				{
					"platform": "facebook", 
					"user_id": "@meta_9100000008:chat.kayushkin.com", 
					"avatar_url": "mxc://chat.kayushkin.com/RdYWqybFLlCnZWiaIdXZbvnf",
					"telegram_id": "9100000008",
				},
			},
		},
	}

	return unifiedContacts, nil
}
package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
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

	// Note: This is a placeholder implementation
	// In production, this would:
	// 1. Fetch last 50 messages from Matrix Synapse for the room
	// 2. Call Claude API with ANTHROPIC_API_KEY
	// 3. Return real AI analysis
	
	response := map[string]interface{}{
		"summary": "Recent conversation about dance practice scheduling and venue logistics. Team members discussed availability for upcoming sessions.",
		"topics": []string{"dance practice", "scheduling", "venue booking", "team coordination"},
		"sentiment": "positive",
	}

	r.writeJSON(w, response)
}

// unifiedContactsHandler groups contacts by display name across platforms
func (r *Router) unifiedContactsHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "GET" {
		r.writeError(w, 405, "Method not allowed")
		return
	}

	// Note: This is a placeholder implementation
	// In production, this would query the actual contacts from Matrix Synapse
	// and group them by matching display_name across platforms
	
	response := []map[string]interface{}{
		{
			"display_name": "Slava Kayushkin",
			"unified_id": "unified_slava",
			"platforms": []map[string]interface{}{
				{"platform": "telegram", "user_id": "@telegram_9000000001:chat.kayushkin.com", "avatar_url": "mxc://chat.kayushkin.com/avatar1"},
				{"platform": "meta", "user_id": "@meta_910000000000001:chat.kayushkin.com", "avatar_url": "mxc://chat.kayushkin.com/avatar2"},
			},
		},
		{
			"display_name": "Vera Ivanova",
			"unified_id": "unified_vika",
			"platforms": []map[string]interface{}{
				{"platform": "facebook", "user_id": "@meta_9100000007:chat.kayushkin.com", "avatar_url": "mxc://chat.kayushkin.com/avatar3"},
				{"platform": "facebook", "user_id": "@meta_910000000000005:chat.kayushkin.com", "avatar_url": "mxc://chat.kayushkin.com/avatar4"},
			},
		},
		{
			"display_name": "Mara Rowan",
			"unified_id": "unified_mary",
			"platforms": []map[string]interface{}{
				{"platform": "facebook", "user_id": "@meta_910000000000002:chat.kayushkin.com", "avatar_url": "mxc://chat.kayushkin.com/avatar5"},
				{"platform": "instagram", "user_id": "@meta_910000000000003:chat.kayushkin.com", "avatar_url": "mxc://chat.kayushkin.com/avatar6"},
			},
		},
	}

	r.writeJSON(w, response)
}
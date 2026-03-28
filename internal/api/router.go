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
	// Health check
	r.mux.HandleFunc("/health", r.healthHandler)
	
	// Authentication endpoints
	r.mux.HandleFunc("/auth/login", r.notImplementedHandler)
	r.mux.HandleFunc("/auth/logout", r.notImplementedHandler)
	r.mux.HandleFunc("/auth/register", r.notImplementedHandler)
	r.mux.HandleFunc("/auth/me", r.notImplementedHandler)
	
	// User management
	r.mux.HandleFunc("/users", r.notImplementedHandler)
	r.mux.HandleFunc("/users/", r.notImplementedHandler)
	
	// Platform connections
	r.mux.HandleFunc("/platforms", r.notImplementedHandler)
	r.mux.HandleFunc("/platforms/connect", r.notImplementedHandler)
	r.mux.HandleFunc("/platforms/disconnect", r.notImplementedHandler)
	r.mux.HandleFunc("/platforms/status", r.notImplementedHandler)
	
	// Conversations
	r.mux.HandleFunc("/conversations", r.notImplementedHandler)
	r.mux.HandleFunc("/conversations/", r.notImplementedHandler)
	
	// Messages
	r.mux.HandleFunc("/messages", r.notImplementedHandler)
	r.mux.HandleFunc("/messages/send", r.notImplementedHandler)
	r.mux.HandleFunc("/messages/", r.notImplementedHandler)
	
	// WebSocket for real-time updates
	r.mux.HandleFunc("/ws", r.notImplementedHandler)
	
	// Media uploads
	r.mux.HandleFunc("/media/upload", r.notImplementedHandler)
	r.mux.HandleFunc("/media/", r.notImplementedHandler)
	
	// Search
	r.mux.HandleFunc("/search", r.notImplementedHandler)
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
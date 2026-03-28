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
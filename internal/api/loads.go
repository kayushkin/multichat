package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

type Load struct {
	ID               int64   `json:"id"`
	ReferenceNumber  string  `json:"reference_number"`
	Shipper          string  `json:"shipper"`
	Consignee        string  `json:"consignee"`
	OriginCity       string  `json:"origin_city"`
	OriginState      string  `json:"origin_state"`
	DestinationCity  string  `json:"destination_city"`
	DestinationState string  `json:"destination_state"`
	Status           string  `json:"status"`
	Rate             float64 `json:"rate"`
	Weight           int     `json:"weight"`
	PickupDate       string  `json:"pickup_date"`
	DeliveryDate     string  `json:"delivery_date"`
	EquipmentType    string  `json:"equipment_type"`
	CreatedAt        string  `json:"created_at"`
	AttachmentStatus string  `json:"attachment_status,omitempty"` // tentative or confirmed (only set when attached to a conversation)
}

// GET /loads?status=open
func (r *Router) loadsHandler(w http.ResponseWriter, req *http.Request) {
	status := req.URL.Query().Get("status")
	var rows_ interface{ Next() bool; Scan(...interface{}) error; Close() error }
	var err error
	if status != "" {
		rows_, err = r.db.Query(`SELECT id, reference_number, shipper, consignee, origin_city, origin_state, destination_city, destination_state, status, rate, weight, pickup_date, delivery_date, equipment_type, created_at FROM loads WHERE status = ? ORDER BY created_at DESC`, status)
	} else {
		rows_, err = r.db.Query(`SELECT id, reference_number, shipper, consignee, origin_city, origin_state, destination_city, destination_state, status, rate, weight, pickup_date, delivery_date, equipment_type, created_at FROM loads ORDER BY created_at DESC`)
	}
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	defer rows_.Close()
	loads := []Load{}
	for rows_.Next() {
		var l Load
		if err := rows_.Scan(&l.ID, &l.ReferenceNumber, &l.Shipper, &l.Consignee, &l.OriginCity, &l.OriginState, &l.DestinationCity, &l.DestinationState, &l.Status, &l.Rate, &l.Weight, &l.PickupDate, &l.DeliveryDate, &l.EquipmentType, &l.CreatedAt); err != nil {
			r.writeError(w, 500, err.Error())
			return
		}
		loads = append(loads, l)
	}
	r.writeJSON(w, loads)
}

// GET /loads/search?q=term
func (r *Router) loadsSearchHandler(w http.ResponseWriter, req *http.Request) {
	q := strings.TrimSpace(req.URL.Query().Get("q"))
	if q == "" {
		r.writeError(w, 400, "q parameter required")
		return
	}
	like := "%" + q + "%"
	rows, err := r.db.Query(`SELECT id, reference_number, shipper, consignee, origin_city, origin_state, destination_city, destination_state, status, rate, weight, pickup_date, delivery_date, equipment_type, created_at FROM loads WHERE reference_number LIKE ? OR shipper LIKE ? OR consignee LIKE ? OR origin_city LIKE ? OR destination_city LIKE ? ORDER BY created_at DESC`, like, like, like, like, like)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	loads := []Load{}
	for rows.Next() {
		var l Load
		if err := rows.Scan(&l.ID, &l.ReferenceNumber, &l.Shipper, &l.Consignee, &l.OriginCity, &l.OriginState, &l.DestinationCity, &l.DestinationState, &l.Status, &l.Rate, &l.Weight, &l.PickupDate, &l.DeliveryDate, &l.EquipmentType, &l.CreatedAt); err != nil {
			r.writeError(w, 500, err.Error())
			return
		}
		loads = append(loads, l)
	}
	r.writeJSON(w, loads)
}

// GET /conversations/{id}/loads — list attached loads
// POST /conversations/{id}/loads — attach a load {load_id}
func (r *Router) conversationLoadsHandler(w http.ResponseWriter, req *http.Request, conversationID string) {
	switch req.Method {
	case "GET":
		rows, err := r.db.Query(`SELECT l.id, l.reference_number, l.shipper, l.consignee, l.origin_city, l.origin_state, l.destination_city, l.destination_state, l.status, l.rate, l.weight, l.pickup_date, l.delivery_date, l.equipment_type, l.created_at, COALESCE(cl.attachment_status, 'tentative') FROM loads l JOIN conversation_loads cl ON l.id = cl.load_id WHERE cl.conversation_id = ? ORDER BY cl.attached_at DESC`, conversationID)
		if err != nil {
			r.writeError(w, 500, err.Error())
			return
		}
		defer rows.Close()
		loads := []Load{}
		for rows.Next() {
			var l Load
			if err := rows.Scan(&l.ID, &l.ReferenceNumber, &l.Shipper, &l.Consignee, &l.OriginCity, &l.OriginState, &l.DestinationCity, &l.DestinationState, &l.Status, &l.Rate, &l.Weight, &l.PickupDate, &l.DeliveryDate, &l.EquipmentType, &l.CreatedAt, &l.AttachmentStatus); err != nil {
				r.writeError(w, 500, err.Error())
				return
			}
			loads = append(loads, l)
		}
		r.writeJSON(w, loads)

	case "POST":
		var body struct {
			LoadID           int64  `json:"load_id"`
			AttachmentStatus string `json:"attachment_status"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.LoadID == 0 {
			r.writeError(w, 400, "load_id required")
			return
		}
		if body.AttachmentStatus != "confirmed" {
			body.AttachmentStatus = "tentative"
		}
		_, err := r.db.Exec(`INSERT INTO conversation_loads (conversation_id, load_id, attachment_status) VALUES (?, ?, ?) ON CONFLICT(conversation_id, load_id) DO UPDATE SET attachment_status = ?`, conversationID, body.LoadID, body.AttachmentStatus, body.AttachmentStatus)
		if err != nil {
			r.writeError(w, 500, err.Error())
			return
		}
		r.writeJSON(w, map[string]string{"status": "attached", "attachment_status": body.AttachmentStatus})

	case "PATCH", "PUT":
		// Toggle attachment status
		var body struct {
			LoadID           int64  `json:"load_id"`
			AttachmentStatus string `json:"attachment_status"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.LoadID == 0 {
			r.writeError(w, 400, "load_id and attachment_status required")
			return
		}
		if body.AttachmentStatus != "confirmed" && body.AttachmentStatus != "tentative" {
			r.writeError(w, 400, "attachment_status must be 'tentative' or 'confirmed'")
			return
		}
		_, err := r.db.Exec(`UPDATE conversation_loads SET attachment_status = ? WHERE conversation_id = ? AND load_id = ?`, body.AttachmentStatus, conversationID, body.LoadID)
		if err != nil {
			r.writeError(w, 500, err.Error())
			return
		}
		r.writeJSON(w, map[string]string{"status": "updated", "attachment_status": body.AttachmentStatus})

	default:
		r.writeError(w, 405, "Method not allowed")
	}
}

// DELETE /conversations/{id}/loads/{load_id}
func (r *Router) conversationLoadDeleteHandler(w http.ResponseWriter, req *http.Request, conversationID string, loadID string) {
	if req.Method != "DELETE" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	_, err := r.db.Exec(`DELETE FROM conversation_loads WHERE conversation_id = ? AND load_id = ?`, conversationID, loadID)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	r.writeJSON(w, map[string]string{"status": "detached"})
}

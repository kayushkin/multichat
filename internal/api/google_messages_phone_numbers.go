package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// GoogleMessagesPhoneNumber is the phone number mautrix-gmessages records for
// one of its puppets.
type GoogleMessagesPhoneNumber struct {
	UserID      string `json:"user_id"`
	PhoneNumber string `json:"phone_number"`
}

// googleMessagesPhoneNumbersHandler serves GET /google-messages/phone-numbers:
// every mautrix-gmessages puppet the bridge's database gives a phone number,
// as the bridge wrote it (E.164), by user id. message-store's importer reads
// it to find the phone archive's thread for a Google Messages room; only
// multichat reads the bridge databases.
func (r *Router) googleMessagesPhoneNumbersHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "GET" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	if r.bridgeDatabases.GoogleMessages == "" {
		r.writeError(w, 503, "GMESSAGES_DB is not set, so the Google Messages bridge's database cannot be read")
		return
	}
	if r.matrix == nil {
		r.writeError(w, 503, "Matrix client not configured (SYNAPSE_URL and MATRIX_TOKEN), so the server name is unknown")
		return
	}
	ownUserID, err := r.matrix.WhoAmI()
	if err != nil {
		r.writeError(w, 502, err.Error())
		return
	}
	_, serverName, _ := strings.Cut(ownUserID, ":")
	numbers, err := readGoogleMessagesPhoneNumbers(r.bridgeDatabases.GoogleMessages, serverName)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	r.writeJSON(w, numbers)
}

func readGoogleMessagesPhoneNumbers(path, serverName string) ([]GoogleMessagesPhoneNumber, error) {
	database, err := sql.Open("sqlite3", "file:"+path+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open GMESSAGES_DB %s: %w", path, err)
	}
	defer database.Close()
	rows, err := database.Query(`SELECT id, json_extract(metadata, '$.phone') FROM ghost WHERE json_extract(metadata, '$.phone') IS NOT NULL AND json_extract(metadata, '$.phone') != ''`)
	if err != nil {
		return nil, fmt.Errorf("read phone numbers from GMESSAGES_DB %s: %w", path, err)
	}
	defer rows.Close()
	numbers := []GoogleMessagesPhoneNumber{}
	for rows.Next() {
		var id, phoneNumber string
		if err := rows.Scan(&id, &phoneNumber); err != nil {
			return nil, fmt.Errorf("read phone numbers from GMESSAGES_DB: %w", err)
		}
		numbers = append(numbers, GoogleMessagesPhoneNumber{UserID: "@gmessages_" + id + ":" + serverName, PhoneNumber: phoneNumber})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(numbers, func(i, j int) bool { return numbers[i].UserID < numbers[j].UserID })
	return numbers, nil
}

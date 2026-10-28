package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/kayushkin/multichat/internal/matrix"
)

// GET /inbound-platforms serves matrix.KnownPlatforms, the platform
// vocabulary a filter may name (the message log's platform, for one).
func (r *Router) inboundPlatformsHandler(w http.ResponseWriter, req *http.Request) {
	r.writeJSON(w, matrix.KnownPlatforms)
}

func decodeStrict(req *http.Request, into any) error {
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return fmt.Errorf("invalid JSON: %v", err)
	}
	return nil
}

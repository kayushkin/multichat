package api

import (
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
)

// mediaPassedHeaders are the response headers GET /media copies from
// Synapse unchanged: the body's type and length, its cache headers, and the
// headers Synapse sets so an uploaded HTML or SVG file cannot run as a page
// (Content-Disposition, Content-Security-Policy, X-Content-Type-Options).
var mediaPassedHeaders = []string{
	"Content-Type",
	"Content-Length",
	"Cache-Control",
	"Expires",
	"ETag",
	"Last-Modified",
	"Content-Disposition",
	"Content-Security-Policy",
	"X-Content-Type-Options",
}

// mediaDefaultCacheControl is added to a successful answer when Synapse
// sends no Cache-Control: an mxc URI never changes what it points at.
const mediaDefaultCacheControl = "private, max-age=86400"

// mediaHandler serves GET /media/{server_name}/{media_id}[?width=W&height=H]:
// the file behind mxc://{server_name}/{media_id}, fetched from Synapse's
// authenticated media API with multichat's Matrix token and streamed through
// with Synapse's status. With width and height both given it is a thumbnail
// scaled to fit them.
func (r *Router) mediaHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "GET" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	if r.matrix == nil {
		r.writeError(w, 503, "Matrix client not configured")
		return
	}
	segments := strings.Split(strings.TrimPrefix(req.URL.Path, "/media/"), "/")
	if len(segments) != 2 || segments[0] == "" || segments[1] == "" {
		r.writeError(w, 400, "the path must be /media/{server_name}/{media_id}")
		return
	}
	query := req.URL.Query()
	width, height := 0, 0
	if query.Has("width") || query.Has("height") {
		var errW, errH error
		width, errW = strconv.Atoi(query.Get("width"))
		height, errH = strconv.Atoi(query.Get("height"))
		if errW != nil || errH != nil || width <= 0 || height <= 0 {
			r.writeError(w, 400, "width and height must both be given, as positive integers")
			return
		}
	}
	resp, err := r.matrix.OpenMedia(req.Context(), segments[0], segments[1], width, height)
	if err != nil {
		r.writeError(w, 502, err.Error())
		return
	}
	defer resp.Body.Close()
	for _, name := range mediaPassedHeaders {
		if value := resp.Header.Get(name); value != "" {
			w.Header().Set(name, value)
		}
	}
	if resp.StatusCode == http.StatusOK && resp.Header.Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", mediaDefaultCacheControl)
	}
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		// The status is already sent; all that is left is to say so.
		log.Printf("media %s/%s: streaming stopped: %v", segments[0], segments[1], err)
	}
}

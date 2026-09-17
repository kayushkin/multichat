package api

import (
	"io"
	"net/http"
)

func (r *Router) uploadHandler(w http.ResponseWriter, req *http.Request, roomID string) {
	if req.Method != "POST" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	if r.matrix == nil {
		r.writeError(w, 503, "Matrix client not configured")
		return
	}

	// 10MB limit
	req.Body = http.MaxBytesReader(w, req.Body, 10<<20)
	if err := req.ParseMultipartForm(10 << 20); err != nil {
		r.writeError(w, 400, "File too large (max 10MB)")
		return
	}

	file, header, err := req.FormFile("file")
	if err != nil {
		r.writeError(w, 400, "No file provided")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		r.writeError(w, 500, "Failed to read file")
		return
	}

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	filename := header.Filename

	// Upload to Matrix media store
	mxcURI, err := r.matrix.UploadMedia(data, contentType, filename)
	if err != nil {
		r.writeError(w, 500, "Upload failed: "+err.Error())
		return
	}

	// Send image message to room
	eventID, err := r.matrix.SendImage(roomID, mxcURI, filename, contentType, len(data))
	if err != nil {
		r.writeError(w, 500, "Send failed: "+err.Error())
		return
	}

	r.writeJSON(w, map[string]string{
		"event_id":     eventID,
		"mxc_uri":      mxcURI,
		"filename":     filename,
		"content_type": contentType,
	})
}

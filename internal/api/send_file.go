package api

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strings"

	"github.com/kayushkin/multichat/internal/matrix"
)

// sendFileHandler serves POST /conversations/{room_id}/send-file, a multipart
// form with `file` and an optional `caption`: upload the file to Synapse and
// send it to the room as an image, video, audio or file message, with the
// caption as the text of the same message. The bridge posts it on the
// platform as our own account, so it reaches real people. The size limit is
// Synapse's own (m.upload.size); over it is a 413 naming both sizes.
func (r *Router) sendFileHandler(w http.ResponseWriter, req *http.Request, roomID string) {
	if req.Method != "POST" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	if r.matrix == nil {
		r.writeError(w, 503, "Matrix client not configured")
		return
	}
	limit, err := r.matrix.MediaUploadSizeLimit()
	if err != nil {
		r.writeError(w, 502, err.Error())
		return
	}
	if limit > 0 {
		// The form's own fields and boundaries take a little room beside the file.
		req.Body = http.MaxBytesReader(w, req.Body, limit+1<<20)
	}
	file, header, err := req.FormFile("file")
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		r.writeError(w, 413, fmt.Sprintf("the file is larger than the %s Synapse takes", humanBytes(limit)))
		return
	}
	if err != nil {
		r.writeError(w, 400, "multipart form with a file field is required: "+err.Error())
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if errors.As(err, &tooLarge) {
		r.writeError(w, 413, fmt.Sprintf("the file is larger than the %s Synapse takes", humanBytes(limit)))
		return
	}
	if err != nil {
		r.writeError(w, 400, "read the file: "+err.Error())
		return
	}
	if len(data) == 0 {
		r.writeError(w, 400, "the file is empty")
		return
	}
	if limit > 0 && int64(len(data)) > limit {
		r.writeError(w, 413, fmt.Sprintf("the file is %s, larger than the %s Synapse takes", humanBytes(int64(len(data))), humanBytes(limit)))
		return
	}
	fileName := header.Filename
	if fileName == "" {
		r.writeError(w, 400, "the file has no name")
		return
	}
	mimeType := strings.TrimSpace(strings.Split(header.Header.Get("Content-Type"), ";")[0])
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = strings.Split(http.DetectContentType(data), ";")[0]
	}
	sent := matrix.SentFile{FileName: fileName, Caption: strings.TrimSpace(req.FormValue("caption")), MimeType: mimeType, Size: len(data)}
	if strings.HasPrefix(mimeType, "image/") {
		if config, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
			sent.Width, sent.Height = config.Width, config.Height
		}
	}
	sent.MXCURI, err = r.matrix.UploadMedia(data, mimeType, fileName)
	if err != nil {
		r.writeError(w, 502, err.Error())
		return
	}
	eventID, err := r.matrix.SendFile(roomID, sent)
	if err != nil {
		r.writeError(w, 502, "uploaded as "+sent.MXCURI+" but not sent: "+err.Error())
		return
	}
	r.writeJSON(w, map[string]any{
		"event_id": eventID, "mxc_uri": sent.MXCURI, "filename": fileName, "mimetype": mimeType,
		"msgtype": matrix.FileMessageType(mimeType), "size": len(data),
	})
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}

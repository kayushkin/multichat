package api

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"strings"
	"testing"

	"github.com/kayushkin/multichat/internal/matrix"
)

func TestSendFile(t *testing.T) {
	var sent map[string]any
	var uploadedType string
	synapse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/_matrix/client/v1/media/config":
			w.Write([]byte(`{"m.upload.size": 2000}`))
		case r.URL.Path == "/_matrix/media/v3/upload":
			uploadedType = r.Header.Get("Content-Type")
			w.Write([]byte(`{"content_uri":"mxc://chat.example/file1"}`))
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/send/m.room.message/"):
			sent = nil
			json.NewDecoder(r.Body).Decode(&sent)
			w.Write([]byte(`{"event_id":"$file-message"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer synapse.Close()
	router := routerForSettingsTests(t)
	router.matrix = matrix.NewClient(synapse.URL, secretValues["MATRIX_TOKEN"])

	post := func(fileName, contentType string, data []byte, caption string) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		if data != nil {
			header := textproto.MIMEHeader{}
			header.Set("Content-Disposition", `form-data; name="file"; filename="`+fileName+`"`)
			header.Set("Content-Type", contentType)
			part, _ := form.CreatePart(header)
			part.Write(data)
		}
		if caption != "" {
			form.WriteField("caption", caption)
		}
		form.Close()
		req := httptest.NewRequest(http.MethodPost, "/conversations/"+url.PathEscape("!room:chat.example")+"/send-file", &body)
		req.Header.Set("Content-Type", form.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+secretValues["MULTICHAT_TOKEN"])
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	var picture bytes.Buffer
	png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 12, 7)))
	rec := post("screenshot.png", "image/png", picture.Bytes(), "  look at this  ")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"msgtype":"m.image"`) || !strings.Contains(rec.Body.String(), "$file-message") {
		t.Fatalf("image: %d %s", rec.Code, rec.Body)
	}
	info := sent["info"].(map[string]any)
	if sent["msgtype"] != "m.image" || sent["body"] != "look at this" || sent["filename"] != "screenshot.png" ||
		sent["url"] != "mxc://chat.example/file1" || info["w"] != 12.0 || info["h"] != 7.0 || uploadedType != "image/png" {
		t.Fatalf("image message = %+v", sent)
	}

	if rec := post("clip.mp4", "video/mp4", []byte("not really a video"), ""); rec.Code != 200 {
		t.Fatalf("video: %d %s", rec.Code, rec.Body)
	}
	if sent["msgtype"] != "m.video" || sent["body"] != "clip.mp4" {
		t.Fatalf("video message = %+v", sent)
	}
	if post("notes.pdf", "application/pdf", []byte("%PDF"), ""); sent["msgtype"] != "m.file" {
		t.Fatalf("file message = %+v", sent)
	}

	if rec := post("big.png", "image/png", bytes.Repeat([]byte("x"), 3000), ""); rec.Code != 413 || !strings.Contains(rec.Body.String(), "2 KB") {
		t.Fatalf("too large: %d %s", rec.Code, rec.Body)
	}
	if rec := post("", "", nil, "just text"); rec.Code != 400 {
		t.Fatalf("no file: %d %s", rec.Code, rec.Body)
	}
	if rec := post("empty.png", "image/png", []byte{}, ""); rec.Code != 400 {
		t.Fatalf("empty file: %d %s", rec.Code, rec.Body)
	}
}

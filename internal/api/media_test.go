package api

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/matrix"
)

// fakeSynapseMedia answers the authenticated media API the way Synapse 1.161
// does, and refuses a request without the Matrix token.
func fakeSynapseMedia(t *testing.T, release <-chan struct{}) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer the-matrix-token-under-test" {
			w.WriteHeader(401)
			return
		}
		switch {
		case r.URL.Path == "/_matrix/client/v1/media/download/chat.example/emoji":
			w.Header().Set("Content-Type", "image/webp")
			w.Header().Set("Content-Length", strconv.Itoa(len(mediaFirstPart)+len("rest")))
			w.Header().Set("Content-Security-Policy", "sandbox;")
			w.Header().Set("X-Not-Passed", "x")
			w.Write([]byte(mediaFirstPart))
			w.(http.Flusher).Flush()
			<-release // the rest only after the client has read the first part
			w.Write([]byte("rest"))
		case r.URL.Path == "/_matrix/client/v1/media/thumbnail/chat.example/emoji":
			if r.URL.Query().Get("width") != "48" || r.URL.Query().Get("height") != "32" || r.URL.Query().Get("method") != "scale" {
				w.WriteHeader(400)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Cache-Control", "public, max-age=5")
			w.Write([]byte("thumb"))
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(404)
			w.Write([]byte(`{"errcode":"M_NOT_FOUND"}`))
		}
	}))
}

// mediaFirstPart is larger than the buffers between a handler and the
// client, so it reaches the client only if the proxy streams it on.
var mediaFirstPart = strings.Repeat("x", 64<<10) + "|"

func TestMediaStreamsSynapsesAuthenticatedMedia(t *testing.T) {
	release := make(chan struct{}, 1)
	synapse := fakeSynapseMedia(t, release)
	defer synapse.Close()
	router := routerForSettingsTests(t)
	router.matrix = matrix.NewClient(synapse.URL, secretValues["MATRIX_TOKEN"])
	server := httptest.NewServer(router)
	defer server.Close()
	// Deferred last so it runs first: a failed test must not leave the fake
	// Synapse waiting and the servers unable to close.
	defer func() {
		select {
		case release <- struct{}{}:
		default:
		}
	}()

	get := func(path string) *http.Response {
		t.Helper()
		request, _ := http.NewRequest("GET", server.URL+path, nil)
		request.Header.Set("Authorization", "Bearer "+secretValues["MULTICHAT_TOKEN"])
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}

	// The headers and the first part arrive while Synapse is still holding
	// the rest: the proxy streams rather than buffering the whole file.
	type firstPart struct {
		response *http.Response
		reader   *bufio.Reader
		part     string
	}
	arrived := make(chan firstPart, 1)
	go func() {
		request, _ := http.NewRequest("GET", server.URL+"/media/chat.example/emoji", nil)
		request.Header.Set("Authorization", "Bearer "+secretValues["MULTICHAT_TOKEN"])
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			arrived <- firstPart{}
			return
		}
		reader := bufio.NewReader(response.Body)
		part, _ := reader.ReadString('|')
		arrived <- firstPart{response, reader, part}
	}()
	var first firstPart
	select {
	case first = <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("nothing arrived before Synapse finished: the body is buffered")
	}
	response := first.response
	if response == nil {
		t.Fatal("the download request failed")
	}
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "image/webp" ||
		response.Header.Get("Content-Length") != strconv.Itoa(len(mediaFirstPart)+4) || response.Header.Get("Cache-Control") != "private, max-age=86400" ||
		response.Header.Get("Content-Security-Policy") != "sandbox;" || response.Header.Get("X-Not-Passed") != "" {
		t.Fatalf("download: %d %v", response.StatusCode, response.Header)
	}
	if first.part != mediaFirstPart {
		t.Fatalf("first part is %d bytes", len(first.part))
	}
	reader := first.reader
	release <- struct{}{}
	rest, _ := io.ReadAll(reader)
	response.Body.Close()
	if string(rest) != "rest" {
		t.Errorf("rest = %q", rest)
	}

	thumbnail := get("/media/chat.example/emoji?width=48&height=32")
	body, _ := io.ReadAll(thumbnail.Body)
	thumbnail.Body.Close()
	if thumbnail.StatusCode != 200 || string(body) != "thumb" || thumbnail.Header.Get("Cache-Control") != "public, max-age=5" {
		t.Errorf("thumbnail: %d %q %v", thumbnail.StatusCode, body, thumbnail.Header)
	}

	missing := get("/media/chat.example/nothing")
	missing.Body.Close()
	if missing.StatusCode != 404 || missing.Header.Get("Cache-Control") != "" {
		t.Errorf("a missing file is Synapse's 404, not cached: %d %v", missing.StatusCode, missing.Header)
	}

	for _, path := range []string{"/media/chat.example", "/media/chat.example/emoji/extra", "/media//emoji",
		"/media/chat.example/emoji?width=48", "/media/chat.example/emoji?width=0&height=5"} {
		response := get(path)
		response.Body.Close()
		if response.StatusCode != 400 {
			t.Errorf("GET %s = %d, want 400", path, response.StatusCode)
		}
	}
	if got := serve(router, http.MethodGet, "/media/chat.example/emoji", "").Code; got != 401 {
		t.Errorf("without the bearer = %d, want 401", got)
	}
}

func TestConversationMessagesCarryFormattingAndReactions(t *testing.T) {
	synapse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/messages"):
			w.Write([]byte(`{"end":"t2","chunk":[
				{"event_id":"$image","sender":"@discord_2:x","origin_server_ts":2,"type":"m.room.message",
				 "content":{"body":"image0.gif","msgtype":"m.image","url":"mxc://x/G","info":{"mimetype":"image/gif"},
				 "m.relates_to":{"m.in_reply_to":{"event_id":"$emoji"}}}},
				{"event_id":"$emoji","sender":"@discord_1:x","origin_server_ts":1,"type":"m.room.message",
				 "content":{"body":"hi","msgtype":"m.text","format":"org.matrix.custom.html",
				 "formatted_body":"hi <img data-mx-emoticon src=\"mxc://x/Q\" alt=\":bearlaugh:\">"}}]}`))
		case r.URL.Path == "/_matrix/client/v3/account/whoami":
			w.Write([]byte(`{"user_id":"@admin:x"}`))
		default:
			w.Write([]byte(`{"displayname":"Someone"}`))
		}
	}))
	defer synapse.Close()
	database, err := db.Initialize(filepath.Join(t.TempDir(), "multichat.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.LogReaction(database, db.LoggedReaction{EventID: "$r", TargetEventID: "$emoji", RoomID: "!room",
		Platform: "discord", SenderUserID: "@discord_3:x", SenderDisplayName: "Jade", Key: "mxc://x/this",
		Shortcode: ":this:", ContentJSON: `{}`, SentAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	router := routerForSettingsTests(t)
	router.db = database
	router.matrix = matrix.NewClient(synapse.URL, secretValues["MATRIX_TOKEN"])

	response := serve(router, http.MethodGet, "/conversations/!room/messages", secretValues["MULTICHAT_TOKEN"])
	if response.Code != 200 {
		t.Fatalf("%d %s", response.Code, response.Body)
	}
	var page struct {
		Messages []map[string]any `json:"messages"`
		End      string           `json:"end"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil || len(page.Messages) != 2 || page.End != "t2" {
		t.Fatalf("page = %s (%v)", response.Body, err)
	}
	emoji, image := page.Messages[0], page.Messages[1]
	if emoji["format"] != "org.matrix.custom.html" || !strings.Contains(emoji["formatted_body"].(string), "data-mx-emoticon") {
		t.Errorf("emoji message = %v", emoji)
	}
	reactions := emoji["reactions"].([]any)
	if len(reactions) != 1 || reactions[0].(map[string]any)["shortcode"] != ":this:" ||
		reactions[0].(map[string]any)["count"] != float64(1) {
		t.Errorf("reactions = %v", reactions)
	}
	if image["media_url"] != "mxc://x/G" || image["media_mimetype"] != "image/gif" ||
		image["reply_to_event_id"] != "$emoji" || len(image["reactions"].([]any)) != 0 {
		t.Errorf("image message = %v", image)
	}
}

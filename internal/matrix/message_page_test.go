package matrix

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// An edit reaches the page as the edited text with the id of the message it
// edits, never as Matrix's "* …" fallback.
func TestAnEditCarriesItsNewContentAndTheMessageItReplaces(t *testing.T) {
	edit := `{"event_id":"$edit","sender":"@discord_1:x","origin_server_ts":2,"content":{
		"msgtype":"m.text","body":"* ### Jazz Night","format":"org.matrix.custom.html","formatted_body":"* <h3>Jazz Night</h3>",
		"m.new_content":{"msgtype":"m.text","body":"### Jazz Night","format":"org.matrix.custom.html","formatted_body":"<h3>Jazz Night</h3>"},
		"m.relates_to":{"rel_type":"m.replace","event_id":"$original"}}}`
	original := `{"event_id":"$original","sender":"@discord_1:x","origin_server_ts":1,"content":{"msgtype":"m.text","body":"### Jazz"}}`
	synapse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/messages"):
			w.Write([]byte(`{"chunk":[` + edit + `,` + original + `],"end":""}`))
		case strings.Contains(r.URL.Path, "/profile/"):
			w.Write([]byte(`{"displayname":"Event-Manager"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer synapse.Close()

	page, err := NewClient(synapse.URL, "token").GetMessagePage("!room:x", 50, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 2 {
		t.Fatalf("messages = %+v", page.Messages)
	}
	got := page.Messages[1]
	if got.EventID != "$edit" || got.ReplacesEventID != "$original" || got.Body != "### Jazz Night" ||
		got.FormattedBody != "<h3>Jazz Night</h3>" {
		t.Fatalf("edit = %+v", got)
	}
	if page.Messages[0].ReplacesEventID != "" {
		t.Fatalf("the original is not an edit: %+v", page.Messages[0])
	}
}

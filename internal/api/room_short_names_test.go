package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func routerWithShortNameStore(t *testing.T, handler http.HandlerFunc) *Router {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	router := routerWithoutAuth(t)
	router.shortNameStoreURL = server.URL
	return router
}

func TestRoomShortNamesAreReadPageByPageAndAnsweredAsOneList(t *testing.T) {
	var queries []string
	router := routerWithShortNameStore(t, func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		count := shortNameListPageSize
		if r.URL.Query().Get("offset") != "0" {
			count = 2
		}
		rows := make([]string, count)
		for index := range rows {
			rows[index] = fmt.Sprintf(`{"id":"short_name_%d","path":"p%d"}`, index, index)
		}
		io.WriteString(w, "["+strings.Join(rows, ",")+"]")
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/conversations/short-names", nil))
	var rows []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &rows); err != nil || recorder.Code != 200 {
		t.Fatalf("%d %.200s %v", recorder.Code, recorder.Body, err)
	}
	if len(rows) != shortNameListPageSize+2 || len(queries) != 2 {
		t.Fatalf("%d rows over %d pages", len(rows), len(queries))
	}
	if !strings.Contains(queries[0], "namespace=messages") || !strings.Contains(queries[0], "entity_type=multichat_room") || !strings.Contains(queries[1], "offset=1000") {
		t.Fatalf("queries %v", queries)
	}
}

func TestResolvingANameNoRoomHasPassesTheStores404Through(t *testing.T) {
	var query string
	router := routerWithShortNameStore(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.WriteHeader(404)
		io.WriteString(w, `{"error":"no record has that path"}`)
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/conversations/short-names/resolve?path=20s30s/general", nil))
	if recorder.Code != 404 || !strings.Contains(recorder.Body.String(), "no record has that path") || query != "namespace=messages&path=20s30s%2Fgeneral" {
		t.Fatalf("%d %s, store asked %q", recorder.Code, recorder.Body, query)
	}
}

func TestRenamingARoomWritesTheWholeNameAsTheOperator(t *testing.T) {
	var posted map[string]string
	router := routerWithShortNameStore(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/short-names" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&posted)
		w.WriteHeader(409)
		io.WriteString(w, `{"error":"another record holds kat"}`)
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("PUT", "/conversations/!room:chat.example/short-name", strings.NewReader(`{"path":" messages/kat "}`)))
	if recorder.Code != 409 || !strings.Contains(recorder.Body.String(), "another record holds kat") {
		t.Fatalf("the store's refusal should pass through: %d %s", recorder.Code, recorder.Body)
	}
	want := map[string]string{"namespace": "messages", "path": "kat", "entity_type": "multichat_room", "entity_id": "!room:chat.example", "created_by": "operator", "reason": shortNameRenameReason}
	for key, value := range want {
		if posted[key] != value {
			t.Fatalf("posted %v, want %s=%q", posted, key, value)
		}
	}
	for _, body := range []string{`{"path":"  "}`, `{"path":"kat","created_by":"someone"}`} {
		recorder = httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("PUT", "/conversations/!room:chat.example/short-name", strings.NewReader(body)))
		if recorder.Code != 400 {
			t.Fatalf("%s: %d %s", body, recorder.Code, recorder.Body)
		}
	}
}

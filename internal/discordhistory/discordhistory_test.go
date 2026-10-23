package discordhistory

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A channel of 150 messages (ids 1..150) served newest first, 100 per page,
// with one 429 before the second page.
func TestFetchChannelPagesBackWaitsOutRateLimitsAndKeepsTheTokenToDiscord(t *testing.T) {
	var requests []string
	rateLimited := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "user-login" {
			http.Error(w, `{"message":"401"}`, http.StatusUnauthorized)
			return
		}
		requests = append(requests, r.URL.RawQuery)
		before := 151
		if b := r.URL.Query().Get("before"); b != "" {
			fmt.Sscan(b, &before)
			if !rateLimited {
				rateLimited = true
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"retry_after": 2.5}`))
				return
			}
		}
		var page []string
		for id := before - 1; id >= 1 && len(page) < 100; id-- {
			attachments := "[]"
			if id == 7 {
				attachments = `[{"id":"a7","filename":"cat.jpg","content_type":"image/jpeg","url":"https://cdn.example/cat.jpg?sig=secret"}]`
			}
			page = append(page, fmt.Sprintf(`{"id":"%d","attachments":%s}`, id, attachments))
		}
		w.Write([]byte("[" + strings.Join(page, ",") + "]"))
	}))
	defer server.Close()

	var slept []time.Duration
	fetcher := &Fetcher{APIBaseURL: server.URL, Token: "user-login", Client: server.Client(), Pause: 3 * time.Second,
		Sleep: func(d time.Duration) { slept = append(slept, d) }, MaximumRateLimitWaits: 3}
	var out bytes.Buffer
	var sawAttachmentOn string
	result, err := fetcher.FetchChannel("600", "", &out, func(id string, attachments []Attachment) error {
		sawAttachmentOn = id + ":" + attachments[0].Filename
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Messages != 150 || result.Pages != 2 || result.Oldest != "1" {
		t.Errorf("result = %+v, want 150 messages in 2 pages ending at id 1", result)
	}
	if lines := strings.Count(out.String(), "\n"); lines != 150 {
		t.Errorf("wrote %d lines, want one per message", lines)
	}
	if sawAttachmentOn != "7:cat.jpg" {
		t.Errorf("attachments seen on %q", sawAttachmentOn)
	}
	if len(requests) != 3 || !strings.Contains(requests[2], "before=51") {
		t.Errorf("requests = %v, want the first page, a rate-limited second, then the second again", requests)
	}
	waitedOut := false
	for _, d := range slept {
		if d == 3500*time.Millisecond {
			waitedOut = true
		}
		if d < time.Second {
			t.Errorf("slept %v: every wait is at least a second", d)
		}
	}
	if !waitedOut {
		t.Errorf("slept %v, want the 2.5 s Discord asked for plus a second", slept)
	}
}

func TestStopBeforeEndsTheFetchAtAMessageAlreadyHeld(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id":"30"},{"id":"20"},{"id":"10"}]`))
	}))
	defer server.Close()
	fetcher := &Fetcher{APIBaseURL: server.URL, Client: server.Client(), Sleep: func(time.Duration) {}}
	var out bytes.Buffer
	result, err := fetcher.FetchChannel("600", "25", &out, nil)
	if err != nil || result.Messages != 1 {
		t.Fatalf("result = %+v, %v; want only the message newer than 25", result, err)
	}
}

func TestAChannelTheAccountCannotReadIsErrNoAccessAndTooManyRateLimitsStop(t *testing.T) {
	forbidden := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Missing Access"}`, http.StatusForbidden)
	}))
	defer forbidden.Close()
	fetcher := &Fetcher{APIBaseURL: forbidden.URL, Client: forbidden.Client(), Sleep: func(time.Duration) {}}
	if _, err := fetcher.FetchChannel("600", "", &bytes.Buffer{}, nil); !errors.Is(err, ErrNoAccess) {
		t.Errorf("403 = %v, want ErrNoAccess", err)
	}

	limited := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"retry_after": 1}`))
	}))
	defer limited.Close()
	fetcher = &Fetcher{APIBaseURL: limited.URL, Client: limited.Client(), Sleep: func(time.Duration) {}, MaximumRateLimitWaits: 2}
	_, err := fetcher.FetchChannel("600", "", &bytes.Buffer{}, nil)
	if err == nil || !strings.Contains(err.Error(), "rate limited 3 times") {
		t.Errorf("endless 429s = %v, want a stop after the third", err)
	}
}

func TestADownloadSendsNoLoginAndNoErrorShowsTheSignedLink(t *testing.T) {
	var sawAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuthorization = r.Header.Get("Authorization")
		http.Error(w, "gone", http.StatusInternalServerError)
	}))
	defer server.Close()
	fetcher := &Fetcher{Token: "user-login", Client: server.Client(), Sleep: func(time.Duration) {}}
	_, err := fetcher.Download(Attachment{URL: server.URL + "/cat.jpg?ex=1&sig=secret"})
	if sawAuthorization != "" {
		t.Errorf("a CDN download sent Authorization %q", sawAuthorization)
	}
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("error = %v: it must exist and must not carry the signed query", err)
	}
}

// Package discordhistory reads a Discord channel's history through the
// bridge's logged-in account, slowly, so the message log can reach back
// before a server was bridged. It writes each message as the raw Discord API
// object, one per line, which cmd/multichat-import-discord-archive reads, and
// can save each image attachment beside it.
//
// It is a user account reading history, so it goes at a walking pace: one
// request at a time, a pause between every request, and it waits out every
// 429 for as long as Discord asks.
package discordhistory

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Fetcher reads history. Token is the account's login and is sent only in the
// Authorization header of requests to APIBaseURL; it never appears in an error.
type Fetcher struct {
	APIBaseURL string
	Token      string
	Client     *http.Client
	// Pause is the wait before every request, history page or attachment.
	Pause time.Duration
	// Sleep waits; tests replace it.
	Sleep func(time.Duration)
	// MaximumRateLimitWaits stops a channel after this many 429s in a row.
	MaximumRateLimitWaits int
}

// ErrNoAccess is a channel the account may not read (403) or that no longer
// exists (404).
var ErrNoAccess = errors.New("the account cannot read this channel")

// Attachment is the part of a Discord attachment the fetcher reads.
type Attachment struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	URL         string `json:"url"`
	Size        int64  `json:"size"`
}

type message struct {
	ID          string       `json:"id"`
	Attachments []Attachment `json:"attachments"`
}

// Result says what one channel's fetch read.
type Result struct {
	Pages    int
	Messages int
	Oldest   string // the oldest message id read
}

func (f *Fetcher) sleep(d time.Duration) {
	if f.Sleep != nil {
		f.Sleep(d)
		return
	}
	time.Sleep(d)
}

// get makes one paced GET and returns the body of a 200, waiting out 429s.
func (f *Fetcher) get(target string, authorized bool) ([]byte, error) {
	waits := 0
	for {
		f.sleep(f.Pause)
		request, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			return nil, err
		}
		if authorized {
			request.Header.Set("Authorization", f.Token)
		}
		response, err := f.Client.Do(request)
		if err != nil {
			return nil, fmt.Errorf("GET %s: %w", redact(target), err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("GET %s: %w", redact(target), err)
		}
		switch {
		case response.StatusCode == http.StatusOK:
			return body, nil
		case response.StatusCode == http.StatusTooManyRequests:
			waits++
			if waits > f.MaximumRateLimitWaits {
				return nil, fmt.Errorf("GET %s: rate limited %d times in a row; stopping", redact(target), waits)
			}
			f.sleep(retryAfter(response, body))
		case response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusNotFound:
			return nil, fmt.Errorf("GET %s: %d: %w", redact(target), response.StatusCode, ErrNoAccess)
		default:
			return nil, fmt.Errorf("GET %s: %d %s", redact(target), response.StatusCode, strings.TrimSpace(string(body)))
		}
	}
}

// retryAfter reads how long Discord asks us to wait: the JSON retry_after,
// else the Retry-After header, else a minute; never less than a second.
func retryAfter(response *http.Response, body []byte) time.Duration {
	var answer struct {
		RetryAfter float64 `json:"retry_after"`
	}
	wait := time.Minute
	if json.Unmarshal(body, &answer) == nil && answer.RetryAfter > 0 {
		wait = time.Duration(answer.RetryAfter * float64(time.Second))
	} else if seconds, err := strconv.ParseFloat(response.Header.Get("Retry-After"), 64); err == nil && seconds > 0 {
		wait = time.Duration(seconds * float64(time.Second))
	}
	return max(wait, time.Second) + time.Second
}

// redact keeps a signed CDN link's query out of an error.
func redact(target string) string {
	if parsed, err := url.Parse(target); err == nil {
		parsed.RawQuery = ""
		return parsed.String()
	}
	return "(unparseable URL)"
}

// FetchChannel reads a channel's whole history, newest first, a page of 100 at
// a time, writing each message to out as one line of raw JSON. Messages older
// than stopBefore (a message id, "" for none) end the fetch. onMessage, when
// set, sees each message's attachments.
func (f *Fetcher) FetchChannel(channelID, stopBefore string, out io.Writer, onMessage func(messageID string, attachments []Attachment) error) (Result, error) {
	var result Result
	before := ""
	for {
		query := url.Values{"limit": {"100"}}
		if before != "" {
			query.Set("before", before)
		}
		body, err := f.get(strings.TrimRight(f.APIBaseURL, "/")+"/channels/"+url.PathEscape(channelID)+"/messages?"+query.Encode(), true)
		if err != nil {
			return result, err
		}
		var page []json.RawMessage
		if err := json.Unmarshal(body, &page); err != nil {
			return result, fmt.Errorf("channel %s: a page that is not a list of messages: %w", channelID, err)
		}
		result.Pages++
		if len(page) == 0 {
			return result, nil
		}
		for _, raw := range page {
			var m message
			if err := json.Unmarshal(raw, &m); err != nil || m.ID == "" {
				return result, fmt.Errorf("channel %s: a message with no id", channelID)
			}
			if stopBefore != "" && snowflakeLess(m.ID, stopBefore) {
				return result, nil
			}
			if _, err := out.Write(append(append([]byte(nil), raw...), '\n')); err != nil {
				return result, err
			}
			result.Messages++
			result.Oldest = m.ID
			if onMessage != nil && len(m.Attachments) > 0 {
				if err := onMessage(m.ID, m.Attachments); err != nil {
					return result, err
				}
			}
		}
		before = result.Oldest
		if len(page) < 100 {
			return result, nil
		}
	}
}

// Download fetches an attachment's bytes from its CDN link, paced like any
// other request. It sends no Authorization: the link is signed.
func (f *Fetcher) Download(attachment Attachment) ([]byte, error) {
	return f.get(attachment.URL, false)
}

// snowflakeLess compares two Discord ids numerically.
func snowflakeLess(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

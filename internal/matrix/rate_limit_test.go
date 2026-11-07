package matrix

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAGetSynapseRefusesForGoingTooFastIsAskedAgainAfterTheWaitItNames(t *testing.T) {
	asked := 0
	synapse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		if asked < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"errcode":"M_LIMIT_EXCEEDED","error":"Too Many Requests","retry_after_ms":5}`))
			return
		}
		w.Write([]byte(`{"displayname":"Jade"}`))
	}))
	defer synapse.Close()
	name, err := NewClient(synapse.URL, "token").DisplayNameOf("@discord_1:x")
	if err != nil || name != "Jade" || asked != 3 {
		t.Fatalf("name %q, err %v, asked %d times; want Jade on the third ask", name, err, asked)
	}
}

func TestAGetSynapseKeepsRefusingFailsWithTheRefusal(t *testing.T) {
	asked := 0
	synapse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"errcode":"M_LIMIT_EXCEEDED","retry_after_ms":1}`))
	}))
	defer synapse.Close()
	_, err := NewClient(synapse.URL, "token").DisplayNameOf("@discord_1:x")
	if err == nil || !strings.Contains(err.Error(), "M_LIMIT_EXCEEDED") || asked != getRateLimitAttempts {
		t.Fatalf("err %v after %d asks; want the 429 after %d", err, asked, getRateLimitAttempts)
	}
}

func TestARefusalWithoutAWaitIsNotAskedAgain(t *testing.T) {
	asked := 0
	synapse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"errcode":"M_LIMIT_EXCEEDED"}`))
	}))
	defer synapse.Close()
	_, err := NewClient(synapse.URL, "token").DisplayNameOf("@discord_1:x")
	if err == nil || !strings.Contains(err.Error(), "retry_after_ms") || asked != 1 {
		t.Fatalf("err %v after %d asks; want a failure naming the missing wait after one", err, asked)
	}
}

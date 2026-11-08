package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kayushkin/llm-bridge/servicesettings"
	"github.com/kayushkin/multichat/internal/botaccounts"
	"github.com/kayushkin/multichat/internal/config"
	"github.com/kayushkin/multichat/internal/matrix"
)

type fakeBots struct {
	sends []string
	files [][]botaccounts.File
	err   error
}

func (f *fakeBots) States() []botaccounts.AccountState {
	return []botaccounts.AccountState{{Account: botaccounts.Account{ID: "message_account_000002", Platform: "discord", DisplayName: "Helper Bot", Kind: "bot"}, State: botaccounts.StateConnected}}
}

func (f *fakeBots) Send(accountID, userID, channelID, body string, files []botaccounts.File) (botaccounts.SendResult, error) {
	f.sends = append(f.sends, fmt.Sprintf("%s|%s|%s|%s", accountID, userID, channelID, body))
	f.files = append(f.files, files)
	if f.err != nil {
		return botaccounts.SendResult{}, f.err
	}
	return botaccounts.SendResult{AccountID: accountID, ChannelID: "dm-1", MessageIDs: []string{"m1"}}, nil
}

func routerWithoutAuth(t *testing.T) *Router {
	t.Helper()
	settings, err := config.NewSettingsRegistry(servicesettings.MapEnvironment(map[string]string{}))
	if err != nil {
		t.Fatal(err)
	}
	return NewRouter(nil, nil, config.Config{}, settings)
}

func post(router *Router, path, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("POST", path, strings.NewReader(body)))
	return recorder
}

func TestSendAsBot(t *testing.T) {
	router := routerWithoutAuth(t)
	if got := post(router, "/messages/send", `{"account_id":"message_account_000002","user_id":"7","body":"hi"}`); got.Code != 503 {
		t.Fatalf("no bot accounts: %d %s", got.Code, got.Body)
	}
	bots := &fakeBots{}
	router.SetBotAccounts(bots)
	got := post(router, "/messages/send", `{"account_id":"message_account_000002","user_id":"7","body":"hi"}`)
	if got.Code != 200 || !strings.Contains(got.Body.String(), `"message_ids":["m1"]`) || bots.sends[0] != "message_account_000002|7||hi" {
		t.Fatalf("send by user: %d %s %v", got.Code, got.Body, bots.sends)
	}
	if got := post(router, "/messages/send", `{"account_id":"message_account_000002","channel_id":"55","body":"hi"}`); got.Code != 200 || bots.sends[1] != "message_account_000002||55|hi" {
		t.Fatalf("send by channel: %d %v", got.Code, bots.sends)
	}
	for name, body := range map[string]string{
		"both user and channel": `{"account_id":"a","user_id":"7","channel_id":"55","body":"hi"}`,
		"neither":               `{"account_id":"a","body":"hi"}`,
		"no body":               `{"account_id":"a","user_id":"7"}`,
		"files without bot":     `{"user_id":"7","body":"hi","files":[{"name":"a.png","data_base64":"AQID"}]}`,
		"file not base64":       `{"account_id":"a","user_id":"7","files":[{"name":"a.png","data_base64":"!!"}]}`,
		"file with no name":     `{"account_id":"a","user_id":"7","files":[{"name":"","data_base64":"AQID"}]}`,
		"channel without bot":   `{"channel_id":"55","body":"hi"}`,
		"unknown field":         `{"account":"a","user_id":"7","body":"hi"}`,
	} {
		if got := post(router, "/messages/send", body); got.Code != 400 {
			t.Errorf("%s: %d %s", name, got.Code, got.Body)
		}
	}
	if got := post(router, "/messages/send", `{"account_id":"message_account_000002","channel_id":"55","files":[{"name":"a.png","content_type":"image/png","data_base64":"AQID"}]}`); got.Code != 200 {
		t.Fatalf("a file alone: %d %s", got.Code, got.Body)
	}
	if last := bots.files[len(bots.files)-1]; len(last) != 1 || last[0].Name != "a.png" || len(last[0].Data) != 3 {
		t.Fatalf("file as sent: %+v", last)
	}
	bots.err = fmt.Errorf("x: %w", botaccounts.ErrUnknownAccount)
	if got := post(router, "/messages/send", `{"account_id":"nope","user_id":"7","body":"hi"}`); got.Code != 404 {
		t.Fatalf("unknown account: %d", got.Code)
	}
	bots.err = errors.New("discord said no")
	if got := post(router, "/messages/send", `{"account_id":"a","user_id":"7","body":"hi"}`); got.Code != 502 {
		t.Fatalf("discord failure: %d", got.Code)
	}
}

func TestAccountsListsOwnAndBotAccounts(t *testing.T) {
	saved := matrix.SelfUserIDs
	defer func() { matrix.SelfUserIDs = saved }()
	matrix.SelfUserIDs = map[string]bool{"@discord_42:chat.example.com": true, "@admin:chat.example.com": true}
	router := routerWithoutAuth(t)
	router.SetBotAccounts(&fakeBots{})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/accounts", nil))
	var answer struct {
		Own  []ownAccount               `json:"own_accounts"`
		Bots []botaccounts.AccountState `json:"bot_accounts"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &answer); err != nil {
		t.Fatal(err, recorder.Body.String())
	}
	if len(answer.Own) != 1 || answer.Own[0].Platform != "discord" || len(answer.Bots) != 1 || answer.Bots[0].State != "connected" {
		t.Fatalf("accounts: %s", recorder.Body)
	}
}

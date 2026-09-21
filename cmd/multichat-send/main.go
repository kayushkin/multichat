// multichat-send messages one person on one platform from the shell — the
// scheduler's shell jobs call it.
//
//	multichat-send -name "Alex Morgan" -platform whatsapp "running 10 min late"
//	multichat-send -user-id @telegram_12345:chat.kayushkin.com "same, by id"
//	echo "body from stdin" | multichat-send -name "…" -platform telegram
//
// Settings come from the environment, all required, and are declared in
// config.SendCommandSettingDefinitions:
//
//	MULTICHAT_URL     multichat's base URL (http://localhost:8402)
//	AUTH_STORE_URL    auth-store, which holds the API token as provider "multichat"
//	AUTH_STORE_TOKEN  auth-store's own bearer
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/kayushkin/llm-bridge/servicesettings"
	"github.com/kayushkin/multichat/internal/client"
	"github.com/kayushkin/multichat/internal/config"
)

const authStoreProvider = "multichat"

func main() {
	name := flag.String("name", "", "contact display name (exact, case-insensitive); needs -platform")
	platform := flag.String("platform", "", "whatsapp | telegram | signal | meta")
	userID := flag.String("user-id", "", "bridge puppet id, instead of -name/-platform")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: multichat-send (-name NAME -platform PLATFORM | -user-id ID) [message]\n\nmessage is the remaining arguments, or stdin when none are given.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	body := strings.Join(flag.Args(), " ")
	if body == "" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fail("read stdin: %v", err)
		}
		body = strings.TrimRight(string(data), "\n")
	}
	if body == "" {
		fail("no message: pass it as arguments or on stdin")
	}
	if *userID == "" && (*name == "" || *platform == "") {
		fail("pass -user-id, or both -name and -platform")
	}
	if *userID != "" && (*name != "" || *platform != "") {
		fail("pass -user-id or -name/-platform, not both")
	}

	settings, err := config.NewSendCommandSettingsRegistry(servicesettings.ProcessEnvironment())
	if err != nil {
		fail("%v", err)
	}
	if err := settings.CheckRequired(); err != nil {
		fail("%v", err)
	}
	multichatURL := settings.String(config.SendSettingMultichatURL)
	token, err := resolveTokenFromAuthStore(settings.String(config.SendSettingAuthStoreURL), settings.String(config.SendSettingAuthStoreToken))
	if err != nil {
		fail("%v", err)
	}
	api := client.New(multichatURL, token)

	target := *userID
	if target == "" {
		contacts, err := api.UnifiedContacts()
		if err != nil {
			fail("%v", err)
		}
		identity, err := client.ResolveContact(contacts, *name, *platform)
		if err != nil {
			fail("%v", err)
		}
		target = identity.UserID
	}
	result, err := api.SendDirectMessage(target, body)
	if err != nil {
		fail("%v", err)
	}
	out, _ := json.Marshal(map[string]any{"user_id": target, "room_id": result.RoomID, "event_id": result.EventID, "created_room": result.CreatedRoom})
	fmt.Println(string(out))
}

func resolveTokenFromAuthStore(authStoreURL, authStoreToken string) (string, error) {
	req, err := http.NewRequest("GET", strings.TrimRight(authStoreURL, "/")+"/api/resolve/"+authStoreProvider, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+authStoreToken)
	req.Header.Set("X-Auth-App", "multichat-send")
	req.Header.Set("X-Auth-Reason", "send")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return "", fmt.Errorf("auth-store: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("auth-store: resolve %s: %d %s", authStoreProvider, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var out struct {
		AuthType string `json:"auth_type"`
		APIKey   string `json:"api_key"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("auth-store: %w", err)
	}
	if out.AuthType != "api_key" || out.APIKey == "" {
		return "", fmt.Errorf("auth-store: provider %s is %q with empty api_key; expected an api_key credential", authStoreProvider, out.AuthType)
	}
	return out.APIKey, nil
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "multichat-send: "+format+"\n", args...)
	os.Exit(1)
}

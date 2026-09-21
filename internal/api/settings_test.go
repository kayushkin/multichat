package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kayushkin/llm-bridge/msg"
	"github.com/kayushkin/llm-bridge/servicesettings"
	"github.com/kayushkin/multichat/internal/config"
)

// Secret values no other test uses, so a /settings body that contains one is
// a leak and nothing else.
var secretValues = map[string]string{
	"MULTICHAT_TOKEN":         "the-api-token-under-test",
	"MATRIX_TOKEN":            "the-matrix-token-under-test",
	"LLMBRIDGE_SERVICE_TOKEN": "the-bridge-token-under-test",
	"OPENAI_API_KEY":          "the-openai-key-under-test",
	"ANTHROPIC_API_KEY":       "the-anthropic-key-under-test",
}

func routerForSettingsTests(t *testing.T) *Router {
	t.Helper()
	variables := map[string]string{"PORT": "9402"}
	for name, value := range secretValues {
		variables[name] = value
	}
	settings, err := config.NewSettingsRegistry(servicesettings.MapEnvironment(variables))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(settings)
	if err != nil {
		t.Fatal(err)
	}
	// The settings route touches neither the database nor Matrix.
	return NewRouter(nil, nil, loaded, settings)
}

func serve(router *Router, method, path, bearer string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(`{"value":"x"}`))
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestSettingsSitsBehindTheBearerAndWithholdsEverySecret(t *testing.T) {
	router := routerForSettingsTests(t)

	if got := serve(router, http.MethodGet, "/settings", "").Code; got != http.StatusUnauthorized {
		t.Errorf("GET /settings with no token = %d, want 401", got)
	}
	if got := serve(router, http.MethodGet, "/settings", "a-wrong-token").Code; got != http.StatusUnauthorized {
		t.Errorf("GET /settings with a wrong token = %d, want 401", got)
	}

	response := serve(router, http.MethodGet, "/settings", secretValues["MULTICHAT_TOKEN"])
	if response.Code != http.StatusOK {
		t.Fatalf("GET /settings with the token = %d %s", response.Code, response.Body)
	}
	for name, value := range secretValues {
		if strings.Contains(response.Body.String(), value) {
			t.Errorf("GET /settings shows the value of %s", name)
		}
	}
	var described msg.ServiceSettings
	if err := json.Unmarshal(response.Body.Bytes(), &described); err != nil {
		t.Fatal(err)
	}
	if described.Service != config.ServiceName || len(described.Settings) != len(config.SettingDefinitions()) {
		t.Errorf("GET /settings describes %q with %d settings, want %q with %d", described.Service, len(described.Settings), config.ServiceName, len(config.SettingDefinitions()))
	}
	// The registry the router was built with is the one it describes: a
	// router that read the environment again would show the default here.
	for _, setting := range described.Settings {
		if setting.Key == config.SettingListenPort && (setting.Value != "9402" || setting.Source != msg.ServiceSettingSourceEnvironment) {
			t.Errorf("listen_port is %q from %s, want 9402 from the environment", setting.Value, setting.Source)
		}
	}
}

// Nothing is Editable, so no write is mounted.
func TestSettingsHasNoWrite(t *testing.T) {
	router := routerForSettingsTests(t)
	token := secretValues["MULTICHAT_TOKEN"]
	if got := serve(router, http.MethodPut, "/settings/listen_port", token).Code; got != http.StatusNotFound {
		t.Errorf("PUT /settings/listen_port = %d, want 404", got)
	}
	if got := serve(router, http.MethodPut, "/settings", token).Code; got != http.StatusMethodNotAllowed {
		t.Errorf("PUT /settings = %d, want 405", got)
	}
}

func TestNewRouterRefusesANilRegistry(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("NewRouter with a nil registry did not panic")
		}
	}()
	NewRouter(nil, nil, config.Config{}, nil)
}

package shortnames

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/kayushkin/llm-bridge/msg"
)

// Model answers one schema-forced prompt and decodes the reply into out. It
// says which model answered, for the run's report.
type Model interface {
	Propose(ctx context.Context, systemPrompt, prompt string, schema map[string]any, out any) (answeredBy string, err error)
}

// BridgeServiceTokenHeader is where llm-bridge-server reads an internal
// service's token. The token itself is the llm_bridge_service_token setting
// (LLMBRIDGE_SERVICE_TOKEN), which the caller hands in.
const BridgeServiceTokenHeader = "X-LLM-Bridge-Service-Token"

// OneShotModel calls llm-bridge-server's POST /oneshot with a model-store
// role, so which model a role means is changed in model-store and never here.
//
// scheduler has a client of its own for this route (internal/oneshot), which
// another module cannot import; llm-bridge exports only the wire types.
type OneShotModel struct {
	BridgeURL string
	// ServiceTokenHeader and ServiceToken are the operator credential
	// llm-bridge-server asks of POST /oneshot.
	ServiceTokenHeader string
	ServiceToken       string
	ModelRole          string
	MaxTokens          int
	HTTPClient         *http.Client
}

// ErrTruncated is a reply cut off at MaxTokens. Its missing names would read
// as names the model declined to give, so the run stops instead.
var ErrTruncated = errors.New("the model's reply hit max_tokens")

func (m OneShotModel) Propose(ctx context.Context, systemPrompt, prompt string, schema map[string]any, out any) (string, error) {
	switch {
	case m.BridgeURL == "":
		return "", errors.New("LLM_BRIDGE_URL is not set")
	case m.ModelRole == "":
		return "", errors.New("SHORT_NAMER_MODEL_ROLE is empty")
	}
	encodedSchema, err := json.Marshal(schema)
	if err != nil {
		return "", fmt.Errorf("encode schema: %w", err)
	}
	body, err := json.Marshal(msg.OneShotRequest{
		Prompt:       prompt,
		SystemPrompt: systemPrompt,
		ModelRole:    m.ModelRole,
		Caller:       CreatedBy,
		Schema:       encodedSchema,
		MaxTokens:    m.MaxTokens,
	})
	if err != nil {
		return "", fmt.Errorf("encode one-shot request: %w", err)
	}
	address := strings.TrimRight(m.BridgeURL, "/") + "/oneshot"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	if m.ServiceToken != "" {
		request.Header.Set(m.ServiceTokenHeader, m.ServiceToken)
	}
	response, err := m.HTTPClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("POST %s: %w", address, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("POST %s: read: %w", address, err)
	}
	if response.StatusCode != http.StatusOK {
		// The bridge names every model it tried and why each failed.
		return "", fmt.Errorf("POST %s (role %s): %d %s", address, m.ModelRole, response.StatusCode, strings.TrimSpace(string(data)))
	}
	var answer msg.OneShotResponse
	if err := json.Unmarshal(data, &answer); err != nil {
		return "", fmt.Errorf("POST %s: decode: %w", address, err)
	}
	if answer.StopReason == "max_tokens" {
		return answer.Model, fmt.Errorf("%w (model %s, max_tokens %d)", ErrTruncated, answer.Model, m.MaxTokens)
	}
	if len(answer.Parsed) == 0 {
		return answer.Model, fmt.Errorf("model %s returned no JSON (stop_reason %q)", answer.Model, answer.StopReason)
	}
	if err := json.Unmarshal(answer.Parsed, out); err != nil {
		return answer.Model, fmt.Errorf("decode the reply of model %s: %w", answer.Model, err)
	}
	return answer.Model, nil
}

// Package principals reaches principal-store, which owns the id of every
// person. multichat links its bridge identities to those ids and asks
// principal-store for a person's name; it never names a person itself.
package principals

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

var (
	// ErrUnknownPrincipal is principal-store's 404 for an id.
	ErrUnknownPrincipal = errors.New("principal-store has no principal with that id")
	// ErrPrincipalStoreFailed wraps every other failure to get an answer.
	ErrPrincipalStoreFailed = errors.New("principal-store failed")
)

// Client calls principal-store at BaseURL.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// Principal is the part of principal-store's row multichat reads.
type Principal struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	DisplayName string `json:"display_name"`
}

// contactKind is principal-store's kind for someone outside this deployment.
const contactKind = "contact"

// Configured reports whether the client has somewhere to call.
func (c Client) Configured() bool { return c.BaseURL != "" }

// CreateContact adds a new contact principal and returns its id.
func (c Client) CreateContact(displayName string) (string, error) {
	if strings.TrimSpace(displayName) == "" {
		return "", errors.New("a new person needs a display name")
	}
	var created Principal
	if err := c.do(http.MethodPost, "/principals", map[string]string{"kind": contactKind, "display_name": displayName}, &created); err != nil {
		return "", err
	}
	if created.ID == "" {
		return "", errors.New("principal-store created a principal and returned no id")
	}
	return created.ID, nil
}

// Get returns one principal, or ErrUnknownPrincipal.
func (c Client) Get(id string) (Principal, error) {
	var principal Principal
	return principal, c.do(http.MethodGet, "/principals/"+url.PathEscape(id), nil, &principal)
}

func (c Client) do(method, path string, body any, into any) error {
	if !c.Configured() {
		return errors.New("PRINCIPAL_STORE_URL is not set")
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, strings.TrimRight(c.BaseURL, "/")+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.HTTPClient.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %s %s: %w", ErrPrincipalStoreFailed, method, path, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("%w: %s %s: read: %w", ErrPrincipalStoreFailed, method, path, err)
	}
	if response.StatusCode == http.StatusNotFound {
		return ErrUnknownPrincipal
	}
	if response.StatusCode >= 300 {
		return fmt.Errorf("%w: %s %s: %d %s", ErrPrincipalStoreFailed, method, path, response.StatusCode, strings.TrimSpace(string(data)))
	}
	if err := json.Unmarshal(data, into); err != nil {
		return fmt.Errorf("%w: %s %s: decode: %w", ErrPrincipalStoreFailed, method, path, err)
	}
	return nil
}

package shortnames

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// The short-name-store contract is ~/repos/short-name-store/CONTRACT.md. This
// file reads and writes only the parts the naming pass needs.

// EntityTypeMultichatRoom is the entity_type of a name that points at a
// Matrix room, whose id is the entity_id verbatim.
const EntityTypeMultichatRoom = "multichat_room"

// NamespaceMessages is the namespace of dash's /messages page.
const NamespaceMessages = "messages"

// CreatedBy is the created_by the pass writes on every name it chooses.
const CreatedBy = "multichat-short-namer"

var (
	// ErrPathTaken is short-name-store's 409: another record holds the path,
	// or its first segment is reserved.
	ErrPathTaken = errors.New("short-name-store refused the path")
	// ErrNamespaceMissing is short-name-store's 404 for the namespace.
	ErrNamespaceMissing = errors.New("short-name-store has no such namespace")
)

// Namespace is the part of short-name-store's namespace the pass reads.
type Namespace struct {
	Name             string   `json:"name"`
	ReservedSegments []string `json:"reserved_segments"`
}

// ShortName is one short-name-store name.
type ShortName struct {
	ID           string `json:"id"`
	Namespace    string `json:"namespace"`
	Path         string `json:"path"`
	EntityType   string `json:"entity_type"`
	EntityID     string `json:"entity_id"`
	CreatedBy    string `json:"created_by"`
	Reason       string `json:"reason"`
	CreatedAt    int64  `json:"created_at"`
	SupersededAt int64  `json:"superseded_at"`
	SupersededBy string `json:"superseded_by"`
}

// Current reports whether no newer name has replaced this one.
func (name ShortName) Current() bool { return name.SupersededAt == 0 }

// NewShortName is the body of POST /short-names.
type NewShortName struct {
	Namespace  string `json:"namespace"`
	Path       string `json:"path"`
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	CreatedBy  string `json:"created_by"`
	Reason     string `json:"reason"`
}

// Store is what the pass asks of short-name-store.
type Store interface {
	Namespace(ctx context.Context, name string) (Namespace, error)
	// AllShortNames lists every name in the namespace, superseded ones and
	// every entity type included, reading every page.
	AllShortNames(ctx context.Context, namespace string) ([]ShortName, error)
	// CreateShortName writes one name. A 409 is ErrPathTaken.
	CreateShortName(ctx context.Context, name NewShortName) (ShortName, error)
}

// StoreClient calls short-name-store at BaseURL.
type StoreClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

func (c StoreClient) Namespace(ctx context.Context, name string) (Namespace, error) {
	var namespace Namespace
	status, err := c.do(ctx, http.MethodGet, "/namespaces/"+url.PathEscape(name), nil, &namespace)
	if status == http.StatusNotFound {
		return Namespace{}, fmt.Errorf("%w: %s: %w", ErrNamespaceMissing, name, err)
	}
	return namespace, err
}

// storePageSize is the contract's largest limit.
const storePageSize = 1000

func (c StoreClient) AllShortNames(ctx context.Context, namespace string) ([]ShortName, error) {
	var all []ShortName
	for offset := 0; ; offset += storePageSize {
		var page []ShortName
		query := url.Values{"namespace": {namespace}, "include_superseded": {"true"},
			"limit": {fmt.Sprint(storePageSize)}, "offset": {fmt.Sprint(offset)}}
		if _, err := c.do(ctx, http.MethodGet, "/short-names?"+query.Encode(), nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < storePageSize {
			return all, nil
		}
	}
}

func (c StoreClient) CreateShortName(ctx context.Context, name NewShortName) (ShortName, error) {
	var created ShortName
	status, err := c.do(ctx, http.MethodPost, "/short-names", name, &created)
	if status == http.StatusConflict {
		return ShortName{}, fmt.Errorf("%w: %w", ErrPathTaken, err)
	}
	return created, err
}

// do calls the store and decodes a 2xx answer into into. Any other answer is
// an error carrying the store's own message, with the status beside it.
func (c StoreClient) do(ctx context.Context, method, path string, body, into any) (int, error) {
	if c.BaseURL == "" {
		return 0, errors.New("SHORT_NAME_STORE_URL is not set")
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, reader)
	if err != nil {
		return 0, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.HTTPClient.Do(request)
	if err != nil {
		return 0, fmt.Errorf("short-name-store %s %s: %w", method, path, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return response.StatusCode, fmt.Errorf("short-name-store %s %s: read: %w", method, path, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := strings.TrimSpace(string(data))
		var answer struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &answer) == nil && answer.Error != "" {
			message = answer.Error
		}
		return response.StatusCode, fmt.Errorf("short-name-store %s %s: %d %s", method, path, response.StatusCode, message)
	}
	if err := json.Unmarshal(data, into); err != nil {
		return response.StatusCode, fmt.Errorf("short-name-store %s %s: decode: %w", method, path, err)
	}
	return response.StatusCode, nil
}

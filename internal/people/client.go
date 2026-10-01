// Package people reaches people-store, which owns every person and every
// handle that reaches one (~/repos/people-store/CONTRACT.md). multichat's
// bridge accounts are people-store identities of kind matrix_user; multichat
// links them through this client and asks people-store for a person's name.
// It never names or stores a person itself.
package people

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

// IdentityKindMatrixUser is people-store's kind for one bridged chat account.
const IdentityKindMatrixUser = "matrix_user"

var (
	// ErrNotFound is people-store's 404.
	ErrNotFound = errors.New("people-store has no such record")
	// ErrInvalid is people-store's 400: it refused what we sent.
	ErrInvalid = errors.New("people-store refused the request")
	// ErrConflict is people-store's 409: the handle belongs to someone else, the
	// pair was refused by the operator, or the person is deleted.
	ErrConflict = errors.New("people-store refused the link")
	// ErrPeopleStoreFailed wraps every other failure to get an answer.
	ErrPeopleStoreFailed = errors.New("people-store failed")
)

// Client calls people-store at BaseURL.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// Person is the part of people-store's person multichat reads.
type Person struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	DeletedAt   int64  `json:"deleted_at"`
}

// Identity is one people-store identity.
type Identity struct {
	ID       string `json:"id"`
	PersonID string `json:"person_id"`
	Kind     string `json:"kind"`
	Value    string `json:"value"`
	Label    string `json:"label"`
	LinkedBy string `json:"linked_by"`
	Reason   string `json:"reason"`
	LinkedAt int64  `json:"linked_at"`
}

// Link is a handle and who links it, and why: one entry of POST /persons
// identities or the body of POST /persons/{id}/identities.
type Link struct {
	Kind     string `json:"kind"`
	Value    string `json:"value"`
	Label    string `json:"label"`
	LinkedBy string `json:"linked_by"`
	Reason   string `json:"reason"`
	Move     bool   `json:"move,omitempty"`
}

// MatrixUserLink is the link of one bridge account, labelled with its platform.
func MatrixUserLink(userID, platform, linkedBy, reason string) Link {
	return Link{Kind: IdentityKindMatrixUser, Value: userID, Label: platform, LinkedBy: linkedBy, Reason: reason}
}

// Configured reports whether the client has somewhere to call.
func (c Client) Configured() bool { return c.BaseURL != "" }

// LinkSources returns people-store's link sources (GET /link-sources).
func (c Client) LinkSources() ([]string, error) {
	var sources []struct {
		Source string `json:"source"`
	}
	if err := c.do(http.MethodGet, "/link-sources", nil, &sources); err != nil {
		return nil, err
	}
	names := make([]string, len(sources))
	for i, source := range sources {
		names[i] = source.Source
	}
	return names, nil
}

// OwnerOf returns the identity of a bridge account, or nil when no person has it.
func (c Client) OwnerOf(userID string) (*Identity, error) {
	var resolution struct {
		Identity Identity `json:"identity"`
	}
	err := c.do(http.MethodGet, "/identities/resolve?kind="+IdentityKindMatrixUser+"&value="+url.QueryEscape(userID), nil, &resolution)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &resolution.Identity, nil
}

// MatrixUserIdentities returns every bridge account attached to a person,
// keyed by user id, reading every page.
func (c Client) MatrixUserIdentities() (map[string]Identity, error) {
	const pageSize = 1000
	identities := map[string]Identity{}
	for offset := 0; ; offset += pageSize {
		var page []Identity
		if err := c.do(http.MethodGet, fmt.Sprintf("/identities?kind=%s&limit=%d&offset=%d", IdentityKindMatrixUser, pageSize, offset), nil, &page); err != nil {
			return nil, err
		}
		for _, identity := range page {
			identities[identity.Value] = identity
		}
		if len(page) < pageSize {
			return identities, nil
		}
	}
}

// Person returns one person, deleted or not, or ErrNotFound.
func (c Client) Person(id string) (Person, error) {
	var person Person
	return person, c.do(http.MethodGet, "/persons/"+url.PathEscape(id), nil, &person)
}

// Persons returns every person, deleted ones included, keyed by id.
func (c Client) Persons() (map[string]Person, error) {
	const pageSize = 1000
	persons := map[string]Person{}
	for offset := 0; ; offset += pageSize {
		var page []Person
		if err := c.do(http.MethodGet, fmt.Sprintf("/persons?include_deleted=true&limit=%d&offset=%d", pageSize, offset), nil, &page); err != nil {
			return nil, err
		}
		for _, person := range page {
			persons[person.ID] = person
		}
		if len(page) < pageSize {
			return persons, nil
		}
	}
}

// CreatePerson makes a person named displayName with these handles, all or
// nothing, and returns the new id.
func (c Client) CreatePerson(displayName string, links []Link) (string, error) {
	if strings.TrimSpace(displayName) == "" {
		return "", errors.New("a new person needs a display name")
	}
	var created Person
	if err := c.do(http.MethodPost, "/persons", map[string]any{"display_name": displayName, "identities": links}, &created); err != nil {
		return "", err
	}
	if created.ID == "" {
		return "", errors.New("people-store created a person and returned no id")
	}
	return created.ID, nil
}

// AddIdentity attaches a handle to a person.
func (c Client) AddIdentity(personID string, link Link) (Identity, error) {
	var identity Identity
	return identity, c.do(http.MethodPost, "/persons/"+url.PathEscape(personID)+"/identities", link, &identity)
}

// RemoveIdentity detaches a handle. people-store records that it is not that
// person, so no automatic source puts it back.
func (c Client) RemoveIdentity(identityID string) error {
	return c.do(http.MethodDelete, "/identities/"+url.PathEscape(identityID), nil, nil)
}

func (c Client) do(method, path string, body any, into any) error {
	if !c.Configured() {
		return errors.New("PEOPLE_STORE_URL is not set")
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
		return fmt.Errorf("%w: %s %s: %w", ErrPeopleStoreFailed, method, path, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("%w: %s %s: read: %w", ErrPeopleStoreFailed, method, path, err)
	}
	message := strings.TrimSpace(string(data))
	var answer struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &answer) == nil && answer.Error != "" {
		message = answer.Error
	}
	switch {
	case response.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrNotFound, message)
	case response.StatusCode == http.StatusBadRequest:
		return fmt.Errorf("%w: %s", ErrInvalid, message)
	case response.StatusCode == http.StatusConflict:
		return fmt.Errorf("%w: %s", ErrConflict, message)
	case response.StatusCode >= 300:
		return fmt.Errorf("%w: %s %s: %d %s", ErrPeopleStoreFailed, method, path, response.StatusCode, message)
	}
	if into == nil {
		return nil
	}
	if err := json.Unmarshal(data, into); err != nil {
		return fmt.Errorf("%w: %s %s: decode: %w", ErrPeopleStoreFailed, method, path, err)
	}
	return nil
}

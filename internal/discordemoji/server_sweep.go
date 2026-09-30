package discordemoji

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kayushkin/multichat/internal/db"
)

// ServerSweeper lists the custom emoji of every Discord server the bridge's
// account is in and the bot is not (the bridged servers are the Refresher's),
// with the account's own login. The operator chose this on 2026-09-30 knowing
// Discord's app never makes these calls, so it goes slowly: one call for the
// server list, then one server every Interval, and only servers not listed
// within StaleAfter. A stopped sweep loses nothing: the next one lists the
// servers still due, oldest first.
type ServerSweeper struct {
	HTTPClient        *http.Client
	DiscordAPIBaseURL string
	Interval          time.Duration
	StaleAfter        time.Duration
	Now               func() time.Time
	Sleep             func(time.Duration)

	mu     sync.Mutex
	status db.DiscordServerSweepStatus
}

// Status is how far the current or last sweep got.
func (s *ServerSweeper) Status() db.DiscordServerSweepStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// Start begins a sweep in the background unless one is running, and reports
// whether it began one.
func (s *ServerSweeper) Start(database *sql.DB, bridgeDatabasePath string) bool {
	s.mu.Lock()
	if s.status.Running {
		s.mu.Unlock()
		return false
	}
	started := s.Now()
	s.status = db.DiscordServerSweepStatus{Running: true, StartedAt: &started}
	s.mu.Unlock()
	go func() {
		err := s.sweep(database, bridgeDatabasePath)
		s.mu.Lock()
		defer s.mu.Unlock()
		finished := s.Now()
		s.status.Running = false
		s.status.FinishedAt = &finished
		if err != nil {
			s.status.Error = err.Error()
		}
	}()
	return true
}

func (s *ServerSweeper) update(change func(*db.DiscordServerSweepStatus)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	change(&s.status)
}

// The pace the operator chose: one server every 30 seconds, each at most
// once a week.
const (
	ServerSweepInterval   = 30 * time.Second
	ServerSweepStaleAfter = 7 * 24 * time.Hour
)

// accountServer is a server as GET /users/@me/guilds lists it.
type accountServer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Discord lists at most this many servers per page.
const serversPerPage = 200

func (s *ServerSweeper) sweep(database *sql.DB, bridgeDatabasePath string) error {
	bridge, err := sql.Open("sqlite3", "file:"+bridgeDatabasePath+"?mode=ro")
	if err != nil {
		return fmt.Errorf("open DISCORD_DB %s: %w", bridgeDatabasePath, err)
	}
	account, err := readDiscordLogin(bridge)
	if err != nil {
		bridge.Close()
		return err
	}
	bridged, err := BridgedServers(bridge)
	bridge.Close()
	if err != nil {
		return err
	}
	isBridged := map[string]bool{}
	for _, server := range bridged {
		isBridged[server.DiscordServerID] = true
	}

	var servers []accountServer
	for after := ""; ; {
		path := fmt.Sprintf("/users/@me/guilds?limit=%d", serversPerPage)
		if after != "" {
			path += "&after=" + after
		}
		var page []accountServer
		if err := s.get(account.token, path, &page); err != nil {
			return err
		}
		servers = append(servers, page...)
		if len(page) < serversPerPage {
			break
		}
		after = page[len(page)-1].ID
	}
	inAccount := map[string]bool{}
	for _, server := range servers {
		inAccount[server.ID] = true
	}

	refreshes, err := db.DiscordEmojiRefreshes(database)
	if err != nil {
		return err
	}
	lastListed := map[string]time.Time{}
	for _, refresh := range refreshes {
		lastListed[refresh.DiscordServerID] = refresh.RefreshedAt
		// A server the account has left, and that is not bridged, goes.
		if !inAccount[refresh.DiscordServerID] && !isBridged[refresh.DiscordServerID] {
			if err := db.DeleteDiscordServerEmoji(database, refresh.DiscordServerID); err != nil {
				return err
			}
		}
	}

	now := s.Now()
	var due []accountServer
	for _, server := range servers {
		listed, ok := lastListed[server.ID]
		if isBridged[server.ID] || (ok && now.Sub(listed) < s.StaleAfter) {
			continue
		}
		due = append(due, server)
	}
	// Never listed first, then the longest ago.
	sort.SliceStable(due, func(i, j int) bool { return lastListed[due[i].ID].Before(lastListed[due[j].ID]) })
	s.update(func(status *db.DiscordServerSweepStatus) {
		status.ServersInAccount = len(servers)
		status.ServersToList = len(due)
	})

	var failed []string
	listed := 0
	for _, server := range due {
		s.Sleep(s.Interval)
		var emoji []discordEmoji
		err := s.get(account.token, "/guilds/"+server.ID+"/emojis", &emoji)
		var refused *discordRefusal
		if errors.As(err, &refused) && refused.status == 401 {
			return fmt.Errorf("Discord refused the account's login after %d of %d servers: %w", listed, len(due), err)
		}
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s (%s): %v", server.Name, server.ID, err))
			continue
		}
		var available []db.DiscordCustomEmoji
		for _, e := range emoji {
			if e.Available {
				available = append(available, db.DiscordCustomEmoji{DiscordEmojiID: e.ID, Name: e.Name, Animated: e.Animated})
			}
		}
		if err := db.ReplaceDiscordServerEmoji(database, server.ID, server.Name, available, s.Now()); err != nil {
			return err
		}
		listed++
		s.update(func(status *db.DiscordServerSweepStatus) { status.ServersListed = listed })
	}
	if len(failed) > 0 {
		return fmt.Errorf("%d servers could not be listed: %s", len(failed), strings.Join(failed, "; "))
	}
	return nil
}

// get makes one call with the account's login; on a 429 it waits as long as
// Discord asks and tries once more.
func (s *ServerSweeper) get(login, path string, out any) error {
	err := discordGet(s.HTTPClient, s.DiscordAPIBaseURL, login, path, out)
	var refused *discordRefusal
	if errors.As(err, &refused) && refused.status == 429 {
		s.Sleep(refused.retryAfter + time.Second)
		err = discordGet(s.HTTPClient, s.DiscordAPIBaseURL, login, path, out)
	}
	return err
}

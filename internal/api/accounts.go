package api

import (
	"net/http"
	"sort"

	"github.com/kayushkin/multichat/internal/botaccounts"
	"github.com/kayushkin/multichat/internal/matrix"
)

// BotAccounts is what the router needs of the bot accounts:
// botaccounts.Manager in production.
type BotAccounts interface {
	States() []botaccounts.AccountState
	Send(accountID, userID, channelID, body string) (botaccounts.SendResult, error)
}

// SetBotAccounts gives the router the bot accounts to send as.
func (r *Router) SetBotAccounts(bots BotAccounts) { r.bots = bots }

// ownAccount is one platform the operator's own account reaches through
// Matrix, with the puppet ids that are the operator there.
type ownAccount struct {
	Kind          string   `json:"kind"`
	Platform      string   `json:"platform"`
	MatrixUserIDs []string `json:"matrix_user_ids"`
}

// accountsHandler — GET /accounts: every account multichat can send as. The
// operator's own, one per platform it knows our puppet on, sent through
// Matrix and its bridges (a send names no account_id); and our bot accounts
// from message-store, each with its connection (a send names its account_id).
// A bridge login's state is not here: scripts/bridge-status.sh asks the
// bridges.
func (r *Router) accountsHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	byPlatform := map[string][]string{}
	for userID := range matrix.SelfUserIDs {
		if platform := matrix.PlatformOfUserID(userID); platform != "" {
			byPlatform[platform] = append(byPlatform[platform], userID)
		}
	}
	own := []ownAccount{}
	for _, platform := range matrix.KnownPlatforms {
		if ids := byPlatform[platform]; len(ids) > 0 {
			sort.Strings(ids)
			own = append(own, ownAccount{Kind: "user", Platform: platform, MatrixUserIDs: ids})
		}
	}
	bots := []botaccounts.AccountState{}
	if r.bots != nil {
		bots = r.bots.States()
	}
	r.writeJSON(w, map[string]any{"own_accounts": own, "bot_accounts": bots, "bot_accounts_configured": r.bots != nil})
}

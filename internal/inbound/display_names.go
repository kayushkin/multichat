package inbound

import "time"

// displayNameLifetime is how long the worker keeps a sender's display name
// before asking Synapse again. Asking once per message got the worker
// rate-limited and its log days behind on 2026-11-06; a name changed within
// this window shows on the next messages once it passes.
const displayNameLifetime = 10 * time.Minute

type rememberedDisplayName struct {
	name      string
	fetchedAt time.Time
}

// displayNameOf returns a sender's display name, from memory while it is
// younger than displayNameLifetime and from Synapse otherwise.
func (w *SyncWorker) displayNameOf(userID string) (string, error) {
	now := time.Now()
	if remembered, ok := w.displayNames[userID]; ok && now.Sub(remembered.fetchedAt) < displayNameLifetime {
		return remembered.name, nil
	}
	name, err := w.Matrix.DisplayNameOf(userID)
	if err != nil {
		return "", err
	}
	if w.displayNames == nil {
		w.displayNames = map[string]rememberedDisplayName{}
	}
	w.displayNames[userID] = rememberedDisplayName{name: name, fetchedAt: now}
	return name, nil
}

package inbound

import (
	"errors"
	"log"
	"time"

	"github.com/kayushkin/multichat/internal/matrix"
)

// A bridge creates a Matrix room for each chat and invites us to it, but
// nothing on the bridge side accepts the invite, and a room we have not
// joined is invisible: it is missing from GET /conversations and none of its
// messages reach the log or the bus. So the worker accepts every invite a
// bridge sends — at startup for invites that arrived while nothing was
// accepting them, and after each sync for new ones.

// isBridgeInviter reports whether an invite came from a bridge: its bot or
// one of its puppets. An invite from a plain Matrix user is left alone.
func isBridgeInviter(userID string) bool {
	return matrix.IsBridgeBot(userID) || matrix.PlatformOfUserID(userID) != ""
}

// acceptPendingInvites joins every room a bridge has invited us to and we
// have not joined yet.
func (w *SyncWorker) acceptPendingInvites() error {
	invites, err := w.Matrix.PendingInvites()
	if err != nil {
		return err
	}
	if len(invites) > 0 {
		log.Printf("inbound sync: %d pending invites at startup", len(invites))
	}
	w.acceptInvites(invites)
	return nil
}

// acceptInvites joins each room in invites whose inviter is a bridge, waiting
// out Synapse's join rate limit. A join that fails for another reason is
// logged and skipped, so one broken room cannot stop the inbox; the invite
// stays pending and the next startup tries it again.
func (w *SyncWorker) acceptInvites(invites map[string]string) {
	for roomID, inviter := range invites {
		if !isBridgeInviter(inviter) {
			log.Printf("inbound sync: not accepting invite to %s from %s: not a bridge", roomID, inviter)
			continue
		}
		joinedAt := time.Now()
		err := w.Matrix.JoinRoom(roomID)
		var limited *matrix.RateLimitedError
		for errors.As(err, &limited) {
			time.Sleep(limited.RetryAfter)
			joinedAt = time.Now()
			err = w.Matrix.JoinRoom(roomID)
		}
		if err != nil {
			log.Printf("inbound sync: WARNING could not accept invite to %s from %s: %v", roomID, inviter, err)
			continue
		}
		w.joinedAt[roomID] = joinedAt
		log.Printf("inbound sync: accepted invite to %s from %s", roomID, inviter)
		w.queueBridgeBotCheck(roomID)
	}
}

// sentBeforeWeJoined reports whether an event predates our joining its room
// in this process. The first sync after a join carries the room's recent
// history; those events are logged, but publishing them would start rules
// on old messages as if they were new.
func (w *SyncWorker) sentBeforeWeJoined(roomID string, event matrix.TimelineEvent) bool {
	joinedAt, ok := w.joinedAt[roomID]
	return ok && time.UnixMilli(event.OriginServerTS).Before(joinedAt)
}

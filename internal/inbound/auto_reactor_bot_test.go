package inbound

import (
	"testing"

	"github.com/kayushkin/bus/messages"
)

// A message to one of our bots has no Matrix room to react in; the
// auto-reactor must leave it before it reads a rule.
func TestAutoReactorLeavesMessagesToOurBotsAlone(t *testing.T) {
	reactor := &AutoReactor{} // no database: reading a rule would panic
	if err := reactor.React(messages.MessagingInbound{EventID: "discord:1", ReceivingAccountID: "message_account_000002"}); err != nil {
		t.Fatal(err)
	}
}

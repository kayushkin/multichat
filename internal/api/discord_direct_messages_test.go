package api

import (
	"reflect"
	"testing"

	"github.com/kayushkin/multichat/internal/matrix"
)

func TestBridgeCommandAndAnswerAreTheCommandAndTheBotsLaterMessages(t *testing.T) {
	messages := []matrix.Message{
		{EventID: "$hello", Sender: "@discordbot:x"},
		{EventID: "$command", Sender: "@admin:x"},
		{EventID: "$bridged", Sender: "@discordbot:x"},
		{EventID: "$someone", Sender: "@discord_1:x"},
	}
	if got, want := bridgeCommandAndAnswer(messages, "$command", "@discordbot:x"), []string{"$command", "$bridged"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("bridgeCommandAndAnswer = %v, want %v", got, want)
	}
	// A command not on the page is still taken back.
	if got, want := bridgeCommandAndAnswer(messages[:1], "$command", "@discordbot:x"), []string{"$command"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("bridgeCommandAndAnswer = %v, want %v", got, want)
	}
}

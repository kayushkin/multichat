package db

import "testing"

func TestAMessageRefReadsBackOnlyAsMessageRefWroteIt(t *testing.T) {
	if got := MessageRef(26563); got != "message_026563" {
		t.Errorf("MessageRef(26563) = %q", got)
	}
	if id, ok := ParseMessageRef("message_026563"); !ok || id != 26563 {
		t.Errorf("ParseMessageRef(message_026563) = %d, %v", id, ok)
	}
	if id, ok := ParseMessageRef("message_1234567"); !ok || id != 1234567 {
		t.Errorf("seven digits = %d, %v", id, ok)
	}
	for _, refused := range []string{"message_26563", "message_0026563", "message_000000", "message_-00001", "entry_026563", "026563", "message_02656x"} {
		if _, ok := ParseMessageRef(refused); ok {
			t.Errorf("ParseMessageRef(%q) accepted it", refused)
		}
	}
}

func TestGetLoggedMessageReadsOneRowWithItsRef(t *testing.T) {
	database, err := Initialize(t.TempDir() + "/multichat.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := LogMessage(database, LoggedMessage{EventID: "$one", RoomID: "!room:x", Platform: "discord",
		SenderUserID: "@discord_1:x", Body: "hello"}); err != nil {
		t.Fatal(err)
	}
	messages, err := ListLoggedMessages(database, MessageLogFilter{Limit: 10})
	if err != nil || len(messages) != 1 {
		t.Fatalf("list = %+v, %v", messages, err)
	}
	got, err := GetLoggedMessage(database, messages[0].ID)
	if err != nil || got.Body != "hello" || got.Ref != MessageRef(messages[0].ID) {
		t.Errorf("get = %+v, %v", got, err)
	}
	if _, err := GetLoggedMessage(database, messages[0].ID+1); err == nil {
		t.Error("a missing row should be an error")
	}
}

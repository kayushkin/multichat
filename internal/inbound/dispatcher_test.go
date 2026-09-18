package inbound

import (
	"bytes"
	"testing"

	"github.com/kayushkin/bus/messages"
	"github.com/kayushkin/multichat/internal/db"
)

func TestRuleMatchesEveryFilterIsOptional(t *testing.T) {
	inbound := messages.MessagingInbound{Platform: "whatsapp", SenderUserID: "@whatsapp_1:x", Body: "urgent: call me", IsDirectMessage: true}
	cases := []struct {
		name string
		rule db.InboundRule
		want bool
	}{
		{"no filters", db.InboundRule{}, true},
		{"platform ok", db.InboundRule{Platform: "whatsapp"}, true},
		{"platform wrong", db.InboundRule{Platform: "telegram"}, false},
		{"sender ok", db.InboundRule{SenderUserID: "@whatsapp_1:x"}, true},
		{"sender wrong", db.InboundRule{SenderUserID: "@whatsapp_2:x"}, false},
		{"body ok", db.InboundRule{BodyPattern: `(?i)^urgent`}, true},
		{"body wrong", db.InboundRule{BodyPattern: `^hello`}, false},
		{"dm only, is dm", db.InboundRule{DirectMessagesOnly: true}, true},
	}
	for _, c := range cases {
		got, err := RuleMatches(c.rule, inbound)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
	group := inbound
	group.IsDirectMessage = false
	if got, _ := RuleMatches(db.InboundRule{DirectMessagesOnly: true}, group); got {
		t.Error("dm-only rule matched a group message")
	}
}

func TestRuleMatchesBadPatternIsAnError(t *testing.T) {
	if _, err := RuleMatches(db.InboundRule{BodyPattern: `(`}, messages.MessagingInbound{}); err == nil {
		t.Fatal("expected an error for an unparseable pattern")
	}
}

func TestPromptTemplateRendersFieldsAndRejectsUnknownOnes(t *testing.T) {
	tmpl, err := ParsePromptTemplate("{{.SenderDisplayName}} on {{.Platform}} said: {{.Body}}")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, PromptFields{SenderDisplayName: "Alex", Platform: "whatsapp", Body: "hi"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "Alex on whatsapp said: hi" {
		t.Errorf("got %q", out.String())
	}
	if _, err := ParsePromptTemplate("{{.NoSuchField}}"); err == nil {
		// Parse cannot know the field set; execution is where it fails.
		tmpl, _ := ParsePromptTemplate("{{.NoSuchField}}")
		if err := tmpl.Execute(&bytes.Buffer{}, PromptFields{}); err == nil {
			t.Error("expected execution to fail on an unknown field")
		}
	}
}

func TestCheckPromptTemplateRefusesAFieldTheDispatcherDoesNotRenderWith(t *testing.T) {
	if err := CheckPromptTemplate("{{.SenderDisplayName}}: {{.Body}} at {{.Timestamp}}"); err != nil {
		t.Errorf("a template over real fields was refused: %v", err)
	}
	if err := CheckPromptTemplate("{{.NoSuchField}}"); err == nil {
		t.Error("a template naming an unknown field was accepted; it would fail only when a message arrived")
	}
	if err := CheckPromptTemplate("{{.Body"); err == nil {
		t.Error("a template that does not parse was accepted")
	}
}

func TestEveryServedPromptFieldRendersInATemplate(t *testing.T) {
	list := PromptFieldList()
	if len(list) == 0 {
		t.Fatal("no prompt fields served")
	}
	for _, field := range list {
		if err := CheckPromptTemplate("{{." + field.Name + "}}"); err != nil {
			t.Errorf("served field %s does not render: %v", field.Name, err)
		}
		if field.Type == "" {
			t.Errorf("served field %s has no type", field.Name)
		}
	}
}

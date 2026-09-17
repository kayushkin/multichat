package inbound

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"text/template"
	"time"

	"github.com/kayushkin/bus"
	"github.com/kayushkin/bus/messages"
	"github.com/kayushkin/llm-bridge/msg"
	"github.com/kayushkin/multichat/internal/db"
)

// DurableConsumerName is this process's JetStream durable on
// messaging.inbound. Another service wanting the same stream picks its own.
const DurableConsumerName = "multichat-inbound-dispatcher"

// PromptFields is what a rule's prompt_template can reference.
type PromptFields struct {
	Body              string
	SenderUserID      string
	SenderDisplayName string
	Platform          string
	RoomID            string
	RoomName          string
	EventID           string
	MessageType       string
	IsDirectMessage   bool
	Timestamp         time.Time
}

// ParsePromptTemplate is the one parser for rule templates, shared by the
// write-time validation in the API and the dispatcher, so a template the API
// accepted is one the dispatcher can render.
func ParsePromptTemplate(text string) (*template.Template, error) {
	return template.New("prompt").Option("missingkey=error").Parse(text)
}

// Dispatcher applies inbound_rules to messaging.inbound.
type Dispatcher struct {
	Database     *sql.DB
	Bus          *bus.Client
	LLMBridgeURL string
	HTTPClient   *http.Client
}

// Run subscribes and blocks forever.
func (d *Dispatcher) Run() {
	err := d.Bus.JetSubscribe(messages.MessagingInboundSubject, DurableConsumerName, func(subject string, data []byte) {
		var inbound messages.MessagingInbound
		if err := json.Unmarshal(data, &inbound); err != nil {
			log.Printf("inbound dispatch: undecodable message on %s: %v", subject, err)
			return
		}
		if err := d.dispatch(inbound); err != nil {
			log.Printf("inbound dispatch: %s: %v", inbound.EventID, err)
		}
	})
	if err != nil {
		log.Fatalf("inbound dispatch: subscribe %s: %v", messages.MessagingInboundSubject, err)
	}
	log.Printf("inbound dispatch: consuming %s as %s", messages.MessagingInboundSubject, DurableConsumerName)
	select {}
}

func (d *Dispatcher) dispatch(inbound messages.MessagingInbound) error {
	rules, err := db.ListInboundRules(d.Database, true)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		matched, err := RuleMatches(rule, inbound)
		if err != nil {
			return fmt.Errorf("rule %d: %w", rule.ID, err)
		}
		if !matched {
			continue
		}
		if err := d.fire(rule, inbound); err != nil {
			return fmt.Errorf("rule %d %q: %w", rule.ID, rule.Name, err)
		}
	}
	return nil
}

// RuleMatches reports whether one rule applies to one message.
func RuleMatches(rule db.InboundRule, inbound messages.MessagingInbound) (bool, error) {
	if rule.Platform != "" && rule.Platform != inbound.Platform {
		return false, nil
	}
	if rule.SenderUserID != "" && rule.SenderUserID != inbound.SenderUserID {
		return false, nil
	}
	if rule.DirectMessagesOnly && !inbound.IsDirectMessage {
		return false, nil
	}
	if rule.BodyPattern != "" {
		pattern, err := regexp.Compile(rule.BodyPattern)
		if err != nil {
			return false, fmt.Errorf("body_pattern: %w", err)
		}
		if !pattern.MatchString(inbound.Body) {
			return false, nil
		}
	}
	return true, nil
}

func (d *Dispatcher) fire(rule db.InboundRule, inbound messages.MessagingInbound) error {
	busSessionID := fmt.Sprintf("multichat-rule%d-%s", rule.ID, inbound.EventID)
	dispatchID, err := db.ClaimInboundDispatch(d.Database, db.InboundDispatch{
		RuleID: rule.ID, EventID: inbound.EventID, RoomID: inbound.RoomID,
		SenderUserID: inbound.SenderUserID, BusSessionID: busSessionID,
	})
	if err == db.ErrAlreadyDispatched {
		log.Printf("inbound dispatch: rule %d already acted on %s (redelivery), skipping", rule.ID, inbound.EventID)
		return nil
	}
	if err != nil {
		return err
	}
	if err := d.startSession(rule, inbound, busSessionID); err != nil {
		if recordErr := db.RecordInboundDispatchError(d.Database, dispatchID, err.Error()); recordErr != nil {
			log.Printf("inbound dispatch: %v", recordErr)
		}
		return err
	}
	log.Printf("inbound dispatch: rule %d %q started %s on instance %s for %s", rule.ID, rule.Name, busSessionID, rule.InstanceID, inbound.EventID)
	return nil
}

func (d *Dispatcher) startSession(rule db.InboundRule, inbound messages.MessagingInbound, busSessionID string) error {
	tmpl, err := ParsePromptTemplate(rule.PromptTemplate)
	if err != nil {
		return fmt.Errorf("prompt_template: %w", err)
	}
	var prompt bytes.Buffer
	if err := tmpl.Execute(&prompt, PromptFields{
		Body: inbound.Body, SenderUserID: inbound.SenderUserID, SenderDisplayName: inbound.SenderDisplayName,
		Platform: inbound.Platform, RoomID: inbound.RoomID, RoomName: inbound.RoomName, EventID: inbound.EventID,
		MessageType: inbound.MessageType, IsDirectMessage: inbound.IsDirectMessage, Timestamp: inbound.Timestamp,
	}); err != nil {
		return fmt.Errorf("render prompt_template: %w", err)
	}
	instance, err := LookupInstance(d.HTTPClient, d.LLMBridgeURL, rule.InstanceID)
	if err != nil {
		return err
	}
	if !instance.Enabled {
		return fmt.Errorf("instance %s is disabled", rule.InstanceID)
	}
	in := messages.BridgeInbound{
		BusSessionID: busSessionID,
		Agent:        rule.AgentID,
		Orchestrator: string(instance.HarnessType),
		Channel:      "multichat",
		Timestamp:    time.Now().UTC(),
		Create: &msg.CreateSessionRequest{
			Harness:    instance.HarnessType,
			InstanceID: instance.ID,
			AgentID:    rule.AgentID,
			Purpose:    "inbound-message",
			Origin:     "multichat",
			Type:       msg.SessionTypeAutonomous,
		},
		Send: &msg.SendMessageRequest{Message: prompt.String()},
	}
	if err := d.Bus.Publish(messages.BridgeInboundSubject, in); err != nil {
		return fmt.Errorf("publish %s: %w", messages.BridgeInboundSubject, err)
	}
	return nil
}

// InstanceLookupError distinguishes "llm-bridge-server says no" (the id is
// wrong: 400 to a caller) from "llm-bridge-server did not answer" (502).
type InstanceLookupError struct {
	Unknown bool // true when the server answered 404
	Err     error
}

func (e *InstanceLookupError) Error() string { return e.Err.Error() }
func (e *InstanceLookupError) Unwrap() error { return e.Err }

// LookupInstance asks llm-bridge-server for one instance by id.
// BridgeServiceTokenHeader is where llm-bridge-server reads an internal
// service's token, and BridgeServiceTokenEnvironmentVariable is where this
// service's unit carries it.
const (
	BridgeServiceTokenHeader              = "X-LLM-Bridge-Service-Token"
	BridgeServiceTokenEnvironmentVariable = "LLMBRIDGE_SERVICE_TOKEN"
)

func LookupInstance(client *http.Client, llmBridgeURL, instanceID string) (msg.Instance, error) {
	if llmBridgeURL == "" {
		return msg.Instance{}, &InstanceLookupError{Err: fmt.Errorf("LLM_BRIDGE_URL is not set; cannot check instance %q", instanceID)}
	}
	request, err := http.NewRequest(http.MethodGet, llmBridgeURL+"/instances/"+instanceID, nil)
	if err != nil {
		return msg.Instance{}, &InstanceLookupError{Err: err}
	}
	// llm-bridge-server gates every route: without this token the check is a
	// 401, which reads as "that instance does not exist" to whoever is saving
	// the rule.
	if token := os.Getenv(BridgeServiceTokenEnvironmentVariable); token != "" {
		request.Header.Set(BridgeServiceTokenHeader, token)
	}
	resp, err := client.Do(request)
	if err != nil {
		return msg.Instance{}, &InstanceLookupError{Err: fmt.Errorf("llm-bridge-server: %w", err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return msg.Instance{}, &InstanceLookupError{Unknown: true, Err: fmt.Errorf("llm-bridge-server knows no instance %q", instanceID)}
	}
	if resp.StatusCode != http.StatusOK {
		return msg.Instance{}, &InstanceLookupError{Err: fmt.Errorf("llm-bridge-server GET /instances/%s: %d", instanceID, resp.StatusCode)}
	}
	var instance msg.Instance
	if err := json.NewDecoder(resp.Body).Decode(&instance); err != nil {
		return msg.Instance{}, &InstanceLookupError{Err: fmt.Errorf("llm-bridge-server GET /instances/%s: %w", instanceID, err)}
	}
	return instance, nil
}

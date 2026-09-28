package matrix

import (
	"encoding/json"
	"fmt"
)

// MessageContentFacts are the fields of an m.room.message content that the
// message log and the conversation view show. Each is passed through as the
// sender wrote it; an absent field is "".
type MessageContentFacts struct {
	MsgType string
	Body    string
	// Format is the content's "format" when it carries a formatted_body
	// (in practice "org.matrix.custom.html"), else "".
	Format        string
	FormattedBody string
	// MediaURL is the mxc:// URI of an image, file, video or audio message.
	MediaURL      string
	MediaMimetype string
	// ReplyToEventID is m.relates_to.m.in_reply_to.event_id.
	ReplyToEventID string
	// ReplacesEventID is set on an edit (m.relates_to rel_type m.replace):
	// the message it edits.
	ReplacesEventID string
	// NewContent is an edit's m.new_content — the edited message itself; the
	// top-level fields of an edit are a fallback ("* new text"). Nil when
	// the event is not an edit or carries none.
	NewContent *MessageContentFacts
}

type rawMessageContent struct {
	MsgType       string `json:"msgtype"`
	Body          string `json:"body"`
	Format        string `json:"format"`
	FormattedBody string `json:"formatted_body"`
	URL           string `json:"url"`
	Info          struct {
		MimeType string `json:"mimetype"`
	} `json:"info"`
	RelatesTo struct {
		RelType   string `json:"rel_type"`
		EventID   string `json:"event_id"`
		InReplyTo struct {
			EventID string `json:"event_id"`
		} `json:"m.in_reply_to"`
	} `json:"m.relates_to"`
	NewContent *rawMessageContent `json:"m.new_content"`
}

func (raw rawMessageContent) facts() MessageContentFacts {
	facts := MessageContentFacts{
		MsgType:        raw.MsgType,
		Body:           raw.Body,
		MediaURL:       raw.URL,
		MediaMimetype:  raw.Info.MimeType,
		ReplyToEventID: raw.RelatesTo.InReplyTo.EventID,
	}
	if raw.FormattedBody != "" {
		facts.Format = raw.Format
		facts.FormattedBody = raw.FormattedBody
	}
	return facts
}

// ParseMessageContent reads an m.room.message content. Empty content (a
// redacted event) gives empty facts.
func ParseMessageContent(content json.RawMessage) (MessageContentFacts, error) {
	if len(content) == 0 {
		return MessageContentFacts{}, nil
	}
	var raw rawMessageContent
	if err := json.Unmarshal(content, &raw); err != nil {
		return MessageContentFacts{}, fmt.Errorf("message content: %w", err)
	}
	facts := raw.facts()
	if raw.RelatesTo.RelType == "m.replace" {
		facts.ReplacesEventID = raw.RelatesTo.EventID
		if raw.NewContent != nil {
			edited := raw.NewContent.facts()
			facts.NewContent = &edited
		}
	}
	return facts, nil
}

// ReactionContent is the content of an m.reaction event.
type ReactionContent struct {
	// TargetEventID is the message reacted to.
	TargetEventID string
	// Key is a unicode emoji, or an mxc:// URI for a custom emoji.
	Key string
	// Shortcode is a custom emoji's ":name:" as the bridge gives it
	// (com.beeper.reaction.shortcode), else "".
	Shortcode string
}

// ParseReactionContent reads an m.reaction content. It fails on content that
// is not an m.annotation, which is the only relation a reaction has.
func ParseReactionContent(content json.RawMessage) (ReactionContent, error) {
	var raw struct {
		RelatesTo struct {
			RelType string `json:"rel_type"`
			EventID string `json:"event_id"`
			Key     string `json:"key"`
		} `json:"m.relates_to"`
		Shortcode string `json:"com.beeper.reaction.shortcode"`
	}
	if err := json.Unmarshal(content, &raw); err != nil {
		return ReactionContent{}, fmt.Errorf("reaction content: %w", err)
	}
	if raw.RelatesTo.RelType != "m.annotation" || raw.RelatesTo.EventID == "" || raw.RelatesTo.Key == "" {
		return ReactionContent{}, fmt.Errorf("reaction content is not an m.annotation with an event and a key: %s", content)
	}
	return ReactionContent{TargetEventID: raw.RelatesTo.EventID, Key: raw.RelatesTo.Key, Shortcode: raw.Shortcode}, nil
}

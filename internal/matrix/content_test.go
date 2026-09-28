package matrix

import (
	"encoding/json"
	"testing"
)

// Contents copied from live mautrix-discord events on 2026-09-28.
func TestParseMessageContentReadsFormattingMediaRepliesAndEdits(t *testing.T) {
	emoji, err := ParseMessageContent(json.RawMessage(`{"body":"Extra caffeine for you today!","format":"org.matrix.custom.html","formatted_body":"Extra caffeine for you today! <img data-mx-emoticon data-mau-animated-emoji src=\"mxc://chat.kayushkin.com/ZdYHnVAcUbGvjrVnoGCZoqxA\" alt=\":bongoooshock:\" title=\":bongoooshock:\" height=\"32\"/>","m.mentions":{},"m.relates_to":{"m.in_reply_to":{"event_id":"$eksDIRPpAT4foswKa45D16sASGh88w-LIvjml9DGdN0"}},"msgtype":"m.text"}`))
	if err != nil || emoji.Format != "org.matrix.custom.html" || emoji.FormattedBody == "" ||
		emoji.ReplyToEventID != "$eksDIRPpAT4foswKa45D16sASGh88w-LIvjml9DGdN0" || emoji.ReplacesEventID != "" || emoji.NewContent != nil {
		t.Errorf("emoji = %+v, %v", emoji, err)
	}
	image, err := ParseMessageContent(json.RawMessage(`{"body":"image0.gif","info":{"h":124,"mimetype":"image/gif","size":897428,"w":220},"msgtype":"m.image","url":"mxc://chat.kayushkin.com/MeiCvHNMQYdEOCoPhEdBSWIQ"}`))
	if err != nil || image.MediaURL != "mxc://chat.kayushkin.com/MeiCvHNMQYdEOCoPhEdBSWIQ" || image.MediaMimetype != "image/gif" || image.Format != "" {
		t.Errorf("image = %+v, %v", image, err)
	}
	edit, err := ParseMessageContent(json.RawMessage(`{"body":"* fixed","msgtype":"m.text","m.new_content":{"body":"fixed","msgtype":"m.text"},"m.relates_to":{"rel_type":"m.replace","event_id":"$original"}}`))
	if err != nil || edit.ReplacesEventID != "$original" || edit.Body != "* fixed" || edit.NewContent == nil || edit.NewContent.Body != "fixed" {
		t.Errorf("edit = %+v, %v", edit, err)
	}
	// A format with no formatted_body says nothing.
	if plain, _ := ParseMessageContent(json.RawMessage(`{"body":"x","format":"org.matrix.custom.html"}`)); plain.Format != "" {
		t.Errorf("plain = %+v", plain)
	}
	if redacted, err := ParseMessageContent(nil); err != nil || redacted != (MessageContentFacts{}) {
		t.Errorf("redacted = %+v, %v", redacted, err)
	}
}

func TestParseReactionContentReadsTheKeyAndShortcode(t *testing.T) {
	custom, err := ParseReactionContent(json.RawMessage(`{"com.beeper.reaction.shortcode":":this:","fi.mau.discord.reaction":{"id":"940000000000000002","mxc":"mxc://chat.kayushkin.com/exampleMediaId0000000000","name":"this"},"m.relates_to":{"event_id":"$a201-jAbWc5rJJaWp2lya3Nz-JSQXhP5KY4-46oA3II","key":"mxc://chat.kayushkin.com/exampleMediaId0000000000","rel_type":"m.annotation"}}`))
	if err != nil || custom != (ReactionContent{TargetEventID: "$a201-jAbWc5rJJaWp2lya3Nz-JSQXhP5KY4-46oA3II",
		Key: "mxc://chat.kayushkin.com/exampleMediaId0000000000", Shortcode: ":this:"}) {
		t.Errorf("custom = %+v, %v", custom, err)
	}
	unicode, err := ParseReactionContent(json.RawMessage(`{"m.relates_to":{"event_id":"$AX","key":"🤣","rel_type":"m.annotation"}}`))
	if err != nil || unicode.Key != "🤣" || unicode.Shortcode != "" {
		t.Errorf("unicode = %+v, %v", unicode, err)
	}
	if _, err := ParseReactionContent(json.RawMessage(`{}`)); err == nil {
		t.Error("a reaction with no annotation was accepted")
	}
}

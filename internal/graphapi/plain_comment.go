package graphapi

import (
	"context"
	"fmt"
	"html"
	"strings"

	"github.com/microsoftgraph/msgraph-sdk-go/models"
	"github.com/microsoftgraph/msgraph-sdk-go/users"
)

// plainTextHTML renders plain text as an HTML fragment that keeps its line
// structure. Each line becomes a div and each blank line an empty div holding
// a break, which is the markup Outlook on the web writes for typed text, so
// the spacing survives stylesheets that zero paragraph margins. Leading
// spaces, and every space that follows another, become non-breaking so that
// indented excerpts keep their columns.
func plainTextHTML(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return ""
	}
	var out strings.Builder
	for line := range strings.SplitSeq(text, "\n") {
		if line == "" {
			out.WriteString("<div><br></div>")
			continue
		}
		out.WriteString("<div>")
		out.WriteString(preserveSpaces(html.EscapeString(line)))
		out.WriteString("</div>")
	}
	return out.String()
}

func preserveSpaces(escaped string) string {
	var out strings.Builder
	previousSpace := true
	for _, r := range escaped {
		switch {
		case r == '\t':
			out.WriteString("&nbsp;&nbsp;&nbsp;&nbsp;")
			previousSpace = true
		case r == ' ' && previousSpace:
			out.WriteString("&nbsp;")
		case r == ' ':
			out.WriteRune(r)
			previousSpace = true
		default:
			out.WriteRune(r)
			previousSpace = false
		}
	}
	return out.String()
}

// plainComment returns the form a plain-text comment must take for a reply to,
// or forward of, the given message. Graph inserts a comment into an HTML reply
// as markup, so line breaks would collapse and angle brackets would be read as
// tags; the text is rendered as HTML when the original is HTML. A comment on a
// plain-text original stays as written.
func (c *Client) plainComment(ctx context.Context, target, messageID, text, action string) (string, error) {
	message, err := c.targetUser(target).Messages().ByMessageId(messageID).Get(ctx,
		&users.ItemMessagesMessageItemRequestBuilderGetRequestConfiguration{
			Headers:         c.messageIDHeaders(nil),
			QueryParameters: &users.ItemMessagesMessageItemRequestBuilderGetQueryParameters{Select: []string{"body"}},
		})
	if err != nil {
		action = "reading the original message's format for " + action
		if target != "" {
			return "", sharedMailboxReadError(action, target, err)
		}
		return "", fmt.Errorf("%s: %w", action, err)
	}
	if message == nil || message.GetBody() == nil || message.GetBody().GetContentType() == nil ||
		*message.GetBody().GetContentType() != models.HTML_BODYTYPE {
		return text, nil
	}
	return plainTextHTML(text), nil
}

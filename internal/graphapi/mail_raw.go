package graphapi

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	abstractions "github.com/microsoft/kiota-abstractions-go"
	"github.com/microsoftgraph/msgraph-sdk-go/models/odataerrors"
)

// attachmentValueTemplate addresses the raw contents of one message attachment.
// The SDK generates a $value builder for messages but not for their
// attachments, so the request is assembled from the attachment builder's path
// parameters. Reusing those parameters keeps /me and delegated /users/{id}
// routing identical to the metadata request that precedes it.
const attachmentValueTemplate = "{+baseurl}/users/{user%2Did}/messages/{message%2Did}/attachments/{attachment%2Did}/$value"

// rawAttachmentContent fetches an attachment's $value. For an item attachment
// Graph returns MIME for a message, a vCard for a contact and iCalendar for an
// event.
func (c *Client) rawAttachmentContent(ctx context.Context, pathParameters map[string]string) ([]byte, error) {
	info := abstractions.NewRequestInformationWithMethodAndUrlTemplateAndPathParameters(
		abstractions.GET, attachmentValueTemplate, pathParameters)
	errorMapping := abstractions.ErrorMappings{
		"XXX": odataerrors.CreateODataErrorFromDiscriminatorValue,
	}
	res, err := c.inner.GetAdapter().SendPrimitive(ctx, info, "[]byte", errorMapping)
	if err != nil {
		return nil, err
	}
	content, _ := res.([]byte)
	if len(content) == 0 {
		return nil, fmt.Errorf("graph returned no content for the attachment")
	}
	return content, nil
}

// itemAttachmentFilename names a downloaded item attachment after the format
// Graph actually returned, which the attachment's contentType does not reliably
// report: Outlook leaves it empty on some forwarded messages.
func itemAttachmentFilename(name string, content []byte) string {
	ext := ".eml"
	head := bytes.TrimLeft(content[:min(len(content), 64)], "\uFEFF \t\r\n")
	switch {
	case bytes.HasPrefix(head, []byte("BEGIN:VCARD")):
		ext = ".vcf"
	case bytes.HasPrefix(head, []byte("BEGIN:VCALENDAR")):
		ext = ".ics"
	}
	if name == "" {
		name = "attachment"
	}
	if strings.HasSuffix(strings.ToLower(name), ext) {
		return name
	}
	return name + ext
}

// GetMessageMIME returns the full message as RFC 5322 MIME, suitable for
// saving as an .eml file, from the target mailbox or from the signed-in user's
// mailbox when target is empty.
func (c *Client) GetMessageMIME(ctx context.Context, target, messageID string) ([]byte, error) {
	if err := validateID(messageID, "message ID"); err != nil {
		return nil, err
	}
	content, err := c.targetUser(target).Messages().ByMessageId(messageID).Content().Get(ctx, nil)
	if err != nil {
		return nil, sharedMailboxReadError("exporting message", target, err)
	}
	if len(content) == 0 {
		return nil, fmt.Errorf("graph returned no MIME content for message %s", messageID)
	}
	return content, nil
}

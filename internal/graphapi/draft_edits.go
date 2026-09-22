package graphapi

import (
	"context"
	"fmt"

	"github.com/microsoftgraph/msgraph-sdk-go/models"
	"github.com/microsoftgraph/msgraph-sdk-go/users"
)

// AttachToDraft adds a file attachment to an existing draft in the target
// mailbox, or in the signed-in user's own mailbox when target is empty. The
// Files must be under 3 MB; larger uploads need an upload session.
func (c *Client) AttachToDraft(ctx context.Context, target, draftID, name, contentType string, content []byte) (*Attachment, error) {
	if err := c.ensureWritable(); err != nil {
		return nil, err
	}
	if err := validateID(draftID, "draft ID"); err != nil {
		return nil, err
	}

	if len(content) >= MaxInlineAttachmentBytes {
		return nil, fmt.Errorf("attachment must be under 3 MB")
	}
	if err := c.requireDraft(ctx, target, draftID); err != nil {
		return nil, err
	}

	att := models.NewFileAttachment()
	att.SetName(&name)
	att.SetContentType(&contentType)
	att.SetContentBytes(content)

	created, err := c.targetUser(target).Messages().ByMessageId(draftID).Attachments().Post(ctx, att, nil)
	if err != nil {
		if target != "" {
			return nil, sharedMailboxDraftError("attaching file to draft", target, err)
		}
		return nil, fmt.Errorf("attaching file to draft: %w", err)
	}

	if created == nil || derefStr(created.GetId()) == "" {
		return nil, fmt.Errorf("attaching file to draft: Graph returned no attachment ID")
	}
	result := Attachment{}
	if created.GetId() != nil {
		result.ID = *created.GetId()
	}
	if created.GetName() != nil {
		result.Name = *created.GetName()
	}
	if created.GetContentType() != nil {
		result.ContentType = *created.GetContentType()
	}
	if created.GetSize() != nil {
		result.Size = *created.GetSize()
	}
	return &result, nil
}

// DraftRecipients carries the recipient lists an UpdateDraft call replaces. A
// nil slice leaves that list untouched; an empty slice clears it.
type DraftRecipients struct {
	To  []string `json:"to" untrusted:"true"`
	CC  []string `json:"cc" untrusted:"true"`
	BCC []string `json:"bcc" untrusted:"true"`
}

// DraftContent carries the subject and body an UpdateDraft call replaces. A
// nil pointer leaves that field untouched. Body replaces the whole body, so on
// a reply or forward draft it also replaces the quoted original that Outlook
// generated; the caller must supply that history again if it is wanted.
type DraftContent struct {
	Subject *string
	Body    *string
	IsHTML  bool
}

func (content DraftContent) empty() bool {
	return content.Subject == nil && content.Body == nil
}

// UpdateDraft replaces the recipient lists, subject or body of an existing
// draft in the target mailbox, or in the signed-in user's own mailbox when
// target is empty. Only what is supplied changes.
func (c *Client) UpdateDraft(ctx context.Context, target, draftID string, recipients DraftRecipients, content DraftContent) error {
	if err := c.ensureWritable(); err != nil {
		return err
	}
	if err := validateID(draftID, "draft ID"); err != nil {
		return err
	}

	if recipients.To == nil && recipients.CC == nil && recipients.BCC == nil && content.empty() {
		return fmt.Errorf("nothing to update: no recipient lists, subject or body supplied")
	}
	if content.IsHTML && content.Body == nil {
		return fmt.Errorf("an HTML body type needs a body to apply to")
	}
	patch := models.NewMessage()
	if recipients.To != nil {
		r, err := makeRecipients(recipients.To)
		if err != nil {
			return fmt.Errorf("invalid to recipient: %w", err)
		}
		patch.SetToRecipients(r)
	}
	if recipients.CC != nil {
		r, err := makeRecipients(recipients.CC)
		if err != nil {
			return fmt.Errorf("invalid cc recipient: %w", err)
		}
		patch.SetCcRecipients(r)
	}
	if recipients.BCC != nil {
		r, err := makeRecipients(recipients.BCC)
		if err != nil {
			return fmt.Errorf("invalid bcc recipient: %w", err)
		}
		patch.SetBccRecipients(r)
	}

	if content.Subject != nil {
		subject := *content.Subject
		patch.SetSubject(&subject)
	}
	if content.Body != nil {
		body := models.NewItemBody()
		text := *content.Body
		bodyType := models.TEXT_BODYTYPE
		if content.IsHTML {
			bodyType = models.HTML_BODYTYPE
		}
		body.SetContent(&text)
		body.SetContentType(&bodyType)
		patch.SetBody(body)
	}

	if err := c.requireDraft(ctx, target, draftID); err != nil {
		return err
	}
	if _, err := c.targetUser(target).Messages().ByMessageId(draftID).Patch(ctx, patch, nil); err != nil {
		if target != "" {
			return sharedMailboxDraftError("updating draft", target, err)
		}
		return fmt.Errorf("updating draft: %w", err)
	}
	return nil
}

// requireDraft avoids modifying a received or already-sent message. Graph also
// rejects recipient updates if the message is sent after this read.
func (c *Client) requireDraft(ctx context.Context, target, draftID string) error {
	message, err := c.targetUser(target).Messages().ByMessageId(draftID).Get(ctx, &users.ItemMessagesMessageItemRequestBuilderGetRequestConfiguration{
		QueryParameters: &users.ItemMessagesMessageItemRequestBuilderGetQueryParameters{Select: []string{"isDraft"}},
	})
	if err != nil {
		return sharedMailboxReadError("checking draft", target, err)
	}
	if message == nil || message.GetIsDraft() == nil || !*message.GetIsDraft() {
		return fmt.Errorf("message is not a draft")
	}
	return nil
}

package graphapi

import (
	"context"
	"fmt"

	"github.com/microsoftgraph/msgraph-sdk-go/models"
)

// MaxCalendarAttachmentBytes is Graph's exclusive upper bound for the simple
// event-attachment endpoint. Larger files require an upload session.
const MaxCalendarAttachmentBytes = 3 << 20

// CalendarAttachment is the stable output shape for an event attachment.
type CalendarAttachment struct {
	ID          string `json:"id"`
	Name        string `json:"name" untrusted:"true"`
	ContentType string `json:"contentType"`
	Size        int32  `json:"size"`
}

// ListCalendarAttachments lists attachments on an event in the target mailbox, or
// in the signed-in user's own mailbox when target is empty.
func (c *Client) ListCalendarAttachments(ctx context.Context, target, eventID string) ([]CalendarAttachment, error) {
	if err := validateID(eventID, "event ID"); err != nil {
		return nil, err
	}
	resp, err := c.targetUser(target).Events().ByEventId(eventID).Attachments().Get(ctx, nil)
	if err != nil {
		return nil, mailboxError("listing event attachments", target, calendarGrantHint, err)
	}
	if resp == nil {
		return []CalendarAttachment{}, nil
	}

	attachments := make([]CalendarAttachment, 0, len(resp.GetValue()))
	for _, a := range resp.GetValue() {
		attachments = append(attachments, calendarAttachmentFromModel(a))
	}
	return attachments, nil
}

// UploadCalendarAttachment adds a small file attachment to an event. Graph's
// simple endpoint accepts files strictly smaller than 3 MB; upload sessions
// are intentionally left for a separate large-file implementation.
func (c *Client) UploadCalendarAttachment(ctx context.Context, target, eventID, name, contentType string, content []byte) (*CalendarAttachment, error) {
	if err := c.ensureWritable(); err != nil {
		return nil, err
	}
	if err := validateID(eventID, "event ID"); err != nil {
		return nil, err
	}
	if len(content) >= MaxCalendarAttachmentBytes {
		return nil, fmt.Errorf("attachment %q is %d bytes; event attachments must be under 3 MB", name, len(content))
	}

	attachment := models.NewFileAttachment()
	attachment.SetName(&name)
	attachment.SetContentType(&contentType)
	attachment.SetContentBytes(content)

	created, err := c.targetUser(target).Events().ByEventId(eventID).Attachments().Post(ctx, attachment, nil)
	if err != nil {
		return nil, mailboxError("uploading event attachment", target, calendarGrantHint, err)
	}
	if created == nil {
		return nil, fmt.Errorf("uploading event attachment: Graph returned no attachment")
	}
	result := calendarAttachmentFromModel(created)
	return &result, nil
}

// DownloadCalendarAttachment downloads a file attachment from an event.
func (c *Client) DownloadCalendarAttachment(ctx context.Context, target, eventID, attachmentID string) (*CalendarAttachment, []byte, error) {
	if err := validateID(eventID, "event ID"); err != nil {
		return nil, nil, err
	}
	if err := validateID(attachmentID, "attachment ID"); err != nil {
		return nil, nil, err
	}

	attachment, err := c.targetUser(target).Events().ByEventId(eventID).Attachments().ByAttachmentId(attachmentID).Get(ctx, nil)
	if err != nil {
		return nil, nil, mailboxError("downloading event attachment", target, calendarGrantHint, err)
	}
	if attachment == nil {
		return nil, nil, fmt.Errorf("downloading event attachment: Graph returned no attachment")
	}
	fileAttachment, ok := attachment.(models.FileAttachmentable)
	if !ok {
		return nil, nil, fmt.Errorf("attachment %q is not a file attachment", derefStr(attachment.GetName()))
	}
	content := fileAttachment.GetContentBytes()
	if len(content) > maxAttachmentBytes {
		return nil, nil, fmt.Errorf("attachment %q is %d bytes, exceeds %d byte limit", derefStr(attachment.GetName()), len(content), maxAttachmentBytes)
	}
	result := calendarAttachmentFromModel(attachment)
	return &result, content, nil
}

// DeleteCalendarAttachment deletes an attachment from an event.
func (c *Client) DeleteCalendarAttachment(ctx context.Context, target, eventID, attachmentID string) error {
	if err := c.ensureWritable(); err != nil {
		return err
	}
	if err := validateID(eventID, "event ID"); err != nil {
		return err
	}
	if err := validateID(attachmentID, "attachment ID"); err != nil {
		return err
	}
	if err := c.targetUser(target).Events().ByEventId(eventID).Attachments().ByAttachmentId(attachmentID).Delete(ctx, nil); err != nil {
		return mailboxError("deleting event attachment", target, calendarGrantHint, err)
	}
	return nil
}

func calendarAttachmentFromModel(a models.Attachmentable) CalendarAttachment {
	attachment := CalendarAttachment{
		Name:        derefStr(a.GetName()),
		ContentType: derefStr(a.GetContentType()),
	}
	if a.GetId() != nil {
		attachment.ID = *a.GetId()
	}
	if a.GetSize() != nil {
		attachment.Size = *a.GetSize()
	}
	return attachment
}

package graphapi

import (
	"context"
	"net/http"
	"testing"
)

// An inline image is the common case in a real thread and the one Graph reports
// worst. A message whose attachments are all inline still reports
// hasAttachments=false, so a caller that trusts that flag never asks for them at
// all; and once it does ask, isInline and contentId are the only way to tie the
// bytes back to the <img src="cid:..."> they fill. Outlook names every pasted
// screenshot image.png, so two attachments on one message are otherwise
// distinguishable only by their position in the response.
func TestGetAttachmentsCarriesInlineImageIdentity(t *testing.T) {
	client := testGraphClient(t, func(req *http.Request) *http.Response {
		return graphJSONResponse(req, `{"value":[
			{
				"@odata.type": "#microsoft.graph.fileAttachment",
				"id": "att-1",
				"name": "image.png",
				"contentType": "image/png",
				"size": 65906,
				"isInline": true,
				"contentId": "11111111-2222-3333-4444-555555555555"
			},
			{
				"@odata.type": "#microsoft.graph.fileAttachment",
				"id": "att-2",
				"name": "report.pdf",
				"contentType": "application/pdf",
				"size": 1024,
				"isInline": false
			}
		]}`)
	})

	got, err := client.GetAttachments(context.Background(), "team@example.com", "AAA")
	if err != nil {
		t.Fatalf("GetAttachments: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("attachments = %d, want 2", len(got))
	}

	if !got[0].IsInline {
		t.Error("inline image reported IsInline=false; a caller cannot then tell it apart " +
			"from a file the sender deliberately attached")
	}
	if got[0].ContentID != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("ContentID = %q, want the content ID the HTML body references", got[0].ContentID)
	}
	if got[1].IsInline {
		t.Error("ordinary file attachment reported IsInline=true")
	}
	if got[1].ContentID != "" {
		t.Errorf("ContentID = %q, want empty for an attachment with no content ID", got[1].ContentID)
	}
}

// DownloadAttachment maps the same two fields from the single-resource response,
// which is a separate code path from the collection above.
func TestDownloadAttachmentCarriesInlineImageIdentity(t *testing.T) {
	client := testGraphClient(t, func(req *http.Request) *http.Response {
		return graphJSONResponse(req, `{
			"@odata.type": "#microsoft.graph.fileAttachment",
			"id": "att-1",
			"name": "image.png",
			"contentType": "image/png",
			"size": 65906,
			"isInline": true,
			"contentId": "11111111-2222-3333-4444-555555555555",
			"contentBytes": "aW1n"
		}`)
	})

	got, err := client.DownloadAttachment(context.Background(), "", "AAA", "att-1")
	if err != nil {
		t.Fatalf("DownloadAttachment: %v", err)
	}
	if !got.IsInline || got.ContentID != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("IsInline = %v, ContentID = %q — a downloaded inline image that cannot name "+
			"its content ID cannot be placed back into the body it came from", got.IsInline, got.ContentID)
	}
	if string(got.Content) != "img" {
		t.Errorf("Content = %q, want the decoded bytes", got.Content)
	}
}

// Graph puts contentId on fileAttachment, not on the attachment base type, so
// reading it needs a cast that an itemAttachment — a forwarded message or a
// contact card — must miss rather than panic on.
func TestGetAttachmentsToleratesNonFileAttachments(t *testing.T) {
	client := testGraphClient(t, func(req *http.Request) *http.Response {
		return graphJSONResponse(req, `{"value":[
			{
				"@odata.type": "#microsoft.graph.itemAttachment",
				"id": "att-3",
				"name": "Forwarded message",
				"contentType": "message/rfc822",
				"size": 4096,
				"isInline": false
			}
		]}`)
	})

	got, err := client.GetAttachments(context.Background(), "", "AAA")
	if err != nil {
		t.Fatalf("GetAttachments: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("attachments = %d, want 1", len(got))
	}
	if got[0].ContentID != "" {
		t.Errorf("ContentID = %q, want empty for an itemAttachment", got[0].ContentID)
	}
	if got[0].Name != "Forwarded message" {
		t.Errorf("Name = %q", got[0].Name)
	}
}

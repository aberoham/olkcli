package graphapi

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
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

func graphRawResponse(req *http.Request, contentType, body string) *http.Response {
	return &http.Response{
		StatusCode:    http.StatusOK,
		Header:        http.Header{"Content-Type": []string{contentType}},
		Body:          io.NopCloser(strings.NewReader(body)),
		Request:       req,
		ContentLength: int64(len(body)),
	}
}

const forwardedMIME = "From: Sender <sender@example.com>\r\nSubject: Forwarded\r\n\r\nBody\r\n"

// itemAttachmentResponder serves an itemAttachment's metadata and then its raw
// MIME from $value, recording the paths requested.
func itemAttachmentResponder(metadata, raw string, paths *[]string) roundTripFunc {
	return func(req *http.Request) *http.Response {
		*paths = append(*paths, req.URL.Path)
		if strings.HasSuffix(req.URL.Path, "/$value") {
			return graphRawResponse(req, "message/rfc822", raw)
		}
		return graphJSONResponse(req, metadata)
	}
}

// Outlook stores a message forwarded as an attachment as an itemAttachment,
// which has no contentBytes. Its content is the MIME behind $value, and it is
// only useful on disk if the saved name says it is an email.
func TestDownloadAttachmentFetchesItemAttachmentMIME(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		attName     string
		raw         string
		wantName    string
	}{
		{"forwarded message", `"message/rfc822"`, "Onboarding documentation", forwardedMIME, "Onboarding documentation.eml"},
		{"empty contentType still downloads", `null`, "Onboarding documentation", forwardedMIME, "Onboarding documentation.eml"},
		{"name already ends in .eml", `"message/rfc822"`, "note.EML", forwardedMIME, "note.EML"},
		{"unnamed message", `null`, "", forwardedMIME, "attachment.eml"},
		{"attached contact", `null`, "Alex Wilbur", "BEGIN:VCARD\r\nFN:Alex Wilbur\r\nEND:VCARD\r\n", "Alex Wilbur.vcf"},
		{"attached event", `null`, "Review", "BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n", "Review.ics"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			metadata := `{
				"@odata.type": "#microsoft.graph.itemAttachment",
				"id": "att-3",
				"name": "` + tc.attName + `",
				"contentType": ` + tc.contentType + `,
				"size": 4096,
				"isInline": false
			}`
			var paths []string
			client := testGraphClient(t, itemAttachmentResponder(metadata, tc.raw, &paths))

			got, err := client.DownloadAttachment(context.Background(), "", "AAA", "att-3")
			if err != nil {
				t.Fatalf("DownloadAttachment: %v", err)
			}
			if string(got.Content) != tc.raw {
				t.Errorf("Content = %q, want the raw $value body", got.Content)
			}
			if got.Name != tc.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tc.wantName)
			}
			want := []string{
				meBuilderPath + "/messages/AAA/attachments/att-3",
				meBuilderPath + "/messages/AAA/attachments/att-3/$value",
			}
			if strings.Join(paths, "\n") != strings.Join(want, "\n") {
				t.Errorf("request paths = %v, want %v", paths, want)
			}
		})
	}
}

// A referenceAttachment is a link to a file in OneDrive or SharePoint. Graph
// answers 405 to its $value, so the download must stop at the metadata and say
// what the attachment is rather than calling it "not a file attachment".
func TestDownloadAttachmentExplainsReferenceAttachment(t *testing.T) {
	calls := 0
	client := testGraphClient(t, func(req *http.Request) *http.Response {
		calls++
		return graphJSONResponse(req, `{
			"@odata.type": "#microsoft.graph.referenceAttachment",
			"id": "att-4",
			"name": "Sales Invoice Template.docx",
			"size": 1060,
			"isInline": false
		}`)
	})

	_, err := client.DownloadAttachment(context.Background(), "", "AAA", "att-4")
	if err == nil || !strings.Contains(err.Error(), "link to a cloud file") {
		t.Fatalf("error = %v, want a message naming the cloud-file link", err)
	}
	if calls != 1 {
		t.Errorf("Graph requests = %d, want 1: a reference attachment has no $value", calls)
	}
}

// The size limit applies before the $value request, so an oversized item is
// refused without downloading it, and again to the bytes actually returned,
// because the reported size is Graph's estimate rather than the MIME length.
func TestDownloadAttachmentEnforcesSizeLimitOnItemAttachments(t *testing.T) {
	t.Run("reported size", func(t *testing.T) {
		var paths []string
		metadata := `{"@odata.type":"#microsoft.graph.itemAttachment","id":"att-5","name":"Huge",` +
			`"size":` + strconv.Itoa(maxAttachmentBytes+1) + `}`
		client := testGraphClient(t, itemAttachmentResponder(metadata, forwardedMIME, &paths))

		_, err := client.DownloadAttachment(context.Background(), "", "AAA", "att-5")
		if err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("error = %v, want size limit error", err)
		}
		if len(paths) != 1 {
			t.Errorf("Graph requests = %v, want only the metadata request", paths)
		}
	})
	t.Run("returned bytes", func(t *testing.T) {
		var paths []string
		metadata := `{"@odata.type":"#microsoft.graph.itemAttachment","id":"att-5","name":"Huge","size":10}`
		raw := strings.Repeat("x", maxAttachmentBytes+1)
		client := testGraphClient(t, itemAttachmentResponder(metadata, raw, &paths))

		_, err := client.DownloadAttachment(context.Background(), "", "AAA", "att-5")
		if err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("error = %v, want size limit error", err)
		}
	})
}

func TestGetMessageMIMEReturnsRawMessage(t *testing.T) {
	var got string
	client := testGraphClient(t, func(req *http.Request) *http.Response {
		got = req.URL.Path
		return graphRawResponse(req, "text/plain", forwardedMIME)
	})

	content, err := client.GetMessageMIME(context.Background(), "shared@example.com", "AAA")
	if err != nil {
		t.Fatalf("GetMessageMIME: %v", err)
	}
	if string(content) != forwardedMIME {
		t.Errorf("content = %q, want the raw MIME", content)
	}
	if got != "/v1.0/users/shared@example.com/messages/AAA/$value" {
		t.Errorf("path = %q, want the delegated mailbox's $value", got)
	}
}

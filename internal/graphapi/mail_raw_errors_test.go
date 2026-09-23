package graphapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

const itemAttachmentMetadata = `{"@odata.type":"#microsoft.graph.itemAttachment",` +
	`"id":"att-3","name":"Onboarding","size":4096}`

func graphErrorResponse(req *http.Request, status int) *http.Response {
	resp := graphJSONResponse(req, `{"error":{"code":"ErrorInternalServerError","message":"Try again later."}}`)
	resp.StatusCode = status
	return resp
}

func TestDownloadAttachmentReportsAFailedOrEmptyItemValue(t *testing.T) {
	tests := []struct {
		name    string
		value   func(*http.Request) *http.Response
		wantErr string
	}{
		{"request fails", func(req *http.Request) *http.Response {
			return graphErrorResponse(req, http.StatusInternalServerError)
		}, "Try again later."},
		{"empty body", func(req *http.Request) *http.Response {
			return graphRawResponse(req, "message/rfc822", "")
		}, "graph returned no content for the attachment"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := testGraphClient(t, func(req *http.Request) *http.Response {
				if strings.HasSuffix(req.URL.Path, "/$value") {
					return tc.value(req)
				}
				return graphJSONResponse(req, itemAttachmentMetadata)
			})
			_, err := client.DownloadAttachment(context.Background(), "", "AAA", "att-3")
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestGetMessageMIMEReportsFailures(t *testing.T) {
	tests := []struct {
		name      string
		messageID string
		response  func(*http.Request) *http.Response
		wantErr   string
	}{
		{"empty ID", "", nil, "message ID"},
		{"request fails", "AAA", func(req *http.Request) *http.Response {
			return graphErrorResponse(req, http.StatusInternalServerError)
		}, "exporting message"},
		{"empty body", "AAA", func(req *http.Request) *http.Response {
			return graphRawResponse(req, "text/plain", "")
		}, "graph returned no MIME content for message AAA"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := testGraphClient(t, func(req *http.Request) *http.Response {
				if tc.response == nil {
					t.Fatalf("unexpected Graph request: %s", req.URL.Path)
				}
				return tc.response(req)
			})
			_, err := client.GetMessageMIME(context.Background(), "shared@example.com", tc.messageID)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

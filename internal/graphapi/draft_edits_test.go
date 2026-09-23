package graphapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestDraftRecipientPatchPreservesOmissionAndClearsEmptyLists(t *testing.T) {
	for _, target := range []string{"", "shared@example.com"} {
		t.Run(target, func(t *testing.T) {
			path := meBuilderPath + "/messages/draft-id"
			if target != "" {
				path = "/v1.0/users/" + target + "/messages/draft-id"
			}
			calls := 0
			client := testGraphClient(t, func(req *http.Request) *http.Response {
				calls++
				if req.URL.Path != path {
					t.Fatalf("path = %s", req.URL.Path)
				}
				switch req.Method {
				case http.MethodGet:
					if req.URL.Query().Get("$select") != "isDraft" {
						t.Fatalf("select = %s", req.URL.RawQuery)
					}
					return graphJSONResponse(req, `{"isDraft":true}`)
				case http.MethodPatch:
					var got map[string]any
					if err := json.NewDecoder(req.Body).Decode(&got); err != nil {
						t.Fatal(err)
					}
					want := map[string]any{"@odata.type": "#microsoft.graph.message", "ccRecipients": []any{}, "bccRecipients": []any{map[string]any{"emailAddress": map[string]any{"address": "bcc@example.com"}}}}
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("PATCH = %#v, want %#v", got, want)
					}
					return graphJSONResponse(req, `{"id":"draft-id"}`)
				default:
					t.Fatalf("unexpected method: %s", req.Method)
					return nil
				}
			})
			client.noSend = true
			err := client.UpdateDraft(context.Background(), target, "draft-id", DraftRecipients{CC: []string{}, BCC: []string{"bcc@example.com"}}, DraftContent{})
			if err != nil || calls != 2 {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
		})
	}
}

func TestDraftAttachmentUploadPayloadAndTarget(t *testing.T) {
	for _, target := range []string{"", "shared@example.com"} {
		t.Run(target, func(t *testing.T) {
			base := meBuilderPath + "/messages/draft-id"
			if target != "" {
				base = "/v1.0/users/" + target + "/messages/draft-id"
			}
			calls := 0
			client := testGraphClient(t, func(req *http.Request) *http.Response {
				calls++
				if req.Method == http.MethodGet && req.URL.Path == base {
					return graphJSONResponse(req, `{"isDraft":true}`)
				}
				if req.Method != http.MethodPost || req.URL.Path != base+"/attachments" {
					t.Fatalf("request = %s %s", req.Method, req.URL)
				}
				var got map[string]any
				if err := json.NewDecoder(req.Body).Decode(&got); err != nil {
					t.Fatal(err)
				}
				want := map[string]any{"@odata.type": "#microsoft.graph.fileAttachment", "name": "report.txt", "contentType": "text/plain", "contentBytes": "aGVsbG8="}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("payload=%#v", got)
				}
				return graphJSONResponse(req, `{"id":"attachment-id","name":"report.txt","contentType":"text/plain","size":5}`)
			})
			client.noSend = true
			att, err := client.AttachToDraft(context.Background(), target, "draft-id", "report.txt", "text/plain", []byte("hello"))
			if err != nil || calls != 2 {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
			if att.ID != "attachment-id" || att.Size != 5 {
				t.Fatalf("attachment=%#v", att)
			}
		})
	}
}

func TestDraftEditsRejectInvalidInputsBeforeRequests(t *testing.T) {
	ctx := context.Background()
	client := &Client{}
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"empty update", func() error { return client.UpdateDraft(ctx, "", "draft-id", DraftRecipients{}, DraftContent{}) }},
		{"invalid recipient", func() error {
			return client.UpdateDraft(ctx, "", "draft-id", DraftRecipients{To: []string{"invalid"}}, DraftContent{})
		}},
		{"invalid ID", func() error { return client.UpdateDraft(ctx, "", "", DraftRecipients{To: []string{}}, DraftContent{}) }},
		{"oversize", func() error {
			_, err := client.AttachToDraft(ctx, "", "draft-id", "f", "text/plain", make([]byte, MaxInlineAttachmentBytes))
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	client.noWrite = true
	if err := client.UpdateDraft(ctx, "shared@example.com", "id", DraftRecipients{CC: []string{}}, DraftContent{}); !errors.Is(err, ErrNoWrite) {
		t.Fatalf("error=%v", err)
	}
	if _, err := client.AttachToDraft(ctx, "shared@example.com", "id", "f", "text/plain", nil); !errors.Is(err, ErrNoWrite) {
		t.Fatalf("error=%v", err)
	}
}

func TestDraftEditsRefuseNonDraftAndMissingDraftState(t *testing.T) {
	for _, body := range []string{`{"isDraft":false}`, `{}`} {
		for _, attach := range []bool{true, false} {
			client := testGraphClient(t, func(req *http.Request) *http.Response {
				if req.Method != http.MethodGet {
					t.Fatalf("unexpected mutation: %s", req.Method)
				}
				return graphJSONResponse(req, body)
			})
			var err error
			if attach {
				_, err = client.AttachToDraft(context.Background(), "", "id", "f", "text/plain", nil)
			} else {
				err = client.UpdateDraft(context.Background(), "", "id", DraftRecipients{CC: []string{}}, DraftContent{})
			}
			if err == nil || !strings.Contains(err.Error(), "not a draft") {
				t.Fatalf("error=%v", err)
			}
		}
	}
}

func TestDraftAttachmentEmptyResponseIsAnError(t *testing.T) {
	client := testGraphClient(t, func(req *http.Request) *http.Response {
		if req.Method == http.MethodGet {
			return graphJSONResponse(req, `{"isDraft":true}`)
		}
		return &http.Response{StatusCode: http.StatusNoContent, Header: http.Header{}, Request: req}
	})
	if _, err := client.AttachToDraft(context.Background(), "", "id", "f", "text/plain", nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestDraftEditsPreserveProviderErrors(t *testing.T) {
	for _, attach := range []bool{false, true} {
		for _, failRead := range []bool{false, true} {
			client := testGraphClient(t, func(req *http.Request) *http.Response {
				if req.Method == http.MethodGet && !failRead {
					return graphJSONResponse(req, `{"isDraft":true}`)
				}
				response := graphJSONResponse(req, `{"error":{"code":"ErrorAccessDenied","message":"Access is denied"}}`)
				response.StatusCode = http.StatusForbidden
				return response
			})
			var err error
			if attach {
				_, err = client.AttachToDraft(context.Background(), "shared@example.com", "id", "f", "text/plain", nil)
			} else {
				err = client.UpdateDraft(context.Background(), "shared@example.com", "id", DraftRecipients{To: []string{}}, DraftContent{})
			}
			code, status := ErrorMetadata(err)
			if code != errorAccessDeniedCode || status != http.StatusForbidden {
				t.Fatalf("error=%v code=%s status=%d", err, code, status)
			}
		}
	}
}

func TestDraftContentPatchSendsOnlyWhatWasSupplied(t *testing.T) {
	subject := "Re: Corrected subject"
	empty := ""
	htmlBody := "<p>Corrected wording</p>"
	textBody := "Corrected wording"
	for _, tc := range []struct {
		name    string
		content DraftContent
		want    map[string]any
	}{
		{
			name:    "subject only",
			content: DraftContent{Subject: &subject},
			want:    map[string]any{"subject": subject},
		},
		{
			name:    "subject cleared",
			content: DraftContent{Subject: &empty},
			want:    map[string]any{"subject": ""},
		},
		{
			name:    "HTML body",
			content: DraftContent{Body: &htmlBody, IsHTML: true},
			want:    map[string]any{"body": map[string]any{"contentType": "html", "content": htmlBody}},
		},
		{
			name:    "text body with subject",
			content: DraftContent{Subject: &subject, Body: &textBody},
			want:    map[string]any{"subject": subject, "body": map[string]any{"contentType": "text", "content": textBody}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			patched := false
			client := testGraphClient(t, func(req *http.Request) *http.Response {
				if req.URL.Path != "/v1.0/users/shared@example.com/messages/draft-id" {
					t.Fatalf("path = %s", req.URL.Path)
				}
				if req.Method == http.MethodGet {
					return graphJSONResponse(req, `{"isDraft":true}`)
				}
				var got map[string]any
				if err := json.NewDecoder(req.Body).Decode(&got); err != nil {
					t.Fatal(err)
				}
				delete(got, "@odata.type")
				if body, ok := got["body"].(map[string]any); ok {
					delete(body, "@odata.type")
				}
				if !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("PATCH = %#v, want %#v", got, tc.want)
				}
				patched = true
				return graphJSONResponse(req, `{"id":"draft-id"}`)
			})
			if err := client.UpdateDraft(context.Background(), "shared@example.com", "draft-id", DraftRecipients{}, tc.content); err != nil {
				t.Fatalf("UpdateDraft: %v", err)
			}
			if !patched {
				t.Fatal("UpdateDraft sent no PATCH")
			}
		})
	}
}

func TestDraftContentRefusesAnHTMLTypeWithoutABody(t *testing.T) {
	client := &Client{}
	subject := "Subject"
	err := client.UpdateDraft(context.Background(), "", "draft-id", DraftRecipients{}, DraftContent{Subject: &subject, IsHTML: true})
	if err == nil || !strings.Contains(err.Error(), "needs a body") {
		t.Fatalf("error = %v, want a refusal of an HTML type with no body", err)
	}
}

func TestSendDraftRefusesAMessageThatIsNotADraft(t *testing.T) {
	for _, target := range []string{"", "shared@example.com"} {
		t.Run(target, func(t *testing.T) {
			client := testGraphClient(t, func(req *http.Request) *http.Response {
				if req.Method != http.MethodGet {
					t.Fatalf("unexpected send of a received message: %s %s", req.Method, req.URL.Path)
				}
				return graphJSONResponse(req, `{"isDraft":false}`)
			})
			err := client.SendDraft(context.Background(), target, "received-id")
			if err == nil || !strings.Contains(err.Error(), "received-id is not a draft") {
				t.Fatalf("error = %v, want a not-a-draft refusal naming the ID", err)
			}
			if strings.Contains(err.Error(), "Send As") {
				t.Errorf("error = %v, should not offer send-permission advice for a non-draft ID", err)
			}
		})
	}
}

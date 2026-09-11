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
			err := client.UpdateDraftRecipients(context.Background(), target, "draft-id", DraftRecipients{CC: []string{}, BCC: []string{"bcc@example.com"}})
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
		{"empty update", func() error { return client.UpdateDraftRecipients(ctx, "", "draft-id", DraftRecipients{}) }},
		{"invalid recipient", func() error {
			return client.UpdateDraftRecipients(ctx, "", "draft-id", DraftRecipients{To: []string{"invalid"}})
		}},
		{"invalid ID", func() error { return client.UpdateDraftRecipients(ctx, "", "", DraftRecipients{To: []string{}}) }},
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
	if err := client.UpdateDraftRecipients(ctx, "shared@example.com", "id", DraftRecipients{CC: []string{}}); !errors.Is(err, ErrNoWrite) {
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
				err = client.UpdateDraftRecipients(context.Background(), "", "id", DraftRecipients{CC: []string{}})
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
				err = client.UpdateDraftRecipients(context.Background(), "shared@example.com", "id", DraftRecipients{To: []string{}})
			}
			code, status := ErrorMetadata(err)
			if code != errorAccessDeniedCode || status != http.StatusForbidden {
				t.Fatalf("error=%v code=%s status=%d", err, code, status)
			}
		}
	}
}

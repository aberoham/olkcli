package graphapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCreateReplyDraftRoutesAndFormats(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		replyAll bool
		html     bool
		content  string
		wantPath string
	}{
		{
			name:     "plain reply in own mailbox",
			content:  "Thanks",
			wantPath: meBuilderPath + "/messages/AAA/createReply",
		},
		{
			name:     "HTML reply-all in own mailbox",
			replyAll: true,
			html:     true,
			content:  "<p>Thanks all</p>",
			wantPath: meBuilderPath + "/messages/AAA/createReplyAll",
		},
		{
			name:     "HTML reply in delegated mailbox",
			target:   "team@example.com",
			html:     true,
			content:  "<p>Thanks</p>",
			wantPath: "/v1.0/users/team@example.com/messages/AAA/createReply",
		},
		{
			name:     "plain reply-all in delegated mailbox",
			target:   "team@example.com",
			replyAll: true,
			content:  "Thanks all",
			wantPath: "/v1.0/users/team@example.com/messages/AAA/createReplyAll",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			var payload replyActionPayload
			client := testGraphClient(t, func(req *http.Request) *http.Response {
				calls++
				if req.Method != http.MethodPost {
					t.Errorf("request method = %q, want POST", req.Method)
				}
				if req.URL.Path != tc.wantPath {
					t.Errorf("request path = %q, want %q", req.URL.Path, tc.wantPath)
				}
				if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
					t.Fatalf("decode request body: %v", err)
				}
				return graphJSONResponse(req, `{"id":"draft-id","subject":"Re: Original subject","body":{"content":"quoted history"}}`)
			})

			draft, err := client.CreateReplyDraft(context.Background(), tc.target, "AAA", tc.content, tc.replyAll, tc.html)
			if err != nil {
				t.Fatalf("CreateReplyDraft: %v", err)
			}
			if calls != 1 {
				t.Fatalf("Graph requests = %d, want 1", calls)
			}
			if draft.ID != "draft-id" || draft.Subject != "Re: Original subject" || draft.Body != "quoted history" {
				t.Errorf("draft = %#v, want returned Graph draft fields", draft)
			}

			if tc.html {
				if payload.Comment != nil {
					t.Errorf("HTML draft sent comment %q alongside message.body", *payload.Comment)
				}
				if payload.Message == nil || payload.Message.Body == nil {
					t.Fatal("HTML draft omitted message.body")
				}
				if payload.Message.Body.ContentType != "html" {
					t.Errorf("content type = %q, want html", payload.Message.Body.ContentType)
				}
				if payload.Message.Body.Content != tc.content {
					t.Errorf("body content = %q, want exact %q", payload.Message.Body.Content, tc.content)
				}
			} else {
				if payload.Comment == nil || *payload.Comment != tc.content {
					t.Errorf("comment = %v, want exact %q", payload.Comment, tc.content)
				}
				if payload.Message != nil {
					t.Error("plain draft unexpectedly sent message.body")
				}
			}
		})
	}
}

func TestCreateReplyDraftCapabilityGuards(t *testing.T) {
	ctx := context.Background()

	noWrite := &Client{noWrite: true}
	if _, err := noWrite.CreateReplyDraft(ctx, "team@example.com", "AAA", "Thanks", false, false); !errors.Is(err, ErrNoWrite) {
		t.Fatalf("CreateReplyDraft under --no-write = %v, want ErrNoWrite", err)
	}

	calls := 0
	noSend := testGraphClient(t, func(req *http.Request) *http.Response {
		calls++
		return graphJSONResponse(req, `{"id":"draft-id","subject":"Re: Subject"}`)
	})
	noSend.SetGuards(false, true)
	if _, err := noSend.CreateReplyDraft(ctx, "", "AAA", "Thanks", false, false); err != nil {
		t.Fatalf("CreateReplyDraft under --no-send: %v", err)
	}
	if calls != 1 {
		t.Fatalf("CreateReplyDraft Graph requests under --no-send = %d, want 1", calls)
	}

	if err := noSend.ReplyMessage(ctx, "", "AAA", "Thanks", false, false); !errors.Is(err, ErrNoSend) {
		t.Fatalf("immediate ReplyMessage under --no-send = %v, want ErrNoSend", err)
	}
}

func TestCreateReplyDraftRejectsInvalidIDBeforeGraph(t *testing.T) {
	client := &Client{}
	if _, err := client.CreateReplyDraft(context.Background(), "team@example.com", "", "Thanks", false, false); err == nil || !strings.Contains(err.Error(), "message ID") {
		t.Fatalf("CreateReplyDraft invalid ID error = %v, want message ID validation", err)
	}
}

func TestCreateReplyDraftDelegatedErrors(t *testing.T) {
	tests := []struct {
		name       string
		code       string
		status     int
		message    string
		want       string
		wantAbsent []string
	}{
		{
			name:    "permission refusal gets draft-specific guidance",
			code:    "ErrorAccessDenied",
			status:  http.StatusForbidden,
			message: "Access is denied.",
			want:    "does not require Mail.Send.Shared, Send As, or Send on Behalf Of",
		},
		{
			name:       "stale ID gets mailbox-scoped ID guidance",
			code:       "ErrorItemNotFound",
			status:     http.StatusNotFound,
			message:    "The item could not be found.",
			want:       "message ID must be one listed from it",
			wantAbsent: []string{"Mail.ReadWrite.Shared", "Full Access"},
		},
		{
			name:       "throttle gets no permission or ID guidance",
			code:       "TooManyRequests",
			status:     http.StatusTooManyRequests,
			message:    "Too many requests.",
			want:       "creating reply draft in team@example.com",
			wantAbsent: []string{"Mail.ReadWrite.Shared", "Full Access", "IDs are scoped to a mailbox"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := testGraphClient(t, func(req *http.Request) *http.Response {
				body := `{"error":{"code":"` + tc.code + `","message":"` + tc.message + `"}}`
				return &http.Response{
					StatusCode:    tc.status,
					Status:        http.StatusText(tc.status),
					Header:        http.Header{"Content-Type": []string{"application/json"}},
					Body:          io.NopCloser(strings.NewReader(body)),
					Request:       req,
					ContentLength: int64(len(body)),
				}
			})

			_, err := client.CreateReplyDraft(context.Background(), "team@example.com", "AAA", "Thanks", false, false)
			if err == nil {
				t.Fatal("CreateReplyDraft error = nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error does not contain %q:\n%s", tc.want, err)
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(err.Error(), absent) {
					t.Errorf("error unexpectedly contains %q:\n%s", absent, err)
				}
			}
			if code, status := ErrorMetadata(err); code != tc.code || status != tc.status {
				t.Errorf("ErrorMetadata = (%q, %d), want (%q, %d)", code, status, tc.code, tc.status)
			}
		})
	}
}

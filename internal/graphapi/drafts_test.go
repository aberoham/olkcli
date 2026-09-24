package graphapi

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
)

func TestListDraftsReportsEveryRecipientList(t *testing.T) {
	client := testGraphClient(t, func(req *http.Request) *http.Response {
		wantPath := "/v1.0/users/team@example.com/mailFolders/drafts/messages"
		if req.Method != http.MethodGet || req.URL.Path != wantPath {
			t.Fatalf("request = %s %s, want GET %s", req.Method, req.URL.Path, wantPath)
		}
		selected := strings.Split(req.URL.Query().Get("$select"), ",")
		for _, field := range []string{"toRecipients", "ccRecipients", "bccRecipients"} {
			if !slices.Contains(selected, field) {
				t.Errorf("$select = %v, missing %s", selected, field)
			}
		}
		return graphJSONResponse(req, `{"value":[{"id":"draft-id","subject":"Re: Original subject",`+generatedReplyRecipients+`}]}`)
	})

	drafts, err := client.ListDrafts(context.Background(), "team@example.com", 5)
	if err != nil {
		t.Fatalf("ListDrafts: %v", err)
	}
	if len(drafts) != 1 {
		t.Fatalf("drafts = %d, want 1", len(drafts))
	}
	draft := drafts[0]
	if strings.Join(draft.To, ",") != "person@example.com" ||
		strings.Join(draft.Cc, ",") != "copied@example.com,second@example.com" ||
		strings.Join(draft.Bcc, ",") != "hidden@example.com" {
		t.Errorf("draft recipients = to %v cc %v bcc %v, want all three lists", draft.To, draft.Cc, draft.Bcc)
	}
}

package graphapi

import (
	"context"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestListMailFoldersTraversesVisibleChildren(t *testing.T) {
	requests := make([]string, 0, 3)
	client := testGraphClient(t, func(req *http.Request) *http.Response {
		requests = append(requests, req.URL.Path)
		if got := req.URL.Query().Get("$select"); !strings.Contains(got, "childFolderCount") || !strings.Contains(got, "parentFolderId") {
			t.Errorf("$select = %q, want traversal fields", got)
		}
		switch req.URL.Path {
		case "/v1.0/users/shared@example.com/mailFolders":
			return graphJSONResponse(req, `{"value":[
				{"id":"inbox-id","displayName":"Inbox","childFolderCount":1},
				{"id":"archive-id","displayName":"Archive","childFolderCount":0}
			]}`)
		case "/v1.0/users/shared@example.com/mailFolders/inbox-id/childFolders":
			return graphJSONResponse(req, `{"value":[
				{"id":"year-id","displayName":"2026","parentFolderId":"inbox-id","childFolderCount":1}
			]}`)
		case "/v1.0/users/shared@example.com/mailFolders/year-id/childFolders":
			return graphJSONResponse(req, `{"value":[
				{"id":"receipts-id","displayName":"Receipts","parentFolderId":"year-id","childFolderCount":0}
			]}`)
		default:
			t.Fatalf("unexpected Graph request: %s", req.URL)
			return nil
		}
	})

	folders, err := client.ListMailFolders(context.Background(), "shared@example.com")
	if err != nil {
		t.Fatalf("ListMailFolders() error = %v", err)
	}
	if got, want := requests, []string{
		"/v1.0/users/shared@example.com/mailFolders",
		"/v1.0/users/shared@example.com/mailFolders/inbox-id/childFolders",
		"/v1.0/users/shared@example.com/mailFolders/year-id/childFolders",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("request paths = %v, want %v", got, want)
	}
	if got, want := folderIDs(folders), []string{"inbox-id", "archive-id", "year-id", "receipts-id"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("folder IDs = %v, want breadth-first traversal %v", got, want)
	}
	if folders[2].ParentFolderID != "inbox-id" || folders[2].ChildFolderCount != 1 {
		t.Errorf("converted child folder = %#v, want parent and child count", folders[2])
	}
}

func TestListMailFoldersFollowsRootContinuation(t *testing.T) {
	requests := 0
	client := testGraphClient(t, func(req *http.Request) *http.Response {
		requests++
		if requests == 1 {
			next := "https://graph.microsoft.com/v1.0/users/shared@example.com/mailFolders?$skiptoken=next"
			return graphJSONResponse(req, `{"value":[{"id":"one","displayName":"One","childFolderCount":0}],"@odata.nextLink":"`+next+`"}`)
		}
		if got := req.URL.Query().Get("$skiptoken"); got != "next" {
			t.Errorf("continuation skip token = %q, want next", got)
		}
		return graphJSONResponse(req, `{"value":[{"id":"two","displayName":"Two","childFolderCount":0}]}`)
	})

	folders, err := client.ListMailFolders(context.Background(), "shared@example.com")
	if err != nil {
		t.Fatalf("ListMailFolders() error = %v", err)
	}
	if got, want := folderIDs(folders), []string{"one", "two"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("folder IDs = %v, want %v", got, want)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

func TestResolveMailFolderPathWalksDelegatedMailboxAndChildPages(t *testing.T) {
	requests := 0
	client := testGraphClient(t, func(req *http.Request) *http.Response {
		requests++
		switch requests {
		case 1:
			if got := req.URL.Path; got != "/v1.0/users/shared@example.com/mailFolders" {
				t.Errorf("root path = %q", got)
			}
			return graphJSONResponse(req, `{"value":[{"id":"inbox-id","displayName":"Inbox","childFolderCount":2}]}`)
		case 2:
			next := "https://graph.microsoft.com/v1.0/users/shared@example.com/mailFolders/inbox-id/childFolders?$skiptoken=next"
			return graphJSONResponse(req, `{"value":[{"id":"old-id","displayName":"2025","childFolderCount":0}],"@odata.nextLink":"`+next+`"}`)
		case 3:
			if got := req.URL.Query().Get("$skiptoken"); got != "next" {
				t.Errorf("child continuation skip token = %q, want next", got)
			}
			return graphJSONResponse(req, `{"value":[{"id":"year-id","displayName":"2026","childFolderCount":0}]}`)
		default:
			t.Fatalf("unexpected Graph request: %s", req.URL)
			return nil
		}
	})

	id, err := client.ResolveMailFolderPath(context.Background(), "shared@example.com", "inbox/2026")
	if err != nil {
		t.Fatalf("ResolveMailFolderPath() error = %v", err)
	}
	if id != "year-id" {
		t.Fatalf("resolved ID = %q, want year-id", id)
	}
	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
}

func TestResolveMailFolderPathPreservesSlashBearingGraphID(t *testing.T) {
	requests := 0
	client := testGraphClient(t, func(req *http.Request) *http.Response {
		requests++
		return graphJSONResponse(req, `{"value":[{"id":"inbox-id","displayName":"Inbox","childFolderCount":0}]}`)
	})

	const graphID = "AAMkAGVm/AAA="
	got, err := client.ResolveMailFolderPath(context.Background(), "", graphID)
	if err != nil {
		t.Fatalf("ResolveMailFolderPath() error = %v", err)
	}
	if got != graphID {
		t.Fatalf("resolved reference = %q, want original Graph ID %q", got, graphID)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want root lookup only", requests)
	}
}

func TestResolveMailFolderPathRejectsMissingAndAmbiguousComponents(t *testing.T) {
	tests := []struct {
		name     string
		children string
		want     string
	}{
		{
			name:     "missing child",
			children: `{"value":[]}`,
			want:     `component "2026" not found`,
		},
		{
			name: "ambiguous child",
			children: `{"value":[
				{"id":"one","displayName":"2026","childFolderCount":0},
				{"id":"two","displayName":"2026","childFolderCount":0}
			]}`,
			want: `folder name "2026" is ambiguous`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			client := testGraphClient(t, func(req *http.Request) *http.Response {
				requests++
				if requests == 1 {
					return graphJSONResponse(req, `{"value":[{"id":"inbox-id","displayName":"Inbox","childFolderCount":1}]}`)
				}
				return graphJSONResponse(req, tc.children)
			})
			id, err := client.ResolveMailFolderPath(context.Background(), "", "Inbox/2026")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ResolveMailFolderPath() = (%q, %v), want %q error", id, err, tc.want)
			}
		})
	}
}

func TestValidateGraphContinuationAllowsMailFolderCollections(t *testing.T) {
	tests := []struct {
		name  string
		url   string
		scope graphContinuationScope
	}{
		{
			name:  "root folders",
			url:   "https://graph.microsoft.com/v1.0/me/mailFolders?$skiptoken=next",
			scope: continuationScope("graph.microsoft.com", "/v1.0/me/mailFolders"),
		},
		{
			name: "delegated child folders",
			url:  "https://graph.microsoft.com/v1.0/users/shared@example.com/mailFolders/" + url.PathEscape("AAMk/AAA=") + "/childFolders?$skiptoken=next",
			scope: continuationScope(
				"graph.microsoft.com",
				"/v1.0/users/shared@example.com/mailFolders/"+url.PathEscape("AAMk/AAA=")+"/childFolders",
			),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateGraphContinuation(tc.url, tc.scope); err != nil {
				t.Fatalf("validateGraphContinuation() error = %v", err)
			}
		})
	}
}

// A folder write that ignores its target lands in the signed-in user's own
// mailbox and still reports success, so the request path is asserted in both
// directions for each of the three writes.
func TestMailFolderWritesAddressTheRequestedMailbox(t *testing.T) {
	const delegated = "/v1.0/users/shared@example.com"
	tests := []struct {
		name       string
		wantMethod string
		wantPath   string
		call       func(*Client, context.Context) error
	}{
		{
			name:       "create at the root of the caller's own mailbox",
			wantMethod: http.MethodPost,
			wantPath:   meBuilderPath + "/mailFolders",
			call: func(c *Client, ctx context.Context) error {
				_, err := c.CreateMailFolder(ctx, "", "", "Projects")
				return err
			},
		},
		{
			name:       "create at the root of a shared mailbox",
			wantMethod: http.MethodPost,
			wantPath:   delegated + "/mailFolders",
			call: func(c *Client, ctx context.Context) error {
				_, err := c.CreateMailFolder(ctx, "shared@example.com", "", "Projects")
				return err
			},
		},
		{
			name:       "create below a parent in a shared mailbox",
			wantMethod: http.MethodPost,
			wantPath:   delegated + "/mailFolders/year-id/childFolders",
			call: func(c *Client, ctx context.Context) error {
				_, err := c.CreateMailFolder(ctx, "shared@example.com", "year-id", "Projects")
				return err
			},
		},
		{
			name:       "create below a parent in the caller's own mailbox",
			wantMethod: http.MethodPost,
			wantPath:   meBuilderPath + "/mailFolders/year-id/childFolders",
			call: func(c *Client, ctx context.Context) error {
				_, err := c.CreateMailFolder(ctx, "", "year-id", "Projects")
				return err
			},
		},
		{
			name:       "rename in a shared mailbox",
			wantMethod: http.MethodPatch,
			wantPath:   delegated + "/mailFolders/year-id",
			call: func(c *Client, ctx context.Context) error {
				_, err := c.RenameMailFolder(ctx, "shared@example.com", "year-id", "Projects")
				return err
			},
		},
		{
			name:       "rename in the caller's own mailbox",
			wantMethod: http.MethodPatch,
			wantPath:   meBuilderPath + "/mailFolders/year-id",
			call: func(c *Client, ctx context.Context) error {
				_, err := c.RenameMailFolder(ctx, "", "year-id", "Projects")
				return err
			},
		},
		{
			name:       "delete from a shared mailbox",
			wantMethod: http.MethodDelete,
			wantPath:   delegated + "/mailFolders/year-id",
			call: func(c *Client, ctx context.Context) error {
				return c.DeleteMailFolder(ctx, "shared@example.com", "year-id")
			},
		},
		{
			name:       "delete from the caller's own mailbox",
			wantMethod: http.MethodDelete,
			wantPath:   meBuilderPath + "/mailFolders/year-id",
			call: func(c *Client, ctx context.Context) error {
				return c.DeleteMailFolder(ctx, "", "year-id")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotMethod, gotPath string
			client := testGraphClient(t, func(req *http.Request) *http.Response {
				gotMethod, gotPath = req.Method, req.URL.Path
				if req.Method == http.MethodDelete {
					return graphEmptyResponse(req)
				}
				return graphJSONResponse(req, `{"id":"folder-id","displayName":"Projects","parentFolderId":"year-id"}`)
			})
			if err := tc.call(client, context.Background()); err != nil {
				t.Fatalf("folder write: %v", err)
			}
			if gotMethod != tc.wantMethod || gotPath != tc.wantPath {
				t.Errorf("request = %s %q, want %s %q", gotMethod, gotPath, tc.wantMethod, tc.wantPath)
			}
		})
	}
}

func TestCreateMailFolderReturnsParent(t *testing.T) {
	client := testGraphClient(t, func(req *http.Request) *http.Response {
		return graphJSONResponse(req, `{"id":"folder-id","displayName":"Projects","parentFolderId":"year-id"}`)
	})
	folder, err := client.CreateMailFolder(context.Background(), "shared@example.com", "year-id", "Projects")
	if err != nil {
		t.Fatalf("CreateMailFolder: %v", err)
	}
	if folder.ID != "folder-id" || folder.DisplayName != "Projects" || folder.ParentFolderID != "year-id" {
		t.Errorf("folder = %#v, want ID, name and parent from the response", folder)
	}
}

// A refused folder write in another mailbox names the mailbox and the grants a
// folder write needs, which are not the sending ones.
func TestMailFolderWritesInSharedMailboxExplainRefusal(t *testing.T) {
	code, message := "ErrorAccessDenied", "Access is denied. Check credentials and try again."
	tests := []struct {
		name   string
		action string
		call   func(*Client, context.Context) error
	}{
		{"create", "creating mail folder in shared@example.com", func(c *Client, ctx context.Context) error {
			_, err := c.CreateMailFolder(ctx, "shared@example.com", "year-id", "Projects")
			return err
		}},
		{"rename", "renaming mail folder in shared@example.com", func(c *Client, ctx context.Context) error {
			_, err := c.RenameMailFolder(ctx, "shared@example.com", "year-id", "Projects")
			return err
		}},
		{"delete", "deleting mail folder in shared@example.com", func(c *Client, ctx context.Context) error {
			return c.DeleteMailFolder(ctx, "shared@example.com", "year-id")
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := testGraphClient(t, func(req *http.Request) *http.Response {
				return replyDraftErrorResponse(req, http.StatusForbidden, code, message)
			})
			err := tc.call(client, context.Background())
			if err == nil {
				t.Fatal("want an error")
			}
			text := err.Error()
			for _, want := range []string{tc.action, "Mail.ReadWrite.Shared", "Full Access"} {
				if !strings.Contains(text, want) {
					t.Errorf("error %q lacks %q", text, want)
				}
			}
			if strings.Contains(text, "Mail.Send.Shared") {
				t.Errorf("error %q carries sending guidance", text)
			}
			if gotCode, status := ErrorMetadata(err); gotCode != code || status != http.StatusForbidden {
				t.Errorf("ErrorMetadata = (%q, %d), want (%q, 403)", gotCode, status, code)
			}
		})
	}
}

func TestRenameAndDeleteMailFolderRefuseWellKnownFolders(t *testing.T) {
	client := testGraphClient(t, func(req *http.Request) *http.Response {
		t.Fatalf("unexpected Graph request: %s %s", req.Method, req.URL)
		return nil
	})
	ctx := context.Background()
	for _, name := range []string{"inbox", "Inbox", "archive", "deleteditems", "junkemail"} {
		if _, err := client.RenameMailFolder(ctx, "shared@example.com", name, "Projects"); err == nil ||
			!strings.Contains(err.Error(), "well-known") {
			t.Errorf("RenameMailFolder(%q) error = %v, want a well-known folder refusal", name, err)
		}
		if err := client.DeleteMailFolder(ctx, "", name); err == nil || !strings.Contains(err.Error(), "well-known") {
			t.Errorf("DeleteMailFolder(%q) error = %v, want a well-known folder refusal", name, err)
		}
	}
}

func folderIDs(folders []MailFolder) []string {
	ids := make([]string, len(folders))
	for i := range folders {
		ids[i] = folders[i].ID
	}
	return ids
}

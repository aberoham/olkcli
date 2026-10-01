package cmd

import (
	"net/http"
	"strings"
	"testing"
)

const (
	folderRootResponse  = `{"value":[{"id":"inbox-id","displayName":"Inbox","childFolderCount":1}]}`
	folderChildResponse = `{"value":[{"id":"year-id","displayName":"2026","childFolderCount":0}]}`
)

// runFolderCommandWithoutGraph runs a folder command that must be settled
// before any Graph request, failing the test if one is made.
func runFolderCommandWithoutGraph(t *testing.T, path, args []string) (string, error) {
	t.Helper()
	output, _, err := runMailCommand(t, path, args, func(req *http.Request) *http.Response {
		t.Fatalf("unexpected Graph request: %s %s", req.Method, req.URL)
		return nil
	})
	return output, err
}

func decodeFolderDisplayName(t *testing.T, req *http.Request) string {
	t.Helper()
	var payload struct {
		DisplayName string `json:"displayName"`
	}
	if err := decodeGraphJSON(req.Body, &payload); err != nil {
		t.Fatalf("decode folder request: %v", err)
	}
	return payload.DisplayName
}

// The defect these cover was silent: with --mailbox set, a folder was created in
// the signed-in user's own mailbox and the command reported success. Only the
// request path shows which mailbox a write reached, so each case asserts it.
func TestMailFoldersCreateAddressesTheRequestedMailbox(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		wantPath string
		wantOut  string
	}{
		{
			name:     "own mailbox",
			args:     []string{"--name", "Projects"},
			wantPath: "/v1.0/me/mailFolders",
			wantOut:  "Folder created: Projects (ID: new-id)\n",
		},
		{
			name:     "delegated mailbox",
			args:     []string{"--mailbox", "shared@example.com", "--name", "Projects"},
			wantPath: "/v1.0/users/shared@example.com/mailFolders",
			wantOut:  "Folder created in shared@example.com: Projects (ID: new-id)\n",
		},
		{
			name:     "below a well-known parent",
			args:     []string{"--name", "Projects", "--parent", "inbox"},
			wantPath: "/v1.0/me/mailFolders/inbox/childFolders",
			wantOut:  "Folder created: Projects (ID: new-id)\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath, gotMethod, gotName string
			output, calls, err := runMailCommand(t, []string{"mail", "folders", "create"}, tc.args,
				func(req *http.Request) *http.Response {
					gotPath, gotMethod = req.URL.Path, req.Method
					gotName = decodeFolderDisplayName(t, req)
					return graphJSONResponse(req, `{"id":"new-id","displayName":"Projects"}`)
				})
			if err != nil {
				t.Fatalf("mail folders create: %v", err)
			}
			if calls != 1 || gotMethod != http.MethodPost || gotPath != tc.wantPath {
				t.Fatalf("calls=%d method=%s path=%q, want one POST to %q", calls, gotMethod, gotPath, tc.wantPath)
			}
			if gotName != "Projects" {
				t.Errorf("displayName = %q, want Projects", gotName)
			}
			if output != tc.wantOut {
				t.Errorf("output = %q, want %q", output, tc.wantOut)
			}
		})
	}
}

func TestMailFoldersCreateResolvesParentPathInDelegatedMailbox(t *testing.T) {
	requests := 0
	output, calls, err := runMailCommand(
		t,
		[]string{"mail", "folders", "create"},
		[]string{"--mailbox", "shared@example.com", "--name", "11 Nov", "--parent", "Inbox/2026"},
		func(req *http.Request) *http.Response {
			requests++
			switch requests {
			case 1:
				if got := req.URL.Path; got != "/v1.0/users/shared@example.com/mailFolders" {
					t.Errorf("folder root request = %q", got)
				}
				return graphJSONResponse(req, folderRootResponse)
			case 2:
				if got := req.URL.Path; got != "/v1.0/users/shared@example.com/mailFolders/inbox-id/childFolders" {
					t.Errorf("child-folder request = %q", got)
				}
				return graphJSONResponse(req, folderChildResponse)
			case 3:
				if req.Method != http.MethodPost {
					t.Errorf("create method = %s, want POST", req.Method)
				}
				if got := req.URL.Path; got != "/v1.0/users/shared@example.com/mailFolders/year-id/childFolders" {
					t.Errorf("create request = %q, want the resolved parent in the target mailbox", got)
				}
				if got := decodeFolderDisplayName(t, req); got != "11 Nov" {
					t.Errorf("displayName = %q, want 11 Nov", got)
				}
				return graphJSONResponse(req, `{"id":"month-id","displayName":"11 Nov","parentFolderId":"year-id"}`)
			default:
				t.Fatalf("unexpected Graph request: %s", req.URL)
				return nil
			}
		},
	)
	if err != nil {
		t.Fatalf("mail folders create: %v", err)
	}
	if calls != 3 {
		t.Fatalf("Graph requests = %d, want 3", calls)
	}
	if want := "Folder created in shared@example.com: 11 Nov (ID: month-id)\n"; output != want {
		t.Errorf("output = %q, want %q", output, want)
	}
}

func TestMailFoldersCreateRefusesSlashInName(t *testing.T) {
	_, err := runFolderCommandWithoutGraph(t, []string{"mail", "folders", "create"},
		[]string{"--name", "Inbox/2026/11 Nov"})
	if err == nil || !strings.Contains(err.Error(), "--parent") {
		t.Fatalf("error = %v, want a refusal pointing at --parent", err)
	}
}

func TestMailFoldersCreateNamesMissingParentComponent(t *testing.T) {
	requests := 0
	_, _, err := runMailCommand(
		t,
		[]string{"mail", "folders", "create"},
		[]string{"--name", "11 Nov", "--parent", "Inbox/2027"},
		func(req *http.Request) *http.Response {
			requests++
			if req.Method != http.MethodGet {
				t.Fatalf("unexpected write with an unresolved parent: %s %s", req.Method, req.URL)
			}
			if requests == 1 {
				return graphJSONResponse(req, folderRootResponse)
			}
			return graphJSONResponse(req, folderChildResponse)
		},
	)
	if err == nil || !strings.Contains(err.Error(), `component "2027" not found`) {
		t.Fatalf("error = %v, want the missing component named", err)
	}
}

func TestMailFoldersDryRunNamesMailboxWithoutCallingGraph(t *testing.T) {
	for _, tc := range []struct {
		name    string
		path    []string
		args    []string
		wantOut string
	}{
		{
			name:    "create in own mailbox",
			path:    []string{"mail", "folders", "create"},
			args:    []string{"--name", "Projects"},
			wantOut: "Would create folder \"Projects\"\n",
		},
		{
			name:    "create below a parent in a delegated mailbox",
			path:    []string{"mail", "folders", "create"},
			args:    []string{"--mailbox", "shared@example.com", "--name", "11 Nov", "--parent", "Inbox/2026"},
			wantOut: "Would create folder \"11 Nov\" under Inbox/2026 in shared@example.com\n",
		},
		{
			name:    "rename in a delegated mailbox",
			path:    []string{"mail", "folders", "rename"},
			args:    []string{"--mailbox", "shared@example.com", "Inbox/Old", "--name", "2026"},
			wantOut: "Would rename folder Inbox/Old to \"2026\" in shared@example.com\n",
		},
		{
			name:    "delete in a delegated mailbox",
			path:    []string{"mail", "folders", "delete"},
			args:    []string{"--mailbox", "shared@example.com", "--force", "Inbox/2026/11 Nov"},
			wantOut: "Would delete folder Inbox/2026/11 Nov from shared@example.com\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"--dry-run"}, tc.args...)
			output, err := runFolderCommandWithoutGraph(t, tc.path, args)
			if err != nil {
				t.Fatalf("dry run: %v", err)
			}
			if output != tc.wantOut {
				t.Errorf("output = %q, want %q", output, tc.wantOut)
			}
		})
	}
}

func TestMailFolderWritesRejectInvalidMailbox(t *testing.T) {
	for _, tc := range []struct {
		name string
		path []string
		args []string
	}{
		{"create", []string{"mail", "folders", "create"}, []string{"--name", "Projects"}},
		{"rename", []string{"mail", "folders", "rename"}, []string{"folder-id", "--name", "Projects"}},
		{"delete", []string{"mail", "folders", "delete"}, []string{"folder-id", "--force"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"--mailbox", "not-an-address", "--dry-run"}, tc.args...)
			_, err := runFolderCommandWithoutGraph(t, tc.path, args)
			if err == nil || !strings.Contains(err.Error(), "--mailbox") {
				t.Fatalf("error = %v, want an invalid --mailbox refusal", err)
			}
		})
	}
}

func TestMailFoldersRenameResolvesPathInDelegatedMailbox(t *testing.T) {
	requests := 0
	output, calls, err := runMailCommand(
		t,
		[]string{"mail", "folders", "rename"},
		[]string{"--mailbox", "shared@example.com", "Inbox/2026", "--name", "Archive 2026"},
		func(req *http.Request) *http.Response {
			requests++
			switch requests {
			case 1:
				return graphJSONResponse(req, folderRootResponse)
			case 2:
				return graphJSONResponse(req, folderChildResponse)
			case 3:
				if req.Method != http.MethodPatch {
					t.Errorf("rename method = %s, want PATCH", req.Method)
				}
				if got := req.URL.Path; got != "/v1.0/users/shared@example.com/mailFolders/year-id" {
					t.Errorf("rename request = %q, want the resolved folder in the target mailbox", got)
				}
				if got := decodeFolderDisplayName(t, req); got != "Archive 2026" {
					t.Errorf("displayName = %q, want Archive 2026", got)
				}
				return graphJSONResponse(req, `{"id":"year-id","displayName":"Archive 2026"}`)
			default:
				t.Fatalf("unexpected Graph request: %s", req.URL)
				return nil
			}
		},
	)
	if err != nil {
		t.Fatalf("mail folders rename: %v", err)
	}
	if calls != 3 {
		t.Fatalf("Graph requests = %d, want 3", calls)
	}
	if want := "Folder renamed in shared@example.com: Archive 2026\n"; output != want {
		t.Errorf("output = %q, want %q", output, want)
	}
}

func TestMailFoldersRenameByIDInOwnMailbox(t *testing.T) {
	var gotPath, gotMethod string
	output, calls, err := runMailCommand(t, []string{"mail", "folders", "rename"},
		[]string{"folder-id", "--name", "Projects"},
		func(req *http.Request) *http.Response {
			gotPath, gotMethod = req.URL.Path, req.Method
			return graphJSONResponse(req, `{"id":"folder-id","displayName":"Projects"}`)
		})
	if err != nil {
		t.Fatalf("mail folders rename: %v", err)
	}
	if calls != 1 || gotMethod != http.MethodPatch || gotPath != "/v1.0/me/mailFolders/folder-id" {
		t.Fatalf("calls=%d method=%s path=%q, want one PATCH of the folder", calls, gotMethod, gotPath)
	}
	if want := "Folder renamed: Projects\n"; output != want {
		t.Errorf("output = %q, want %q", output, want)
	}
}

func TestMailFoldersRenameRefusesSlashInName(t *testing.T) {
	_, err := runFolderCommandWithoutGraph(t, []string{"mail", "folders", "rename"},
		[]string{"folder-id", "--name", "2026/Receipts"})
	if err == nil || !strings.Contains(err.Error(), `"/"`) {
		t.Fatalf("error = %v, want a slash refusal", err)
	}
}

func TestMailFoldersDeleteResolvesPathInDelegatedMailbox(t *testing.T) {
	requests := 0
	output, calls, err := runMailCommand(
		t,
		[]string{"mail", "folders", "delete"},
		[]string{"--mailbox", "shared@example.com", "--force", "Inbox/2026"},
		func(req *http.Request) *http.Response {
			requests++
			switch requests {
			case 1:
				return graphJSONResponse(req, folderRootResponse)
			case 2:
				return graphJSONResponse(req, folderChildResponse)
			case 3:
				if req.Method != http.MethodDelete {
					t.Errorf("delete method = %s, want DELETE", req.Method)
				}
				if got := req.URL.Path; got != "/v1.0/users/shared@example.com/mailFolders/year-id" {
					t.Errorf("delete request = %q, want the resolved folder in the target mailbox", got)
				}
				return graphNoContentResponse(req)
			default:
				t.Fatalf("unexpected Graph request: %s", req.URL)
				return nil
			}
		},
	)
	if err != nil {
		t.Fatalf("mail folders delete: %v", err)
	}
	if calls != 3 {
		t.Fatalf("Graph requests = %d, want 3", calls)
	}
	if want := "Folder deleted from shared@example.com.\n"; output != want {
		t.Errorf("output = %q, want %q", output, want)
	}
}

func TestMailFoldersDeleteByIDInOwnMailbox(t *testing.T) {
	var gotPath, gotMethod string
	output, calls, err := runMailCommand(t, []string{"mail", "folders", "delete"},
		[]string{"folder-id", "--force"},
		func(req *http.Request) *http.Response {
			gotPath, gotMethod = req.URL.Path, req.Method
			return graphNoContentResponse(req)
		})
	if err != nil {
		t.Fatalf("mail folders delete: %v", err)
	}
	if calls != 1 || gotMethod != http.MethodDelete || gotPath != "/v1.0/me/mailFolders/folder-id" {
		t.Fatalf("calls=%d method=%s path=%q, want one DELETE of the folder", calls, gotMethod, gotPath)
	}
	if want := "Folder deleted.\n"; output != want {
		t.Errorf("output = %q, want %q", output, want)
	}
}

func TestMailFoldersDeleteRefusesWithoutForce(t *testing.T) {
	_, err := runFolderCommandWithoutGraph(t, []string{"mail", "folders", "delete"},
		[]string{"--mailbox", "shared@example.com", "Inbox/2026"})
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("error = %v, want a --force refusal", err)
	}
}

func TestMailFoldersRenameAndDeleteRefuseWellKnownFolders(t *testing.T) {
	for _, tc := range []struct {
		name string
		path []string
		args []string
	}{
		{"rename inbox", []string{"mail", "folders", "rename"}, []string{"inbox", "--name", "Projects"}},
		{"rename by display case", []string{"mail", "folders", "rename"}, []string{"Inbox", "--name", "Projects"}},
		{"delete archive", []string{"mail", "folders", "delete"}, []string{"archive", "--force"}},
		{"delete deleted items", []string{"mail", "folders", "delete"}, []string{"deleteditems", "--force"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runFolderCommandWithoutGraph(t, tc.path, tc.args)
			if err == nil || !strings.Contains(err.Error(), "well-known") {
				t.Fatalf("error = %v, want a well-known folder refusal", err)
			}
		})
	}
}

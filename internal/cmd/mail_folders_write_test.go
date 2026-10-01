package cmd

import (
	"io"
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

// runFolderWrite runs a folder command against a mailbox holding Inbox/2026.
// Folder reads under mailboxPath are answered from that tree, in whatever order
// the command makes them, and every other request is handed to write, which
// stands in for the mutation under test.
func runFolderWrite(
	t *testing.T,
	mailboxPath string,
	path, args []string,
	write func(*http.Request) *http.Response,
) (string, error) {
	t.Helper()
	output, _, err := runMailCommand(t, path, args, func(req *http.Request) *http.Response {
		if req.Method == http.MethodGet {
			switch req.URL.Path {
			case mailboxPath + "/mailFolders":
				return graphJSONResponse(req, folderRootResponse)
			case mailboxPath + "/mailFolders/inbox-id/childFolders":
				return graphJSONResponse(req, folderChildResponse)
			}
			t.Fatalf("unexpected folder read: %s", req.URL)
		}
		return write(req)
	})
	return output, err
}

func graphErrorResponse(req *http.Request, status int, code, message string) *http.Response {
	body := `{"error":{"code":"` + code + `","message":"` + message + `"}}`
	return &http.Response{
		StatusCode:    status,
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          io.NopCloser(strings.NewReader(body)),
		Request:       req,
		ContentLength: int64(len(body)),
	}
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
	var gotMethod, gotPath, gotName string
	output, err := runFolderWrite(
		t,
		"/v1.0/users/shared@example.com",
		[]string{"mail", "folders", "create"},
		[]string{"--mailbox", "shared@example.com", "--name", "11 Nov", "--parent", "Inbox/2026"},
		func(req *http.Request) *http.Response {
			gotMethod, gotPath = req.Method, req.URL.Path
			gotName = decodeFolderDisplayName(t, req)
			return graphJSONResponse(req, `{"id":"month-id","displayName":"11 Nov","parentFolderId":"year-id"}`)
		},
	)
	if err != nil {
		t.Fatalf("mail folders create: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/v1.0/users/shared@example.com/mailFolders/year-id/childFolders" {
		t.Errorf("create request = %s %q, want a POST below the resolved parent in the target mailbox",
			gotMethod, gotPath)
	}
	if gotName != "11 Nov" {
		t.Errorf("displayName = %q, want 11 Nov", gotName)
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
	_, err := runFolderWrite(
		t,
		"/v1.0/me",
		[]string{"mail", "folders", "create"},
		[]string{"--name", "11 Nov", "--parent", "Inbox/2027"},
		func(req *http.Request) *http.Response {
			t.Fatalf("unexpected write with an unresolved parent: %s %s", req.Method, req.URL)
			return nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), `component "2027" not found`) {
		t.Fatalf("error = %v, want the missing component named", err)
	}
}

// A slash-bearing reference whose first component names no top-level folder is
// left unchanged, because a Graph ID may itself contain a slash. When Graph then
// rejects it, the error has to say that the value was treated as an ID, or a
// mistyped path reads as a malformed-ID failure with no way forward.
func TestMailFolderWritesExplainAnUnresolvedPath(t *testing.T) {
	for _, tc := range []struct {
		name string
		path []string
		args []string
	}{
		{"create", []string{"mail", "folders", "create"}, []string{"--name", "Projects", "--parent", "Missing/2026"}},
		{"rename", []string{"mail", "folders", "rename"}, []string{"Missing/2026", "--name", "Projects"}},
		{"delete", []string{"mail", "folders", "delete"}, []string{"Missing/2026", "--force"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runFolderWrite(t, "/v1.0/me", tc.path, tc.args, func(req *http.Request) *http.Response {
				return graphErrorResponse(req, http.StatusBadRequest, "ErrorInvalidIdMalformed", "Id is malformed.")
			})
			if err == nil {
				t.Fatal("want an error")
			}
			for _, want := range []string{"Id is malformed", `"Missing"`, "top-level folder"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q lacks %q", err, want)
				}
			}
		})
	}
}

// An unresolved path that is not a well-formed ID is rejected before any write.
// The explanation still applies, and must not say that a request was made.
func TestMailFolderWritesExplainAnUnresolvedPathRejectedLocally(t *testing.T) {
	_, err := runFolderWrite(
		t,
		"/v1.0/me",
		[]string{"mail", "folders", "delete"},
		[]string{"Missing/11 Nov", "--force"},
		func(req *http.Request) *http.Response {
			t.Fatalf("unexpected write for a malformed folder ID: %s %s", req.Method, req.URL)
			return nil
		},
	)
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"invalid characters", `"Missing"`, "treated as a folder ID"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "sent to Graph") {
		t.Errorf("error %q claims a request that was never made", err)
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
	var gotMethod, gotPath, gotName string
	output, err := runFolderWrite(
		t,
		"/v1.0/users/shared@example.com",
		[]string{"mail", "folders", "rename"},
		[]string{"--mailbox", "shared@example.com", "Inbox/2026", "--name", "Archive 2026"},
		func(req *http.Request) *http.Response {
			gotMethod, gotPath = req.Method, req.URL.Path
			gotName = decodeFolderDisplayName(t, req)
			return graphJSONResponse(req, `{"id":"year-id","displayName":"Archive 2026"}`)
		},
	)
	if err != nil {
		t.Fatalf("mail folders rename: %v", err)
	}
	if gotMethod != http.MethodPatch || gotPath != "/v1.0/users/shared@example.com/mailFolders/year-id" {
		t.Errorf("rename request = %s %q, want a PATCH of the resolved folder in the target mailbox",
			gotMethod, gotPath)
	}
	if gotName != "Archive 2026" {
		t.Errorf("displayName = %q, want Archive 2026", gotName)
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
	var gotMethod, gotPath string
	output, err := runFolderWrite(
		t,
		"/v1.0/users/shared@example.com",
		[]string{"mail", "folders", "delete"},
		[]string{"--mailbox", "shared@example.com", "--force", "Inbox/2026"},
		func(req *http.Request) *http.Response {
			gotMethod, gotPath = req.Method, req.URL.Path
			return graphNoContentResponse(req)
		},
	)
	if err != nil {
		t.Fatalf("mail folders delete: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/v1.0/users/shared@example.com/mailFolders/year-id" {
		t.Errorf("delete request = %s %q, want a DELETE of the resolved folder in the target mailbox",
			gotMethod, gotPath)
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

func TestMailFoldersRenameAndDeleteRefuseWellKnownNames(t *testing.T) {
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

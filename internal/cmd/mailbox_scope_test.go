package cmd

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	scopeFilePlaceholder = "{file}"
	scopeDirPlaceholder  = "{dir}"

	// scopeGenericResponse parses as any single Graph entity these commands read
	// back, and as a one-item collection of the same.
	scopeGenericResponse = `{"id":"item-id","displayName":"Name","value":[{"id":"item-id","displayName":"Name"}]}`

	// scopeEventResponse carries an HTML body, which updating an event body reads
	// first so that it can keep an online meeting's join details.
	scopeEventResponse = `{"id":"event-id","subject":"Review","body":{"contentType":"html","content":"<p>Old</p>"}}`

	scopeFileAttachmentResponse = `{"@odata.type":"#microsoft.graph.fileAttachment","id":"attachment-id",` +
		`"name":"notes.txt","contentType":"text/plain","size":1,"contentBytes":"eA=="}`
	scopeTaskAttachmentResponse = `{"@odata.type":"#microsoft.graph.taskFileAttachment","id":"attachment-id",` +
		`"name":"notes.txt","contentType":"text/plain","size":1,"contentBytes":"eA=="}`
)

// mailboxScopedCommand is one command that reads or writes mailbox-scoped data.
// dryRun marks the commands that implement --dry-run.
type mailboxScopedCommand struct {
	path     []string
	args     []string
	response string
	dryRun   bool
}

// mailboxScopedCommands lists every command that used to address the signed-in
// user's own mailbox whatever --mailbox said. Each must now send every request
// to the mailbox it was given.
var mailboxScopedCommands = []mailboxScopedCommand{
	{path: []string{"mail", "mark"}, args: []string{"message-id", "--read"}},
	{path: []string{"mail", "flag"}, args: []string{"message-id", "flagged"}, dryRun: true},
	{path: []string{"mail", "categorize"}, args: []string{"message-id", "--categories", "green"}, dryRun: true},
	{path: []string{"mail", "importance"}, args: []string{"message-id", "high"}, dryRun: true},
	{path: []string{"mail", "rules", "list"}},
	{
		path:   []string{"mail", "rules", "create"},
		args:   []string{"--name", "Rule", "--from", "sender@example.com", "--mark-read"},
		dryRun: true,
	},
	{path: []string{"mail", "rules", "delete"}, args: []string{"rule-id", "--force"}},
	{path: []string{"mail", "categories", "list"}},
	{path: []string{"mail", "categories", "create"}, args: []string{"--name", "Green"}, dryRun: true},
	{path: []string{"mail", "categories", "delete"}, args: []string{"category-id", "--force"}, dryRun: true},
	{path: []string{"mail", "ooo", "get"}},
	{path: []string{"mail", "ooo", "set"}, args: []string{"--message", "Away"}, dryRun: true},
	{path: []string{"mail", "ooo", "off"}},

	{
		path:   []string{"calendar", "create"},
		args:   []string{"--subject", "Review", "--start", "2026-11-02T10:00:00Z", "--end", "2026-11-02T11:00:00Z"},
		dryRun: true,
	},
	{
		path: []string{"calendar", "create"},
		args: []string{
			"--subject", "Review", "--start", "2026-11-02T10:00:00Z", "--end", "2026-11-02T11:00:00Z",
			"--calendar", "calendar-id",
		},
	},
	{path: []string{"calendar", "update"}, args: []string{"event-id", "--subject", "Review"}},
	{
		path:     []string{"calendar", "update"},
		args:     []string{"event-id", "--body", "Agenda"},
		response: scopeEventResponse,
	},
	{path: []string{"calendar", "delete"}, args: []string{"event-id", "--force"}},
	{path: []string{"calendar", "respond"}, args: []string{"event-id", "accept"}},
	{path: []string{"calendar", "attachments", "list"}, args: []string{"event-id"}},
	{
		path:   []string{"calendar", "attachments", "add"},
		args:   []string{"event-id", scopeFilePlaceholder},
		dryRun: true,
	},
	{
		path:     []string{"calendar", "attachments", "download"},
		args:     []string{"event-id", "attachment-id", "--out", scopeDirPlaceholder},
		response: scopeFileAttachmentResponse,
	},
	{
		path:   []string{"calendar", "attachments", "delete"},
		args:   []string{"event-id", "attachment-id", "--force"},
		dryRun: true,
	},

	{
		path:   []string{"contacts", "create"},
		args:   []string{"--first-name", "Sample", "--last-name", "Contact"},
		dryRun: true,
	},
	{path: []string{"contacts", "update"}, args: []string{"contact-id", "--company", "Example"}},
	{path: []string{"contacts", "update"}, args: []string{"contact-id", "--city", "London"}},
	{path: []string{"contacts", "delete"}, args: []string{"contact-id", "--force"}},

	{path: []string{"todo", "lists", "list"}},
	{path: []string{"todo", "lists", "create"}, args: []string{"--name", "Work"}, dryRun: true},
	{path: []string{"todo", "lists", "delete"}, args: []string{"list-id", "--force"}, dryRun: true},
	{path: []string{"todo", "list"}, args: []string{"--list", "list-id"}},
	{path: []string{"todo", "list"}},
	{path: []string{"todo", "get"}, args: []string{"task-id", "--list", "list-id"}},
	{path: []string{"todo", "create"}, args: []string{"--title", "Task", "--list", "list-id"}, dryRun: true},
	{path: []string{"todo", "complete"}, args: []string{"task-id", "--list", "list-id"}, dryRun: true},
	{
		path:   []string{"todo", "update"},
		args:   []string{"task-id", "--title", "New", "--list", "list-id"},
		dryRun: true,
	},
	{path: []string{"todo", "delete"}, args: []string{"task-id", "--force", "--list", "list-id"}, dryRun: true},
	{path: []string{"todo", "checklist", "list"}, args: []string{"task-id", "--list", "list-id"}},
	{
		path:   []string{"todo", "checklist", "create"},
		args:   []string{"task-id", "--name", "Step", "--list", "list-id"},
		dryRun: true,
	},
	{
		path:   []string{"todo", "checklist", "toggle"},
		args:   []string{"task-id", "item-id", "--list", "list-id"},
		dryRun: true,
	},
	{
		path:   []string{"todo", "checklist", "update"},
		args:   []string{"task-id", "item-id", "--name", "Step", "--list", "list-id"},
		dryRun: true,
	},
	{
		path:   []string{"todo", "checklist", "delete"},
		args:   []string{"task-id", "item-id", "--force", "--list", "list-id"},
		dryRun: true,
	},
	{path: []string{"todo", "attach", "list"}, args: []string{"task-id", "--list", "list-id"}},
	{
		path:   []string{"todo", "attach", "upload"},
		args:   []string{"task-id", scopeFilePlaceholder, "--list", "list-id"},
		dryRun: true,
	},
	{
		path:     []string{"todo", "attach", "download"},
		args:     []string{"task-id", "attachment-id", "--out", scopeDirPlaceholder, "--list", "list-id"},
		response: scopeTaskAttachmentResponse,
	},
	{
		path:   []string{"todo", "attach", "delete"},
		args:   []string{"task-id", "attachment-id", "--force", "--list", "list-id"},
		dryRun: true,
	},
	{path: []string{"todo", "links", "list"}, args: []string{"task-id", "--list", "list-id"}},
	{
		path:   []string{"todo", "links", "create"},
		args:   []string{"task-id", "--name", "Ticket", "--list", "list-id"},
		dryRun: true,
	},
	{
		path:   []string{"todo", "links", "delete"},
		args:   []string{"task-id", "resource-id", "--force", "--list", "list-id"},
		dryRun: true,
	},
}

func (c mailboxScopedCommand) name() string {
	return strings.Join(c.path, " ") + " " + strings.Join(c.args, " ")
}

// resolvedArgs replaces the file and directory placeholders with paths inside a
// temporary directory owned by the test.
func (c mailboxScopedCommand) resolvedArgs(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	args := make([]string, 0, len(c.args))
	for _, arg := range c.args {
		switch arg {
		case scopeFilePlaceholder:
			file := filepath.Join(dir, "notes.txt")
			if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
				t.Fatalf("writing fixture file: %v", err)
			}
			arg = file
		case scopeDirPlaceholder:
			arg = dir
		}
		args = append(args, arg)
	}
	return args
}

// runScopedCommand runs the command and returns the path of every Graph request
// it made.
func runScopedCommand(
	t *testing.T,
	c mailboxScopedCommand,
	extra ...string,
) (paths []string, output string, err error) {
	t.Helper()
	args := append(c.resolvedArgs(t), extra...)
	output, _, err = runMailCommand(t, c.path, args, func(req *http.Request) *http.Response {
		paths = append(paths, req.URL.Path)
		if req.Method == http.MethodDelete {
			return graphNoContentResponse(req)
		}
		body := c.response
		if body == "" {
			body = scopeGenericResponse
		}
		response := graphJSONResponse(req, body)
		// Graph confirms an honoured Prefer header; the event read checks for it.
		if prefer := req.Header.Get("Prefer"); prefer != "" {
			response.Header.Set("Preference-Applied", prefer)
		}
		return response
	})
	return paths, output, err
}

// A command that ignores --mailbox acts on the signed-in user's own mailbox and
// reports success, so only the request paths show which mailbox it reached.
// Every request counts, reads included: a lookup made in one mailbox yields IDs
// that the following write cannot use in another.
func TestMailboxScopedCommandsAddressTheRequestedMailbox(t *testing.T) {
	for _, tc := range []struct {
		name   string
		extra  []string
		prefix string
	}{
		{"own mailbox", nil, "/v1.0/me/"},
		{"delegated mailbox", []string{"--mailbox", "shared@example.com"}, "/v1.0/users/shared@example.com/"},
	} {
		for _, command := range mailboxScopedCommands {
			t.Run(tc.name+"/"+command.name(), func(t *testing.T) {
				paths, _, err := runScopedCommand(t, command, tc.extra...)
				if err != nil {
					t.Fatalf("command failed: %v", err)
				}
				if len(paths) == 0 {
					t.Fatal("command made no Graph request")
				}
				for _, path := range paths {
					if !strings.HasPrefix(path, tc.prefix) {
						t.Errorf("request path %q, want every request under %q", path, tc.prefix)
					}
				}
			})
		}
	}
}

func TestMailboxScopedCommandsRejectInvalidMailbox(t *testing.T) {
	for _, command := range mailboxScopedCommands {
		t.Run(command.name(), func(t *testing.T) {
			paths, _, err := runScopedCommand(t, command, "--mailbox", "not-an-address")
			if err == nil || !strings.Contains(err.Error(), "--mailbox") {
				t.Fatalf("error = %v, want an invalid --mailbox refusal", err)
			}
			if len(paths) != 0 {
				t.Fatalf("requests made before the refusal: %v", paths)
			}
		})
	}
}

func TestMailboxScopedDryRunsNameTheMailbox(t *testing.T) {
	for _, command := range mailboxScopedCommands {
		if !command.dryRun {
			continue
		}
		t.Run(command.name(), func(t *testing.T) {
			paths, output, err := runScopedCommand(t, command, "--dry-run", "--mailbox", "shared@example.com")
			if err != nil {
				t.Fatalf("dry run: %v", err)
			}
			if len(paths) != 0 {
				t.Fatalf("dry run made requests: %v", paths)
			}
			if !strings.Contains(output, "shared@example.com") {
				t.Errorf("dry run output %q does not name the mailbox", output)
			}
		})
	}
}

func TestMailboxScopedDryRunsWithoutAMailboxAreUnchanged(t *testing.T) {
	for _, command := range mailboxScopedCommands {
		if !command.dryRun {
			continue
		}
		t.Run(command.name(), func(t *testing.T) {
			_, output, err := runScopedCommand(t, command, "--dry-run")
			if err != nil {
				t.Fatalf("dry run: %v", err)
			}
			if strings.Contains(output, ownMailboxLabel) || strings.Contains(output, "Mailbox:") {
				t.Errorf("dry run output %q names a mailbox although none was given", output)
			}
		})
	}
}

package cmd

import (
	"net/http"
	"strings"
	"testing"
)

var mailboxUnawareMailWrites = []struct {
	name string
	path []string
	args []string
}{
	{"mail mark", []string{"mail", "mark"}, []string{"message-id", "--read"}},
	{"mail flag", []string{"mail", "flag"}, []string{"message-id", "flagged"}},
	{"mail categorize", []string{"mail", "categorize"}, []string{"message-id", "--categories", "green"}},
	{"mail importance", []string{"mail", "importance"}, []string{"message-id", "high"}},
}

// These commands address the signed-in user's own mailbox whatever --mailbox
// says. Run with the flag set they must stop before any request and say which
// mailbox they could not act on, including for a dry run, which would otherwise
// describe a write that was never going to reach the named mailbox.
func TestMailboxUnawareMailWritesRefuseAMailboxTarget(t *testing.T) {
	for _, tc := range mailboxUnawareMailWrites {
		for _, extra := range [][]string{nil, {"--dry-run"}} {
			name := tc.name
			if extra != nil {
				name += " " + extra[0]
			}
			t.Run(name, func(t *testing.T) {
				args := append([]string{"--mailbox", "shared@example.com"}, tc.args...)
				args = append(args, extra...)
				output, calls, err := runMailCommand(t, tc.path, args, func(req *http.Request) *http.Response {
					t.Fatalf("unexpected Graph request: %s %s", req.Method, req.URL)
					return nil
				})
				if err == nil {
					t.Fatalf("want a refusal, got output %q", output)
				}
				if calls != 0 {
					t.Fatalf("Graph requests = %d, want 0", calls)
				}
				for _, want := range []string{tc.name, "--mailbox", "shared@example.com", "your own mailbox"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("refusal %q lacks %q", err, want)
					}
				}
				if output != "" {
					t.Errorf("refused command printed %q", output)
				}
			})
		}
	}
}

func TestMailboxUnawareMailWritesRefuseAMailboxFromTheEnvironment(t *testing.T) {
	t.Setenv("OLK_MAILBOX", "shared@example.com")
	for _, tc := range mailboxUnawareMailWrites {
		t.Run(tc.name, func(t *testing.T) {
			_, calls, err := runMailCommand(t, tc.path, tc.args, func(req *http.Request) *http.Response {
				t.Fatalf("unexpected Graph request: %s %s", req.Method, req.URL)
				return nil
			})
			if err == nil || !strings.Contains(err.Error(), "shared@example.com") || calls != 0 {
				t.Fatalf("error=%v calls=%d, want a refusal naming the mailbox before any request", err, calls)
			}
		})
	}
}

func TestMailboxUnawareMailWritesStillActOnOwnMailbox(t *testing.T) {
	for _, tc := range mailboxUnawareMailWrites {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath, gotMethod string
			_, calls, err := runMailCommand(t, tc.path, tc.args, func(req *http.Request) *http.Response {
				gotPath, gotMethod = req.URL.Path, req.Method
				return graphJSONResponse(req, `{"id":"message-id"}`)
			})
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if calls != 1 || gotMethod != http.MethodPatch || gotPath != "/v1.0/me/messages/message-id" {
				t.Fatalf("calls=%d method=%s path=%q, want one PATCH of the caller's own message",
					calls, gotMethod, gotPath)
			}
		})
	}
}

package cmd

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/rlrghb/olkcli/internal/graphapi"
)

func TestMailReplyDraftCommandRoutesFormatsAndReportsDraft(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantPath    string
		wantComment string
		wantHTML    string
		wantOutput  string
	}{
		{
			name:        "plain reply in own mailbox",
			args:        []string{"AAA", "--body", "Thanks", "--draft"},
			wantPath:    "/v1.0/me/messages/AAA/createReply",
			wantComment: "Thanks",
			wantOutput:  "Reply draft created in your own mailbox: Re: Original subject (ID: draft-id)\n",
		},
		{
			name:       "HTML reply-all in own mailbox",
			args:       []string{"AAA", "--body", "<p>Thanks all</p>", "--reply-all", "--html", "--draft"},
			wantPath:   "/v1.0/me/messages/AAA/createReplyAll",
			wantHTML:   "<p>Thanks all</p>",
			wantOutput: "Reply-all draft created in your own mailbox: Re: Original subject (ID: draft-id)\n",
		},
		{
			name:       "HTML reply in delegated mailbox",
			args:       []string{"AAA", "--body", "<p>Thanks</p>", "--html", "--draft", "--mailbox", "team@example.com"},
			wantPath:   "/v1.0/users/team@example.com/messages/AAA/createReply",
			wantHTML:   "<p>Thanks</p>",
			wantOutput: "Reply draft created in team@example.com: Re: Original subject (ID: draft-id)\n",
		},
		{
			name:        "plain reply-all in delegated mailbox",
			args:        []string{"AAA", "--body", "Thanks all", "--reply-all", "--draft", "--mailbox", "team@example.com"},
			wantPath:    "/v1.0/users/team@example.com/messages/AAA/createReplyAll",
			wantComment: "Thanks all",
			wantOutput:  "Reply-all draft created in team@example.com: Re: Original subject (ID: draft-id)\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			output, calls, err := runMailCommand(t, []string{"mail", "reply"}, tc.args, func(req *http.Request) *http.Response {
				if req.Method != http.MethodPost {
					t.Errorf("request method = %q, want POST", req.Method)
				}
				if req.URL.Path != tc.wantPath {
					t.Errorf("request path = %q, want %q", req.URL.Path, tc.wantPath)
				}
				var payload struct {
					Comment *string `json:"comment"`
					Message *struct {
						Body *struct {
							ContentType string `json:"contentType"`
							Content     string `json:"content"`
						} `json:"body"`
					} `json:"message"`
				}
				if err := decodeGraphJSON(req.Body, &payload); err != nil {
					t.Fatalf("decode Graph request: %v", err)
				}
				if tc.wantHTML != "" {
					if payload.Comment != nil {
						t.Errorf("HTML draft sent comment %q", *payload.Comment)
					}
					if payload.Message == nil || payload.Message.Body == nil {
						t.Fatal("HTML draft omitted message.body")
					}
					if payload.Message.Body.ContentType != "html" || payload.Message.Body.Content != tc.wantHTML {
						t.Errorf("HTML body = %#v, want exact HTML content", payload.Message.Body)
					}
				} else {
					if payload.Comment == nil || *payload.Comment != tc.wantComment {
						t.Errorf("comment = %v, want %q", payload.Comment, tc.wantComment)
					}
					if payload.Message != nil {
						t.Error("plain draft unexpectedly sent message.body")
					}
				}
				return graphJSONResponse(req, `{"id":"draft-id","subject":"Re: Original subject"}`)
			})
			if err != nil {
				t.Fatalf("mail reply --draft: %v", err)
			}
			if calls != 1 {
				t.Fatalf("Graph requests = %d, want 1", calls)
			}
			if output != tc.wantOutput {
				t.Fatalf("output = %q, want %q", output, tc.wantOutput)
			}
			if strings.Contains(output, "sent") {
				t.Fatalf("draft output used sent-success wording: %q", output)
			}
		})
	}
}

func TestMailReplyDraftDryRunExactOutputAndZeroGraphRequests(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "plain reply draft",
			args: []string{"AAA", "--body", "Thanks", "--draft", "--dry-run"},
			want: "Would create reply draft for message AAA in your own mailbox\n",
		},
		{
			name: "HTML reply draft",
			args: []string{"AAA", "--body", "<p>Thanks</p>", "--html", "--draft", "--dry-run"},
			want: "Would create reply draft for message AAA in your own mailbox\n",
		},
		{
			name: "delegated HTML reply-all draft",
			args: []string{"AAA", "--body", "<p>Thanks all</p>", "--reply-all", "--html", "--draft", "--mailbox", "team@example.com", "--dry-run"},
			want: "Would create reply-all draft for message AAA in team@example.com\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			output, calls, err := runMailCommand(t, []string{"mail", "reply"}, tc.args, func(req *http.Request) *http.Response {
				t.Errorf("unexpected Graph request during dry run: %s %s", req.Method, req.URL.Path)
				return graphJSONResponse(req, `{}`)
			})
			if err != nil {
				t.Fatalf("mail reply --draft --dry-run: %v", err)
			}
			if calls != 0 {
				t.Fatalf("Graph requests = %d, want 0", calls)
			}
			if output != tc.want {
				t.Fatalf("output = %q, want %q", output, tc.want)
			}
		})
	}
}

func TestMailReplyDraftCommandCapabilityGuards(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantErr   error
		wantCalls int
	}{
		{
			name:    "no-write blocks draft creation",
			args:    []string{"AAA", "--body", "Thanks", "--draft", "--no-write"},
			wantErr: graphapi.ErrNoWrite,
		},
		{
			name:      "no-send allows draft creation",
			args:      []string{"AAA", "--body", "Thanks", "--draft", "--no-send"},
			wantCalls: 1,
		},
		{
			name:    "no-send still blocks immediate reply",
			args:    []string{"AAA", "--body", "Thanks", "--no-send"},
			wantErr: graphapi.ErrNoSend,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, calls, err := runGuardedMailReplyCommand(t, tc.args, func(req *http.Request) *http.Response {
				return graphJSONResponse(req, `{"id":"draft-id","subject":"Re: Subject"}`)
			})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("command error = %v, want %v", err, tc.wantErr)
			}
			if calls != tc.wantCalls {
				t.Fatalf("Graph requests = %d, want %d", calls, tc.wantCalls)
			}
		})
	}
}

func runGuardedMailReplyCommand(
	t *testing.T,
	args []string,
	responder func(*http.Request) *http.Response,
) (output string, calls int, err error) {
	t.Helper()
	client := testMailListClient(t, func(req *http.Request) *http.Response {
		calls++
		return responder(req)
	})
	cli := &CLI{}
	parser, err := newKongParser(cli)
	if err != nil {
		return "", calls, err
	}
	kctx, err := parser.Parse(append([]string{"mail", "reply"}, args...))
	if err != nil {
		return "", calls, err
	}
	client.SetGuards(cli.NoWrite, cli.NoSend)
	output, _, err = captureStd(func() error {
		return kctx.Run(&RunContext{Ctx: context.Background(), Flags: &cli.RootFlags, client: client})
	})
	return output, calls, err
}

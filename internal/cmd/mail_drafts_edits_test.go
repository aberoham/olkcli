package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rlrghb/olkcli/internal/graphapi"
)

func TestDraftUpdateCommandClearAndOmit(t *testing.T) {
	output, calls, err := runDraftEditCommand(t, []string{"mail", "drafts", "update"}, []string{"id", "--cc=", "--mailbox", "shared@example.com", "--json", "--no-send"}, func(req *http.Request) *http.Response {
		if req.URL.Path != "/v1.0/users/shared@example.com/messages/id" {
			t.Fatalf("path=%s", req.URL.Path)
		}
		if req.Method == http.MethodGet {
			return graphJSONResponse(req, `{"isDraft":true}`)
		}
		if req.Method != http.MethodPatch {
			t.Fatalf("method=%s", req.Method)
		}
		var payload map[string]json.RawMessage
		if err := decodeGraphJSON(req.Body, &payload); err != nil {
			t.Fatal(err)
		}
		if len(payload) != 2 || string(payload["ccRecipients"]) != "[]" {
			t.Fatalf("payload=%s", payload)
		}
		return graphJSONResponse(req, `{"id":"id"}`)
	})
	if err != nil || calls != 2 {
		t.Fatalf("error=%v calls=%d", err, calls)
	}
	var envelope struct {
		Results struct {
			ID         string                   `json:"id"`
			Recipients graphapi.DraftRecipients `json:"recipients"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Results.ID != "id" || envelope.Results.Recipients.CC == nil || envelope.Results.Recipients.To != nil {
		t.Fatalf("output=%s", output)
	}
}

func TestDraftEditCommandDryRunsAndValidation(t *testing.T) {
	file := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(file, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, sub string
		args      []string
		wantError bool
	}{
		{"update dry run", "update", []string{"id", "--to", "to@example.com", "--dry-run", "--json"}, false},
		{"attach dry run", "attach", []string{"id", file, "--dry-run", "--json"}, false},
		{"empty update", "update", []string{"id"}, true},
		{"invalid recipient", "update", []string{"id", "--to", "invalid", "--dry-run"}, true},
		{"invalid update mailbox", "update", []string{"id", "--cc=", "--mailbox", "invalid"}, true},
		{"invalid attach mailbox", "attach", []string{"id", file, "--mailbox", "invalid"}, true},
		{"guarded update", "update", []string{"id", "--cc=", "--no-write"}, true},
		{"guarded attachment", "attach", []string{"id", file, "--no-write"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, calls, err := runDraftEditCommand(t, []string{"mail", "drafts", tc.sub}, tc.args, func(req *http.Request) *http.Response { t.Fatalf("unexpected Graph request: %s", req.URL); return nil })
			if (err != nil) != tc.wantError || calls != 0 {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
			if !tc.wantError && (!json.Valid([]byte(output)) || !strings.Contains(output, `"dryRun": true`)) {
				t.Fatalf("output=%s", output)
			}
		})
	}
}

func TestDraftAttachCommandOutputsJSONAndPlain(t *testing.T) {
	file := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(file, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"--json", "--plain"} {
		output, calls, err := runDraftEditCommand(t, []string{"mail", "drafts", "attach"}, []string{"id", file, "--mailbox", "shared@example.com", format, "--no-send"}, func(req *http.Request) *http.Response {
			if req.Method == http.MethodGet && req.URL.Path == "/v1.0/users/shared@example.com/messages/id" {
				return graphJSONResponse(req, `{"isDraft":true}`)
			}
			if req.Method != http.MethodPost || req.URL.Path != "/v1.0/users/shared@example.com/messages/id/attachments" {
				t.Fatalf("request=%s %s", req.Method, req.URL)
			}
			return graphJSONResponse(req, `{"id":"attachment-id","name":"report.txt","size":5}`)
		})
		if err != nil || calls != 2 {
			t.Fatalf("error=%v calls=%d", err, calls)
		}
		if format == "--json" && !json.Valid([]byte(output)) {
			t.Fatalf("invalid JSON: %s", output)
		}
		if format == "--plain" && !strings.Contains(output, "id\tshared@example.com\tattachment-id\treport.txt\t5\tfalse") {
			t.Fatalf("plain output=%q", output)
		}
	}
}

func TestDraftAttachRejectsSizeBoundaryAndDirectory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "large")
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(graphapi.MaxInlineAttachmentBytes); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{file, dir} {
		_, calls, err := runDraftEditCommand(t, []string{"mail", "drafts", "attach"}, []string{"id", path, "--dry-run"}, func(req *http.Request) *http.Response { t.Fatal("unexpected request"); return nil })
		if err == nil || calls != 0 {
			t.Fatalf("error=%v calls=%d", err, calls)
		}
	}
}

func runDraftEditCommand(t *testing.T, path, args []string, responder func(*http.Request) *http.Response) (output string, calls int, err error) {
	t.Helper()
	client := testMailListClient(t, func(req *http.Request) *http.Response { calls++; return responder(req) })
	cli := &CLI{}
	parser, err := newKongParser(cli)
	if err != nil {
		return "", calls, err
	}
	kctx, err := parser.Parse(append(path, args...))
	if err != nil {
		return "", calls, err
	}
	client.SetGuards(cli.NoWrite, cli.NoSend)
	output, _, err = captureStd(func() error {
		return kctx.Run(&RunContext{Ctx: context.Background(), Flags: &cli.RootFlags, client: client})
	})
	return output, calls, err
}

package cmd

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// pipeStdin replaces os.Stdin with a pipe holding content, as an MCP client's
// connection would, and returns what is left unread once the test is done.
func pipeStdin(t *testing.T, content string) (unread func() string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(content); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	saved := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = saved
		_ = r.Close()
	})
	return func() string {
		data, _ := io.ReadAll(r)
		return string(data)
	}
}

// Under MCP, stdin is the client's connection. A tool call that omits its body
// must be refused without reading it; reading would take the client's next
// messages as the body.
func TestMCPBodyToolsNeverReadStdin(t *testing.T) {
	const protocol = `{"jsonrpc":"2.0","id":7,"method":"tools/call"}` + "\n"
	for _, tool := range []string{"mail_drafts_create", "mail_send"} {
		t.Run(tool, func(t *testing.T) {
			unread := pipeStdin(t, protocol)
			cfg := &mcpConfig{allowWrite: setOf("mail_drafts_create"), allowSend: setOf("mail_send")}
			srv, _, err := buildMCPServer(cfg)
			if err != nil {
				t.Fatalf("buildMCPServer: %v", err)
			}
			serverT, clientT := mcp.NewInMemoryTransports()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			ss, err := srv.Connect(ctx, serverT, nil)
			if err != nil {
				t.Fatalf("server connect: %v", err)
			}
			defer func() { _ = ss.Close() }()
			cs, err := mcp.NewClient(&mcp.Implementation{Name: "olk-test", Version: "v0"}, nil).Connect(ctx, clientT, nil)
			if err != nil {
				t.Fatalf("client connect: %v", err)
			}
			defer func() { _ = cs.Close() }()

			res, err := cs.CallTool(ctx, &mcp.CallToolParams{
				Name:      tool,
				Arguments: map[string]any{"to": []any{"a@example.com"}, "subject": "s"},
			})
			if err != nil {
				t.Fatalf("CallTool: %v", err)
			}
			if !res.IsError || !strings.Contains(resultText(res), "body is required") {
				t.Errorf("result = %q (error %v), want a body-required error", resultText(res), res.IsError)
			}
			if got := unread(); got != protocol {
				t.Errorf("stdin left %q, want it untouched", got)
			}
		})
	}
}

func TestCLIBodyStillReadsPipedStdin(t *testing.T) {
	pipeStdin(t, "  piped body\n")
	got, err := (&RunContext{}).bodyOrStdin("")
	if err != nil || got != "piped body" {
		t.Errorf("bodyOrStdin = %q, %v; want the piped body", got, err)
	}
	if got, err := (&RunContext{stdinIsProtocol: true}).bodyOrStdin("given"); err != nil || got != "given" {
		t.Errorf("an explicit body under MCP = %q, %v", got, err)
	}
}

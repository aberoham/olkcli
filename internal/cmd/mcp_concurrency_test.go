package cmd

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Concurrent tool calls must not race on os.Stdout/os.Stderr: one call's
// parser construction reads them while another call has them swapped. Run
// with -race.
func TestMCPConcurrentCallsDoNotRace(t *testing.T) {
	cs := connectE2E(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "version"})
			if err != nil {
				t.Errorf("CallTool: %v", err)
				return
			}
			if res.IsError || !strings.Contains(resultText(res), "version") {
				t.Errorf("version = %q (error %v)", resultText(res), res.IsError)
			}
		}()
	}
	wg.Wait()
}

// holdCallSlot occupies the slot as a long-running call would, until the test
// ends.
func holdCallSlot(t *testing.T) {
	t.Helper()
	release, err := acquireCallSlot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
}

func versionHandler(t *testing.T, timeout int) mcp.ToolHandler {
	t.Helper()
	b := testBinding(t, "version")
	b.env.timeout = timeout
	return makeHandler(b)
}

// callBounded runs handler in the background and fails the test if it has not
// returned within limit, so a regression fails rather than hangs.
func callBounded(t *testing.T, ctx context.Context, handler mcp.ToolHandler, limit time.Duration) *mcp.CallToolResult {
	t.Helper()
	type outcome struct {
		res *mcp.CallToolResult
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := handler(ctx, &mcp.CallToolRequest{})
		done <- outcome{res, err}
	}()
	select {
	case o := <-done:
		if o.err != nil {
			t.Fatal(o.err)
		}
		return o.res
	case <-time.After(limit):
		t.Fatalf("call still waiting after %v", limit)
		return nil
	}
}

// A call queued behind another returns as soon as its client cancels it, and
// never runs. The cancellation comes after the call is already waiting.
func TestMCPQueuedCallHonoursCancellation(t *testing.T) {
	holdCallSlot(t)
	handler := versionHandler(t, 60)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(200*time.Millisecond, cancel)
	res := callBounded(t, ctx, handler, 5*time.Second)
	if text := resultText(res); !res.IsError || !strings.Contains(text, "not run") || !strings.Contains(text, "canceled") {
		t.Errorf("result = %q (error %v), want a cancelled not-run error", text, res.IsError)
	}
}

// A queued call's deadline covers its wait, so a stuck call ahead of it cannot
// hold it forever.
func TestMCPQueuedCallTimesOut(t *testing.T) {
	holdCallSlot(t)
	res := callBounded(t, context.Background(), versionHandler(t, 1), 5*time.Second)
	if text := resultText(res); !res.IsError || !strings.Contains(text, "deadline exceeded") {
		t.Errorf("result = %q (error %v), want a deadline error", text, res.IsError)
	}
}

// A call already cancelled when its turn comes does not run.
func TestMCPCancelledCallDoesNotRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := versionHandler(t, 60)(ctx, &mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || strings.Contains(resultText(res), "olk ") {
		t.Errorf("result = %q (error %v), want it not run", resultText(res), res.IsError)
	}
}

// A call builds its parser only once it has its turn: kong reads os.Stdout and
// os.Stderr, which the running call has swapped. An argument that fails to
// parse shows when parsing happened: refused at once means it parsed while
// another call held the turn; a deadline means it waited.
func TestMCPCallsParseOnlyOnTheirTurn(t *testing.T) {
	b := testBinding(t, "mail", "get")
	b.env.timeout = 1
	call := func() *mcp.CallToolResult {
		res, err := makeHandler(b)(context.Background(), &mcp.CallToolRequest{
			Params: &mcp.CallToolParamsRaw{Arguments: []byte(`{"id":"m","format":"bogus"}`)},
		})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	// Control: on its own turn the arguments fail to parse.
	if text := resultText(call()); !strings.Contains(text, "parse error") {
		t.Fatalf("control result = %q, want a parse error", text)
	}
	holdCallSlot(t)
	if text := resultText(call()); !strings.Contains(text, "deadline exceeded") {
		t.Errorf("result = %q, want it to wait for its turn", text)
	}
}

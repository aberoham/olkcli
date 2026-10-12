package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/rlrghb/olkcli/internal/outfmt"
)

func TestCaptureStd_DirectAndPrinter(t *testing.T) {
	origOut, origErr := os.Stdout, os.Stderr

	stdout, stderr, err := captureStd(func() error {
		fmt.Println("direct stdout line")
		fmt.Fprintln(os.Stderr, "direct stderr line")
		// outfmt.Printer defaults to os.Stdout, which is redirected during capture.
		p := outfmt.NewPrinter(true, false, false, "", "UTC", false, false)
		return p.PrintJSON(map[string]string{"k": "v"}, 1, "")
	})
	if err != nil {
		t.Fatalf("captureStd returned err: %v", err)
	}

	if !strings.Contains(stdout, "direct stdout line") {
		t.Errorf("stdout missing direct write: %q", stdout)
	}
	if !strings.Contains(stdout, "\"k\"") {
		t.Errorf("stdout missing printer JSON: %q", stdout)
	}
	if !strings.Contains(stderr, "direct stderr line") {
		t.Errorf("stderr missing direct write: %q", stderr)
	}

	// Globals must be restored.
	if os.Stdout != origOut || os.Stderr != origErr {
		t.Error("os.Stdout/os.Stderr not restored after captureStd")
	}
}

func TestCaptureStd_RestoresOnPanic(t *testing.T) {
	origOut, origErr := os.Stdout, os.Stderr

	_, _, err := captureStd(func() error {
		panic("boom")
	})
	if err == nil || !strings.Contains(err.Error(), "panic") {
		t.Errorf("expected panic error, got %v", err)
	}
	if os.Stdout != origOut || os.Stderr != origErr {
		t.Error("os.Stdout/os.Stderr not restored after panic")
	}
}

// TestCaptureStd_Concurrent runs under -race to confirm the mutex serializes
// access to the process-global streams without interleaving output.
func TestCaptureStd_Concurrent(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			want := fmt.Sprintf("line-%d", n)
			out, _, err := captureStd(func() error {
				fmt.Print(want)
				return nil
			})
			if err != nil {
				t.Errorf("captureStd: %v", err)
			}
			if out != want {
				t.Errorf("got %q, want %q (interleaved capture)", out, want)
			}
		}(i)
	}
	wg.Wait()
}

// A command printing far past the cap leaves only the cap's worth in memory.
func TestCaptureStdLimitBoundsWhatIsKept(t *testing.T) {
	chunk := strings.Repeat("x", 1<<20)
	out, errOut, err := captureStdLimit(1000, func() error {
		for i := 0; i < 20; i++ {
			fmt.Fprint(os.Stdout, chunk)
			fmt.Fprint(os.Stderr, chunk)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1001 || len(errOut) != 1001 {
		t.Errorf("kept %d stdout and %d stderr bytes, want 1001 each", len(out), len(errOut))
	}
}

// A failed call's output is capped like a successful one's, and the error
// itself survives the cap.
func TestFailedResultIsCappedAndKeepsTheError(t *testing.T) {
	res := failedResult(strings.Repeat("o", 5000), strings.Repeat("e", 5000), errors.New("3 of 3 attachments could not be saved"), 1000)
	text := resultText(res)
	if !res.IsError {
		t.Error("not marked as an error")
	}
	if !strings.Contains(text, "3 of 3 attachments could not be saved") {
		t.Errorf("the error was lost: %s", text)
	}
	if len(text) > 3000 {
		t.Errorf("failed result is %d bytes, want it capped", len(text))
	}
}

// Truncation is marked before trimming: output cut just after a newline must
// still say it was cut.
func TestTruncationSurvivesTrimming(t *testing.T) {
	captured := strings.Repeat("x", 1000) + "\n" // limit+1 bytes, as capture keeps
	text := resultText(failedResult(capText(captured, 1000), "", errors.New("failed"), 1000))
	if !strings.Contains(text, "output truncated") {
		t.Errorf("truncation notice lost: %q", text[len(text)-120:])
	}
}

package cmd

import (
	"fmt"
	"io"
	"os"
	"sync"
)

// captureMu keeps two captures from overlapping: captureStd reassigns the
// process-global os.Stdout/os.Stderr. MCP tool calls are already serialized,
// parsing included, by callSlot; this guards any other caller.
var captureMu sync.Mutex

// captureStd runs fn while redirecting os.Stdout and os.Stderr to in-memory
// pipes, returning everything fn wrote to each. The original streams are always
// restored, even if fn panics (the panic is converted into an error). This is
// what keeps command output (221 direct os.Stdout writes plus the outfmt
// Printer) from corrupting the MCP transport's own use of stdout.
func captureStd(fn func() error) (stdoutText, stderrText string, runErr error) {
	return captureStdLimit(-1, fn)
}

// captureStdLimit is captureStd keeping at most limit+1 bytes of each stream,
// or all of it when limit is negative, and discarding the rest as it arrives,
// so a runaway command cannot grow memory without bound. The extra byte lets
// capText tell that output was cut.
func captureStdLimit(limit int, fn func() error) (stdoutText, stderrText string, runErr error) {
	captureMu.Lock()
	defer captureMu.Unlock()

	origOut, origErr := os.Stdout, os.Stderr

	outR, outW, err := os.Pipe()
	if err != nil {
		return "", "", fmt.Errorf("creating stdout pipe: %w", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		outW.Close()
		outR.Close()
		return "", "", fmt.Errorf("creating stderr pipe: %w", err)
	}

	os.Stdout, os.Stderr = outW, errW

	var (
		wg     sync.WaitGroup
		outBuf []byte
		errBuf []byte
	)
	wg.Add(2)
	drain := func(r io.Reader) []byte {
		if limit < 0 {
			data, _ := io.ReadAll(r)
			return data
		}
		data, _ := io.ReadAll(io.LimitReader(r, int64(limit)+1))
		_, _ = io.Copy(io.Discard, r) // keep the writer from blocking
		return data
	}
	go func() { defer wg.Done(); outBuf = drain(outR) }()
	go func() { defer wg.Done(); errBuf = drain(errR) }()

	func() {
		defer func() {
			if r := recover(); r != nil {
				runErr = fmt.Errorf("panic: %v", r)
			}
		}()
		runErr = fn()
	}()

	// Restore globals first, then close the write ends so the drain goroutines
	// observe EOF.
	os.Stdout, os.Stderr = origOut, origErr
	outW.Close()
	errW.Close()
	wg.Wait()
	outR.Close()
	errR.Close()

	return string(outBuf), string(errBuf), runErr
}

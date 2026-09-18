package cmd

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testForwardedMIME = "From: Sender <sender@example.com>\r\nSubject: Forwarded\r\n\r\nBody\r\n"

func graphRawResponse(req *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode:    http.StatusOK,
		Header:        http.Header{"Content-Type": []string{"message/rfc822"}},
		Body:          io.NopCloser(strings.NewReader(body)),
		Request:       req,
		ContentLength: int64(len(body)),
	}
}

// mixedAttachmentsResponse serves a message whose attachments are, in order, a
// cloud-file link that cannot be downloaded, a forwarded email stored as an
// itemAttachment with no contentType, and an ordinary file.
func mixedAttachmentsResponse(req *http.Request, paths *[]string) *http.Response {
	*paths = append(*paths, req.URL.Path)
	switch {
	case strings.HasSuffix(req.URL.Path, "/attachments"):
		return graphJSONResponse(req, `{"value":[
			{"@odata.type":"#microsoft.graph.referenceAttachment","id":"ref","name":"Deck.pptx","size":10},
			{"@odata.type":"#microsoft.graph.itemAttachment","id":"item","name":"Onboarding","size":100},
			{"@odata.type":"#microsoft.graph.fileAttachment","id":"file","name":"file.txt","size":4}
		]}`)
	case strings.HasSuffix(req.URL.Path, "/attachments/ref"):
		return graphJSONResponse(req, `{"@odata.type":"#microsoft.graph.referenceAttachment",
			"id":"ref","name":"Deck.pptx","size":10}`)
	case strings.HasSuffix(req.URL.Path, "/attachments/item"):
		return graphJSONResponse(req, `{"@odata.type":"#microsoft.graph.itemAttachment",
			"id":"item","name":"Onboarding","contentType":null,"size":100}`)
	case strings.HasSuffix(req.URL.Path, "/attachments/item/$value"):
		return graphRawResponse(req, testForwardedMIME)
	default:
		return graphJSONResponse(req, `{"@odata.type":"#microsoft.graph.fileAttachment",
			"id":"file","name":"file.txt","size":4,"contentBytes":"dGVzdA=="}`)
	}
}

func runMailCommandWithStderr(
	t *testing.T,
	args []string,
	responder func(*http.Request) *http.Response,
) (stdout, stderr string, err error) {
	t.Helper()
	client := testMailListClient(t, responder)
	cli := &CLI{}
	parser, err := newKongParser(cli)
	if err != nil {
		return "", "", err
	}
	kctx, err := parser.Parse(args)
	if err != nil {
		return "", "", err
	}
	return captureStd(func() error {
		return kctx.Run(&RunContext{Ctx: context.Background(), Flags: &cli.RootFlags, client: client})
	})
}

// One attachment that cannot be saved must not cost the caller the others.
// Before this, the first failure returned immediately and every attachment
// listed after it was silently left behind.
func TestMailAttachmentsSaveContinuesPastAFailure(t *testing.T) {
	outDir := t.TempDir()
	var paths []string
	stdout, stderr, err := runMailCommandWithStderr(t,
		[]string{"mail", "attachments", "message-id", "--mailbox", "shared@example.com", "--save", "--out", outDir},
		func(req *http.Request) *http.Response { return mixedAttachmentsResponse(req, &paths) })

	if err == nil || !strings.Contains(err.Error(), "1 of 3 attachments could not be saved") {
		t.Fatalf("error = %v, want a non-zero exit naming the one failure", err)
	}
	if !strings.Contains(stderr, "Deck.pptx") || !strings.Contains(stderr, "link to a cloud file") {
		t.Errorf("stderr = %q, want the failed attachment and its reason", stderr)
	}
	eml, readErr := os.ReadFile(filepath.Join(outDir, "Onboarding.eml"))
	if readErr != nil {
		t.Fatalf("forwarded email not saved as .eml: %v", readErr)
	}
	if string(eml) != testForwardedMIME {
		t.Errorf("Onboarding.eml = %q, want the raw MIME", eml)
	}
	if content, _ := os.ReadFile(filepath.Join(outDir, "file.txt")); string(content) != "test" {
		t.Errorf("file.txt = %q, want the file listed after the failure to be saved too", content)
	}
	if strings.Count(stdout, "Saved:") != 2 {
		t.Errorf("stdout = %q, want two Saved lines", stdout)
	}
	// Every request, $value included, must address the delegated mailbox.
	for _, p := range paths {
		if !strings.HasPrefix(p, "/v1.0/users/shared@example.com/messages/message-id/") {
			t.Errorf("request %q left the delegated mailbox", p)
		}
	}
	if !containsPath(paths, "/v1.0/users/shared@example.com/messages/message-id/attachments/item/$value") {
		t.Errorf("paths = %v, want the delegated $value request", paths)
	}
}

func containsPath(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}

func TestMailAttachmentsDownloadsOneItemAttachmentFromDelegatedMailbox(t *testing.T) {
	outDir := t.TempDir()
	var paths []string
	stdout, _, err := runMailCommandWithStderr(t,
		[]string{"mail", "attachments", "message-id", "--mailbox", "shared@example.com",
			"--attachment-id", "item", "--out", outDir},
		func(req *http.Request) *http.Response { return mixedAttachmentsResponse(req, &paths) })
	if err != nil {
		t.Fatalf("mail attachments: %v", err)
	}
	want := []string{
		"/v1.0/users/shared@example.com/messages/message-id/attachments/item",
		"/v1.0/users/shared@example.com/messages/message-id/attachments/item/$value",
	}
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Errorf("paths = %v, want %v", paths, want)
	}
	if !strings.Contains(stdout, "Onboarding.eml") {
		t.Errorf("stdout = %q, want the saved .eml path", stdout)
	}
}

func TestMailGetFormatEML(t *testing.T) {
	mime := func(req *http.Request, paths *[]string) *http.Response {
		*paths = append(*paths, req.URL.Path)
		return graphRawResponse(req, testForwardedMIME)
	}

	t.Run("stdout from a delegated mailbox", func(t *testing.T) {
		var paths []string
		stdout, _, err := runMailCommandWithStderr(t,
			[]string{"mail", "get", "message-id", "--mailbox", "shared@example.com", "--format", "eml"},

			func(req *http.Request) *http.Response { return mime(req, &paths) })
		if err != nil {
			t.Fatalf("mail get: %v", err)
		}
		if stdout != testForwardedMIME {
			t.Errorf("stdout = %q, want the unaltered MIME", stdout)
		}
		if strings.Join(paths, "") != "/v1.0/users/shared@example.com/messages/message-id/$value" {
			t.Errorf("paths = %v, want the delegated mailbox's $value", paths)
		}
	})

	t.Run("to a file", func(t *testing.T) {
		var paths []string
		out := filepath.Join(t.TempDir(), "message.eml")
		stdout, _, err := runMailCommandWithStderr(t,
			[]string{"mail", "get", "message-id", "--format", "eml", "--out", out},
			func(req *http.Request) *http.Response { return mime(req, &paths) })
		if err != nil {
			t.Fatalf("mail get: %v", err)
		}
		if content, _ := os.ReadFile(out); string(content) != testForwardedMIME {
			t.Errorf("%s = %q, want the MIME", out, content)
		}
		if !strings.Contains(stdout, "Saved: "+out) {
			t.Errorf("stdout = %q, want the saved path", stdout)
		}
	})

	// Raw MIME cannot carry untrusted-content markers without ceasing to be a
	// valid .eml, so a caller that asked for wrapping gets a file or nothing.
	t.Run("refused on stdout under wrap-untrusted", func(t *testing.T) {
		var paths []string
		_, _, err := runMailCommandWithStderr(t,
			[]string{"mail", "get", "message-id", "--format", "eml", "--wrap-untrusted"},
			func(req *http.Request) *http.Response { return mime(req, &paths) })
		if err == nil || !strings.Contains(err.Error(), "--out") {
			t.Fatalf("error = %v, want a pointer to --out", err)
		}
		if len(paths) != 0 {
			t.Errorf("Graph requests = %v, want none", paths)
		}
	})

	t.Run("--out without eml is rejected", func(t *testing.T) {
		var paths []string
		_, _, err := runMailCommandWithStderr(t,
			[]string{"mail", "get", "message-id", "--out", filepath.Join(t.TempDir(), "x")},
			func(req *http.Request) *http.Response { return mime(req, &paths) })
		if err == nil || !strings.Contains(err.Error(), "--format eml") {
			t.Fatalf("error = %v, want --out rejected outside eml", err)
		}
	})
}

// The MCP tool runs the same command from a rebuilt argv, so the fix reaches
// agents only if that argv still saves item attachments. This drives the
// server's own argv builder and call preparation rather than assuming it.
func TestMCPMailAttachmentsSavesItemAttachment(t *testing.T) {
	outDir := t.TempDir()
	b := bindingsMap(t, &mcpConfig{})["mail_attachments"]
	if b == nil {
		t.Fatal("mail_attachments is not registered")
	}
	argv, err := buildArgv(b, map[string]any{"id": "message-id", "save": true, "out": outDir})
	if err != nil {
		t.Fatalf("buildArgv: %v", err)
	}
	cli, kctx, err := prepareCall(argv, &b.env)
	if err != nil {
		t.Fatalf("prepareCall(%v): %v", argv, err)
	}
	var paths []string
	client := testMailListClient(t, func(req *http.Request) *http.Response { return mixedAttachmentsResponse(req, &paths) })
	stdout, _, runErr := captureStd(func() error {
		return kctx.Run(&RunContext{Ctx: context.Background(), Flags: &cli.RootFlags, client: client})
	})
	if runErr == nil {
		t.Error("want a non-zero result: the cloud-file link cannot be saved")
	}
	if content, err := os.ReadFile(filepath.Join(outDir, "Onboarding.eml")); err != nil || string(content) != testForwardedMIME {
		t.Errorf("Onboarding.eml = %q, %v; want the forwarded email saved through MCP", content, err)
	}
	if !strings.Contains(stdout, "file.txt") {
		t.Errorf("stdout = %q, want the file after the failure saved too", stdout)
	}
}

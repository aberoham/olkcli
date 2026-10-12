package cmd

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rlrghb/olkcli/internal/config"
	"github.com/rlrghb/olkcli/internal/secrets"
)

// newTestScope returns a scope rooted in a fresh directory.
func newTestScope(t *testing.T) *saveScope {
	t.Helper()
	return mustScope(t, filepath.Join(t.TempDir(), "downloads"), false, false)
}

func mustScope(t *testing.T, dir string, noWrite, dryRun bool) *saveScope {
	t.Helper()
	s, err := newSaveScope(dir, noWrite, dryRun)
	if err != nil {
		t.Fatalf("newSaveScope: %v", err)
	}
	if s.root != nil {
		t.Cleanup(func() { _ = s.root.Close() })
	}
	return s
}

// empty fails the test if dir holds anything.
func assertEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	if len(entries) != 0 {
		t.Errorf("%s gained %v", dir, entries)
	}
}

func TestSaveScope_OffUnlessADirectoryIsNamed(t *testing.T) {
	for name, scope := range map[string]*saveScope{
		"no dir":   mustScope(t, "", false, false),
		"no-write": mustScope(t, t.TempDir(), true, false),
		"zero":     {},
	} {
		if scope.enabled() {
			t.Errorf("%s: scope enabled", name)
		}
		if _, err := scope.dir(t.TempDir()); !errors.Is(err, errSavesOff) {
			t.Errorf("%s: dir err = %v, want errSavesOff", name, err)
		}
		if _, err := scope.file(filepath.Join(t.TempDir(), "x.eml")); !errors.Is(err, errSavesOff) {
			t.Errorf("%s: file err = %v, want errSavesOff", name, err)
		}
		if _, err := scope.writeNew(t.TempDir(), "x", []byte("x")); !errors.Is(err, errSavesOff) {
			t.Errorf("%s: writeNew err = %v, want errSavesOff", name, err)
		}
	}
}

func TestSaveScope_DryRunCreatesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "downloads")
	s := mustScope(t, dir, false, true)
	if _, err := s.dir("sub"); err != nil {
		t.Fatalf("dir: %v", err)
	}
	if _, err := s.writeNew(s.path, "x", []byte("x")); err == nil {
		t.Error("a dry run wrote a file")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("a dry run created --download-dir: %v", err)
	}
}

func TestSaveScope_WritesUnderTheRoot(t *testing.T) {
	s := newTestScope(t)
	for _, out := range []string{".", "", "sub/dir", s.path, filepath.Join(s.path, "a")} {
		dir, err := s.dir(out)
		if err != nil {
			t.Errorf("dir(%q): %v", out, err)
			continue
		}
		saved, err := s.writeNew(dir, "f.txt", []byte("x"))
		if err != nil {
			t.Errorf("writeNew in %q: %v", out, err)
			continue
		}
		if data, err := os.ReadFile(saved); err != nil || string(data) != "x" {
			t.Errorf("%s: %q, %v", saved, data, err)
		}
	}
	got, err := s.file("exports/msg.eml")
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	if want := filepath.Join(s.path, "exports", "msg.eml"); got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

// The paths an injected prompt would aim for: shell start-up files, launch
// agents and SSH keys, by absolute path or traversal.
func TestSaveScope_RefusesPathsOutsideTheRoot(t *testing.T) {
	s := newTestScope(t)
	home := t.TempDir()
	for _, out := range []string{
		filepath.Join(home, "Library", "LaunchAgents"),
		filepath.Join(home, ".ssh"),
		"../outside",
		"sub/../../outside",
		filepath.Dir(s.path),
	} {
		if _, err := s.dir(out); err == nil {
			t.Errorf("dir(%q) accepted", out)
		}
		if _, err := s.writeNew(out, "authorized_keys", []byte("x")); err == nil {
			t.Errorf("writeNew(%q) accepted", out)
		}
	}
	for _, out := range []string{filepath.Join(home, ".zshenv"), "../.zshenv", "/etc/x"} {
		if _, err := s.file(out); err == nil {
			t.Errorf("file(%q) accepted", out)
		}
	}
	assertEmpty(t, home)
}

func TestSaveScope_RefusesSymlinksThatLeaveTheRoot(t *testing.T) {
	s := newTestScope(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(s.path, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{"escape", "escape/deeper"} {
		if _, err := s.dir(out); err == nil {
			t.Errorf("dir(%q) through a symlink out of the root was accepted", out)
		}
	}
	if _, err := s.writeNew(filepath.Join(s.path, "escape"), "x", []byte("x")); err == nil {
		t.Error("writeNew through a symlink out of the root was accepted")
	}
	assertEmpty(t, outside)
}

// A directory checked by dir and swapped for a symlink before the write, as a
// local process racing a multi-attachment save could, still leads nowhere.
func TestSaveScope_DirectorySwappedAfterTheCheck(t *testing.T) {
	s := newTestScope(t)
	dir, err := s.dir("inbox")
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := s.writeNew(dir, "authorized_keys", []byte("x")); err == nil {
		t.Error("a write followed a swapped-in symlink")
	}
	assertEmpty(t, outside)
}

// Replacing the root's own path redirects nothing: the scope holds the
// directory open.
func TestSaveScope_RootPathSwapped(t *testing.T) {
	s := newTestScope(t)
	moved := s.path + "-moved"
	if err := os.Rename(s.path, moved); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, s.path); err != nil {
		t.Fatal(err)
	}
	// The write lands in the held directory, and the moved root is reported
	// rather than a path that now names the symlink's target.
	saved, err := s.writeNew(s.path, "x", []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "moved or replaced") || saved != "" {
		t.Errorf("writeNew = %q, %v; want a moved-root error", saved, err)
	}
	assertEmpty(t, outside)
	if _, err := os.Stat(filepath.Join(moved, "x")); err != nil {
		t.Errorf("the write did not land in the held directory: %v", err)
	}
}

func TestSaveScope_RootMayItselfBeASymlink(t *testing.T) {
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	s := mustScope(t, link, false, false)
	dir, err := s.dir("sub")
	if err != nil {
		t.Fatalf("dir under a symlinked root: %v", err)
	}
	if _, err := s.writeNew(dir, "x", []byte("x")); err != nil {
		t.Fatalf("writeNew under a symlinked root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "sub", "x")); err != nil {
		t.Error(err)
	}
}

func TestSaveScope_CollisionsGetANewName(t *testing.T) {
	s := newTestScope(t)
	for _, want := range []string{"a.pdf", "a(1).pdf", "a(2).pdf"} {
		saved, err := s.writeNew(s.path, "a.pdf", []byte("x"))
		if err != nil || saved != filepath.Join(s.path, want) {
			t.Errorf("writeNew = %q, %v; want %s", saved, err, want)
		}
	}
}

// An MCP call's out path, relative or omitted, lands under --download-dir once
// kong has parsed it, and an absolute one elsewhere is refused at the save.
func TestMCPOutPathsResolveUnderTheDownloadDir(t *testing.T) {
	s := newTestScope(t)
	env := &callEnv{saves: s}
	cases := []struct {
		name string
		path []string
		args map[string]any
		want string // "" means the save must be refused
	}{
		{"default out", []string{"mail", "attachments"}, map[string]any{"id": "M", "save": true}, s.path},
		{"relative out", []string{"mail", "attachments"}, map[string]any{"id": "M", "save": true, "out": "inbox"}, filepath.Join(s.path, "inbox")},
		{"absolute out", []string{"calendar", "attachments", "download"}, map[string]any{"event-id": "E", "attachment-id": "A", "out": t.TempDir()}, ""},
		{"home out", []string{"calendar", "attachments", "download"}, map[string]any{"event-id": "E", "attachment-id": "A", "out": "~/Library/LaunchAgents"}, ""},
		{"traversal out", []string{"mail", "attachments"}, map[string]any{"id": "M", "save": true, "out": "../../.ssh"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := bindingFor(t, strings.Join(tc.path, "_"), env, tc.path...)
			argv, err := buildArgv(b, tc.args)
			if err != nil {
				t.Fatalf("buildArgv: %v", err)
			}
			_, kctx, err := prepareCall(argv, env)
			if err != nil {
				t.Fatalf("prepareCall: %v", err)
			}
			out := kctx.Selected().Target.FieldByName("Out").String()
			got, err := (&RunContext{saves: s}).saveDir(out)
			if tc.want == "" {
				if err == nil {
					t.Errorf("out %q was accepted as %q", out, got)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("saveDir(%q) = %q, %v; want %q", out, got, err, tc.want)
			}
		})
	}
}

// attachmentGraph answers every request with one file attachment, readable as
// a list or as the attachment itself.
func attachmentGraph(calls *int) func(*http.Request) *http.Response {
	return func(req *http.Request) *http.Response {
		*calls++
		if strings.HasSuffix(req.URL.Path, "/$value") {
			return graphMIMEResponse(req)
		}
		return graphJSONResponse(req, `{"value":[{"@odata.type":"#microsoft.graph.fileAttachment","id":"att-1","name":"authorized_keys","size":4}],`+
			`"@odata.type":"#microsoft.graph.fileAttachment","id":"att-1","name":"authorized_keys","contentBytes":"dGVzdA=="}`)
	}
}

// runSaving runs a command as an MCP call would, with saves confined to s.
func runSaving(t *testing.T, s *saveScope, argv ...string) error {
	t.Helper()
	calls := 0
	client := testMailListClient(t, attachmentGraph(&calls)) //nolint:bodyclose // a fake transport; the client closes what it returns
	cli := &CLI{}
	parser, err := newKongParser(cli)
	if err != nil {
		t.Fatal(err)
	}
	kctx, err := parser.Parse(argv)
	if err != nil {
		t.Fatalf("parse %v: %v", argv, err)
	}
	_, _, err = captureStd(func() error {
		return kctx.Run(&RunContext{Ctx: context.Background(), Flags: &cli.RootFlags, client: client, saves: s})
	})
	return err
}

// Each command an MCP tool reaches must write through the scope: inside it
// the file lands, outside it nothing is written anywhere.
func TestSavingCommandsWriteOnlyThroughTheScope(t *testing.T) {
	commands := map[string]func(out string) []string{
		"mail attachments --save": func(out string) []string {
			return []string{"mail", "attachments", "msg-1", "--save", "--out", out}
		},
		"mail attachments --attachment-id": func(out string) []string {
			return []string{"mail", "attachments", "msg-1", "--attachment-id", "att-1", "--out", out}
		},
		"calendar attachments download": func(out string) []string {
			return []string{"calendar", "attachments", "download", "event-1", "att-1", "--out", out}
		},
		"mail get --format eml": func(out string) []string {
			return []string{"mail", "get", "msg-1", "--format", "eml", "--out", filepath.Join(out, "authorized_keys")}
		},
	}
	for name, argv := range commands {
		t.Run(name, func(t *testing.T) {
			s := newTestScope(t)
			if err := runSaving(t, s, argv(filepath.Join(s.path, "in"))...); err != nil {
				t.Fatalf("inside the root: %v", err)
			}
			if _, err := os.Stat(filepath.Join(s.path, "in", "authorized_keys")); err != nil {
				t.Errorf("file not written under the root: %v", err)
			}

			outside := t.TempDir()
			if err := runSaving(t, s, argv(outside)...); err == nil {
				t.Error("a save outside the root succeeded")
			}
			assertEmpty(t, outside)

			if err := runSaving(t, mustScope(t, "", false, false), argv(filepath.Join(s.path, "off"))...); !errors.Is(err, errSavesOff) {
				t.Errorf("saving with saves off = %v, want errSavesOff", err)
			}
		})
	}
}

// --dry-run previews a download without fetching or writing it.
func TestDownloadsHonourDryRun(t *testing.T) {
	out := filepath.Join(t.TempDir(), "new")
	for _, argv := range [][]string{
		{"calendar", "attachments", "download", "event-1", "att-1", "--out", out},
		{"mail", "get", "msg-1", "--format", "eml", "--out", filepath.Join(out, "m.eml")},
		{"todo", "attach", "download", "task-1", "att-1", "--list", "list-1", "--out", out},
	} {
		cli := &CLI{}
		parser, err := newKongParser(cli)
		if err != nil {
			t.Fatal(err)
		}
		kctx, err := parser.Parse(append(argv, "--dry-run"))
		if err != nil {
			t.Fatalf("parse %v: %v", argv, err)
		}
		// No account and an inert store: signing in would fail, so a preview
		// must come before it.
		stdout, _, err := captureStd(func() error {
			return kctx.Run(&RunContext{Ctx: context.Background(), Flags: &cli.RootFlags,
				cfg: &config.Config{}, store: &secrets.KeyringStore{}})
		})
		if err != nil || !strings.HasPrefix(stdout, "Would ") {
			t.Errorf("%v: output %q, %v", argv, stdout, err)
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Errorf("%v: dry run created %s", argv, out)
		}
	}
}

func TestRunContext_CLISavesAreUnconfined(t *testing.T) {
	out := filepath.Join(t.TempDir(), "anywhere")
	r := &RunContext{}
	if got, err := r.saveDir(out); err != nil || got != out {
		t.Errorf("saveDir = %q, %v", got, err)
	}
	if saved, err := r.writeNew(out, "x.eml", []byte("x")); err != nil || saved != filepath.Join(out, "x.eml") {
		t.Errorf("writeNew = %q, %v", saved, err)
	}
}

func TestSaveOnlyToolsNeedADownloadDir(t *testing.T) {
	for name, tc := range map[string]struct {
		saves *saveScope
		want  bool
	}{
		"no scope":     {nil, false},
		"off":          {mustScope(t, "", false, false), false},
		"no-write":     {mustScope(t, t.TempDir(), true, false), false},
		"download dir": {newTestScope(t), true},
	} {
		bindings := bindingsMap(t, &mcpConfig{env: callEnv{saves: tc.saves}})
		if _, ok := bindings["calendar_attachments_download"]; ok != tc.want {
			t.Errorf("%s: calendar_attachments_download exposed = %v, want %v", name, ok, tc.want)
		}
		// Listing attachments is unaffected.
		if _, ok := bindings["mail_attachments"]; !ok {
			t.Errorf("%s: mail_attachments withheld", name)
		}
	}
}

// A multi-attachment save checks its directory and then downloads. Swapping
// the directory for a symlink during the download, as a racing local process
// could, must not redirect the write.
func TestAttachmentSaveSurvivesADirectorySwapMidDownload(t *testing.T) {
	s := newTestScope(t)
	in := filepath.Join(s.path, "in")
	outside := t.TempDir()
	calls := 0
	serve := attachmentGraph(&calls) //nolint:bodyclose // a fake transport; the client closes what it returns
	swapped := false
	client := testMailListClient(t, func(req *http.Request) *http.Response { //nolint:bodyclose // a fake transport; the client closes what it returns
		if !swapped && strings.Contains(req.URL.Path, "/attachments/") {
			swapped = true
			if err := os.Remove(in); err != nil {
				t.Errorf("removing %s: %v", in, err)
			}
			if err := os.Symlink(outside, in); err != nil {
				t.Errorf("swapping %s: %v", in, err)
			}
		}
		return serve(req)
	})
	cli := &CLI{}
	parser, err := newKongParser(cli)
	if err != nil {
		t.Fatal(err)
	}
	kctx, err := parser.Parse([]string{"mail", "attachments", "msg-1", "--save", "--out", in})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = captureStd(func() error {
		return kctx.Run(&RunContext{Ctx: context.Background(), Flags: &cli.RootFlags, client: client, saves: s})
	})
	if !swapped {
		t.Fatal("the download never ran")
	}
	if err == nil {
		t.Error("the save succeeded through the swapped-in symlink")
	}
	assertEmpty(t, outside)
}

// The CLI writes an export to --out exactly as given: a path through a
// symlink and back up resolves as the operating system resolves it.
func TestCLIEMLExportKeepsTheGivenPath(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "b", "nested")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(base, "a"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(base, "a", "link")); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(base, "a", "link") + "/../message.eml"
	_, _, err := runMailCommand(t, []string{"mail", "get"}, []string{"msg-1", "--format", "eml", "--out", out},
		graphMIMEResponse)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "b", "message.eml")); err != nil {
		t.Errorf("export did not follow the given path: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "a", "message.eml")); !os.IsNotExist(err) {
		t.Errorf("export was written to a lexically cleaned path")
	}
}

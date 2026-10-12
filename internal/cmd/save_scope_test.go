package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestScope returns a scope rooted in a fresh directory, with symlinks in the
// temp path (macOS /var -> /private/var) already resolved.
func newTestScope(t *testing.T) *saveScope {
	t.Helper()
	s, err := newSaveScope(filepath.Join(t.TempDir(), "downloads"), false)
	if err != nil {
		t.Fatalf("newSaveScope: %v", err)
	}
	return s
}

func TestSaveScope_OffUnlessADirectoryIsNamed(t *testing.T) {
	for name, scope := range map[string]*saveScope{
		"no dir":   mustScope(t, "", false),
		"no-write": mustScope(t, t.TempDir(), true),
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
	}
}

func mustScope(t *testing.T, dir string, noWrite bool) *saveScope {
	t.Helper()
	s, err := newSaveScope(dir, noWrite)
	if err != nil {
		t.Fatalf("newSaveScope: %v", err)
	}
	return s
}

func TestSaveScope_AcceptsPathsUnderTheRoot(t *testing.T) {
	s := newTestScope(t)
	for _, out := range []string{".", "", "sub/dir", s.root, filepath.Join(s.root, "a")} {
		got, err := s.dir(out)
		if err != nil {
			t.Errorf("dir(%q): %v", out, err)
			continue
		}
		if !within(s.root, got) {
			t.Errorf("dir(%q) = %q, outside %q", out, got, s.root)
		}
		if info, err := os.Stat(got); err != nil || !info.IsDir() {
			t.Errorf("dir(%q) did not create %q", out, got)
		}
	}
	got, err := s.file("exports/msg.eml")
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	if want := filepath.Join(s.root, "exports", "msg.eml"); got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

// The paths an injected prompt would aim for: shell start-up files, launch
// agents and SSH keys, by absolute path, home shorthand or traversal.
func TestSaveScope_RefusesPathsOutsideTheRoot(t *testing.T) {
	s := newTestScope(t)
	home := t.TempDir()
	for _, out := range []string{
		filepath.Join(home, "Library", "LaunchAgents"),
		filepath.Join(home, ".ssh"),
		"../outside",
		"sub/../../outside",
		filepath.Dir(s.root),
	} {
		if _, err := s.dir(out); err == nil {
			t.Errorf("dir(%q) accepted", out)
		}
	}
	for _, out := range []string{filepath.Join(home, ".zshenv"), "../.zshenv", "/etc/x"} {
		if _, err := s.file(out); err == nil {
			t.Errorf("file(%q) accepted", out)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "Library")); !os.IsNotExist(err) {
		t.Errorf("a refused directory was created: %v", err)
	}
}

func TestSaveScope_RefusesSymlinksThatLeaveTheRoot(t *testing.T) {
	s := newTestScope(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(s.root, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{"escape", "escape/deeper"} {
		if _, err := s.dir(out); err == nil {
			t.Errorf("dir(%q) through a symlink out of the root was accepted", out)
		}
	}
	if _, err := s.file("escape/x.eml"); err == nil {
		t.Error("file through a symlink out of the root was accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "deeper")); !os.IsNotExist(err) {
		t.Errorf("a directory was created through the symlink: %v", err)
	}
}

func TestSaveScope_RootMayItselfBeASymlink(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	s := mustScope(t, link, false)
	if _, err := s.dir("sub"); err != nil {
		t.Errorf("dir under a symlinked root: %v", err)
	}
}

func TestRunContext_CLISavesAreUnconfined(t *testing.T) {
	out := filepath.Join(t.TempDir(), "anywhere")
	r := &RunContext{}
	if got, err := r.saveDir(out); err != nil || got != out {
		t.Errorf("saveDir = %q, %v", got, err)
	}
	if got, err := r.saveFile(filepath.Join(out, "x.eml")); err != nil || got != filepath.Join(out, "x.eml") {
		t.Errorf("saveFile = %q, %v", got, err)
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
		{"default out", []string{"mail", "attachments"}, map[string]any{"id": "M", "save": true}, s.root},
		{"relative out", []string{"mail", "attachments"}, map[string]any{"id": "M", "save": true, "out": "inbox"}, filepath.Join(s.root, "inbox")},
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

func TestMCPEMLExportIsConfined(t *testing.T) {
	s := newTestScope(t)
	r := &RunContext{saves: s}
	if _, err := r.saveFile(filepath.Join(t.TempDir(), ".zshenv")); err == nil {
		t.Error("an .eml export outside --download-dir was accepted")
	}
	got, err := r.saveFile(filepath.Join(s.root, "m.eml"))
	if err != nil || got != filepath.Join(s.root, "m.eml") {
		t.Errorf("saveFile = %q, %v", got, err)
	}
}

func TestSaveOnlyToolsNeedADownloadDir(t *testing.T) {
	for name, tc := range map[string]struct {
		saves *saveScope
		want  bool
	}{
		"no scope":     {nil, false},
		"off":          {mustScope(t, "", false), false},
		"no-write":     {mustScope(t, t.TempDir(), true), false},
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

package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// saveScope confines the local files an MCP tool call may write. The CLI has
// none, so its --out paths are the operator's own choice; an MCP call's paths
// are the agent's, and the agent reads untrusted mail, so they must stay under
// the directory the operator named with `olk mcp --download-dir`.
//
// Every directory it creates and every file it opens goes through an os.Root
// held for the server's lifetime, so a directory swapped for a symlink between
// one step and the next still cannot lead outside. It lives on RunContext, not
// RootFlags, so no tool argument or OLK_* variable can reach it.
type saveScope struct {
	path    string   // resolved --download-dir; empty refuses every save
	root    *os.Root // nil in a dry run, which creates nothing
	refusal string   // why saving is off, when path is empty
}

// errSavesOff is returned for any save through MCP that the operator has not
// enabled.
var errSavesOff = errors.New("saving files is off for MCP tools")

// newSaveScope resolves the operator's --download-dir. With --no-write, or
// without a directory, every save is refused. A dry run neither creates nor
// opens the directory.
func newSaveScope(dir string, noWrite, dryRun bool) (*saveScope, error) {
	switch {
	case noWrite:
		return &saveScope{refusal: "--no-write forbids saving files"}, nil
	case dir == "":
		return &saveScope{refusal: "start `olk mcp` with --download-dir to allow it"}, nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolving --download-dir: %w", err)
	}
	if dryRun {
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			abs = real
		}
		return &saveScope{path: filepath.Clean(abs)}, nil
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("creating --download-dir: %w", err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("resolving --download-dir: %w", err)
	}
	root, err := os.OpenRoot(real)
	if err != nil {
		return nil, fmt.Errorf("opening --download-dir: %w", err)
	}
	return &saveScope{path: real, root: root}, nil
}

// enabled reports whether this scope permits any save.
func (s *saveScope) enabled() bool { return s.path != "" }

// rel returns out as a path relative to the root, refusing anything that
// lexically leaves it. A relative out is taken under the root, so the default
// "." is the root itself. Symlinks are left to os.Root, which refuses any that
// lead outside when the path is used.
func (s *saveScope) rel(out string) (string, error) {
	if !s.enabled() {
		return "", fmt.Errorf("%w: %s", errSavesOff, s.refusal)
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(s.path, out)
	}
	rel, err := filepath.Rel(s.path, filepath.Clean(out))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside --download-dir %s", out, s.path)
	}
	return rel, nil
}

// dir confines and creates an output directory, returning it as an absolute
// path for display and for writeNew.
func (s *saveScope) dir(out string) (string, error) {
	rel, err := s.rel(out)
	if err != nil {
		return "", err
	}
	if s.root != nil {
		if err := s.root.MkdirAll(rel, 0o700); err != nil {
			return "", fmt.Errorf("creating output directory: %w", err)
		}
	}
	return filepath.Join(s.path, rel), nil
}

// file confines an output file path: its directory as dir does, and its name
// kept as given.
func (s *saveScope) file(out string) (string, error) {
	name := filepath.Base(out)
	if name == "." || name == ".." || name == string(filepath.Separator) {
		return "", fmt.Errorf("%s does not name a file", out)
	}
	if !filepath.IsAbs(out) && s.enabled() {
		out = filepath.Join(s.path, out)
	}
	d, err := s.dir(filepath.Dir(out))
	if err != nil {
		return "", err
	}
	return filepath.Join(d, name), nil
}

// writeNew creates name in dir through the root, never overwriting; see
// writeExclusive.
func (s *saveScope) writeNew(dir, name string, content []byte) (string, error) {
	rel, err := s.rel(dir)
	if err != nil {
		return "", err
	}
	if s.root == nil {
		return "", errors.New("dry run: nothing is written")
	}
	saved, err := writeExclusive(func(p string) (*os.File, error) {
		return s.root.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	}, filepath.Join(rel, name), content)
	if saved != "" {
		saved = filepath.Join(s.path, saved)
	}
	return saved, err
}

// saveDir validates and creates the directory a command writes into: as given
// for the CLI, confined for an MCP call.
func (r *RunContext) saveDir(out string) (string, error) {
	if r.saves != nil {
		return r.saves.dir(out)
	}
	if err := validateOutDir(out); err != nil {
		return "", err
	}
	return out, nil
}

// saveFile returns the file path a command writes to: as given for the CLI,
// confined for an MCP call.
func (r *RunContext) saveFile(out string) (string, error) {
	if r.saves != nil {
		return r.saves.file(out)
	}
	return out, nil
}

// writeNew creates name in a directory from saveDir or saveFile without
// overwriting anything, returning the path written.
func (r *RunContext) writeNew(dir, name string, content []byte) (string, error) {
	if r.saves != nil {
		return r.saves.writeNew(dir, name, content)
	}
	return safeWriteFile(filepath.Join(dir, name), content)
}

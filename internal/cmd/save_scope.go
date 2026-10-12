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
// It lives on RunContext, not RootFlags, so no tool argument or OLK_* variable
// can reach it.
type saveScope struct {
	root    string // resolved --download-dir; empty refuses every save
	refusal string // why saving is off, when root is empty
}

// errSavesOff is returned for any save through MCP that the operator has not
// enabled.
var errSavesOff = errors.New("saving files is off for MCP tools")

// newSaveScope resolves the operator's --download-dir, creating it if needed.
// With --no-write, or without a directory, every save is refused.
func newSaveScope(dir string, noWrite bool) (*saveScope, error) {
	switch {
	case noWrite:
		return &saveScope{refusal: "--no-write forbids saving files"}, nil
	case dir == "":
		return &saveScope{refusal: "start `olk mcp` with --download-dir to allow it"}, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating --download-dir: %w", err)
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("resolving --download-dir: %w", err)
	}
	real, err = filepath.Abs(real)
	if err != nil {
		return nil, fmt.Errorf("resolving --download-dir: %w", err)
	}
	info, err := os.Stat(real)
	if err != nil {
		return nil, fmt.Errorf("checking --download-dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("--download-dir %s is not a directory", dir)
	}
	return &saveScope{root: real}, nil
}

// enabled reports whether this scope permits any save.
func (s *saveScope) enabled() bool { return s.root != "" }

// dir confines an output directory. A relative one is taken under the root, so
// the default "." means the root itself. Every existing component, symlinks
// resolved, must stay inside the root, both before and after the missing ones
// are created.
func (s *saveScope) dir(out string) (string, error) {
	if !s.enabled() {
		return "", fmt.Errorf("%w: %s", errSavesOff, s.refusal)
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(s.root, out)
	}
	out = filepath.Clean(out)
	if !within(s.root, out) {
		return "", fmt.Errorf("%s is outside --download-dir %s", out, s.root)
	}
	if err := s.checkResolved(existingAncestor(out)); err != nil {
		return "", err
	}
	if err := validateOutDir(out); err != nil {
		return "", err
	}
	if err := s.checkResolved(out); err != nil {
		return "", err
	}
	return out, nil
}

// file confines an output file path: its directory as dir does, and its name
// kept as given.
func (s *saveScope) file(out string) (string, error) {
	name := filepath.Base(out)
	if name == "." || name == ".." || name == string(filepath.Separator) {
		return "", fmt.Errorf("%s does not name a file", out)
	}
	dir := filepath.Dir(out)
	if !filepath.IsAbs(out) {
		dir = filepath.Join(s.root, dir)
	}
	d, err := s.dir(dir)
	if err != nil {
		return "", err
	}
	return filepath.Join(d, name), nil
}

func (s *saveScope) checkResolved(path string) error {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", path, err)
	}
	if !within(s.root, real) {
		return fmt.Errorf("%s resolves outside --download-dir %s", path, s.root)
	}
	return nil
}

// existingAncestor returns path or its nearest ancestor that exists.
func existingAncestor(path string) string {
	for {
		if _, err := os.Lstat(path); err == nil {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		path = parent
	}
}

// within reports whether path is root or lies beneath it. Both must be clean
// and absolute.
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
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

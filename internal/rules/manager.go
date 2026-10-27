package rules

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/x0ptr/promptfix/internal/fsutil"
	"github.com/x0ptr/promptfix/internal/store"
)

var (
	ErrNotFound = errors.New("rule not found")
	ErrExists   = errors.New("rule already exists")
	ErrSymlink  = errors.New("rule is a symlink (read-only)")
)

// Rule is one entry of a rules directory listing.
type Rule struct {
	Rel      string // slash-separated path relative to the rules directory
	Paths    []string
	Lines    int
	Link     string // symlink target, if the file is a symlink
	ReadOnly string // reason why fields cannot be edited, if any
}

// Manager reads and writes the rules of one scope and keeps their history
// as raw bytes below HistoryDir.
type Manager struct {
	Scope      Scope
	HistoryDir string

	now func() time.Time
}

// NewManager returns a manager for scope with history below dataDir.
func NewManager(scope Scope, dataDir string) *Manager {
	return &Manager{
		Scope:      scope,
		HistoryDir: filepath.Join(dataDir, "rules-history", scope.ID),
		now:        time.Now,
	}
}

// Normalize turns user input into a clean relative rule path: forward
// slashes, ".md" appended if there is no extension, no escaping the
// rules directory.
func Normalize(rel string) (string, error) {
	rel = strings.TrimSpace(strings.ReplaceAll(rel, `\`, "/"))
	if rel == "" {
		return "", errors.New("empty rule path")
	}
	if path.IsAbs(rel) || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return "", fmt.Errorf("%q: rule path must be relative to the rules directory", rel)
	}
	rel = path.Clean(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("%q: rule path must stay inside the rules directory", rel)
	}
	if path.Ext(rel) == "" {
		rel += ".md"
	}
	if path.Ext(rel) != ".md" {
		return "", fmt.Errorf("%q: rule files must end in .md", rel)
	}
	return rel, nil
}

func (m *Manager) file(rel string) string {
	return filepath.Join(m.Scope.Dir, filepath.FromSlash(rel))
}

func (m *Manager) historyDir(rel string) string {
	return filepath.Join(m.HistoryDir, url.PathEscape(rel))
}

// Path returns the absolute path of a rule.
func (m *Manager) Path(rel string) string { return m.file(rel) }

// Exists reports whether the rule exists (file or symlink).
func (m *Manager) Exists(rel string) bool {
	_, err := os.Lstat(m.file(rel))
	return err == nil
}

// List returns all rules sorted by path. Symlinked directories are not
// followed.
func (m *Manager) List() ([]Rule, error) {
	var out []Rule
	err := filepath.WalkDir(m.Scope.Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && p == m.Scope.Dir {
				return fs.SkipAll
			}
			return err
		}
		if d.IsDir() || filepath.Ext(p) != ".md" {
			return nil
		}
		rel, err := filepath.Rel(m.Scope.Dir, p)
		if err != nil {
			return err
		}
		r := Rule{Rel: filepath.ToSlash(rel)}
		if d.Type()&fs.ModeSymlink != 0 {
			r.Link, _ = os.Readlink(p)
			r.ReadOnly = "symlink to " + r.Link
		}
		data, err := os.ReadFile(p)
		if err != nil {
			if r.Link == "" {
				return err
			}
			r.ReadOnly = "broken symlink to " + r.Link
			out = append(out, r)
			return nil
		}
		f := Parse(data)
		r.Paths = f.Paths()
		r.Lines = countLines(f.Body())
		if r.ReadOnly == "" {
			r.ReadOnly = f.ReadOnly()
		}
		out = append(out, r)
		return nil
	})
	return out, err
}

func countLines(s string) int {
	s = strings.TrimRight(s, "\r\n")
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// Read returns the parsed rule.
func (m *Manager) Read(rel string) (*File, error) {
	data, err := os.ReadFile(m.file(rel))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%s:%s: %w", m.Scope.Kind, rel, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return Parse(data), nil
}

// IsSymlink reports whether the rule is a symlink.
func (m *Manager) IsSymlink(rel string) bool {
	fi, err := os.Lstat(m.file(rel))
	return err == nil && fi.Mode()&fs.ModeSymlink != 0
}

// Write stores data as the rule content. A different existing version is
// archived first; identical content is a no-op. It reports whether
// anything was written.
func (m *Manager) Write(rel string, data []byte) (bool, error) {
	if m.IsSymlink(rel) {
		return false, fmt.Errorf("%s: %w", rel, ErrSymlink)
	}
	old, err := os.ReadFile(m.file(rel))
	switch {
	case err == nil:
		if bytes.Equal(old, data) {
			return false, nil
		}
		if err := m.archive(rel, old); err != nil {
			return false, err
		}
	case !errors.Is(err, fs.ErrNotExist):
		return false, err
	}
	return true, fsutil.WriteAtomic(m.file(rel), data, 0o644)
}

func (m *Manager) archive(rel string, data []byte) error {
	id := m.now().UTC().Format(store.RevisionFormat)
	return fsutil.WriteAtomic(filepath.Join(m.historyDir(rel), id+".md"), data, 0o644)
}

// Delete removes a rule; the content stays in the history.
func (m *Manager) Delete(rel string) error {
	if m.IsSymlink(rel) {
		return fmt.Errorf("%s: %w", rel, ErrSymlink)
	}
	data, err := os.ReadFile(m.file(rel))
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s:%s: %w", m.Scope.Kind, rel, ErrNotFound)
	}
	if err != nil {
		return err
	}
	if err := m.archive(rel, data); err != nil {
		return err
	}
	if err := os.Remove(m.file(rel)); err != nil {
		return err
	}
	m.removeEmptyDirs(filepath.Dir(m.file(rel)))
	return nil
}

// removeEmptyDirs removes empty subdirectories up to the rules directory.
func (m *Manager) removeEmptyDirs(dir string) {
	for dir != m.Scope.Dir && strings.HasPrefix(dir, m.Scope.Dir) {
		if os.Remove(dir) != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// Transfer copies (or moves) a rule to another scope. An existing target is
// only replaced with overwrite set; its old content goes to the history.
func (m *Manager) Transfer(rel string, dst *Manager, move, overwrite bool) error {
	if m.IsSymlink(rel) {
		return fmt.Errorf("%s: %w", rel, ErrSymlink)
	}
	data, err := os.ReadFile(m.file(rel))
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s:%s: %w", m.Scope.Kind, rel, ErrNotFound)
	}
	if err != nil {
		return err
	}
	if dst.Exists(rel) && !overwrite {
		return fmt.Errorf("%s:%s: %w", dst.Scope.Kind, rel, ErrExists)
	}
	if _, err := dst.Write(rel, data); err != nil {
		return err
	}
	if move {
		return m.Delete(rel)
	}
	return nil
}

// History returns the revisions of a rule, newest first.
func (m *Manager) History(rel string) ([]store.Revision, error) {
	entries, err := os.ReadDir(m.historyDir(rel))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var revs []store.Revision
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok {
			continue
		}
		t, err := time.Parse(store.RevisionFormat, id)
		if err != nil {
			continue
		}
		revs = append(revs, store.Revision{ID: id, Time: t})
	}
	sort.Slice(revs, func(i, j int) bool { return revs[i].Time.After(revs[j].Time) })
	return revs, nil
}

// Revision returns the raw content of a historic version.
func (m *Manager) Revision(rel, id string) ([]byte, error) {
	if strings.ContainsAny(id, `/\`) {
		return nil, fmt.Errorf("invalid revision %q", id)
	}
	data, err := os.ReadFile(filepath.Join(m.historyDir(rel), id+".md"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("revision %q of %s not found", id, rel)
	}
	return data, err
}

// Restore makes a historic version current; the current one is archived.
func (m *Manager) Restore(rel, id string) error {
	data, err := m.Revision(rel, id)
	if err != nil {
		return err
	}
	_, err = m.Write(rel, data)
	return err
}

// Prune deletes history entries of one rule, or of all rules of the scope
// if rel is empty. It returns the number of deleted entries.
func (m *Manager) Prune(rel string, o store.PruneOptions) (int, error) {
	rels := []string{rel}
	if rel == "" {
		entries, err := os.ReadDir(m.HistoryDir)
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		if err != nil {
			return 0, err
		}
		rels = rels[:0]
		for _, e := range entries {
			if r, err := url.PathUnescape(e.Name()); err == nil && e.IsDir() {
				rels = append(rels, r)
			}
		}
	}
	cutoff := m.now().Add(-o.OlderThan)
	n := 0
	for _, r := range rels {
		revs, err := m.History(r)
		if err != nil {
			return n, err
		}
		for i, rev := range revs {
			if !o.Drop(i, rev.Time, cutoff) {
				continue
			}
			if err := os.Remove(filepath.Join(m.historyDir(r), rev.ID+".md")); err != nil {
				return n, err
			}
			n++
		}
		_ = os.Remove(m.historyDir(r))
	}
	return n, nil
}

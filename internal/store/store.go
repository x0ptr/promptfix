// Package store persists prompts as YAML files and keeps their history.
//
// Layout below the data directory:
//
//	prompts/<slug>.yaml
//	history/<slug>/<revision>.yaml
package store

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/x0ptr/promptfix/internal/prompt"
)

// RevisionFormat is the time layout of history file names (UTC, no colons
// so it is valid on Windows).
const RevisionFormat = "20060102T150405.000000000Z"

var (
	ErrNotFound = errors.New("prompt not found")
	ErrExists   = errors.New("prompt already exists")
)

// Store reads and writes prompts below Dir.
type Store struct {
	Dir  string
	Keep int // history entries to keep per prompt; 0 = unlimited

	now func() time.Time
}

// New returns a store rooted at dir.
func New(dir string, keep int) *Store {
	return &Store{Dir: dir, Keep: keep, now: time.Now}
}

// Revision is one historic version of a prompt.
type Revision struct {
	ID   string
	Time time.Time
}

func (s *Store) promptPath(slug string) string {
	return filepath.Join(s.Dir, "prompts", slug+".yaml")
}

func (s *Store) historyDir(slug string) string {
	return filepath.Join(s.Dir, "history", slug)
}

// Exists reports whether a prompt with the given name exists.
func (s *Store) Exists(name string) bool {
	_, err := os.Stat(s.promptPath(prompt.Slug(name)))
	return err == nil
}

// Load reads the current version of a prompt by name or slug.
func (s *Store) Load(name string) (*prompt.Prompt, error) {
	slug := prompt.Slug(name)
	p, err := readFile(s.promptPath(slug))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%q: %w", name, ErrNotFound)
	}
	return p, err
}

// List returns all prompts sorted by slug.
func (s *Store) List() ([]*prompt.Prompt, error) {
	entries, err := os.ReadDir(filepath.Join(s.Dir, "prompts"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*prompt.Prompt
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		p, err := readFile(filepath.Join(s.Dir, "prompts", e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// Save writes p as the current version. If a different version exists, it is
// moved to the history first. Saving unchanged content is a no-op.
// It reports whether anything was written.
func (s *Store) Save(p *prompt.Prompt) (bool, error) {
	if err := prompt.ValidateName(p.Name); err != nil {
		return false, err
	}
	slug := p.Slug()
	old, err := readFile(s.promptPath(slug))
	switch {
	case err == nil:
		if old.SameContent(p) {
			return false, nil
		}
		if p.Created.IsZero() {
			p.Created = old.Created
		}
		if err := s.archive(slug); err != nil {
			return false, err
		}
	case !errors.Is(err, fs.ErrNotExist):
		return false, err
	}
	now := s.now().UTC()
	if p.Created.IsZero() {
		p.Created = now
	}
	p.Updated = now
	p.Version = prompt.FormatVersion
	if err := writeFile(s.promptPath(slug), p); err != nil {
		return false, err
	}
	if s.Keep > 0 {
		if _, err := s.Prune(slug, PruneOptions{Keep: s.Keep}); err != nil {
			return true, err
		}
	}
	return true, nil
}

// archive moves the current version of slug into its history.
func (s *Store) archive(slug string) error {
	dir := s.historyDir(slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	id := s.now().UTC().Format(RevisionFormat)
	return os.Rename(s.promptPath(slug), filepath.Join(dir, id+".yaml"))
}

// Delete removes the current version of a prompt. The removed version is kept
// in the history, so it can be restored until it is pruned.
func (s *Store) Delete(name string) error {
	slug := prompt.Slug(name)
	if !s.Exists(slug) {
		return fmt.Errorf("%q: %w", name, ErrNotFound)
	}
	return s.archive(slug)
}

// Copy stores a copy of the current version of src under the name dst.
func (s *Store) Copy(src, dst string) error {
	p, err := s.Load(src)
	if err != nil {
		return err
	}
	if err := s.checkNew(dst); err != nil {
		return err
	}
	p.Name = dst
	p.Created = time.Time{}
	_, err = s.Save(p)
	return err
}

// Rename renames a prompt including its history.
func (s *Store) Rename(src, dst string) error {
	p, err := s.Load(src)
	if err != nil {
		return err
	}
	oldSlug := p.Slug()
	if prompt.Slug(dst) == oldSlug {
		// Only the display name changes.
		p.Name = dst
		return writeFile(s.promptPath(oldSlug), p)
	}
	if err := s.checkNew(dst); err != nil {
		return err
	}
	p.Name = dst
	if err := writeFile(s.promptPath(p.Slug()), p); err != nil {
		return err
	}
	if err := os.Remove(s.promptPath(oldSlug)); err != nil {
		return err
	}
	err = os.Rename(s.historyDir(oldSlug), s.historyDir(p.Slug()))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Store) checkNew(name string) error {
	if err := prompt.ValidateName(name); err != nil {
		return err
	}
	if s.Exists(name) {
		return fmt.Errorf("%q: %w", name, ErrExists)
	}
	return nil
}

// History returns the revisions of a prompt, newest first.
func (s *Store) History(name string) ([]Revision, error) {
	entries, err := os.ReadDir(s.historyDir(prompt.Slug(name)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var revs []Revision
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".yaml")
		if !ok {
			continue
		}
		t, err := time.Parse(RevisionFormat, id)
		if err != nil {
			continue
		}
		revs = append(revs, Revision{ID: id, Time: t})
	}
	sort.Slice(revs, func(i, j int) bool { return revs[i].Time.After(revs[j].Time) })
	return revs, nil
}

// LoadRevision reads one historic version of a prompt.
func (s *Store) LoadRevision(name, id string) (*prompt.Prompt, error) {
	if strings.ContainsAny(id, `/\`) {
		return nil, fmt.Errorf("invalid revision %q", id)
	}
	p, err := readFile(filepath.Join(s.historyDir(prompt.Slug(name)), id+".yaml"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("revision %q of %q not found", id, name)
	}
	return p, err
}

// Restore makes a historic version current again. The current version, if
// any, is moved to the history.
func (s *Store) Restore(name, id string) error {
	p, err := s.LoadRevision(name, id)
	if err != nil {
		return err
	}
	// Keep the slug even if the prompt was renamed after this revision.
	p.Name = name
	if cur, err := s.Load(name); err == nil {
		p.Name = cur.Name
	}
	_, err = s.Save(p)
	return err
}

// PruneOptions selects the history entries to delete. With Keep and
// OlderThan both set, an entry is deleted if either rule matches.
type PruneOptions struct {
	Keep      int           // keep the newest N entries; 0 = no limit
	OlderThan time.Duration // delete entries older than this; 0 = no limit
	All       bool          // delete every entry
}

// Prune deletes history entries of one prompt, or of all prompts if name is
// empty. It returns the number of deleted entries.
func (s *Store) Prune(name string, o PruneOptions) (int, error) {
	slugs := []string{prompt.Slug(name)}
	if name == "" {
		entries, err := os.ReadDir(filepath.Join(s.Dir, "history"))
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		if err != nil {
			return 0, err
		}
		slugs = slugs[:0]
		for _, e := range entries {
			if e.IsDir() {
				slugs = append(slugs, e.Name())
			}
		}
	}
	cutoff := s.now().Add(-o.OlderThan)
	n := 0
	for _, slug := range slugs {
		revs, err := s.History(slug)
		if err != nil {
			return n, err
		}
		for i, r := range revs {
			drop := o.All ||
				(o.Keep > 0 && i >= o.Keep) ||
				(o.OlderThan > 0 && r.Time.Before(cutoff))
			if !drop {
				continue
			}
			if err := os.Remove(filepath.Join(s.historyDir(slug), r.ID+".yaml")); err != nil {
				return n, err
			}
			n++
		}
		// Remove the directory once it is empty; ignore the error otherwise.
		_ = os.Remove(s.historyDir(slug))
	}
	return n, nil
}

func readFile(path string) (*prompt.Prompt, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p prompt.Prompt
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if p.Version > prompt.FormatVersion {
		return nil, fmt.Errorf("%s: file format version %d is newer than supported (%d)", path, p.Version, prompt.FormatVersion)
	}
	return &p, nil
}

// writeFile writes p atomically: temp file in the same directory, then rename.
func writeFile(path string, p *prompt.Prompt) error {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(p); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

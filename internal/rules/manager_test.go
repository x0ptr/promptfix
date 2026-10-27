package rules

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/x0ptr/promptfix/internal/store"
)

func newTestManager(t *testing.T, kind Kind) *Manager {
	t.Helper()
	root := t.TempDir()
	var sc Scope
	if kind == User {
		t.Setenv("CLAUDE_CONFIG_DIR", root)
		var err error
		if sc, err = UserScope(); err != nil {
			t.Fatal(err)
		}
	} else {
		var err error
		if sc, err = ProjectScope(root); err != nil {
			t.Fatal(err)
		}
	}
	m := NewManager(sc, t.TempDir())
	clock := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time {
		clock = clock.Add(time.Second)
		return clock
	}
	return m
}

func mustWrite(t *testing.T, m *Manager, rel, content string) {
	t.Helper()
	if _, err := m.Write(rel, []byte(content)); err != nil {
		t.Fatal(err)
	}
}

func TestNormalize(t *testing.T) {
	ok := map[string]string{
		"tenant":                  "tenant.md",
		" security/tenant-id.md ": "security/tenant-id.md",
		`a\b`:                     "a/b.md",
		"a/./b/../c":              "a/c.md",
	}
	for in, want := range ok {
		if got, err := Normalize(in); err != nil || got != want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "../x", "/abs/x.md", "a/../../x", "x.txt", "."} {
		if _, err := Normalize(in); err == nil {
			t.Errorf("Normalize(%q) should fail", in)
		}
	}
}

// R1: a project rule with paths lands in .claude/rules/<name>.md.
func TestWriteListRead(t *testing.T) {
	m := newTestManager(t, Project)
	mustWrite(t, m, "security/tenant-id.md", string(New([]string{"src/api/**"}, "Never trust request params.\n")))
	mustWrite(t, m, "style.md", "Use gofmt.\nKeep it short.\n")

	path := filepath.Join(m.Scope.Root, ".claude", "rules", "security", "tenant-id.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "---\npaths:\n  - \"src/api/**\"\n---\nNever trust request params.\n" {
		t.Fatalf("file content %q", data)
	}

	list, err := m.List()
	if err != nil || len(list) != 2 {
		t.Fatalf("List = %v, %v", list, err)
	}
	if list[0].Rel != "security/tenant-id.md" || !slices.Equal(list[0].Paths, []string{"src/api/**"}) || list[0].Lines != 1 {
		t.Errorf("rule 0 = %+v", list[0])
	}
	if list[1].Rel != "style.md" || list[1].Paths != nil || list[1].Lines != 2 {
		t.Errorf("rule 1 = %+v", list[1])
	}
}

func TestListMissingDir(t *testing.T) {
	m := newTestManager(t, User)
	if list, err := m.List(); err != nil || len(list) != 0 {
		t.Fatalf("List = %v, %v", list, err)
	}
}

// R4: deleting keeps the content restorable.
func TestDeleteRestoreHistory(t *testing.T) {
	m := newTestManager(t, User)
	mustWrite(t, m, "sub/a.md", "v1")
	if wrote, _ := m.Write("sub/a.md", []byte("v1")); wrote {
		t.Fatal("identical write should be a no-op")
	}
	mustWrite(t, m, "sub/a.md", "v2")
	if err := m.Delete("sub/a.md"); err != nil {
		t.Fatal(err)
	}
	if m.Exists("sub/a.md") {
		t.Fatal("not deleted")
	}
	if _, err := os.Stat(filepath.Join(m.Scope.Dir, "sub")); !errors.Is(err, os.ErrNotExist) {
		t.Error("empty subdirectory should be removed")
	}
	revs, err := m.History("sub/a.md")
	if err != nil || len(revs) != 2 {
		t.Fatalf("History = %v, %v", revs, err)
	}
	if err := m.Restore("sub/a.md", revs[0].ID); err != nil {
		t.Fatal(err)
	}
	if f, _ := m.Read("sub/a.md"); f.Body() != "v2" {
		t.Fatalf("restored %q", f.Body())
	}
	if err := m.Restore("sub/a.md", revs[1].ID); err != nil {
		t.Fatal(err)
	}
	if f, _ := m.Read("sub/a.md"); f.Body() != "v1" {
		t.Fatalf("restored %q", f.Body())
	}

	n, err := m.Prune("", store.PruneOptions{Keep: 1})
	if err != nil || n != 2 {
		t.Fatalf("Prune keep=1: %d, %v", n, err)
	}
	n, err = m.Prune("sub/a.md", store.PruneOptions{All: true})
	if err != nil || n != 1 {
		t.Fatalf("Prune all: %d, %v", n, err)
	}
}

func TestTransfer(t *testing.T) {
	user := newTestManager(t, User)
	proj := newTestManager(t, Project)
	mustWrite(t, proj, "a.md", "project a")

	if err := proj.Transfer("a.md", user, false, false); err != nil {
		t.Fatal(err)
	}
	if !user.Exists("a.md") || !proj.Exists("a.md") {
		t.Fatal("copy should keep both")
	}
	mustWrite(t, proj, "a.md", "project a v2")
	if err := proj.Transfer("a.md", user, true, false); !errors.Is(err, ErrExists) {
		t.Fatalf("expected ErrExists, got %v", err)
	}
	if err := proj.Transfer("a.md", user, true, true); err != nil {
		t.Fatal(err)
	}
	if proj.Exists("a.md") {
		t.Fatal("move should remove the source")
	}
	if f, _ := user.Read("a.md"); f.Body() != "project a v2" {
		t.Fatalf("user content %q", f.Body())
	}
	if revs, _ := user.History("a.md"); len(revs) != 1 {
		t.Fatal("overwritten target should be archived")
	}
}

func TestSymlinkReadOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	m := newTestManager(t, User)
	target := filepath.Join(t.TempDir(), "shared.md")
	os.WriteFile(target, []byte("shared"), 0o644)
	os.MkdirAll(m.Scope.Dir, 0o755)
	if err := os.Symlink(target, filepath.Join(m.Scope.Dir, "link.md")); err != nil {
		t.Fatal(err)
	}
	list, _ := m.List()
	if len(list) != 1 || list[0].Link != target || list[0].ReadOnly == "" {
		t.Fatalf("list = %+v", list)
	}
	if _, err := m.Write("link.md", []byte("x")); !errors.Is(err, ErrSymlink) {
		t.Fatalf("write: %v", err)
	}
	if err := m.Delete("link.md"); !errors.Is(err, ErrSymlink) {
		t.Fatalf("delete: %v", err)
	}
	if data, _ := os.ReadFile(target); string(data) != "shared" {
		t.Fatal("target modified")
	}
}

// R5: without a git root there is no project scope.
func TestFindGitRoot(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b")
	os.MkdirAll(deep, 0o755)
	if _, ok := FindGitRoot(deep); ok {
		// The temp dir could be inside a git repo on some machines.
		t.Skip("temp dir is inside a git repository")
	}
	os.Mkdir(filepath.Join(root, ".git"), 0o755)
	got, ok := FindGitRoot(deep)
	if !ok || got != root {
		t.Fatalf("FindGitRoot = %q, %v", got, ok)
	}
	// Worktrees and submodules have a .git file.
	sub := filepath.Join(root, "a")
	os.WriteFile(filepath.Join(sub, ".git"), []byte("gitdir: x"), 0o644)
	if got, _ := FindGitRoot(deep); got != sub {
		t.Fatalf("FindGitRoot with .git file = %q", got)
	}
}

func TestScopes(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	u, err := UserScope()
	if err != nil || u.Dir != filepath.Join(home, ".claude", "rules") {
		t.Fatalf("UserScope = %+v, %v", u, err)
	}
	p1, _ := ProjectScope("/x/my-proj")
	p2, _ := ProjectScope("/y/my-proj")
	abs, _ := filepath.Abs("/x/my-proj") // gets a drive letter on Windows
	if p1.ID == p2.ID || p1.Dir != filepath.Join(abs, ".claude", "rules") {
		t.Fatalf("project scopes %+v %+v", p1, p2)
	}
}

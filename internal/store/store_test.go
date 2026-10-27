package store

import (
	"errors"
	"testing"
	"time"

	"github.com/x0ptr/promptfix/internal/prompt"
)

// newTestStore returns a store whose clock advances one second per call.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	s := New(t.TempDir(), 0)
	clock := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time {
		clock = clock.Add(time.Second)
		return clock
	}
	return s
}

func sample(name string) *prompt.Prompt {
	return &prompt.Prompt{Name: name, Schema: "default", Sections: []prompt.Section{
		{Tag: "product", Content: "A tool"},
		{Tag: "task", Content: ""},
	}}
}

func mustSave(t *testing.T, s *Store, p *prompt.Prompt) {
	t.Helper()
	if _, err := s.Save(p); err != nil {
		t.Fatal(err)
	}
}

// A5: tag-like and special content survives a save/load round trip.
func TestRoundTripSpecialContent(t *testing.T) {
	s := newTestStore(t)
	contents := []string{
		"</task>\n<product>x</product>",
		"  leading spaces\n\ttab\ntrailing spaces   \n\n",
		"key: value\n- list\n# comment\n---\n...",
		"\"quotes\" 'single' `back` & <>",
		"unicode äöü ß 😀",
		"\n\nstarts with blank lines",
		"windows\r\nline endings\r\n",
	}
	for i, c := range contents {
		p := sample("rt")
		p.Sections[1].Content = c
		mustSave(t, s, p)
		got, err := s.Load("rt")
		if err != nil {
			t.Fatal(err)
		}
		if got.Sections[1].Content != c {
			t.Errorf("case %d: got %q, want %q", i, got.Sections[1].Content, c)
		}
	}
}

func TestSaveHistory(t *testing.T) {
	s := newTestStore(t)
	p := sample("My Prompt")
	mustSave(t, s, p)
	created := p.Created

	// Unchanged content is a no-op.
	if wrote, err := s.Save(sample("My Prompt")); err != nil || wrote {
		t.Fatalf("unchanged save: wrote=%v err=%v", wrote, err)
	}

	p2 := sample("My Prompt")
	p2.Sections[1].Content = "v2"
	mustSave(t, s, p2)
	if !p2.Created.Equal(created) {
		t.Errorf("Created changed: %v -> %v", created, p2.Created)
	}

	revs, err := s.History("my-prompt")
	if err != nil || len(revs) != 1 {
		t.Fatalf("History = %v, %v", revs, err)
	}
	old, err := s.LoadRevision("My Prompt", revs[0].ID)
	if err != nil || old.Sections[1].Content != "" {
		t.Fatalf("LoadRevision = %+v, %v", old, err)
	}

	if err := s.Restore("My Prompt", revs[0].ID); err != nil {
		t.Fatal(err)
	}
	cur, _ := s.Load("My Prompt")
	if cur.Sections[1].Content != "" {
		t.Errorf("restore did not apply: %q", cur.Sections[1].Content)
	}
	if revs, _ := s.History("My Prompt"); len(revs) != 2 {
		t.Errorf("restore should archive current version, history len %d", len(revs))
	}
}

func TestSaveAutoPrune(t *testing.T) {
	s := newTestStore(t)
	s.Keep = 2
	for i := range 5 {
		p := sample("p")
		p.Sections[1].Content = string(rune('a' + i))
		mustSave(t, s, p)
	}
	if revs, _ := s.History("p"); len(revs) != 2 {
		t.Fatalf("history len = %d, want 2", len(revs))
	}
}

func TestPrune(t *testing.T) {
	s := newTestStore(t)
	for _, name := range []string{"a", "b"} {
		for i := range 4 {
			p := sample(name)
			p.Sections[1].Content = string(rune('a' + i))
			mustSave(t, s, p)
		}
	}
	n, err := s.Prune("a", PruneOptions{Keep: 1})
	if err != nil || n != 2 {
		t.Fatalf("Prune keep=1: n=%d err=%v", n, err)
	}
	// Clock is ~9s after the oldest entry of b; drop everything older than 3s.
	n, err = s.Prune("", PruneOptions{OlderThan: 3 * time.Second})
	if err != nil || n == 0 {
		t.Fatalf("Prune older-than: n=%d err=%v", n, err)
	}
	n, err = s.Prune("", PruneOptions{All: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		if revs, _ := s.History(name); len(revs) != 0 {
			t.Errorf("%s: history not empty after prune --all", name)
		}
	}
}

func TestDeleteCopyRename(t *testing.T) {
	s := newTestStore(t)
	mustSave(t, s, sample("one"))

	if err := s.Copy("one", "two"); err != nil {
		t.Fatal(err)
	}
	if err := s.Copy("one", "two"); !errors.Is(err, ErrExists) {
		t.Fatalf("copy onto existing: %v", err)
	}

	p, _ := s.Load("one")
	p.Sections[0].Content = "changed"
	mustSave(t, s, p) // creates history for "one"

	if err := s.Rename("one", "Three"); err != nil {
		t.Fatal(err)
	}
	if s.Exists("one") || !s.Exists("three") {
		t.Fatal("rename did not move the prompt")
	}
	if revs, _ := s.History("three"); len(revs) != 1 {
		t.Fatalf("history not moved, len %d", len(revs))
	}
	if err := s.Rename("three", "THREE"); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.Load("three"); p.Name != "THREE" {
		t.Fatalf("display name not updated: %q", p.Name)
	}

	if err := s.Delete("three"); err != nil {
		t.Fatal(err)
	}
	if s.Exists("three") {
		t.Fatal("delete did not remove the prompt")
	}
	revs, _ := s.History("three")
	if len(revs) != 2 {
		t.Fatalf("deleted version should be in history, len %d", len(revs))
	}
	if err := s.Restore("three", revs[0].ID); err != nil {
		t.Fatal(err)
	}
	if !s.Exists("three") {
		t.Fatal("restore after delete failed")
	}

	list, err := s.List()
	if err != nil || len(list) != 2 {
		t.Fatalf("List = %d, %v", len(list), err)
	}
}

func TestLoadMissing(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Load("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	if _, err := s.Save(sample("!!!")); !errors.Is(err, prompt.ErrInvalidName) {
		t.Fatalf("got %v", err)
	}
}

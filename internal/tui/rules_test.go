package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/x0ptr/promptfix/internal/rules"
)

type rulesHarness struct {
	t          *testing.T
	m          *RulesModel
	user, proj *rules.Manager
}

func newRulesHarness(t *testing.T) *rulesHarness {
	t.Helper()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	us, _ := rules.UserScope()
	ps, _ := rules.ProjectScope(t.TempDir())
	data := t.TempDir()
	h := &rulesHarness{t: t, user: rules.NewManager(us, data), proj: rules.NewManager(ps, data)}
	return h
}

func (h *rulesHarness) start(o RulesOptions) {
	h.t.Helper()
	if o.Scopes == nil {
		o.Scopes = []*rules.Manager{h.user, h.proj}
		o.Start = 1
	}
	m, err := NewRules(o)
	if err != nil {
		h.t.Fatal(err)
	}
	h.m = m
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
}

func (h *rulesHarness) key(name string) tea.Cmd {
	var k tea.KeyPressMsg
	switch {
	case strings.HasPrefix(name, "ctrl+"):
		k = tea.KeyPressMsg{Code: rune(name[5]), Mod: tea.ModCtrl}
	case name == "enter":
		k = tea.KeyPressMsg{Code: tea.KeyEnter}
	case name == "esc":
		k = tea.KeyPressMsg{Code: tea.KeyEscape}
	case name == "tab":
		k = tea.KeyPressMsg{Code: tea.KeyTab}
	case name == "down":
		k = tea.KeyPressMsg{Code: tea.KeyDown}
	default:
		k = tea.KeyPressMsg{Code: rune(name[0]), Text: name}
	}
	_, cmd := h.m.Update(k)
	return cmd
}

func (h *rulesHarness) typeText(s string) {
	for _, r := range s {
		if r == '\n' {
			h.key("enter")
			continue
		}
		h.m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// R1: create a project rule with paths through the UI.
func TestRulesCreate(t *testing.T) {
	h := newRulesHarness(t)
	h.start(RulesOptions{})
	h.key("n")
	h.typeText("security/tenant-id")
	h.key("enter")
	if h.m.mode != rEdit || h.m.rel != "security/tenant-id.md" {
		t.Fatalf("mode=%v rel=%q status=%q", h.m.mode, h.m.rel, h.m.status)
	}
	h.typeText("Derive tenant_id from the token only.")
	h.key("tab")
	h.typeText("src/api/**\nsrc/db/*.sql")
	h.key("ctrl+s")
	got := readFile(t, filepath.Join(h.proj.Scope.Dir, "security", "tenant-id.md"))
	want := "---\npaths:\n  - \"src/api/**\"\n  - \"src/db/*.sql\"\n---\nDerive tenant_id from the token only."
	if got != want {
		t.Fatalf("file:\n%q\nwant\n%q", got, want)
	}
	if h.m.dirty() {
		t.Error("dirty after save")
	}
	h.key("esc")
	if h.m.mode != rList || len(h.m.list) != 1 {
		t.Fatalf("back to list: mode=%v list=%v", h.m.mode, h.m.list)
	}
}

// R2: switching scopes shows the right lists; same names are flagged.
func TestRulesScopes(t *testing.T) {
	h := newRulesHarness(t)
	h.user.Write("shared.md", []byte("u"))
	h.user.Write("only-user.md", []byte("u"))
	h.proj.Write("shared.md", []byte("p"))
	h.start(RulesOptions{})
	if len(h.m.list) != 1 || !h.m.other["shared.md"] {
		t.Fatalf("project list %+v other %v", h.m.list, h.m.other)
	}
	h.key("tab")
	if h.m.mgr() != h.user || len(h.m.list) != 2 {
		t.Fatalf("user list %+v", h.m.list)
	}
	if !strings.Contains(h.m.View().Content, "also in project") {
		t.Error("shared rule not flagged")
	}
}

// R3: editing only the body keeps unknown frontmatter byte-identical.
func TestRulesEditPreserves(t *testing.T) {
	h := newRulesHarness(t)
	src := "---\n# keep me\nowner: x\npaths:\n- a/**\n---\n\tTabbed body\r\nline\n"
	h.proj.Write("r.md", []byte(src))
	h.start(RulesOptions{})
	h.key("enter")
	if h.m.dirty() {
		t.Fatal("opening must not change anything")
	}
	h.key("ctrl+s")
	if got := readFile(t, h.proj.Path("r.md")); got != src {
		t.Fatalf("untouched save changed file:\n%q", got)
	}
	h.m.bodyTA.SetValue("new body\n")
	h.key("ctrl+s")
	want := "---\n# keep me\nowner: x\npaths:\n- a/**\n---\nnew body\n"
	if got := readFile(t, h.proj.Path("r.md")); got != want {
		t.Fatalf("body edit:\n%q\nwant\n%q", got, want)
	}
}

func TestRulesReadOnly(t *testing.T) {
	h := newRulesHarness(t)
	h.proj.Write("bad.md", []byte("---\npaths: [x\n---\nbody"))
	h.start(RulesOptions{})
	h.key("enter")
	if h.m.readOnly() == "" {
		t.Fatal("expected read-only")
	}
	h.typeText("zzz")
	h.key("ctrl+s")
	if got := readFile(t, h.proj.Path("bad.md")); got != "---\npaths: [x\n---\nbody" {
		t.Fatalf("read-only file changed: %q", got)
	}
}

// R4: delete via UI, then restore from history.
func TestRulesDeleteRestore(t *testing.T) {
	h := newRulesHarness(t)
	h.proj.Write("a.md", []byte("v1"))
	h.start(RulesOptions{})
	h.key("d")
	if h.m.mode != rConfirm {
		t.Fatal("delete should ask")
	}
	h.key("y")
	if h.proj.Exists("a.md") || len(h.m.list) != 0 {
		t.Fatal("not deleted")
	}
	// Recreate so it is selectable, then restore the deleted version.
	h.proj.Write("a.md", []byte("v2"))
	h.m.reload()
	h.key("h")
	if h.m.mode != rHistory || len(h.m.revs) != 1 {
		t.Fatalf("history mode=%v revs=%v", h.m.mode, h.m.revs)
	}
	h.key("enter")
	h.key("y")
	if got := readFile(t, h.proj.Path("a.md")); got != "v1" {
		t.Fatalf("restored %q", got)
	}
}

func TestRulesCopyMove(t *testing.T) {
	h := newRulesHarness(t)
	h.proj.Write("a.md", []byte("p"))
	h.user.Write("a.md", []byte("u"))
	h.start(RulesOptions{})
	h.key("c")
	if h.m.mode != rConfirm {
		t.Fatal("existing target should ask")
	}
	h.key("n")
	if readFile(t, h.user.Path("a.md")) != "u" {
		t.Fatal("declined overwrite changed target")
	}
	h.key("m")
	h.key("y")
	if h.proj.Exists("a.md") || readFile(t, h.user.Path("a.md")) != "p" {
		t.Fatal("move failed")
	}
}

// R5: user scope only.
func TestRulesUserOnly(t *testing.T) {
	h := newRulesHarness(t)
	h.start(RulesOptions{Scopes: []*rules.Manager{h.user}, Note: "No git repository found"})
	h.key("tab")
	if !h.m.statusErr || h.m.mgr() != h.user {
		t.Fatal("tab must not switch without project scope")
	}
	if !strings.Contains(h.m.View().Content, "No git repository found") {
		t.Error("note not shown")
	}
}

func TestRulesDiscardConfirm(t *testing.T) {
	h := newRulesHarness(t)
	h.start(RulesOptions{Open: "x.md", New: true})
	h.typeText("text")
	h.key("esc")
	if h.m.mode != rConfirm {
		t.Fatal("unsaved changes should ask")
	}
	h.key("n")
	if h.m.mode != rEdit {
		t.Fatal("n should return to the editor")
	}
	h.key("esc")
	h.key("y")
	if h.m.mode != rList || h.proj.Exists("x.md") {
		t.Fatal("discard should not write")
	}
}

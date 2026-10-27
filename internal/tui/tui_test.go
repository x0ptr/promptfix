package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/x0ptr/promptfix/internal/prompt"
	"github.com/x0ptr/promptfix/internal/schema"
)

func testSchema() schema.Schema {
	s, _ := schema.Default().Schema("")
	return s
}

func newPrompt(s schema.Schema) *prompt.Prompt {
	p := &prompt.Prompt{Name: "test", Schema: s.Name}
	for _, sec := range s.Sections {
		p.Sections = append(p.Sections, prompt.Section{Tag: sec.Tag})
	}
	return p
}

type harness struct {
	t     *testing.T
	m     *Model
	saves []string
}

func newHarness(t *testing.T, p *prompt.Prompt, wizard bool) *harness {
	h := &harness{t: t}
	h.m = New(Options{
		Prompt: p,
		Schema: testSchema(),
		Wizard: wizard,
		Save: func(p *prompt.Prompt) error {
			h.saves = append(h.saves, prompt.Render(p))
			return nil
		},
	})
	h.m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return h
}

// key sends a named key such as "ctrl+n", "enter" or "esc".
func (h *harness) key(name string) tea.Cmd {
	var k tea.KeyPressMsg
	switch {
	case strings.HasPrefix(name, "ctrl+"):
		k = tea.KeyPressMsg{Code: rune(name[5]), Mod: tea.ModCtrl}
	case name == "enter":
		k = tea.KeyPressMsg{Code: tea.KeyEnter}
	case name == "esc":
		k = tea.KeyPressMsg{Code: tea.KeyEscape}
	case name == "down":
		k = tea.KeyPressMsg{Code: tea.KeyDown}
	default:
		k = tea.KeyPressMsg{Code: rune(name[0]), Text: name}
	}
	if k.String() != name {
		h.t.Fatalf("key %q encodes as %q", name, k.String())
	}
	_, cmd := h.m.Update(k)
	return cmd
}

func (h *harness) typeText(s string) {
	for _, r := range s {
		if r == '\n' {
			h.key("enter")
			continue
		}
		h.m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// A1: walk the wizard, fill sections, save; output has all tags in order.
func TestWizardFillAndSave(t *testing.T) {
	p := newPrompt(testSchema())
	h := newHarness(t, p, true)

	h.typeText("A prompt builder\nfor the CLI")
	h.key("ctrl+n")
	h.key("ctrl+n")
	h.typeText("Go 1.27")
	for range len(p.Sections) - 2 {
		h.key("ctrl+n")
	}
	if h.m.mode != modeOverview {
		t.Fatalf("after last section mode = %v, want overview", h.m.mode)
	}
	h.key("s")
	if len(h.saves) != 1 {
		t.Fatalf("saves = %d", len(h.saves))
	}
	out := h.saves[0]
	if !strings.HasPrefix(out, "<product>\nA prompt builder\nfor the CLI\n</product>\n") {
		t.Errorf("unexpected product section:\n%s", out)
	}
	if !strings.Contains(out, "<technik>\nGo 1.27\n</technik>") || !strings.Contains(out, "<glossar></glossar>") {
		t.Errorf("unexpected output:\n%s", out)
	}
	last := -1
	for _, sec := range testSchema().Sections {
		i := strings.Index(out, "<"+sec.Tag+">")
		if i < 0 {
			i = strings.Index(out, "<"+sec.Tag+"></")
		}
		if i <= last {
			t.Errorf("tag %q missing or out of order", sec.Tag)
		}
		last = i
	}
	if !isQuit(h.key("q")) {
		t.Error("q after save should quit")
	}
}

// A2: abort after step 3 with save; the draft keeps all input.
func TestAbortAfterStepThreeKeepsDraft(t *testing.T) {
	p := newPrompt(testSchema())
	h := newHarness(t, p, true)
	h.typeText("one")
	h.key("ctrl+n")
	h.typeText("two")
	h.key("ctrl+n")
	h.typeText("three")

	if isQuit(h.key("ctrl+c")) {
		t.Fatal("ctrl+c with unsaved changes must ask first")
	}
	if !isQuit(h.key("y")) {
		t.Fatal("y should save and quit")
	}
	if len(h.saves) != 1 {
		t.Fatalf("saves = %d", len(h.saves))
	}
	for i, want := range []string{"one", "two", "three"} {
		if got := p.Sections[i].Content; got != want {
			t.Errorf("section %d = %q, want %q", i, got, want)
		}
	}

	// Resume via edit: starts in the overview with the content intact.
	h2 := newHarness(t, p, false)
	if h2.m.mode != modeOverview {
		t.Fatal("edit should start in overview")
	}
	h2.key("down")
	h2.key("down")
	h2.key("enter")
	if got := h2.m.ta.Value(); got != "three" {
		t.Errorf("resumed section 3 = %q", got)
	}
}

// A3: jump directly to a section from the overview, change it, save.
func TestJumpEditSave(t *testing.T) {
	p := newPrompt(testSchema())
	p.Sections[0].Content = "keep"
	h := newHarness(t, p, false)
	for range 6 {
		h.key("down")
	}
	h.key("enter")
	if h.m.idx != 6 || p.Sections[6].Tag != "aufgabe" {
		t.Fatalf("jumped to %d", h.m.idx)
	}
	h.typeText("AP1")
	h.key("ctrl+s")
	if len(h.saves) != 1 || !strings.Contains(h.saves[0], "<aufgabe>\nAP1\n</aufgabe>") {
		t.Fatalf("save output: %v", h.saves)
	}
	if p.Sections[0].Content != "keep" {
		t.Error("other section changed")
	}
	if h.m.dirty {
		t.Error("dirty after save")
	}
	if !isQuit(h.key("ctrl+c")) {
		t.Error("ctrl+c without changes should quit directly")
	}
}

func TestDiscardChanges(t *testing.T) {
	h := newHarness(t, newPrompt(testSchema()), true)
	h.typeText("x")
	h.key("ctrl+c")
	if h.m.mode != modeConfirmQuit {
		t.Fatal("expected confirm")
	}
	h.key("esc")
	if h.m.mode != modeSection {
		t.Fatal("esc should cancel")
	}
	h.key("ctrl+c")
	if !isQuit(h.key("n")) || len(h.saves) != 0 {
		t.Fatal("n should quit without saving")
	}
}

func TestAddMissingSchemaSection(t *testing.T) {
	p := &prompt.Prompt{Name: "old", Sections: []prompt.Section{
		{Tag: "product", Content: "x"},
		{Tag: "aufgabe", Content: "y"},
	}}
	h := newHarness(t, p, false)
	rows := h.m.rows()
	if len(rows) != len(testSchema().Sections) {
		t.Fatalf("rows = %d", len(rows))
	}
	// rows: product, aufgabe, then missing in schema order: glossar first.
	h.key("down")
	h.key("down")
	h.key("enter")
	if got := p.Sections[1].Tag; got != "glossar" {
		t.Fatalf("inserted %q at 1, want glossar", got)
	}
	if h.m.idx != 1 || !h.m.dirty {
		t.Fatal("should edit the new section and be dirty")
	}
}

func TestPreview(t *testing.T) {
	p := newPrompt(testSchema())
	p.Sections[0].Content = "hello"
	h := newHarness(t, p, true)
	h.key("ctrl+r")
	if h.m.mode != modePreview || !strings.Contains(h.m.vp.GetContent(), "<product>\nhello\n</product>") {
		t.Fatalf("preview not shown: %q", h.m.vp.GetContent())
	}
	h.key("esc")
	if h.m.mode != modeSection {
		t.Fatal("esc should return to the section")
	}
	_ = h.m.View()
}

func TestExternalEditor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as editor")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "ed.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'from editor\\n' > \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := newPrompt(testSchema())
	h := newHarness(t, p, true)
	h.m.opts.Editor = script
	if cmd := h.key("ctrl+e"); cmd == nil {
		t.Fatal("expected exec command")
	}
	// Simulate what Bubble Tea does: run the editor, then deliver the result.
	sec := p.Sections[0]
	f, _ := os.CreateTemp(dir, "x-*.md")
	f.WriteString(sec.Content)
	f.Close()
	if err := runScript(script, f.Name()); err != nil {
		t.Fatal(err)
	}
	h.m.Update(editorDoneMsg{path: f.Name(), orig: sec.Content})
	if got := p.Sections[0].Content; got != "from editor" {
		t.Fatalf("content = %q", got)
	}
}

func runScript(script, arg string) error {
	return exec.Command(script, arg).Run()
}

package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/x0ptr/promptfix/internal/agent"
	"github.com/x0ptr/promptfix/internal/prompt"
	"github.com/x0ptr/promptfix/internal/rules"
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

// The textarea normalizes tabs and CR; navigating through a section must
// not change its content.
func TestUntouchedContentKept(t *testing.T) {
	p := newPrompt(testSchema())
	p.Sections[0].Content = "\tindented\r\nline"
	h := newHarness(t, p, true)
	h.key("ctrl+n")
	h.key("ctrl+p")
	h.key("ctrl+o")
	if p.Sections[0].Content != "\tindented\r\nline" || h.m.dirty {
		t.Fatalf("content changed to %q, dirty=%v", p.Sections[0].Content, h.m.dirty)
	}
}

func TestExportSectionAsRule(t *testing.T) {
	p := newPrompt(testSchema())
	p.Sections[0].Content = "rule text"
	type call struct {
		scope   rules.Kind
		rel     string
		content string
	}
	var calls []call
	h := newHarness(t, p, true)
	h.m.opts.ExportScopes = []rules.Kind{rules.Project, rules.User}
	h.m.opts.ExportRule = func(k rules.Kind, rel, content string) error {
		calls = append(calls, call{k, rel, content})
		return nil
	}
	h.key("ctrl+x")
	if h.m.mode != modeExport || h.m.exportInput.Value() != "product.md" {
		t.Fatalf("mode=%v value=%q", h.m.mode, h.m.exportInput.Value())
	}
	h.m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	h.key("enter")
	if len(calls) != 1 || calls[0] != (call{rules.User, "product.md", "rule text"}) {
		t.Fatalf("calls = %+v", calls)
	}
	if h.m.mode != modeSection {
		t.Fatal("should return to the section")
	}
}

func launchCfg(found ...agent.ID) LaunchConfig {
	var agents []agent.Agent
	for _, id := range agent.Supported {
		a := agent.Agent{ID: id}
		if slices.Contains(found, id) {
			a.Path = "/bin/" + string(id)
		}
		agents = append(agents, a)
	}
	return LaunchConfig{Agents: agents, Mux: agent.Tmux, Last: agent.Options{Agent: agent.Claude, Mode: agent.Handoff}}
}

// S2: handoff saves the prompt and returns the choice; the CLI then execs.
func TestLaunchHandoff(t *testing.T) {
	p := newPrompt(testSchema())
	h := newHarness(t, p, true)
	h.m.opts.Launch = launchCfg(agent.Claude, agent.OpenCode)
	var remembered agent.Options
	h.m.opts.Remember = func(o agent.Options) { remembered = o }
	h.typeText("hello")
	h.key("ctrl+g")
	if h.m.mode != modeLaunch {
		t.Fatal("ctrl+g should open the picker")
	}
	h.key("down")
	h.key("down") // model field
	h.typeText("opus")
	h.key("down") // permissions
	h.m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if !isQuit(h.key("enter")) {
		t.Fatal("handoff should quit the TUI")
	}
	want := agent.Options{Agent: agent.Claude, Mode: agent.Handoff, Model: "opus", PermissionMode: "plan"}
	if h.m.launch == nil || *h.m.launch != want || remembered != want {
		t.Fatalf("launch=%+v remembered=%+v", h.m.launch, remembered)
	}
	if len(h.saves) != 1 || !strings.Contains(h.saves[0], "hello") {
		t.Fatal("prompt must be saved before starting")
	}
}

// S3: split calls the split starter with the rendered prompt and stays open.
func TestLaunchSplit(t *testing.T) {
	p := newPrompt(testSchema())
	p.Sections[0].Content = "x"
	h := newHarness(t, p, false)
	h.m.opts.Launch = launchCfg(agent.Claude, agent.OpenCode)
	var got agent.Options
	var gotPrompt string
	h.m.opts.Split = func(o agent.Options, pr string) error { got, gotPrompt = o, pr; return nil }
	h.key("ctrl+g")
	h.m.Update(tea.KeyPressMsg{Code: tea.KeyRight}) // agent: opencode
	h.key("down")
	h.m.Update(tea.KeyPressMsg{Code: tea.KeyRight}) // mode: split
	h.key("down")
	h.m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}) // --auto
	if isQuit(h.key("enter")) {
		t.Fatal("split must keep promptfix running")
	}
	want := agent.Options{Agent: agent.OpenCode, Mode: agent.Split, Auto: true}
	if got != want || gotPrompt != prompt.Render(p) {
		t.Fatalf("split got %+v %q", got, gotPrompt)
	}
	if h.m.mode != modeOverview || h.m.launch != nil {
		t.Fatal("should return to the overview")
	}
}

// S1: missing agents cannot be selected; no agent at all is reported.
func TestLaunchDetection(t *testing.T) {
	h := newHarness(t, newPrompt(testSchema()), false)
	h.m.opts.Launch = launchCfg()
	h.key("ctrl+g")
	if h.m.mode == modeLaunch || !strings.Contains(h.m.status, "only claude and opencode") {
		t.Fatalf("mode=%v status=%q", h.m.mode, h.m.status)
	}

	cfg := launchCfg(agent.OpenCode)
	h.m.opts.Launch = cfg
	h.key("ctrl+g")
	if h.m.picker.o.Agent != agent.OpenCode {
		t.Fatal("should preselect the installed agent")
	}
	h.m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if h.m.picker.o.Agent != agent.OpenCode || !strings.Contains(h.m.picker.err, "only claude and opencode") {
		t.Fatalf("switched to missing agent: %+v err=%q", h.m.picker.o, h.m.picker.err)
	}
	if !strings.Contains(h.m.View().Content, "not found") {
		t.Error("missing agent not shown")
	}

	cfg.Mux = agent.NoMux
	h.m.opts.Launch = cfg
	h.key("esc")
	h.key("ctrl+g")
	h.key("down")
	h.m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if h.m.picker.o.Mode != agent.Handoff || !strings.Contains(h.m.picker.err, "split needs") {
		t.Fatal("split must not be selectable without a multiplexer")
	}
}

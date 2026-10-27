// Package tui implements the interactive prompt editor: a step-by-step
// section editor, an overview of all sections and a preview.
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/x0ptr/promptfix/internal/prompt"
	"github.com/x0ptr/promptfix/internal/schema"
)

// Options configures the editor.
type Options struct {
	Prompt *prompt.Prompt
	// Schema provides titles, help texts and examples, and the sections that
	// can be added to a prompt that lacks them.
	Schema schema.Schema
	Save   func(*prompt.Prompt) error
	// Editor is the external editor command line, e.g. "vim" or "code -w".
	Editor string
	// Wizard starts at the first section instead of the overview.
	Wizard bool
}

// Result describes how the editor was left.
type Result struct {
	Saved   bool // saved at least once
	Unsaved bool // quit with unsaved changes discarded
}

// Run starts the editor and blocks until the user quits.
func Run(o Options) (Result, error) {
	m := New(o)
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return Result{}, err
	}
	return Result{Saved: m.saved, Unsaved: m.dirty}, nil
}

type mode int

const (
	modeSection mode = iota
	modeOverview
	modePreview
	modeConfirmQuit
)

// row is one overview entry: a section of the prompt (idx >= 0) or a schema
// section the prompt does not contain yet (idx == -1).
type row struct {
	tag string
	idx int
}

type editorDoneMsg struct {
	path string
	orig string
	err  error
}

// Model is the Bubble Tea model of the editor.
type Model struct {
	opts Options
	p    *prompt.Prompt

	mode     mode
	prevMode mode
	idx      int // edited section
	cursor   int // overview row

	ta textarea.Model
	vp viewport.Model

	width, height int

	dirty     bool
	saved     bool
	status    string
	statusErr bool
	initCmd   tea.Cmd
}

// New returns an editor model for o.Prompt.
func New(o Options) *Model {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.MaxHeight = 0
	ta.CharLimit = 0
	// ctrl+n/p/e are editor commands; keep only the arrow/end keys.
	ta.KeyMap.LineNext = key.NewBinding(key.WithKeys("down"))
	ta.KeyMap.LinePrevious = key.NewBinding(key.WithKeys("up"))
	ta.KeyMap.LineEnd = key.NewBinding(key.WithKeys("end"))

	m := &Model{opts: o, p: o.Prompt, ta: ta, vp: viewport.New(), width: 80, height: 24}
	m.resize()
	if o.Wizard && len(m.p.Sections) > 0 {
		m.initCmd = m.openSection(0)
	} else {
		m.mode = modeOverview
	}
	return m
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd { return m.initCmd }

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil
	case editorDoneMsg:
		return m, m.editorDone(msg)
	case tea.KeyPressMsg:
		m.status = ""
		switch m.mode {
		case modeSection:
			return m, m.updateSection(msg)
		case modeOverview:
			return m, m.updateOverview(msg)
		case modePreview:
			return m, m.updatePreview(msg)
		case modeConfirmQuit:
			return m, m.updateConfirm(msg)
		}
	}
	var cmd tea.Cmd
	switch m.mode {
	case modeSection:
		m.ta, cmd = m.ta.Update(msg)
	case modePreview:
		m.vp, cmd = m.vp.Update(msg)
	}
	return m, cmd
}

func (m *Model) updateSection(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+n":
		m.commit()
		if m.idx < len(m.p.Sections)-1 {
			return m.openSection(m.idx + 1)
		}
		m.showOverview()
		return nil
	case "ctrl+p":
		m.commit()
		if m.idx > 0 {
			return m.openSection(m.idx - 1)
		}
		return nil
	case "ctrl+o", "esc":
		m.commit()
		m.showOverview()
		return nil
	case "ctrl+r":
		m.commit()
		m.showPreview()
		return nil
	case "ctrl+s":
		m.commit()
		m.save()
		return nil
	case "ctrl+e":
		m.commit()
		return m.openEditor()
	case "ctrl+c":
		m.commit()
		return m.requestQuit()
	}
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	return cmd
}

func (m *Model) updateOverview(msg tea.KeyPressMsg) tea.Cmd {
	rows := m.rows()
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(rows)-1 {
			m.cursor++
		}
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.cursor = len(rows) - 1
	case "enter":
		if len(rows) == 0 {
			return nil
		}
		r := rows[m.cursor]
		if r.idx < 0 {
			r.idx = m.addSection(r.tag)
		}
		return m.openSection(r.idx)
	case "p":
		m.showPreview()
	case "s", "ctrl+s":
		m.save()
	case "q", "esc", "ctrl+c":
		return m.requestQuit()
	}
	return nil
}

func (m *Model) updatePreview(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q", "ctrl+r":
		m.mode = m.prevMode
		if m.mode == modeSection {
			return m.ta.Focus()
		}
		return nil
	case "c":
		m.setStatus("Copied to clipboard (OSC 52)", false)
		return tea.SetClipboard(prompt.Render(m.p))
	case "ctrl+s":
		m.save()
		return nil
	case "ctrl+c":
		return m.requestQuit()
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return cmd
}

func (m *Model) updateConfirm(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "y":
		if m.save() {
			return tea.Quit
		}
		m.mode = m.prevMode
	case "n":
		return tea.Quit
	case "c", "esc":
		m.mode = m.prevMode
		if m.mode == modeSection {
			return m.ta.Focus()
		}
	}
	return nil
}

// commit copies the textarea content into the edited section.
func (m *Model) commit() {
	if m.mode != modeSection {
		return
	}
	s := &m.p.Sections[m.idx]
	if v := m.ta.Value(); v != s.Content {
		s.Content = v
		m.dirty = true
	}
}

func (m *Model) openSection(i int) tea.Cmd {
	m.idx = i
	m.mode = modeSection
	sec := m.p.Sections[i]
	m.ta.SetValue(sec.Content)
	m.ta.MoveToBegin()
	m.ta.Placeholder = ""
	if def, ok := m.opts.Schema.Section(sec.Tag); ok {
		m.ta.Placeholder = strings.TrimRight(def.Example, "\n")
	}
	return m.ta.Focus()
}

func (m *Model) showOverview() {
	m.ta.Blur()
	m.mode = modeOverview
	for i, r := range m.rows() {
		if r.idx == m.idx {
			m.cursor = i
		}
	}
}

func (m *Model) showPreview() {
	m.ta.Blur()
	m.prevMode = m.mode
	m.mode = modePreview
	m.vp.SetContent(prompt.Render(m.p))
	m.vp.GotoTop()
}

func (m *Model) requestQuit() tea.Cmd {
	if !m.dirty {
		return tea.Quit
	}
	m.ta.Blur()
	m.prevMode = m.mode
	m.mode = modeConfirmQuit
	return nil
}

// save stores the prompt and reports whether it succeeded.
func (m *Model) save() bool {
	if err := m.opts.Save(m.p); err != nil {
		m.setStatus("Save failed: "+err.Error(), true)
		return false
	}
	m.dirty = false
	m.saved = true
	m.setStatus("Saved", false)
	return true
}

func (m *Model) setStatus(s string, isErr bool) {
	m.status, m.statusErr = s, isErr
}

// rows returns the prompt sections followed by schema sections not yet in
// the prompt.
func (m *Model) rows() []row {
	rows := make([]row, 0, len(m.p.Sections))
	for i, s := range m.p.Sections {
		rows = append(rows, row{tag: s.Tag, idx: i})
	}
	for _, def := range m.opts.Schema.Sections {
		if m.p.Index(def.Tag) < 0 {
			rows = append(rows, row{tag: def.Tag, idx: -1})
		}
	}
	return rows
}

// addSection inserts an empty section for tag after the last prompt section
// that precedes it in the schema, and returns its index.
func (m *Model) addSection(tag string) int {
	pos := 0
	for _, def := range m.opts.Schema.Sections {
		if def.Tag == tag {
			break
		}
		if i := m.p.Index(def.Tag); i >= 0 {
			pos = i + 1
		}
	}
	secs := append([]prompt.Section{}, m.p.Sections[:pos]...)
	secs = append(secs, prompt.Section{Tag: tag})
	m.p.Sections = append(secs, m.p.Sections[pos:]...)
	m.dirty = true
	return pos
}

func (m *Model) openEditor() tea.Cmd {
	sec := m.p.Sections[m.idx]
	f, err := os.CreateTemp("", "promptfix-"+sec.Tag+"-*.md")
	if err != nil {
		m.setStatus("Editor: "+err.Error(), true)
		return nil
	}
	_, err = f.WriteString(sec.Content)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		m.setStatus("Editor: "+err.Error(), true)
		return nil
	}
	args := strings.Fields(m.opts.Editor)
	if len(args) == 0 {
		os.Remove(f.Name())
		m.setStatus("No editor configured", true)
		return nil
	}
	cmd := exec.Command(args[0], append(args[1:], f.Name())...)
	path, orig := f.Name(), sec.Content
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorDoneMsg{path: path, orig: orig, err: err}
	})
}

func (m *Model) editorDone(msg editorDoneMsg) tea.Cmd {
	defer os.Remove(msg.path)
	if msg.err != nil {
		m.setStatus("Editor: "+msg.err.Error(), true)
		return m.ta.Focus()
	}
	data, err := os.ReadFile(msg.path)
	if err != nil {
		m.setStatus("Editor: "+err.Error(), true)
		return m.ta.Focus()
	}
	content := string(data)
	// Most editors append a final newline; drop it if the original had none.
	if !strings.HasSuffix(msg.orig, "\n") {
		content = strings.TrimSuffix(strings.TrimSuffix(content, "\n"), "\r")
	}
	m.ta.SetValue(content)
	m.commit()
	return m.ta.Focus()
}

// Layout: header (3 lines) + blank + body + blank + footer (2 lines).
const chromeHeight = 7

func (m *Model) resize() {
	h := max(m.height-chromeHeight, 3)
	m.ta.SetWidth(m.width)
	m.ta.SetHeight(h)
	m.vp.SetWidth(m.width)
	m.vp.SetHeight(h)
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true)
	dimStyle    = lipgloss.NewStyle().Faint(true)
	accentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	selStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
)

// View implements tea.Model.
func (m *Model) View() tea.View {
	var header, body, keys string
	switch m.mode {
	case modeSection:
		sec := m.p.Sections[m.idx]
		def, _ := m.opts.Schema.Section(sec.Tag)
		def.Tag = sec.Tag
		header = m.headerLine(fmt.Sprintf("%d/%d", m.idx+1, len(m.p.Sections))) + "\n" +
			titleStyle.Render(def.DisplayTitle()) + " " + dimStyle.Render("<"+sec.Tag+">") + "\n" +
			dimStyle.Render(truncate(def.Help, m.width))
		body = m.ta.View()
		keys = "^n next · ^p prev · ^o overview · ^e editor · ^r preview · ^s save · ^c quit"
	case modeOverview:
		header = m.headerLine("overview") + "\n" +
			titleStyle.Render("Sections") + "\n" +
			dimStyle.Render(fmt.Sprintf("%d of %d filled", m.p.Filled(), len(m.p.Sections)))
		body = m.overviewView()
		keys = "↑/↓ move · enter edit · p preview · s save · q quit"
	case modePreview:
		header = m.headerLine("preview") + "\n" +
			titleStyle.Render("Preview") + "\n" +
			dimStyle.Render(fmt.Sprintf("%3.0f%%", m.vp.ScrollPercent()*100))
		body = m.vp.View()
		keys = "↑/↓ scroll · c copy · ctrl+s save · esc back"
	case modeConfirmQuit:
		header = m.headerLine("quit") + "\n" + titleStyle.Render("Unsaved changes") + "\n"
		body = "Save before quitting?"
		keys = "y save and quit · n discard and quit · esc cancel"
	}
	status := ""
	if m.status != "" {
		status = okStyle.Render(m.status)
		if m.statusErr {
			status = warnStyle.Render(m.status)
		}
	} else if m.dirty {
		status = dimStyle.Render("unsaved changes")
	}
	v := tea.NewView(header + "\n\n" + body + "\n\n" + status + "\n" + dimStyle.Render(truncate(keys, m.width)))
	v.AltScreen = true
	return v
}

func (m *Model) headerLine(right string) string {
	return accentStyle.Render("promptfix") + dimStyle.Render(" · "+m.p.Name+" · "+right)
}

func (m *Model) overviewView() string {
	rows := m.rows()
	// Keep the cursor visible on short terminals.
	h := max(m.height-chromeHeight, 1)
	start := max(0, min(m.cursor-h+1, len(rows)-h))
	var b strings.Builder
	for i := start; i < len(rows) && i < start+h; i++ {
		r := rows[i]
		def, _ := m.opts.Schema.Section(r.tag)
		def.Tag = r.tag
		cursor, name := "  ", def.DisplayTitle()
		if i == m.cursor {
			cursor, name = selStyle.Render("> "), selStyle.Render(name)
		}
		fmt.Fprintf(&b, "%s%-*s %s  %s\n", cursor, 26+len(name)-lipgloss.Width(name), name,
			dimStyle.Render(fmt.Sprintf("%-16s", "<"+r.tag+">")), m.rowStatus(r, def))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *Model) rowStatus(r row, def schema.Section) string {
	if r.idx < 0 {
		return accentStyle.Render("+ add")
	}
	c := m.p.Sections[r.idx].Content
	switch {
	case !prompt.IsBlank(c):
		n := strings.Count(strings.TrimRight(c, "\n"), "\n") + 1
		return okStyle.Render(fmt.Sprintf("✓ %d line%s", n, plural(n)))
	case def.Required:
		return warnStyle.Render("! required")
	default:
		return dimStyle.Render("· empty")
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func truncate(s string, w int) string {
	if w <= 1 || lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r)) > w-1 {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

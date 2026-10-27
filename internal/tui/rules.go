package tui

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/x0ptr/promptfix/internal/rules"
	"github.com/x0ptr/promptfix/internal/store"
)

// RulesOptions configures the rules manager UI.
type RulesOptions struct {
	// Scopes holds one or two managers (user, project).
	Scopes []*rules.Manager
	// Start is the index of the scope shown first.
	Start int
	// Note is shown in the list, e.g. why there is no project scope.
	Note string
	// Editor is the external editor command line.
	Editor string
	// Open, if set, opens this rule (relative path) in the editor directly.
	// With New set, it is created.
	Open string
	New  bool
}

// RunRules starts the rules UI and blocks until the user quits.
func RunRules(o RulesOptions) error {
	m, err := NewRules(o)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(m).Run()
	return err
}

type rulesMode int

const (
	rList rulesMode = iota
	rEdit
	rNew
	rHistory
	rConfirm
)

type confirm struct {
	question string
	yes      func() tea.Cmd
	back     rulesMode
}

// RulesModel is the Bubble Tea model of the rules UI.
type RulesModel struct {
	opts  RulesOptions
	mode  rulesMode
	scope int

	list   []rules.Rule
	other  map[string]bool // rel paths that exist in the other scope
	cursor int

	// editor state
	rel                     string
	isNew                   bool
	base                    []byte      // content on disk (nil for new rules)
	file                    *rules.File // parsed content being edited
	pathsTA, bodyTA         textarea.Model
	loadedPaths, loadedBody string
	focus                   int // 0 paths, 1 body
	symlink                 bool

	nameInput textinput.Model
	revs      []store.Revision
	hcursor   int
	confirm   confirm

	width, height int
	status        string
	statusErr     bool
}

// NewRules returns a rules UI model.
func NewRules(o RulesOptions) (*RulesModel, error) {
	if len(o.Scopes) == 0 {
		return nil, errors.New("no rule scope available")
	}
	m := &RulesModel{opts: o, scope: o.Start, width: 80, height: 24}
	m.pathsTA = newTextarea()
	m.pathsTA.Placeholder = "src/api/**/*.go  (one glob per line; empty = rule always applies)"
	m.bodyTA = newTextarea()
	m.bodyTA.Placeholder = "Rule text (Markdown)"
	m.nameInput = textinput.New()
	m.nameInput.Placeholder = "security/tenant-id.md"
	m.resize()
	if err := m.reload(); err != nil {
		return nil, err
	}
	if o.Open != "" {
		if o.New {
			m.openNew(o.Open)
		} else if err := m.openRule(o.Open); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func newTextarea() textarea.Model {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.MaxHeight = 0
	ta.CharLimit = 0
	ta.KeyMap.LineNext = key.NewBinding(key.WithKeys("down"))
	ta.KeyMap.LinePrevious = key.NewBinding(key.WithKeys("up"))
	ta.KeyMap.LineEnd = key.NewBinding(key.WithKeys("end"))
	return ta
}

func (m *RulesModel) mgr() *rules.Manager { return m.opts.Scopes[m.scope] }

func (m *RulesModel) otherMgr() *rules.Manager {
	if len(m.opts.Scopes) < 2 {
		return nil
	}
	return m.opts.Scopes[1-m.scope]
}

func (m *RulesModel) reload() error {
	list, err := m.mgr().List()
	if err != nil {
		return err
	}
	m.list = list
	m.other = map[string]bool{}
	if o := m.otherMgr(); o != nil {
		ol, err := o.List()
		if err != nil {
			return err
		}
		for _, r := range ol {
			m.other[r.Rel] = true
		}
	}
	m.cursor = min(m.cursor, max(len(m.list)-1, 0))
	return nil
}

// Init implements tea.Model.
func (m *RulesModel) Init() tea.Cmd {
	if m.mode == rEdit {
		return m.focusField()
	}
	return nil
}

// Update implements tea.Model.
func (m *RulesModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		case rList:
			return m, m.updateList(msg)
		case rEdit:
			return m, m.updateEdit(msg)
		case rNew:
			return m, m.updateNew(msg)
		case rHistory:
			return m, m.updateHistory(msg)
		case rConfirm:
			return m, m.updateConfirm(msg)
		}
	}
	return m, nil
}

func (m *RulesModel) setStatus(s string, isErr bool) { m.status, m.statusErr = s, isErr }

func (m *RulesModel) setErr(err error) { m.setStatus(err.Error(), true) }

func (m *RulesModel) ask(question string, yes func() tea.Cmd) {
	m.confirm = confirm{question: question, yes: yes, back: m.mode}
	m.mode = rConfirm
}

func (m *RulesModel) updateConfirm(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "y":
		m.mode = m.confirm.back
		return m.confirm.yes()
	case "n", "esc":
		m.mode = m.confirm.back
		if m.mode == rEdit {
			return m.focusField()
		}
	case "ctrl+c":
		return tea.Quit
	}
	return nil
}

func (m *RulesModel) selected() (rules.Rule, bool) {
	if len(m.list) == 0 {
		return rules.Rule{}, false
	}
	return m.list[m.cursor], true
}

func (m *RulesModel) updateList(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.list)-1 {
			m.cursor++
		}
	case "tab":
		if m.otherMgr() == nil {
			m.setStatus("Only the user scope is available. "+m.opts.Note, true)
			return nil
		}
		m.scope = 1 - m.scope
		m.cursor = 0
		if err := m.reload(); err != nil {
			m.setErr(err)
		}
	case "enter", "e":
		if r, ok := m.selected(); ok {
			if err := m.openRule(r.Rel); err != nil {
				m.setErr(err)
				return nil
			}
			return m.focusField()
		}
	case "n":
		m.mode = rNew
		m.nameInput.SetValue("")
		return m.nameInput.Focus()
	case "d":
		r, ok := m.selected()
		if !ok {
			return nil
		}
		m.ask(fmt.Sprintf("Delete %s rule %s? It stays restorable from history.", m.mgr().Scope.Kind, r.Rel), func() tea.Cmd {
			if err := m.mgr().Delete(r.Rel); err != nil {
				m.setErr(err)
				return nil
			}
			m.setStatus("Deleted "+r.Rel, false)
			m.reloadOrErr()
			return nil
		})
	case "c", "m":
		r, ok := m.selected()
		if !ok {
			return nil
		}
		dst := m.otherMgr()
		if dst == nil {
			m.setStatus("Only the user scope is available. "+m.opts.Note, true)
			return nil
		}
		move := msg.String() == "m"
		verb := map[bool]string{false: "Copied", true: "Moved"}[move]
		do := func(overwrite bool) tea.Cmd {
			if err := m.mgr().Transfer(r.Rel, dst, move, overwrite); err != nil {
				m.setErr(err)
				return nil
			}
			m.setStatus(fmt.Sprintf("%s %s to %s", verb, r.Rel, dst.Scope.Kind), false)
			m.reloadOrErr()
			return nil
		}
		if dst.Exists(r.Rel) {
			m.ask(fmt.Sprintf("%s rule %s exists. Overwrite? The old version goes to history.", dst.Scope.Kind, r.Rel), func() tea.Cmd { return do(true) })
			return nil
		}
		return do(false)
	case "h":
		r, ok := m.selected()
		if !ok {
			return nil
		}
		return m.showHistory(r.Rel)
	case "r":
		m.reloadOrErr()
	case "q", "esc", "ctrl+c":
		return tea.Quit
	}
	return nil
}

func (m *RulesModel) reloadOrErr() {
	if err := m.reload(); err != nil {
		m.setErr(err)
	}
}

func (m *RulesModel) updateNew(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.nameInput.Blur()
		m.mode = rList
		return nil
	case "ctrl+c":
		return tea.Quit
	case "enter":
		rel, err := rules.Normalize(m.nameInput.Value())
		if err != nil {
			m.setErr(err)
			return nil
		}
		if m.mgr().Exists(rel) {
			m.setStatus(fmt.Sprintf("%s rule %s already exists", m.mgr().Scope.Kind, rel), true)
			return nil
		}
		m.nameInput.Blur()
		m.openNew(rel)
		return m.focusField()
	}
	var cmd tea.Cmd
	m.nameInput, cmd = m.nameInput.Update(msg)
	return cmd
}

func (m *RulesModel) openNew(rel string) {
	m.rel, m.isNew, m.base, m.symlink = rel, true, nil, false
	m.loadFile(rules.Parse(nil))
	m.focus = 1
	m.mode = rEdit
}

func (m *RulesModel) openRule(rel string) error {
	f, err := m.mgr().Read(rel)
	if err != nil {
		return err
	}
	m.rel, m.isNew, m.base = rel, false, f.Raw()
	m.symlink = m.mgr().IsSymlink(rel)
	m.loadFile(f)
	m.focus = 1
	m.mode = rEdit
	return nil
}

// loadFile shows f in the editor fields.
func (m *RulesModel) loadFile(f *rules.File) {
	m.file = f
	if f.ReadOnly() != "" || m.symlink {
		m.pathsTA.SetValue("")
		m.bodyTA.SetValue(string(f.Raw()))
	} else {
		m.pathsTA.SetValue(strings.Join(f.Paths(), "\n"))
		m.bodyTA.SetValue(f.Body())
	}
	m.loadedPaths, m.loadedBody = m.pathsTA.Value(), m.bodyTA.Value()
	m.pathsTA.MoveToBegin()
	m.bodyTA.MoveToBegin()
}

func (m *RulesModel) readOnly() string {
	if m.symlink {
		return "symlink (read-only)"
	}
	return m.file.ReadOnly()
}

// current returns the file content as currently edited. Fields the user
// did not touch keep their exact original bytes.
func (m *RulesModel) current() ([]byte, error) {
	if m.readOnly() != "" {
		return m.file.Raw(), nil
	}
	paths := m.file.Paths()
	if v := m.pathsTA.Value(); v != m.loadedPaths {
		paths = parsePaths(v)
	}
	body := m.file.Body()
	if v := m.bodyTA.Value(); v != m.loadedBody {
		body = v
		if m.file.LineEnding() == "\r\n" {
			body = strings.ReplaceAll(v, "\n", "\r\n")
		}
	}
	return m.file.With(paths, body)
}

func parsePaths(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func (m *RulesModel) dirty() bool {
	cur, err := m.current()
	if err != nil {
		return false
	}
	return m.isNew || !bytes.Equal(cur, m.base)
}

func (m *RulesModel) focusField() tea.Cmd {
	m.pathsTA.Blur()
	m.bodyTA.Blur()
	if m.readOnly() != "" {
		return nil
	}
	if m.focus == 0 {
		return m.pathsTA.Focus()
	}
	return m.bodyTA.Focus()
}

func (m *RulesModel) updateEdit(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "tab", "shift+tab":
		m.focus = 1 - m.focus
		return m.focusField()
	case "ctrl+s":
		m.save()
		return nil
	case "ctrl+e":
		if m.symlink {
			m.setStatus("Symlinks are read-only; edit the target instead", true)
			return nil
		}
		return m.openExternal()
	case "esc":
		return m.leaveEdit(func() tea.Cmd {
			m.mode = rList
			m.reloadOrErr()
			return nil
		})
	case "ctrl+c":
		return m.leaveEdit(func() tea.Cmd { return tea.Quit })
	}
	if m.readOnly() != "" {
		return nil
	}
	var cmd tea.Cmd
	if m.focus == 0 {
		m.pathsTA, cmd = m.pathsTA.Update(msg)
	} else {
		m.bodyTA, cmd = m.bodyTA.Update(msg)
	}
	return cmd
}

func (m *RulesModel) leaveEdit(then func() tea.Cmd) tea.Cmd {
	m.pathsTA.Blur()
	m.bodyTA.Blur()
	if !m.dirty() {
		return then()
	}
	m.ask("Unsaved changes. Discard them?", then)
	return nil
}

func (m *RulesModel) save() bool {
	data, err := m.current()
	if err != nil {
		m.setErr(err)
		return false
	}
	if _, err := m.mgr().Write(m.rel, data); err != nil {
		m.setErr(err)
		return false
	}
	m.isNew, m.base = false, data
	m.file = rules.Parse(data)
	m.loadedPaths, m.loadedBody = m.pathsTA.Value(), m.bodyTA.Value()
	m.setStatus(fmt.Sprintf("Saved %s rule %s", m.mgr().Scope.Kind, m.rel), false)
	return true
}

func (m *RulesModel) openExternal() tea.Cmd {
	data, err := m.current()
	if err != nil {
		m.setErr(err)
		return nil
	}
	f, err := os.CreateTemp("", "promptfix-rule-*.md")
	if err != nil {
		m.setErr(err)
		return nil
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	args := strings.Fields(m.opts.Editor)
	if err == nil && len(args) == 0 {
		err = errors.New("no editor configured")
	}
	if err != nil {
		os.Remove(f.Name())
		m.setErr(err)
		return nil
	}
	path := f.Name()
	cmd := exec.Command(args[0], append(args[1:], path)...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorDoneMsg{path: path, err: err}
	})
}

func (m *RulesModel) editorDone(msg editorDoneMsg) tea.Cmd {
	defer os.Remove(msg.path)
	if msg.err != nil {
		m.setStatus("Editor: "+msg.err.Error(), true)
		return m.focusField()
	}
	data, err := os.ReadFile(msg.path)
	if err != nil {
		m.setErr(err)
		return m.focusField()
	}
	// The whole file was edited; reparse it (it may have become editable).
	m.loadFile(rules.Parse(data))
	return m.focusField()
}

func (m *RulesModel) showHistory(rel string) tea.Cmd {
	revs, err := m.mgr().History(rel)
	if err != nil {
		m.setErr(err)
		return nil
	}
	if len(revs) == 0 {
		m.setStatus("No history for "+rel, false)
		return nil
	}
	m.rel, m.revs, m.hcursor = rel, revs, 0
	m.mode = rHistory
	return nil
}

func (m *RulesModel) updateHistory(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "up", "k":
		if m.hcursor > 0 {
			m.hcursor--
		}
	case "down", "j":
		if m.hcursor < len(m.revs)-1 {
			m.hcursor++
		}
	case "enter":
		rev := m.revs[m.hcursor]
		m.ask(fmt.Sprintf("Restore %s from %s? The current version goes to history.", m.rel, rev.Time.Local().Format("2006-01-02 15:04:05")), func() tea.Cmd {
			if err := m.mgr().Restore(m.rel, rev.ID); err != nil {
				m.setErr(err)
				return nil
			}
			m.setStatus("Restored "+m.rel, false)
			m.mode = rList
			m.reloadOrErr()
			return nil
		})
	case "esc", "q":
		m.mode = rList
	case "ctrl+c":
		return tea.Quit
	}
	return nil
}

// Layout of the editor: header (3) + blank + label + paths (3) + blank +
// label + body + blank + footer (2).
const (
	pathsHeight     = 3
	rulesEditChrome = 13
)

func (m *RulesModel) resize() {
	m.pathsTA.SetWidth(m.width)
	m.pathsTA.SetHeight(pathsHeight)
	m.bodyTA.SetWidth(m.width)
	m.bodyTA.SetHeight(max(m.height-rulesEditChrome, 3))
	m.nameInput.SetWidth(max(m.width-4, 10))
}

func (m *RulesModel) scopeTabs() string {
	var parts []string
	for i, s := range m.opts.Scopes {
		label := s.Scope.Kind.String()
		if i == m.scope {
			parts = append(parts, selStyle.Render("● "+label))
		} else {
			parts = append(parts, dimStyle.Render("○ "+label))
		}
	}
	return strings.Join(parts, "  ")
}

func (m *RulesModel) headerLine(right string) string {
	return accentStyle.Render("promptfix") + dimStyle.Render(" · rules · "+right)
}

// View implements tea.Model.
func (m *RulesModel) View() tea.View {
	var header, body, keys string
	switch m.mode {
	case rList:
		header = m.headerLine("list") + "\n" +
			m.scopeTabs() + "\n" +
			dimStyle.Render(truncate(m.mgr().Scope.Dir, m.width))
		body = m.listView()
		keys = "↑/↓ · enter edit · n new · d delete · c copy · m move · h history · tab scope · q quit"
	case rEdit:
		title := m.rel
		if m.isNew {
			title += " (new)"
		}
		header = m.headerLine(m.mgr().Scope.Kind.String()) + "\n" +
			titleStyle.Render(title) + "\n" +
			dimStyle.Render(truncate(m.mgr().Path(m.rel), m.width))
		if ro := m.readOnly(); ro != "" {
			body = warnStyle.Render("Read-only: "+ro) + "\n" + strings.Repeat("\n", pathsHeight) + "\n" +
				dimStyle.Render("file content") + "\n" + m.bodyTA.View()
			keys = "^e edit whole file in editor · esc back"
			if m.symlink {
				keys = "esc back"
			}
		} else {
			body = fieldLabel("paths", m.focus == 0) + "\n" + m.pathsTA.View() + "\n\n" +
				fieldLabel("content", m.focus == 1) + "\n" + m.bodyTA.View()
			keys = "tab switch field · ^s save · ^e editor · esc back · ^c quit"
		}
	case rNew:
		header = m.headerLine("new") + "\n" +
			titleStyle.Render("New "+m.mgr().Scope.Kind.String()+" rule") + "\n" +
			dimStyle.Render("Path relative to "+m.mgr().Scope.Dir+"; subdirectories allowed")
		body = m.nameInput.View()
		keys = "enter create · esc cancel"
	case rHistory:
		header = m.headerLine("history") + "\n" + titleStyle.Render(m.rel) + "\n" +
			dimStyle.Render(fmt.Sprintf("%d version(s)", len(m.revs)))
		var b strings.Builder
		for i, r := range m.revs {
			cur := "  "
			line := r.Time.Local().Format("2006-01-02 15:04:05") + "  " + dimStyle.Render(r.ID)
			if i == m.hcursor {
				cur = selStyle.Render("> ")
			}
			b.WriteString(cur + line + "\n")
		}
		body = strings.TrimRight(b.String(), "\n")
		keys = "↑/↓ move · enter restore · esc back"
	case rConfirm:
		header = m.headerLine("confirm") + "\n" + titleStyle.Render("Confirm") + "\n"
		body = m.confirm.question
		keys = "y yes · n no"
	}
	status := ""
	switch {
	case m.status != "" && m.statusErr:
		status = warnStyle.Render(truncate(m.status, m.width))
	case m.status != "":
		status = okStyle.Render(truncate(m.status, m.width))
	case m.mode == rEdit && m.dirty():
		status = dimStyle.Render("unsaved changes")
	}
	v := tea.NewView(header + "\n\n" + body + "\n\n" + status + "\n" + dimStyle.Render(truncate(keys, m.width)))
	v.AltScreen = true
	return v
}

func fieldLabel(name string, focused bool) string {
	if focused {
		return selStyle.Render(name)
	}
	return dimStyle.Render(name)
}

func (m *RulesModel) listView() string {
	var b strings.Builder
	if m.opts.Note != "" {
		b.WriteString(dimStyle.Render(m.opts.Note) + "\n\n")
	}
	if len(m.list) == 0 {
		b.WriteString(dimStyle.Render("No rules yet. Press n to create one."))
		return b.String()
	}
	h := max(m.height-chromeHeight-strings.Count(b.String(), "\n"), 1)
	start := max(0, min(m.cursor-h+1, len(m.list)-h))
	otherName := ""
	if o := m.otherMgr(); o != nil {
		otherName = o.Scope.Kind.String()
	}
	for i := start; i < len(m.list) && i < start+h; i++ {
		r := m.list[i]
		cursor, name := "  ", r.Rel
		if i == m.cursor {
			cursor, name = selStyle.Render("> "), selStyle.Render(name)
		}
		applies := "always"
		if len(r.Paths) > 0 {
			applies = strings.Join(r.Paths, ", ")
		}
		info := dimStyle.Render(fmt.Sprintf("%-28s %4d line%s", truncate(applies, 28), r.Lines, plural(r.Lines)))
		var flags []string
		if r.ReadOnly != "" {
			flags = append(flags, warnStyle.Render("read-only"))
		}
		if m.other[r.Rel] {
			flags = append(flags, accentStyle.Render("⇄ also in "+otherName))
		}
		fmt.Fprintf(&b, "%s%-*s %s  %s\n", cursor, 32+len(name)-lipgloss.Width(name), name, info, strings.Join(flags, " "))
	}
	return strings.TrimRight(b.String(), "\n")
}

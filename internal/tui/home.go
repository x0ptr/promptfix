package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/x0ptr/promptfix/internal/prompt"
	"github.com/x0ptr/promptfix/internal/store"
)

// MenuItem is one entry of the start menu.
type MenuItem struct {
	Title string
	Desc  string
}

// RunMenu shows a menu and returns the chosen index, or -1 on quit.
func RunMenu(items []MenuItem) (int, error) {
	m := &menuModel{items: items, choice: -1}
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return -1, err
	}
	return m.choice, nil
}

type menuModel struct {
	items  []MenuItem
	cursor int
	choice int
}

func (m *menuModel) Init() tea.Cmd { return nil }

func (m *menuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch k.String() {
	case "up", "k":
		m.cursor = max(m.cursor-1, 0)
	case "down", "j":
		m.cursor = min(m.cursor+1, len(m.items)-1)
	case "enter":
		m.choice = m.cursor
		return m, tea.Quit
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	default:
		// Number keys select directly.
		if s := k.String(); len(s) == 1 && s[0] >= '1' && int(s[0]-'1') < len(m.items) {
			m.choice = int(s[0] - '1')
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *menuModel) View() tea.View {
	var b strings.Builder
	b.WriteString(accentStyle.Render("promptfix") + "\n\n")
	for i, it := range m.items {
		cur, title := "  ", it.Title
		if i == m.cursor {
			cur, title = selStyle.Render("> "), selStyle.Render(title)
		}
		fmt.Fprintf(&b, "%s%d %-*s %s\n", cur, i+1, 12+len(title)-lipgloss.Width(title), title, dimStyle.Render(it.Desc))
	}
	b.WriteString("\n" + dimStyle.Render("↑/↓ move · enter select · q quit"))
	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

// PromptAction is what the user chose in the prompt list.
type PromptAction int

const (
	PromptBack PromptAction = iota
	PromptQuit
	PromptEdit
	PromptNew
)

// RunPromptList shows the saved prompts. It returns the chosen action and,
// for PromptEdit, the slug of the prompt.
func RunPromptList(st *store.Store) (PromptAction, string, error) {
	m := &promptListModel{st: st}
	if err := m.reload(); err != nil {
		return PromptBack, "", err
	}
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return PromptBack, "", err
	}
	return m.action, m.slug, nil
}

type promptListModel struct {
	st         *store.Store
	list       []*prompt.Prompt
	cursor     int
	confirmDel bool
	action     PromptAction
	slug       string
	status     string
	statusErr  bool
}

func (m *promptListModel) reload() error {
	list, err := m.st.List()
	m.list = list
	m.cursor = min(m.cursor, max(len(list)-1, 0))
	return err
}

func (m *promptListModel) Init() tea.Cmd { return nil }

func (m *promptListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	m.status = ""
	if m.confirmDel {
		m.confirmDel = false
		if k.String() == "y" {
			p := m.list[m.cursor]
			if err := m.st.Delete(p.Slug()); err != nil {
				m.status, m.statusErr = err.Error(), true
			} else {
				m.status, m.statusErr = "Deleted "+p.Name+" (restorable from history)", false
			}
			if err := m.reload(); err != nil {
				m.status, m.statusErr = err.Error(), true
			}
		}
		return m, nil
	}
	switch k.String() {
	case "up", "k":
		m.cursor = max(m.cursor-1, 0)
	case "down", "j":
		m.cursor = min(m.cursor+1, max(len(m.list)-1, 0))
	case "enter", "e":
		if len(m.list) > 0 {
			m.action, m.slug = PromptEdit, m.list[m.cursor].Slug()
			return m, tea.Quit
		}
	case "n":
		m.action = PromptNew
		return m, tea.Quit
	case "d":
		if len(m.list) > 0 {
			m.confirmDel = true
		}
	case "q", "esc":
		m.action = PromptBack
		return m, tea.Quit
	case "ctrl+c":
		m.action = PromptQuit
		return m, tea.Quit
	}
	return m, nil
}

func (m *promptListModel) View() tea.View {
	var b strings.Builder
	b.WriteString(accentStyle.Render("promptfix") + dimStyle.Render(" · prompts") + "\n\n")
	if len(m.list) == 0 {
		b.WriteString(dimStyle.Render("No prompts yet. Press n to create one.") + "\n")
	}
	for i, p := range m.list {
		cur, name := "  ", p.Name
		if i == m.cursor {
			cur, name = selStyle.Render("> "), selStyle.Render(name)
		}
		fmt.Fprintf(&b, "%s%-*s %s\n", cur, 30+len(name)-lipgloss.Width(name), name,
			dimStyle.Render(fmt.Sprintf("%-10s %d/%d filled  %s", p.Schema, p.Filled(), len(p.Sections), p.Updated.Local().Format("2006-01-02 15:04"))))
	}
	b.WriteString("\n")
	switch {
	case m.confirmDel:
		b.WriteString(warnStyle.Render(fmt.Sprintf("Delete %q? y/n", m.list[m.cursor].Name)))
	case m.status != "" && m.statusErr:
		b.WriteString(warnStyle.Render(m.status))
	case m.status != "":
		b.WriteString(okStyle.Render(m.status))
	}
	b.WriteString("\n" + dimStyle.Render("↑/↓ move · enter edit · n new · d delete · esc back"))
	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/x0ptr/promptfix/internal/agent"
)

// LaunchConfig describes the agents available to the picker.
type LaunchConfig struct {
	Agents []agent.Agent
	Mux    agent.Mux
	Last   agent.Options
}

// anyFound reports whether at least one agent is installed.
func (c LaunchConfig) anyFound() bool {
	return slices.ContainsFunc(c.Agents, agent.Agent.Found)
}

type launchField int

const (
	fAgent launchField = iota
	fMode
	fModel
	fPermission
	fAuto
)

// launchPicker selects agent, mode and agent options. It is embedded in the
// prompt editor and also runs standalone (RunLaunchPicker).
type launchPicker struct {
	cfg   LaunchConfig
	o     agent.Options
	field int
	model textinput.Model
	err   string
}

func newLaunchPicker(cfg LaunchConfig) *launchPicker {
	p := &launchPicker{cfg: cfg, o: cfg.Last}
	p.model = textinput.New()
	p.model.Prompt = ""
	p.model.Placeholder = "default (e.g. opus, sonnet)"
	p.model.SetWidth(32)
	p.model.SetSuggestions(agent.ModelSuggestions)
	p.model.ShowSuggestions = true
	p.model.KeyMap.AcceptSuggestion = key.NewBinding(key.WithKeys("tab"))
	p.model.SetValue(p.o.Model)
	if a, err := agent.Find(cfg.Agents, p.o.Agent); err != nil || !a.Found() {
		for _, a := range cfg.Agents {
			if a.Found() {
				p.o.Agent = a.ID
				break
			}
		}
	}
	if p.o.Mode != agent.Split || agent.SplitUnavailable(cfg.Mux) != "" {
		p.o.Mode = agent.Handoff
	}
	return p
}

func (p *launchPicker) fields() []launchField {
	if p.o.Agent == agent.Claude {
		return []launchField{fAgent, fMode, fModel, fPermission}
	}
	return []launchField{fAgent, fMode, fAuto}
}

func (p *launchPicker) current() launchField { return p.fields()[p.field] }

// options returns the chosen options with fields of other agents cleared.
func (p *launchPicker) options() agent.Options {
	o := p.o
	if o.Agent == agent.Claude {
		o.Model = strings.TrimSpace(p.model.Value())
		o.Auto = false
	} else {
		o.Model, o.PermissionMode = "", ""
	}
	return o
}

func (p *launchPicker) focus() tea.Cmd {
	if p.current() == fModel {
		return p.model.Focus()
	}
	p.model.Blur()
	return nil
}

// update handles a key; done is set when the user confirmed, cancel when
// the user backed out.
func (p *launchPicker) update(msg tea.KeyPressMsg) (done, cancel bool, cmd tea.Cmd) {
	p.err = ""
	fields := p.fields()
	switch msg.String() {
	case "esc":
		return false, true, nil
	case "enter":
		o := p.options()
		if _, err := agent.Find(p.cfg.Agents, o.Agent); err != nil {
			p.err = err.Error()
			return false, false, nil
		}
		return true, false, nil
	case "up", "shift+tab":
		p.field = (p.field + len(fields) - 1) % len(fields)
		return false, false, p.focus()
	case "down":
		p.field = (p.field + 1) % len(fields)
		return false, false, p.focus()
	case "tab":
		if p.current() != fModel {
			p.field = (p.field + 1) % len(fields)
			return false, false, p.focus()
		}
	case "left", "right", "space":
		if p.change(msg.String() == "left") {
			return false, false, nil
		}
	}
	if p.current() == fModel {
		p.model, cmd = p.model.Update(msg)
	}
	return false, false, cmd
}

// change cycles the value of the current field; it reports whether the key
// was consumed.
func (p *launchPicker) change(back bool) bool {
	step := 1
	if back {
		step = -1
	}
	switch p.current() {
	case fAgent:
		var found []agent.ID
		for _, a := range p.cfg.Agents {
			if a.Found() {
				found = append(found, a.ID)
			}
		}
		if len(found) < 2 {
			if len(p.cfg.Agents) > len(found) {
				p.err = agent.SupportNote + " Install the missing one to switch."
			}
			return true
		}
		i := slices.Index(found, p.o.Agent)
		p.o.Agent = found[(i+step+len(found))%len(found)]
		p.field = 0
	case fMode:
		if why := agent.SplitUnavailable(p.cfg.Mux); why != "" {
			p.err = "Split is not available: " + why
			return true
		}
		if p.o.Mode == agent.Handoff {
			p.o.Mode = agent.Split
		} else {
			p.o.Mode = agent.Handoff
		}
	case fPermission:
		modes := agent.PermissionModes
		i := max(slices.Index(modes, p.o.PermissionMode), 0)
		p.o.PermissionMode = modes[(i+step+len(modes))%len(modes)]
	case fAuto:
		p.o.Auto = !p.o.Auto
	case fModel:
		return false
	}
	return true
}

func (p *launchPicker) view() string {
	var b strings.Builder
	row := func(f launchField, label, value string) {
		cur := "  "
		if p.current() == f {
			cur, label = selStyle.Render("> "), selStyle.Render(label)
		} else {
			label = dimStyle.Render(label)
		}
		fmt.Fprintf(&b, "%s%-*s %s\n", cur, 12+len(label)-lipgloss.Width(label), label, value)
	}
	var agents []string
	for _, a := range p.cfg.Agents {
		switch {
		case !a.Found():
			agents = append(agents, dimStyle.Render("○ "+string(a.ID)+" ("+a.InstallHint()+")"))
		case a.ID == p.o.Agent:
			agents = append(agents, selStyle.Render("● "+string(a.ID)))
		default:
			agents = append(agents, "○ "+string(a.ID))
		}
	}
	row(fAgent, "Agent", strings.Join(agents, "  "))
	split := "○ split"
	if why := agent.SplitUnavailable(p.cfg.Mux); why != "" {
		split = dimStyle.Render("○ split (" + why + ")")
	} else if p.o.Mode == agent.Split {
		split = selStyle.Render("● split") + dimStyle.Render(" ("+string(p.cfg.Mux)+")")
	}
	handoff := "○ handoff"
	if p.o.Mode == agent.Handoff {
		handoff = selStyle.Render("● handoff")
	}
	row(fMode, "Mode", handoff+"  "+split)
	if p.o.Agent == agent.Claude {
		row(fModel, "Model", p.model.View())
		perm := p.o.PermissionMode
		if perm == "" {
			perm = "default"
		}
		row(fPermission, "Permissions", "‹ "+perm+" ›")
	} else {
		box := "[ ]"
		if p.o.Auto {
			box = "[x]"
		}
		row(fAuto, "Auto-approve", box+dimStyle.Render(" --auto: approve permissions that are not explicitly denied"))
	}
	b.WriteString("\n" + dimStyle.Render(agent.SupportNote))
	if p.o.Mode == agent.Handoff {
		b.WriteString("\n" + dimStyle.Render("Handoff: promptfix exits and the agent takes over this terminal."))
	}
	if p.err != "" {
		b.WriteString("\n" + warnStyle.Render(p.err))
	}
	return b.String()
}

const launchKeys = "↑/↓ field · ←/→ change · tab complete model · enter start · esc cancel"

// RunLaunchPicker shows the picker on its own. ok is false if the user
// cancelled.
func RunLaunchPicker(cfg LaunchConfig, title string) (agent.Options, bool, error) {
	m := &pickerModel{p: newLaunchPicker(cfg), title: title}
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return agent.Options{}, false, err
	}
	return m.p.options(), m.ok, nil
}

type pickerModel struct {
	p     *launchPicker
	title string
	ok    bool
}

func (m *pickerModel) Init() tea.Cmd { return m.p.focus() }

func (m *pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, isKey := msg.(tea.KeyPressMsg)
	if !isKey {
		return m, nil
	}
	if k.String() == "ctrl+c" {
		return m, tea.Quit
	}
	done, cancel, cmd := m.p.update(k)
	if done {
		m.ok = true
		return m, tea.Quit
	}
	if cancel {
		return m, tea.Quit
	}
	return m, cmd
}

func (m *pickerModel) View() tea.View {
	v := tea.NewView(accentStyle.Render("promptfix") + dimStyle.Render(" · start agent") + "\n" +
		titleStyle.Render(m.title) + "\n\n" + m.p.view() + "\n\n" + dimStyle.Render(launchKeys))
	v.AltScreen = true
	return v
}

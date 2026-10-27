// Package agent detects the supported coding agents (claude, opencode) and
// starts them with a prompt, either in place of promptfix (handoff) or in a
// new terminal multiplexer pane (split).
package agent

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
)

// ID identifies a supported agent.
type ID string

const (
	Claude   ID = "claude"
	OpenCode ID = "opencode"
)

// Supported lists the agents promptfix can start, in display order.
var Supported = []ID{Claude, OpenCode}

// SupportNote is shown wherever agents are picked.
const SupportNote = "Currently only claude and opencode are supported."

// PermissionModes are the claude permission modes offered in the picker;
// "" keeps claude's default.
var PermissionModes = []string{"", "plan", "acceptEdits", "auto", "manual"}

// ModelSuggestions are claude model aliases offered in the picker.
var ModelSuggestions = []string{"opus", "sonnet", "haiku", "fable"}

// Agent is a supported agent and where it was found.
type Agent struct {
	ID   ID
	Path string // empty if not installed
}

// Found reports whether the agent is installed.
func (a Agent) Found() bool { return a.Path != "" }

// InstallHint tells how to install the agent.
func (a Agent) InstallHint() string {
	switch a.ID {
	case Claude:
		return "not found – install Claude Code: https://claude.com/claude-code"
	case OpenCode:
		return "not found – install opencode: https://opencode.ai"
	}
	return "not found"
}

// fallbacks are default install locations checked when the agent is not on PATH.
func fallbacks(id ID) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	switch id {
	case Claude:
		return []string{
			filepath.Join(home, ".local", "bin", "claude"+ext),
			filepath.Join(home, ".claude", "local", "claude"+ext),
		}
	case OpenCode:
		return []string{filepath.Join(home, ".opencode", "bin", "opencode"+ext)}
	}
	return nil
}

// Detect looks up all supported agents: PATH first, then default install
// locations. It runs no agent binary.
func Detect() []Agent {
	out := make([]Agent, len(Supported))
	for i, id := range Supported {
		out[i] = Agent{ID: id}
		if p, err := exec.LookPath(string(id)); err == nil {
			out[i].Path = p
			continue
		}
		for _, p := range fallbacks(id) {
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				out[i].Path = p
				break
			}
		}
	}
	return out
}

// Find returns the detected agent with the given ID.
func Find(agents []Agent, id ID) (Agent, error) {
	for _, a := range agents {
		if a.ID == id {
			if !a.Found() {
				return a, fmt.Errorf("%s %s", id, a.InstallHint())
			}
			return a, nil
		}
	}
	return Agent{}, fmt.Errorf("unsupported agent %q. %s", id, SupportNote)
}

// Mode is how the agent is started.
type Mode string

const (
	Handoff Mode = "handoff"
	Split   Mode = "split"
)

// Options is a launch configuration; it is remembered between runs.
type Options struct {
	Agent          ID     `yaml:"agent"`
	Mode           Mode   `yaml:"mode"`
	Model          string `yaml:"model,omitempty"`           // claude only
	PermissionMode string `yaml:"permission_mode,omitempty"` // claude only
	Auto           bool   `yaml:"auto,omitempty"`            // opencode only
}

// Validate checks the options independently of what is installed.
func (o Options) Validate() error {
	if !slices.Contains(Supported, o.Agent) {
		return fmt.Errorf("unsupported agent %q. %s", o.Agent, SupportNote)
	}
	if o.Mode != Handoff && o.Mode != Split {
		return fmt.Errorf("unknown mode %q (handoff or split)", o.Mode)
	}
	if o.Agent != Claude && (o.Model != "" || o.PermissionMode != "") {
		return fmt.Errorf("--model and --permission-mode are claude options")
	}
	if o.Agent != OpenCode && o.Auto {
		return fmt.Errorf("--auto is an opencode option")
	}
	return nil
}

// Args returns the full command line that starts agent a with prompt.
func Args(a Agent, o Options, prompt string) []string {
	args := []string{a.Path}
	switch a.ID {
	case Claude:
		if o.Model != "" {
			args = append(args, "--model", o.Model)
		}
		if o.PermissionMode != "" {
			args = append(args, "--permission-mode", o.PermissionMode)
		}
		// "--" ends option parsing, so the prompt is never read as a flag.
		args = append(args, "--", prompt)
	case OpenCode:
		if o.Auto {
			args = append(args, "--auto")
		}
		args = append(args, "--prompt", prompt)
	}
	return args
}

// maxWindowsCmdLine is the Windows command line limit (CreateProcess).
const maxWindowsCmdLine = 32767

// ErrTooLong is returned when the prompt does not fit on the command line.
var ErrTooLong = errors.New("prompt is too long for the Windows command line (~32k characters); use `promptfix copy` or `export` instead")

// CheckLength rejects command lines Windows cannot start.
func CheckLength(args []string) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	n := 0
	for _, a := range args {
		n += len(a) + 3 // quotes and separator
	}
	if n > maxWindowsCmdLine {
		return ErrTooLong
	}
	return nil
}

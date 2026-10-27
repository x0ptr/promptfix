package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Mux is a terminal multiplexer that can open a split pane.
type Mux string

const (
	NoMux   Mux = ""
	Tmux    Mux = "tmux"
	Zellij  Mux = "zellij"
	WezTerm Mux = "wezterm"
)

// DetectMux returns the multiplexer promptfix runs in, if any.
func DetectMux() Mux {
	switch {
	case os.Getenv("TMUX") != "":
		return Tmux
	case os.Getenv("ZELLIJ") != "":
		return Zellij
	case os.Getenv("WEZTERM_PANE") != "":
		return WezTerm
	}
	return NoMux
}

// SplitUnavailable explains why split mode cannot be used, or "".
func SplitUnavailable(m Mux) string {
	if m == NoMux {
		return "split needs tmux, zellij or WezTerm"
	}
	return ""
}

// SplitArgs returns the multiplexer command that opens a pane to the right
// running cmd in dir.
func SplitArgs(m Mux, dir string, cmd []string) ([]string, error) {
	switch m {
	case Tmux:
		return append([]string{"tmux", "split-window", "-h", "-c", dir, "--"}, cmd...), nil
	case Zellij:
		return append([]string{"zellij", "run", "--direction", "right", "--cwd", dir, "--"}, cmd...), nil
	case WezTerm:
		return append([]string{"wezterm", "cli", "split-pane", "--right", "--cwd", dir, "--"}, cmd...), nil
	}
	return nil, errors.New(SplitUnavailable(m))
}

// launchFile is what promptfix hands to the split pane: the multiplexer runs
// "promptfix agent-exec --file <path>", so the prompt never passes through a
// shell.
type launchFile struct {
	Args []string `json:"args"`
	Dir  string   `json:"dir"`
}

// WriteLaunchFile stores args and dir in a private temp file and returns its path.
func WriteLaunchFile(args []string, dir string) (string, error) {
	data, err := json.Marshal(launchFile{Args: args, Dir: dir})
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "promptfix-launch-*.json")
	if err != nil {
		return "", err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// ReadLaunchFile reads and deletes a launch file. Only supported agents may
// be started from it.
func ReadLaunchFile(path string) (args []string, dir string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	os.Remove(path)
	var lf launchFile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil, "", fmt.Errorf("launch file: %w", err)
	}
	if len(lf.Args) == 0 {
		return nil, "", errors.New("launch file: no command")
	}
	name := strings.TrimSuffix(filepath.Base(lf.Args[0]), ".exe")
	if name != string(Claude) && name != string(OpenCode) {
		return nil, "", fmt.Errorf("launch file: refusing to start %q. %s", lf.Args[0], SupportNote)
	}
	return lf.Args, lf.Dir, nil
}

// StartSplit opens the agent in a new pane. self is the promptfix
// executable that runs in the pane as launcher.
func StartSplit(m Mux, self string, args []string, dir string) error {
	file, err := WriteLaunchFile(args, dir)
	if err != nil {
		return err
	}
	split, err := SplitArgs(m, dir, []string{self, "agent-exec", "--file", file})
	if err != nil {
		os.Remove(file)
		return err
	}
	out, err := exec.Command(split[0], split[1:]...).CombinedOutput()
	if err != nil {
		os.Remove(file)
		return fmt.Errorf("%s: %v: %s", split[0], err, strings.TrimSpace(string(out)))
	}
	return nil
}

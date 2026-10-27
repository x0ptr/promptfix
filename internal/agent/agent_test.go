package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

const tricky = "<product>\n\"quotes\" 'single' $(rm -rf /) `id` $HOME \\ ; | &\n</product>\n--model x\n"

// S5: options end up on the command line exactly as chosen.
func TestArgs(t *testing.T) {
	claude := Agent{ID: Claude, Path: "/bin/claude"}
	got := Args(claude, Options{Agent: Claude, Model: "opus", PermissionMode: "plan"}, tricky)
	want := []string{"/bin/claude", "--model", "opus", "--permission-mode", "plan", "--", tricky}
	if !slices.Equal(got, want) {
		t.Errorf("claude: %q", got)
	}
	if got := Args(claude, Options{Agent: Claude}, "p"); !slices.Equal(got, []string{"/bin/claude", "--", "p"}) {
		t.Errorf("claude defaults: %q", got)
	}
	oc := Agent{ID: OpenCode, Path: "/bin/opencode"}
	if got := Args(oc, Options{Agent: OpenCode, Auto: true}, tricky); !slices.Equal(got, []string{"/bin/opencode", "--auto", "--prompt", tricky}) {
		t.Errorf("opencode: %q", got)
	}
}

func TestValidate(t *testing.T) {
	ok := []Options{
		{Agent: Claude, Mode: Handoff, Model: "opus"},
		{Agent: OpenCode, Mode: Split, Auto: true},
	}
	for _, o := range ok {
		if err := o.Validate(); err != nil {
			t.Errorf("%+v: %v", o, err)
		}
	}
	bad := []Options{
		{Agent: "codex", Mode: Handoff},
		{Agent: Claude, Mode: "headless"},
		{Agent: OpenCode, Mode: Handoff, Model: "x"},
		{Agent: Claude, Mode: Handoff, Auto: true},
	}
	for _, o := range bad {
		if err := o.Validate(); err == nil {
			t.Errorf("%+v: expected error", o)
		}
	}
}

func fakeAgent(t *testing.T, dir string, id ID) string {
	t.Helper()
	p := filepath.Join(dir, string(id))
	if runtime.GOOS == "windows" {
		p += ".exe"
	}
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// S1: detection via PATH; missing agents are reported, not selectable.
func TestDetect(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	claude := fakeAgent(t, bin, Claude)
	agents := Detect()
	if len(agents) != 2 || agents[0].Path != claude || agents[1].Found() {
		t.Fatalf("Detect = %+v", agents)
	}
	if _, err := Find(agents, OpenCode); err == nil {
		t.Error("missing opencode should not be selectable")
	}
	if _, err := Find(agents, "codex"); err == nil {
		t.Error("unsupported agent")
	}
}

func TestDetectFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PATH", t.TempDir())
	dir := filepath.Join(home, ".opencode", "bin")
	os.MkdirAll(dir, 0o755)
	oc := fakeAgent(t, dir, OpenCode)
	if a := Detect(); a[1].Path != oc {
		t.Fatalf("fallback not found: %+v", a)
	}
}

// S4: the prompt survives the launch file unchanged.
func TestLaunchFileRoundTrip(t *testing.T) {
	args := []string{"/usr/bin/claude", "--", tricky}
	path, err := WriteLaunchFile(args, "/work dir")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Errorf("launch file mode %v", fi.Mode().Perm())
		}
	}
	got, dir, err := ReadLaunchFile(path)
	if err != nil || dir != "/work dir" || !slices.Equal(got, args) {
		t.Fatalf("got %q %q %v", got, dir, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("launch file not deleted")
	}
}

func TestLaunchFileRejectsOtherCommands(t *testing.T) {
	path, _ := WriteLaunchFile([]string{"/bin/sh", "-c", "id"}, "/")
	if _, _, err := ReadLaunchFile(path); err == nil {
		t.Fatal("must refuse non-agent commands")
	}
}

func TestSplitArgs(t *testing.T) {
	cmd := []string{"/bin/promptfix", "agent-exec", "--file", "/tmp/f"}
	got, err := SplitArgs(Tmux, "/w", cmd)
	if err != nil || !slices.Equal(got, []string{"tmux", "split-window", "-h", "-c", "/w", "--", "/bin/promptfix", "agent-exec", "--file", "/tmp/f"}) {
		t.Errorf("tmux: %q %v", got, err)
	}
	for _, m := range []Mux{Zellij, WezTerm} {
		if _, err := SplitArgs(m, "/w", cmd); err != nil {
			t.Errorf("%s: %v", m, err)
		}
	}
	if _, err := SplitArgs(NoMux, "/w", cmd); err == nil {
		t.Error("no mux should fail")
	}
}

func TestDetectMux(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("ZELLIJ", "")
	t.Setenv("WEZTERM_PANE", "")
	if DetectMux() != NoMux {
		t.Fatal("expected none")
	}
	t.Setenv("ZELLIJ", "0")
	if DetectMux() != Zellij {
		t.Fatal("expected zellij")
	}
}

func TestState(t *testing.T) {
	dir := t.TempDir()
	if o := LoadLast(dir); o.Agent != Claude || o.Mode != Handoff {
		t.Fatalf("default %+v", o)
	}
	want := Options{Agent: OpenCode, Mode: Split, Auto: true}
	if err := SaveLast(dir, want); err != nil {
		t.Fatal(err)
	}
	if got := LoadLast(dir); got != want {
		t.Fatalf("got %+v", got)
	}
}

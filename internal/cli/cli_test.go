package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/x0ptr/promptfix/internal/prompt"
	"github.com/x0ptr/promptfix/internal/store"
)

func setup(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PROMPTFIX_HOME", filepath.Join(dir, "data"))
	t.Setenv("PROMPTFIX_CONFIG", filepath.Join(dir, "config.yaml"))
	return dir
}

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func seed(t *testing.T, name, content string) {
	t.Helper()
	st := store.New(os.Getenv("PROMPTFIX_HOME"), 0)
	p := &prompt.Prompt{Name: name, Schema: "default", Sections: []prompt.Section{
		{Tag: "product", Content: content},
		{Tag: "glossary"},
	}}
	if _, err := st.Save(p); err != nil {
		t.Fatal(err)
	}
}

func TestShowListExport(t *testing.T) {
	dir := setup(t)
	seed(t, "My Prompt", "v1")

	out, err := run(t, "show", "My Prompt")
	if err != nil || out != "<product>\nv1\n</product>\n\n<glossary></glossary>\n" {
		t.Fatalf("show: %q, %v", out, err)
	}
	out, err = run(t, "list")
	if err != nil || !strings.Contains(out, "my-prompt") || !strings.Contains(out, "1/2") {
		t.Fatalf("list: %q, %v", out, err)
	}

	file := filepath.Join(dir, "out.txt")
	if _, err := run(t, "export", "my-prompt", "-o", file); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "export", "my-prompt", "-o", file); err == nil {
		t.Fatal("export should refuse to overwrite")
	}
	if _, err := run(t, "export", "my-prompt", "-o", file, "--force"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(file)
	if !strings.HasPrefix(string(data), "<product>\nv1") {
		t.Fatalf("export content %q", data)
	}
}

func TestHistoryRestorePrune(t *testing.T) {
	setup(t)
	seed(t, "p", "v1")
	seed(t, "p", "v2")

	out, err := run(t, "history", "p")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("history: %q", out)
	}
	rev := strings.Fields(lines[1])[0]

	out, _ = run(t, "show", "p", "--rev", rev)
	if !strings.Contains(out, "v1") {
		t.Fatalf("show --rev: %q", out)
	}
	if _, err := run(t, "restore", "p", rev); err != nil {
		t.Fatal(err)
	}
	out, _ = run(t, "show", "p")
	if !strings.Contains(out, "v1") {
		t.Fatalf("after restore: %q", out)
	}

	if _, err := run(t, "prune"); err == nil {
		t.Fatal("prune without options should fail")
	}
	if out, err := run(t, "prune", "--all"); err != nil || !strings.Contains(out, "Deleted 2") {
		t.Fatalf("prune: %q, %v", out, err)
	}
}

func TestCpMvRm(t *testing.T) {
	setup(t)
	seed(t, "a", "x")
	if _, err := run(t, "cp", "a", "b"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "mv", "b", "c"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "rm", "c", "--force"); err != nil {
		t.Fatal(err)
	}
	out, _ := run(t, "list")
	if !strings.Contains(out, "a ") || strings.Contains(out, "\nc ") || strings.Contains(out, "\nb ") {
		t.Fatalf("list: %q", out)
	}
	if _, err := run(t, "show", "c"); err == nil {
		t.Fatal("removed prompt still shown")
	}
}

func TestConfig(t *testing.T) {
	dir := setup(t)
	if _, err := run(t, "config", "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "config", "init"); err == nil {
		t.Fatal("init should not overwrite")
	}
	if out, err := run(t, "config", "validate"); err != nil || !strings.Contains(out, "OK") {
		t.Fatalf("validate: %q, %v", out, err)
	}
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("default_schema: nope\n"), 0o644)
	if _, err := run(t, "config", "validate"); err == nil {
		t.Fatal("validate should fail")
	}
	if out, err := run(t, "config", "path"); err != nil || !strings.Contains(out, "config.yaml") {
		t.Fatalf("path: %q, %v", out, err)
	}
	if _, err := run(t, "new", "x", "--schema", "missing"); err == nil {
		t.Fatal("broken config / unknown schema should fail")
	}
}

func TestParseAge(t *testing.T) {
	for in, ok := range map[string]bool{"30d": true, "2w": true, "12h": true, "": true, "x": false, "-1d": false} {
		if _, err := parseAge(in); (err == nil) != ok {
			t.Errorf("parseAge(%q) err=%v", in, err)
		}
	}
}

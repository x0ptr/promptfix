package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/x0ptr/promptfix/internal/rules"
)

// rulesSetup creates a git repo with a subdirectory as working directory
// and an isolated user scope. It returns the repo root.
func rulesSetup(t *testing.T) string {
	t.Helper()
	setup(t)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), "claude"))
	repo := t.TempDir()
	os.Mkdir(filepath.Join(repo, ".git"), 0o755)
	sub := filepath.Join(repo, "pkg", "x")
	os.MkdirAll(sub, 0o755)
	t.Chdir(sub)
	return repo
}

// runIn runs the CLI with stdin from a string (non-interactive).
func runIn(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(stdin)
	f.Seek(0, 0)
	old := os.Stdin
	os.Stdin = f
	defer func() { os.Stdin = old; f.Close() }()
	return run(t, args...)
}

// R1 + R2 via CLI.
func TestRulesCLI(t *testing.T) {
	repo := rulesSetup(t)
	if _, err := runIn(t, "Token only.", "rules", "new", "security/tenant-id", "--paths", "src/api/**,src/db/*.sql"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(repo, ".claude", "rules", "security", "tenant-id.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "---\npaths:\n  - \"src/api/**\"\n  - \"src/db/*.sql\"\n---\nToken only.\n" {
		t.Fatalf("file %q", data)
	}
	if _, err := runIn(t, "Be brief.", "rules", "new", "style", "--user"); err != nil {
		t.Fatal(err)
	}
	if _, err := runIn(t, "x", "rules", "new", "style", "--user"); err == nil {
		t.Fatal("new on existing rule should fail")
	}

	out, err := run(t, "rules", "list")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"user     style.md", "project  security/tenant-id.md", "src/api/**, src/db/*.sql"} {
		if !strings.Contains(out, want) {
			t.Errorf("list missing %q:\n%s", want, out)
		}
	}

	if _, err := run(t, "rules", "cp", "style"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "rules", "show", "style"); err == nil || !strings.Contains(err.Error(), "user and project") {
		t.Fatalf("ambiguous show should fail, got %v", err)
	}
	out, err = run(t, "rules", "show", "style", "--project")
	if err != nil || out != "Be brief.\n" {
		t.Fatalf("show: %q, %v", out, err)
	}
	if out, _ := run(t, "rules", "list"); !strings.Contains(out, "also in user") {
		t.Errorf("shared rule not flagged:\n%s", out)
	}
}

// R4 via CLI.
func TestRulesCLIHistory(t *testing.T) {
	rulesSetup(t)
	runIn(t, "v1", "rules", "new", "a")
	if _, err := run(t, "rules", "rm", "a", "--force"); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "rules", "history", "a")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("history %q", out)
	}
	if _, err := run(t, "rules", "restore", "a", strings.Fields(lines[1])[0]); err != nil {
		t.Fatal(err)
	}
	if out, _ := run(t, "rules", "show", "a"); out != "v1\n" {
		t.Fatalf("restored %q", out)
	}
	if out, err := run(t, "rules", "prune", "--all"); err != nil || !strings.Contains(out, "Deleted 1") {
		t.Fatalf("prune %q %v", out, err)
	}
}

// R5: outside a git repository only the user scope exists.
func TestRulesCLINoGit(t *testing.T) {
	setup(t)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), "claude"))
	dir := t.TempDir()
	t.Chdir(dir)
	if _, ok := rules.FindGitRoot(dir); ok {
		t.Skip("temp dir is inside a git repository")
	}
	_, err := runIn(t, "x", "rules", "new", "a")
	if err == nil || !strings.Contains(err.Error(), "not in a git repository") {
		t.Fatalf("expected git error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude")); !os.IsNotExist(err) {
		t.Fatal("must not create .claude outside a repository")
	}
	if _, err := runIn(t, "x", "rules", "new", "a", "--user"); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "rules", "list")
	if err != nil || !strings.Contains(out, "No git repository") || !strings.Contains(out, "user   a.md") {
		t.Fatalf("list %q %v", out, err)
	}
	// --root selects a project explicitly.
	if _, err := runIn(t, "y", "rules", "new", "b", "--root", dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "rules", "b.md")); err != nil {
		t.Fatal(err)
	}
}

func TestRulesFromPrompt(t *testing.T) {
	rulesSetup(t)
	seed(t, "p", "the product")
	if _, err := run(t, "rules", "from-prompt", "p", "product", "product-rule"); err != nil {
		t.Fatal(err)
	}
	if out, _ := run(t, "rules", "show", "product-rule"); out != "the product\n" {
		t.Fatalf("show %q", out)
	}
	if _, err := run(t, "rules", "from-prompt", "p", "nope", "x"); err == nil {
		t.Fatal("unknown tag should fail")
	}
}

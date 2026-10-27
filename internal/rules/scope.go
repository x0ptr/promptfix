// Package rules manages Claude Code rule files (.claude/rules/**/*.md) in the
// user scope and in a project, with history.
package rules

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/x0ptr/promptfix/internal/prompt"
)

// Kind is the scope of a rule.
type Kind int

const (
	User Kind = iota
	Project
)

func (k Kind) String() string {
	if k == User {
		return "user"
	}
	return "project"
}

// Scope is a rules directory.
type Scope struct {
	Kind Kind
	Root string // ~/.claude or the project root
	Dir  string // rules directory
	ID   string // stable identifier used for the history directory
}

// UserScope returns $CLAUDE_CONFIG_DIR/rules, or ~/.claude/rules.
// On Windows the home directory is %USERPROFILE%.
func UserScope() (Scope, error) {
	root := os.Getenv("CLAUDE_CONFIG_DIR")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Scope{}, err
		}
		root = filepath.Join(home, ".claude")
	}
	return Scope{Kind: User, Root: root, Dir: filepath.Join(root, "rules"), ID: "user"}, nil
}

// ProjectScope returns the scope of the project rooted at root.
func ProjectScope(root string) (Scope, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return Scope{}, err
	}
	sum := sha256.Sum256([]byte(abs))
	id := "project-" + prompt.Slug(filepath.Base(abs)) + "-" + hex.EncodeToString(sum[:4])
	return Scope{Kind: Project, Root: abs, Dir: filepath.Join(abs, ".claude", "rules"), ID: id}, nil
}

// FindGitRoot returns the first directory from start upwards that contains
// .git (a directory, or a file for worktrees and submodules).
func FindGitRoot(start string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

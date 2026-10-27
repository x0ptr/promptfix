// Package cli wires the promptfix commands.
package cli

import (
	"os"
	"runtime"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/x0ptr/promptfix/internal/schema"
	"github.com/x0ptr/promptfix/internal/store"
	"github.com/x0ptr/promptfix/internal/tui"
)

// Version is set at build time via -ldflags "-X ...cli.Version=...".
var Version = "dev"

// app lazily loads config and store so that commands like `config path`
// work even with a broken config file.
type app struct {
	cfg   *schema.Config
	store *store.Store
}

func (a *app) config() (*schema.Config, error) {
	if a.cfg != nil {
		return a.cfg, nil
	}
	path, err := store.ConfigPath()
	if err != nil {
		return nil, err
	}
	a.cfg, err = schema.Load(path)
	return a.cfg, err
}

func (a *app) prompts() (*store.Store, error) {
	if a.store != nil {
		return a.store, nil
	}
	cfg, err := a.config()
	if err != nil {
		return nil, err
	}
	dir, err := store.DataDir()
	if err != nil {
		return nil, err
	}
	a.store = store.New(dir, cfg.History.Keep)
	return a.store, nil
}

// editor returns the external editor command line.
func (a *app) editor() string {
	if a.cfg != nil && a.cfg.Editor != "" {
		return a.cfg.Editor
	}
	for _, env := range []string{"VISUAL", "EDITOR"} {
		if e := strings.TrimSpace(os.Getenv(env)); e != "" {
			return e
		}
	}
	if runtime.GOOS == "windows" {
		return "notepad"
	}
	return "vi"
}

// Execute runs the root command.
func Execute() error {
	return newRoot().Execute()
}

func newRoot() *cobra.Command {
	a := &app{}
	root := &cobra.Command{
		Use:           "promptfix",
		Short:         "Build structured prompts step by step",
		Long:          "promptfix builds prompts from a tag schema in an interactive terminal UI.\nIt works fully offline.",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) {
				return cmd.Help()
			}
			return a.home(cmd)
		},
	}
	root.AddCommand(
		newCmd(a), editCmd(a), listCmd(a), showCmd(a), exportCmd(a), copyCmd(a),
		cpCmd(a), mvCmd(a), rmCmd(a),
		historyCmd(a), restoreCmd(a), pruneCmd(a),
		runCmd(a), rulesCmd(a), configCmd(a), versionCmd(), agentExecCmd(),
	)
	return root
}

// home runs the start menu: prompts or rules, until the user quits.
func (a *app) home(cmd *cobra.Command) error {
	items := []tui.MenuItem{
		{Title: "Prompts", Desc: "create and edit prompts"},
		{Title: "Rules", Desc: "Claude Code rules (user and project)"},
	}
	for {
		choice, err := tui.RunMenu(items)
		if err != nil || choice < 0 {
			return err
		}
		switch choice {
		case 0:
			quit, err := a.promptsHome(cmd)
			if err != nil || quit {
				return err
			}
		case 1:
			if err := a.runRulesUI(scopeFlags{}, "", false); err != nil {
				return err
			}
		}
	}
}

// promptsHome runs the prompt list until the user goes back (false) or
// quits (true).
func (a *app) promptsHome(cmd *cobra.Command) (bool, error) {
	st, err := a.prompts()
	if err != nil {
		return false, err
	}
	for {
		action, slug, err := tui.RunPromptList(st)
		if err != nil {
			return false, err
		}
		switch action {
		case tui.PromptBack:
			return false, nil
		case tui.PromptQuit:
			return true, nil
		case tui.PromptEdit:
			err = a.editPrompt(cmd, slug)
		case tui.PromptNew:
			err = a.newPrompt(cmd, "", "")
		case tui.PromptRun:
			err = a.runPrompt(cmd, slug, nil)
		}
		if err != nil {
			return false, err
		}
	}
}

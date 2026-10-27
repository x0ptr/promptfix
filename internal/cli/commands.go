package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"charm.land/huh/v2"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/x0ptr/promptfix/internal/clipboard"
	"github.com/x0ptr/promptfix/internal/prompt"
	"github.com/x0ptr/promptfix/internal/schema"
	"github.com/x0ptr/promptfix/internal/store"
	"github.com/x0ptr/promptfix/internal/tui"
)

func newCmd(a *app) *cobra.Command {
	var schemaName string
	cmd := &cobra.Command{
		Use:   "new [name]",
		Short: "Create a prompt with the step-by-step wizard",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.config()
			if err != nil {
				return err
			}
			st, err := a.prompts()
			if err != nil {
				return err
			}
			sch, err := cfg.Schema(schemaName)
			if err != nil {
				return fmt.Errorf("%w (available: %s)", err, strings.Join(cfg.SchemaNames(), ", "))
			}
			validate := func(name string) error {
				if err := prompt.ValidateName(name); err != nil {
					return err
				}
				if st.Exists(name) {
					return fmt.Errorf("%q: %w", name, store.ErrExists)
				}
				return nil
			}
			name := ""
			if len(args) == 1 {
				name = args[0]
			} else if err := huh.NewInput().Title("Prompt name").Value(&name).Validate(validate).Run(); err != nil {
				if errors.Is(err, huh.ErrUserAborted) {
					return nil
				}
				return err
			}
			if err := validate(name); err != nil {
				return err
			}
			p := &prompt.Prompt{Name: name, Schema: sch.Name}
			for _, sec := range sch.Sections {
				p.Sections = append(p.Sections, prompt.Section{Tag: sec.Tag})
			}
			return runEditor(cmd, a, st, p, sch, true)
		},
	}
	cmd.Flags().StringVar(&schemaName, "schema", "", "schema to use (default from config)")
	cmd.RegisterFlagCompletionFunc("schema", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		cfg, err := a.config()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		return cfg.SchemaNames(), cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

func editCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:               "edit <name>",
		Short:             "Edit a prompt, starting from the section overview",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeNames(a, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.config()
			if err != nil {
				return err
			}
			st, err := a.prompts()
			if err != nil {
				return err
			}
			p, err := st.Load(args[0])
			if err != nil {
				return err
			}
			sch, err := cfg.Schema(p.Schema)
			if err != nil {
				// The prompt is self-contained; without its schema there are
				// just no help texts and no sections to add.
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v; editing without schema hints\n", err)
				sch = schema.Schema{Name: p.Schema}
			}
			return runEditor(cmd, a, st, p, sch, false)
		},
	}
}

func runEditor(cmd *cobra.Command, a *app, st *store.Store, p *prompt.Prompt, sch schema.Schema, wizard bool) error {
	res, err := tui.Run(tui.Options{
		Prompt: p,
		Schema: sch,
		Editor: a.editor(),
		Wizard: wizard,
		Save: func(p *prompt.Prompt) error {
			_, err := st.Save(p)
			return err
		},
	})
	if err != nil {
		return err
	}
	switch {
	case res.Unsaved:
		fmt.Fprintln(cmd.ErrOrStderr(), "Changes discarded.")
	case res.Saved:
		fmt.Fprintf(cmd.ErrOrStderr(), "Saved %q. Show it with: promptfix show %s\n", p.Name, p.Slug())
	}
	return nil
}

func listCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List saved prompts",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := a.prompts()
			if err != nil {
				return err
			}
			list, err := st.List()
			if err != nil {
				return err
			}
			if len(list) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "No prompts yet. Create one with: promptfix new")
				return nil
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "SLUG\tNAME\tSCHEMA\tFILLED\tUPDATED")
			for _, p := range list {
				fmt.Fprintf(w, "%s\t%s\t%s\t%d/%d\t%s\n", p.Slug(), p.Name, p.Schema,
					p.Filled(), len(p.Sections), p.Updated.Local().Format("2006-01-02 15:04"))
			}
			return w.Flush()
		},
	}
}

// loadForOutput loads the current version or, with rev set, a revision.
func loadForOutput(a *app, name, rev string) (*prompt.Prompt, error) {
	st, err := a.prompts()
	if err != nil {
		return nil, err
	}
	if rev != "" {
		return st.LoadRevision(name, rev)
	}
	return st.Load(name)
}

func showCmd(a *app) *cobra.Command {
	var rev string
	cmd := &cobra.Command{
		Use:               "show <name>",
		Short:             "Print the rendered prompt to stdout",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeNames(a, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := loadForOutput(a, args[0], rev)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), prompt.Render(p))
			return err
		},
	}
	cmd.Flags().StringVar(&rev, "rev", "", "render a history revision (see `history`)")
	return cmd
}

func exportCmd(a *app) *cobra.Command {
	var rev, out string
	var force bool
	cmd := &cobra.Command{
		Use:               "export <name> -o <file>",
		Short:             "Write the rendered prompt to a file",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeNames(a, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := loadForOutput(a, args[0], rev)
			if err != nil {
				return err
			}
			flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
			if force {
				flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
			}
			f, err := os.OpenFile(out, flags, 0o644)
			if errors.Is(err, os.ErrExist) {
				return fmt.Errorf("%s exists (use --force to overwrite)", out)
			}
			if err != nil {
				return err
			}
			if _, err := f.WriteString(prompt.Render(p)); err != nil {
				f.Close()
				return err
			}
			return f.Close()
		},
	}
	cmd.Flags().StringVarP(&out, "output", "o", "", "output file")
	cmd.Flags().StringVar(&rev, "rev", "", "render a history revision (see `history`)")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "overwrite an existing file")
	cmd.MarkFlagRequired("output")
	return cmd
}

func copyCmd(a *app) *cobra.Command {
	var rev string
	cmd := &cobra.Command{
		Use:               "copy <name>",
		Short:             "Copy the rendered prompt to the clipboard (OSC 52)",
		Long:              "Copy the rendered prompt to the clipboard using the OSC 52 terminal sequence.\nNo external tools are needed, but the terminal must support OSC 52.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeNames(a, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := loadForOutput(a, args[0], rev)
			if err != nil {
				return err
			}
			if err := clipboard.Copy(prompt.Render(p)); err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "Copied to clipboard.")
			return nil
		},
	}
	cmd.Flags().StringVar(&rev, "rev", "", "copy a history revision (see `history`)")
	return cmd
}

func cpCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:               "cp <name> <new-name>",
		Short:             "Copy a prompt",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeNames(a, 1),
		RunE: func(_ *cobra.Command, args []string) error {
			st, err := a.prompts()
			if err != nil {
				return err
			}
			return st.Copy(args[0], args[1])
		},
	}
}

func mvCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:               "mv <name> <new-name>",
		Short:             "Rename a prompt (including its history)",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeNames(a, 1),
		RunE: func(_ *cobra.Command, args []string) error {
			st, err := a.prompts()
			if err != nil {
				return err
			}
			return st.Rename(args[0], args[1])
		},
	}
}

func rmCmd(a *app) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:               "rm <name>",
		Short:             "Remove a prompt (it stays restorable from history until pruned)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeNames(a, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := a.prompts()
			if err != nil {
				return err
			}
			p, err := st.Load(args[0])
			if err != nil {
				return err
			}
			if !force {
				if !term.IsTerminal(os.Stdin.Fd()) {
					return errors.New("not a terminal; use --force to remove without asking")
				}
				ok := false
				err := huh.NewConfirm().Title(fmt.Sprintf("Remove %q?", p.Name)).Value(&ok).Run()
				if err != nil && !errors.Is(err, huh.ErrUserAborted) {
					return err
				}
				if !ok {
					return nil
				}
			}
			if err := st.Delete(p.Slug()); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Removed %q. Restore with: promptfix history %s\n", p.Name, p.Slug())
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "do not ask for confirmation")
	return cmd
}

func historyCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:               "history <name>",
		Short:             "List the saved versions of a prompt",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeNames(a, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := a.prompts()
			if err != nil {
				return err
			}
			revs, err := st.History(args[0])
			if err != nil {
				return err
			}
			if len(revs) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "No history.")
				return nil
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "REVISION\tSAVED")
			for _, r := range revs {
				fmt.Fprintf(w, "%s\t%s\n", r.ID, r.Time.Local().Format("2006-01-02 15:04:05"))
			}
			return w.Flush()
		},
	}
}

func restoreCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "restore <name> <revision>",
		Short: "Make a history revision the current version",
		Args:  cobra.ExactArgs(2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return completeNames(a, 1)(cmd, args, "")
			}
			st, err := a.prompts()
			if err != nil || len(args) > 1 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			revs, _ := st.History(args[0])
			ids := make([]string, len(revs))
			for i, r := range revs {
				ids[i] = r.ID
			}
			return ids, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := a.prompts()
			if err != nil {
				return err
			}
			if err := st.Restore(args[0], args[1]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Restored %s.\n", args[1])
			return nil
		},
	}
}

func pruneCmd(a *app) *cobra.Command {
	var keep int
	var olderThan string
	var all bool
	cmd := &cobra.Command{
		Use:   "prune [name]",
		Short: "Delete history versions (of one prompt, or of all)",
		Example: "  promptfix prune --all\n" +
			"  promptfix prune my-prompt --keep 5\n" +
			"  promptfix prune --older-than 30d",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeNames(a, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !all && keep <= 0 && olderThan == "" {
				return errors.New("specify --all, --keep N or --older-than AGE")
			}
			age, err := parseAge(olderThan)
			if err != nil {
				return err
			}
			st, err := a.prompts()
			if err != nil {
				return err
			}
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			n, err := st.Prune(name, store.PruneOptions{Keep: keep, OlderThan: age, All: all})
			fmt.Fprintf(cmd.ErrOrStderr(), "Deleted %d version(s).\n", n)
			return err
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "delete all history versions")
	cmd.Flags().IntVar(&keep, "keep", 0, "keep the newest N versions per prompt")
	cmd.Flags().StringVar(&olderThan, "older-than", "", "delete versions older than AGE (e.g. 30d, 2w, 12h)")
	return cmd
}

// parseAge parses durations with day (d) and week (w) units in addition to
// the units of time.ParseDuration.
func parseAge(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	for suffix, unit := range map[string]time.Duration{"d": 24 * time.Hour, "w": 7 * 24 * time.Hour} {
		if n, ok := strings.CutSuffix(s, suffix); ok {
			v, err := strconv.Atoi(n)
			if err != nil || v < 0 {
				return 0, fmt.Errorf("invalid age %q", s)
			}
			return time.Duration(v) * unit, nil
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid age %q", s)
	}
	return d, nil
}

func configCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Manage the configuration file"}
	var force bool
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Write the default configuration file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := store.ConfigPath()
			if err != nil {
				return err
			}
			if _, err := os.Stat(path); err == nil && !force {
				return fmt.Errorf("%s exists (use --force to overwrite)", path)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(path, schema.DefaultConfigYAML(), 0o644); err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "Wrote", path)
			return nil
		},
	}
	initCmd.Flags().BoolVarP(&force, "force", "f", false, "overwrite an existing file")
	cmd.AddCommand(
		initCmd,
		&cobra.Command{
			Use:   "path",
			Short: "Print the config file and data directory paths",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				cfgPath, err := store.ConfigPath()
				if err != nil {
					return err
				}
				dataDir, err := store.DataDir()
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "config: %s\ndata:   %s\n", cfgPath, dataDir)
				return nil
			},
		},
		&cobra.Command{
			Use:   "validate",
			Short: "Check the configuration file",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				cfg, err := a.config()
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "OK – schemas: %s (default: %s)\n",
					strings.Join(cfg.SchemaNames(), ", "), cfg.DefaultSchema)
				return nil
			},
		},
	)
	return cmd
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), "promptfix", Version)
		},
	}
}

// completeNames completes prompt slugs for the first n positional arguments.
func completeNames(a *app, n int) cobra.CompletionFunc {
	return func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if len(args) >= n {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		st, err := a.prompts()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		list, _ := st.List()
		names := make([]string, len(list))
		for i, p := range list {
			names[i] = p.Slug() + "\t" + p.Name
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}
}

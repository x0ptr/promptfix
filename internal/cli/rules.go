package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"charm.land/huh/v2"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/x0ptr/promptfix/internal/rules"
	"github.com/x0ptr/promptfix/internal/store"
	"github.com/x0ptr/promptfix/internal/tui"
)

const noProjectNote = "No git repository found here – only user rules are available (use --root DIR for another project)."

// scopeFlags selects the rule scope(s) of a command.
type scopeFlags struct {
	user, project bool
	root          string
}

// ruleScopes holds the user scope and, if found, the project scope.
type ruleScopes struct {
	user, project *rules.Manager
	note          string
}

func (a *app) ruleScopes(f scopeFlags) (*ruleScopes, error) {
	dataDir, err := store.DataDir()
	if err != nil {
		return nil, err
	}
	us, err := rules.UserScope()
	if err != nil {
		return nil, err
	}
	s := &ruleScopes{user: rules.NewManager(us, dataDir)}
	root := f.root
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		root, _ = rules.FindGitRoot(cwd)
	}
	if root == "" {
		s.note = noProjectNote
		return s, nil
	}
	ps, err := rules.ProjectScope(root)
	if err != nil {
		return nil, err
	}
	s.project = rules.NewManager(ps, dataDir)
	return s, nil
}

// all returns the available scopes, filtered by the flags.
func (s *ruleScopes) all(f scopeFlags) ([]*rules.Manager, error) {
	switch {
	case f.user && !f.project:
		return []*rules.Manager{s.user}, nil
	case f.project && !f.user:
		if s.project == nil {
			return nil, errors.New(noProjectNote)
		}
		return []*rules.Manager{s.project}, nil
	}
	if s.project == nil {
		return []*rules.Manager{s.user}, nil
	}
	return []*rules.Manager{s.user, s.project}, nil
}

// forCreate returns the scope new rules go to: the flag, else the project.
func (s *ruleScopes) forCreate(f scopeFlags) (*rules.Manager, error) {
	if f.user && f.project {
		return nil, errors.New("use either --user or --project")
	}
	if f.user {
		return s.user, nil
	}
	if s.project == nil {
		return nil, errors.New("not in a git repository; use --user or --root DIR")
	}
	return s.project, nil
}

// existing returns the scope that holds rel (as file or in its history).
// Without a flag, a rule present in both scopes is ambiguous.
func (s *ruleScopes) existing(f scopeFlags, rel string) (*rules.Manager, error) {
	cands, err := s.all(f)
	if err != nil {
		return nil, err
	}
	var found []*rules.Manager
	for _, m := range cands {
		if m.Exists(rel) {
			found = append(found, m)
		}
	}
	if len(found) == 0 {
		// Deleted rules are still reachable through their history.
		for _, m := range cands {
			if revs, _ := m.History(rel); len(revs) > 0 {
				found = append(found, m)
			}
		}
	}
	switch len(found) {
	case 0:
		return nil, fmt.Errorf("%s: %w", rel, rules.ErrNotFound)
	case 1:
		return found[0], nil
	}
	return nil, fmt.Errorf("%s exists in user and project scope; use --user or --project", rel)
}

func (s *ruleScopes) other(m *rules.Manager) (*rules.Manager, error) {
	if s.project == nil {
		return nil, errors.New(noProjectNote)
	}
	if m == s.user {
		return s.project, nil
	}
	return s.user, nil
}

// exportScopes returns the scopes a prompt section can be exported to,
// project first.
func (s *ruleScopes) exportScopes() []rules.Kind {
	if s.project == nil {
		return []rules.Kind{rules.User}
	}
	return []rules.Kind{rules.Project, rules.User}
}

func (s *ruleScopes) byKind(k rules.Kind) (*rules.Manager, error) {
	if k == rules.User {
		return s.user, nil
	}
	if s.project == nil {
		return nil, errors.New(noProjectNote)
	}
	return s.project, nil
}

// createRule writes a new rule; existing rules are only replaced with
// overwrite set.
func createRule(m *rules.Manager, rel string, paths []string, body string, overwrite bool) (string, error) {
	rel, err := rules.Normalize(rel)
	if err != nil {
		return "", err
	}
	if m.Exists(rel) && !overwrite {
		return "", fmt.Errorf("%s rule %s: %w", m.Scope.Kind, rel, rules.ErrExists)
	}
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	_, err = m.Write(rel, rules.New(paths, body))
	return rel, err
}

func rulesCmd(a *app) *cobra.Command {
	var f scopeFlags
	cmd := &cobra.Command{
		Use:   "rules",
		Short: "Manage Claude Code rules (~/.claude/rules, <git-root>/.claude/rules)",
		Long: "Manage Claude Code rule files in the user scope ($CLAUDE_CONFIG_DIR or ~/.claude)\n" +
			"and in the current project (the git root found upwards from the working directory).\n" +
			"Without a subcommand, the interactive rules manager opens.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runRulesUI(f, "", false)
		},
	}
	cmd.PersistentFlags().BoolVar(&f.user, "user", false, "user scope only")
	cmd.PersistentFlags().BoolVar(&f.project, "project", false, "project scope only")
	cmd.PersistentFlags().StringVar(&f.root, "root", "", "project root (default: git root of the working directory)")

	complete := func(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		s, err := a.ruleScopes(f)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		ms, _ := s.all(f)
		var out []string
		for _, m := range ms {
			list, _ := m.List()
			for _, r := range list {
				out = append(out, r.Rel+"\t"+m.Scope.Kind.String())
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}

	var paths []string
	var force bool
	newRule := &cobra.Command{
		Use:   "new <path>",
		Short: "Create a rule (interactive, or from stdin when piped)",
		Example: "  promptfix rules new security/tenant-id\n" +
			"  echo 'Use gofmt.' | promptfix rules new style --user --paths '**/*.go'",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rel, err := rules.Normalize(args[0])
			if err != nil {
				return err
			}
			if term.IsTerminal(os.Stdin.Fd()) {
				s, err := a.ruleScopes(f)
				if err != nil {
					return err
				}
				m, err := s.forCreate(f)
				if err != nil {
					return err
				}
				if m.Exists(rel) {
					return fmt.Errorf("%s rule %s: %w", m.Scope.Kind, rel, rules.ErrExists)
				}
				uf := f
				uf.user, uf.project = m.Scope.Kind == rules.User, m.Scope.Kind == rules.Project
				return a.runRulesUI(uf, rel, true)
			}
			body, err := io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}
			s, err := a.ruleScopes(f)
			if err != nil {
				return err
			}
			m, err := s.forCreate(f)
			if err != nil {
				return err
			}
			if _, err := createRule(m, rel, paths, string(body), force); err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "Created", m.Path(rel))
			return nil
		},
	}
	newRule.Flags().StringSliceVar(&paths, "paths", nil, "globs the rule applies to (comma-separated; only with piped stdin)")
	newRule.Flags().BoolVarP(&force, "force", "f", false, "overwrite an existing rule (only with piped stdin)")

	var rmForce, xferForce bool
	rm := &cobra.Command{
		Use:               "rm <path>",
		Short:             "Delete a rule (restorable from history until pruned)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: complete,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, rel, err := a.existingRule(f, args[0])
			if err != nil {
				return err
			}
			if !rmForce {
				if !term.IsTerminal(os.Stdin.Fd()) {
					return errors.New("not a terminal; use --force to delete without asking")
				}
				ok := false
				err := huh.NewConfirm().Title(fmt.Sprintf("Delete %s rule %s?", m.Scope.Kind, rel)).Value(&ok).Run()
				if err != nil && !errors.Is(err, huh.ErrUserAborted) {
					return err
				}
				if !ok {
					return nil
				}
			}
			if err := m.Delete(rel); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Deleted %s rule %s. Restore with: promptfix rules history %s --%s\n", m.Scope.Kind, rel, rel, m.Scope.Kind)
			return nil
		},
	}
	rm.Flags().BoolVarP(&rmForce, "force", "f", false, "do not ask for confirmation")

	transfer := func(move bool) *cobra.Command {
		use, short := "cp", "Copy a rule to the other scope"
		if move {
			use, short = "mv", "Move a rule to the other scope"
		}
		c := &cobra.Command{
			Use:               use + " <path>",
			Short:             short,
			Args:              cobra.ExactArgs(1),
			ValidArgsFunction: complete,
			RunE: func(cmd *cobra.Command, args []string) error {
				s, err := a.ruleScopes(f)
				if err != nil {
					return err
				}
				rel, err := rules.Normalize(args[0])
				if err != nil {
					return err
				}
				src, err := s.existing(f, rel)
				if err != nil {
					return err
				}
				dst, err := s.other(src)
				if err != nil {
					return err
				}
				if err := src.Transfer(rel, dst, move, xferForce); err != nil {
					if errors.Is(err, rules.ErrExists) {
						return fmt.Errorf("%w (use --force to overwrite; the old version goes to history)", err)
					}
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "%s → %s\n", src.Path(rel), dst.Path(rel))
				return nil
			},
		}
		c.Flags().BoolVarP(&xferForce, "force", "f", false, "overwrite the rule in the target scope")
		return c
	}

	var pruneKeep int
	var pruneOlder string
	var pruneAll bool
	prune := &cobra.Command{
		Use:   "prune [path]",
		Short: "Delete rule history versions",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !pruneAll && pruneKeep <= 0 && pruneOlder == "" {
				return errors.New("specify --all, --keep N or --older-than AGE")
			}
			age, err := parseAge(pruneOlder)
			if err != nil {
				return err
			}
			s, err := a.ruleScopes(f)
			if err != nil {
				return err
			}
			ms, err := s.all(f)
			if err != nil {
				return err
			}
			rel := ""
			if len(args) == 1 {
				if rel, err = rules.Normalize(args[0]); err != nil {
					return err
				}
			}
			total := 0
			for _, m := range ms {
				n, err := m.Prune(rel, store.PruneOptions{Keep: pruneKeep, OlderThan: age, All: pruneAll})
				total += n
				if err != nil {
					return err
				}
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Deleted %d version(s).\n", total)
			return nil
		},
	}
	prune.Flags().BoolVar(&pruneAll, "all", false, "delete all history versions")
	prune.Flags().IntVar(&pruneKeep, "keep", 0, "keep the newest N versions per rule")
	prune.Flags().StringVar(&pruneOlder, "older-than", "", "delete versions older than AGE (e.g. 30d, 2w, 12h)")

	var fromForce bool
	fromPrompt := &cobra.Command{
		Use:     "from-prompt <prompt> <tag> <path>",
		Short:   "Create a rule from a prompt section",
		Example: "  promptfix rules from-prompt my-api way_of_working workflow",
		Args:    cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := a.prompts()
			if err != nil {
				return err
			}
			p, err := st.Load(args[0])
			if err != nil {
				return err
			}
			i := p.Index(args[1])
			if i < 0 {
				return fmt.Errorf("prompt %q has no section <%s>", p.Name, args[1])
			}
			s, err := a.ruleScopes(f)
			if err != nil {
				return err
			}
			m, err := s.forCreate(f)
			if err != nil {
				return err
			}
			rel, err := createRule(m, args[2], nil, p.Sections[i].Content, fromForce)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "Created", m.Path(rel))
			return nil
		},
	}
	fromPrompt.Flags().BoolVarP(&fromForce, "force", "f", false, "overwrite an existing rule")

	cmd.AddCommand(
		&cobra.Command{
			Use:     "list",
			Aliases: []string{"ls"},
			Short:   "List rules of both scopes",
			Args:    cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				s, err := a.ruleScopes(f)
				if err != nil {
					return err
				}
				ms, err := s.all(f)
				if err != nil {
					return err
				}
				if s.note != "" && !f.user {
					fmt.Fprintln(cmd.ErrOrStderr(), s.note)
				}
				w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
				fmt.Fprintln(w, "SCOPE\tRULE\tAPPLIES TO\tLINES\tNOTE")
				for _, m := range ms {
					list, err := m.List()
					if err != nil {
						return err
					}
					for _, r := range list {
						applies := "always"
						if len(r.Paths) > 0 {
							applies = strings.Join(r.Paths, ", ")
						}
						note := r.ReadOnly
						if o, err := s.other(m); err == nil && o.Exists(r.Rel) {
							note = strings.TrimPrefix(note+"; also in "+o.Scope.Kind.String(), "; ")
						}
						fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", m.Scope.Kind, r.Rel, applies, r.Lines, note)
					}
				}
				return w.Flush()
			},
		},
		&cobra.Command{
			Use:               "show <path>",
			Short:             "Print a rule file",
			Args:              cobra.ExactArgs(1),
			ValidArgsFunction: complete,
			RunE: func(cmd *cobra.Command, args []string) error {
				m, rel, err := a.existingRule(f, args[0])
				if err != nil {
					return err
				}
				r, err := m.Read(rel)
				if err != nil {
					return err
				}
				_, err = cmd.OutOrStdout().Write(r.Raw())
				return err
			},
		},
		newRule,
		&cobra.Command{
			Use:               "edit <path>",
			Short:             "Edit a rule interactively",
			Args:              cobra.ExactArgs(1),
			ValidArgsFunction: complete,
			RunE: func(cmd *cobra.Command, args []string) error {
				m, rel, err := a.existingRule(f, args[0])
				if err != nil {
					return err
				}
				if !m.Exists(rel) {
					return fmt.Errorf("%s rule %s: %w (see `rules history`)", m.Scope.Kind, rel, rules.ErrNotFound)
				}
				uf := f
				uf.user, uf.project = m.Scope.Kind == rules.User, m.Scope.Kind == rules.Project
				return a.runRulesUI(uf, rel, false)
			},
		},
		rm,
		transfer(false),
		transfer(true),
		&cobra.Command{
			Use:               "history <path>",
			Short:             "List the saved versions of a rule",
			Args:              cobra.ExactArgs(1),
			ValidArgsFunction: complete,
			RunE: func(cmd *cobra.Command, args []string) error {
				m, rel, err := a.existingRule(f, args[0])
				if err != nil {
					return err
				}
				revs, err := m.History(rel)
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
		},
		&cobra.Command{
			Use:               "restore <path> <revision>",
			Short:             "Make a history revision of a rule current",
			Args:              cobra.ExactArgs(2),
			ValidArgsFunction: complete,
			RunE: func(cmd *cobra.Command, args []string) error {
				m, rel, err := a.existingRule(f, args[0])
				if err != nil {
					return err
				}
				if err := m.Restore(rel, args[1]); err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Restored %s\n", m.Path(rel))
				return nil
			},
		},
		prune,
		fromPrompt,
	)
	return cmd
}

func (a *app) existingRule(f scopeFlags, arg string) (*rules.Manager, string, error) {
	rel, err := rules.Normalize(arg)
	if err != nil {
		return nil, "", err
	}
	s, err := a.ruleScopes(f)
	if err != nil {
		return nil, "", err
	}
	m, err := s.existing(f, rel)
	return m, rel, err
}

// runRulesUI opens the rules manager; with open set, that rule is opened
// (or created with isNew) directly.
func (a *app) runRulesUI(f scopeFlags, open string, isNew bool) error {
	if _, err := a.config(); err != nil {
		return err
	}
	s, err := a.ruleScopes(f)
	if err != nil {
		return err
	}
	if f.project && s.project == nil {
		return errors.New(noProjectNote)
	}
	o := tui.RulesOptions{Editor: a.editor(), Note: s.note, Open: open, New: isNew}
	o.Scopes = []*rules.Manager{s.user}
	if s.project != nil {
		o.Scopes = append(o.Scopes, s.project)
		if !f.user {
			o.Start = 1
		}
	}
	return tui.RunRules(o)
}

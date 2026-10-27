package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/x0ptr/promptfix/internal/agent"
	"github.com/x0ptr/promptfix/internal/prompt"
	"github.com/x0ptr/promptfix/internal/store"
	"github.com/x0ptr/promptfix/internal/tui"
)

// launchConfig detects agents and multiplexer and loads the last choice.
func (a *app) launchConfig() tui.LaunchConfig {
	cfg := tui.LaunchConfig{Agents: agent.Detect(), Mux: agent.DetectMux()}
	if dir, err := store.DataDir(); err == nil {
		cfg.Last = agent.LoadLast(dir)
	}
	return cfg
}

func (a *app) rememberLaunch(o agent.Options) {
	if dir, err := store.DataDir(); err == nil {
		_ = agent.SaveLast(dir, o)
	}
}

// startAgent starts the agent with the rendered prompt in the current
// directory. Handoff does not return on success (Unix). It prints nothing,
// so it is safe to call while the TUI is running.
func startAgent(o agent.Options, rendered string) error {
	if err := o.Validate(); err != nil {
		return err
	}
	ag, err := agent.Find(agent.Detect(), o.Agent)
	if err != nil {
		return err
	}
	args := agent.Args(ag, o, rendered)
	if err := agent.CheckLength(args); err != nil {
		return err
	}
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	if o.Mode == agent.Split {
		self, err := os.Executable()
		if err != nil {
			return err
		}
		return agent.StartSplit(agent.DetectMux(), self, args, dir)
	}
	return agent.Exec(args, dir)
}

// runPrompt opens the picker (unless opts is complete) and starts the agent
// with the saved prompt.
func (a *app) runPrompt(cmd *cobra.Command, name string, opts *agent.Options) error {
	st, err := a.prompts()
	if err != nil {
		return err
	}
	p, err := st.Load(name)
	if err != nil {
		return err
	}
	o := opts
	if o == nil {
		cfg := a.launchConfig()
		chosen, ok, err := tui.RunLaunchPicker(cfg, p.Name)
		if err != nil || !ok {
			return err
		}
		a.rememberLaunch(chosen)
		o = &chosen
	}
	if err := startAgent(*o, prompt.Render(p)); err != nil {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Started %s in a split pane.\n", o.Agent)
	return nil
}

func runCmd(a *app) *cobra.Command {
	var o agent.Options
	var agentName, mode string
	cmd := &cobra.Command{
		Use:   "run <name>",
		Short: "Start claude or opencode with a saved prompt",
		Long: "Start an agent with the rendered prompt in the current directory.\n" +
			"Without --agent a picker opens. " + agent.SupportNote + "\n\n" +
			"Modes: handoff (promptfix exits, the agent takes over the terminal) and\n" +
			"split (new pane to the right; inside tmux, zellij or WezTerm).",
		Example: "  promptfix run my-api\n" +
			"  promptfix run my-api --agent claude --model opus --permission-mode plan\n" +
			"  promptfix run my-api --agent opencode --mode split --auto",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeNames(a, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if agentName == "" {
				if cmd.Flags().Changed("mode") || cmd.Flags().Changed("model") || cmd.Flags().Changed("permission-mode") || cmd.Flags().Changed("auto") {
					return errors.New("options need --agent")
				}
				return a.runPrompt(cmd, args[0], nil)
			}
			o.Agent, o.Mode = agent.ID(agentName), agent.Mode(mode)
			return a.runPrompt(cmd, args[0], &o)
		},
	}
	cmd.Flags().StringVar(&agentName, "agent", "", "claude or opencode (skips the picker)")
	cmd.Flags().StringVar(&mode, "mode", string(agent.Handoff), "handoff or split")
	cmd.Flags().StringVar(&o.Model, "model", "", "claude: model, e.g. opus, sonnet")
	cmd.Flags().StringVar(&o.PermissionMode, "permission-mode", "", "claude: permission mode, e.g. plan, acceptEdits")
	cmd.Flags().BoolVar(&o.Auto, "auto", false, "opencode: auto-approve permissions that are not explicitly denied")
	cmd.RegisterFlagCompletionFunc("agent", cobra.FixedCompletions([]string{"claude", "opencode"}, cobra.ShellCompDirectiveNoFileComp))
	cmd.RegisterFlagCompletionFunc("mode", cobra.FixedCompletions([]string{"handoff", "split"}, cobra.ShellCompDirectiveNoFileComp))
	cmd.RegisterFlagCompletionFunc("model", cobra.FixedCompletions(agent.ModelSuggestions, cobra.ShellCompDirectiveNoFileComp))
	cmd.RegisterFlagCompletionFunc("permission-mode", cobra.FixedCompletions(agent.PermissionModes[1:], cobra.ShellCompDirectiveNoFileComp))
	return cmd
}

// agentExecCmd runs inside a split pane: it reads the launch file written by
// StartSplit and replaces itself with the agent.
func agentExecCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:    "agent-exec",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			args, dir, err := agent.ReadLaunchFile(file)
			if err == nil {
				err = agent.Exec(args, dir)
			}
			// The pane closes when we exit; keep the error readable.
			fmt.Fprintln(os.Stderr, "promptfix:", err)
			fmt.Fprint(os.Stderr, "Press Enter to close.")
			fmt.Fscanln(os.Stdin)
			return err
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "launch file")
	cmd.MarkFlagRequired("file")
	return cmd
}

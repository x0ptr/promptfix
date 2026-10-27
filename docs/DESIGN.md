# promptfix – Design (AP1)

Interactive CLI that builds structured prompts from a tag schema, step by step.
Offline only: no network access, no AI calls at runtime.

## Decisions

| Topic | Decision |
|---|---|
| Language / toolchain | Go 1.27, `CGO_ENABLED=0` |
| TUI | Bubble Tea v2 + Bubbles (textarea, viewport) + Lip Gloss; Huh v2 for the name prompt of `new` and the confirm of `rm` |
| CLI framework | Cobra (subcommands, help, shell completion) |
| YAML | `go.yaml.in/yaml/v3` (maintained successor of `gopkg.in/yaml.v3`) |
| Storage format | Structured YAML per prompt; the tag prompt is *rendered*, never parsed back |
| Schema | Built-in default (9 sections), extensible/overridable via YAML config |
| Empty sections | Rendered as `<tag></tag>` |
| Clipboard | OSC 52 escape sequence only, no external tools |
| History | Every save keeps the previous version; `prune` deletes history |
| UI language | English |
| Distribution | Local build + `install.sh` (bash); cross-compile via `make dist` |

## Paths

| | Linux / macOS | Windows |
|---|---|---|
| Data | `$XDG_DATA_HOME/promptfix` or `$HOME/.local/share/promptfix` | `%LOCALAPPDATA%\promptfix` |
| Config | `$XDG_CONFIG_HOME/promptfix/config.yaml` or `$HOME/.config/promptfix/config.yaml` | `%APPDATA%\promptfix\config.yaml` |

macOS deliberately uses the XDG layout (not `~/Library`), as requested.
Override for tests/power users: `PROMPTFIX_HOME` (data) and `PROMPTFIX_CONFIG` (config file).

Data layout:

```
promptfix/
  prompts/<slug>.yaml                  current version
  history/<slug>/<revision>.yaml       previous versions
```

Revision IDs are UTC timestamps without colons (`20261027T221656.255044140Z`), valid on all platforms.
Slugs: lowercase, German umlauts transliterated (`ä`→`ae`, `ß`→`ss`), other characters collapsed to `-`.

## Data model

### Prompt file (`prompts/<slug>.yaml`)

```yaml
version: 1                # file format version
name: My API project      # display name; slug derived from it
schema: default           # schema used at creation (informational)
created: 2026-10-27T21:00:00Z
updated: 2026-10-27T21:30:00Z
sections:                 # ordered; this order is the render order
  - tag: product
    content: |
      ...
  - tag: glossar
    content: ""
```

A prompt is **self-contained**: it stores its own ordered section list. Changing
the config later never breaks or silently alters existing prompts. On `edit`,
tags that exist in the schema but not in the prompt are offered for adding.

### Config (`config.yaml`)

```yaml
default_schema: default
editor: ""                # optional, overrides $VISUAL/$EDITOR
history:
  keep: 0                 # 0 = unlimited; >0 = auto-prune to N entries on save
schemas:
  - name: default         # same name overrides the built-in default
    sections:
      - tag: product      # must match ^[a-z_][a-z0-9_-]*$, unique per schema
        title: Product
        help: Short description of the product.
        example: |
          ...
        required: false
```

Validation errors (bad tag names, duplicates, unknown `default_schema`) are
reported with file path and line; `promptfix config validate` checks explicitly.

### Built-in default schema

`product, glossar, technik, querschnitt, abweichungen, nicht_ziele, aufgabe,
abnahme, arbeitsweise` – with help texts and the examples from the original
spec as placeholders.

## Rendering

```
<product>
{content}
</product>

<glossar></glossar>
```

- Sections in stored order, separated by one blank line.
- Content is emitted verbatim (no escaping); only trailing whitespace/newlines are trimmed.
- Empty content → `<tag></tag>`.

## Commands

| Command | Purpose |
|---|---|
| `new [name] [--schema s]` | Start wizard |
| `edit <name>` | Open overview, jump to any section |
| `list` | Saved prompts (name, updated, filled/total sections) |
| `show <name>` | Render to stdout |
| `export <name> -o file` | Render to file |
| `copy <name>` | Render to clipboard via OSC 52 |
| `cp <name> <new>` / `mv <name> <new>` / `rm <name>` | Manage prompts (`rm` asks, `-f` skips; the removed version stays in the history and can be restored until pruned) |
| `history <name>` | List versions |
| `restore <name> <timestamp>` | Restore version (current one goes to history) |
| `prune [name] [--keep N] [--older-than 30d] [--all]` | Delete history |
| `config init` / `config path` / `config validate` | Manage config |
| `version` | Build info |

`show`, `export`, `copy` accept `--rev <timestamp>` to render a historic version.

## TUI

**Wizard / section editor** – one section per screen: title, help, textarea
(placeholder = example), progress `3/9`.

**Overview** – all sections with status (empty / filled / required-missing);
enter edits, `p` preview, `s` save, `q` quit.

**Preview** – scrollable rendered prompt.

Key bindings (Enter is a newline inside the textarea):

| Key | Action |
|---|---|
| `ctrl+n` / `ctrl+p` | Next / previous section |
| `ctrl+o` | Overview |
| `ctrl+e` | Edit section in external editor (`tea.ExecProcess`; fallback `vi` / `notepad`) |
| `ctrl+r` | Preview |
| `ctrl+s` | Save (draft) |
| `esc` | Back to overview; from overview: quit (asks if unsaved) |
| `ctrl+c` | Quit (asks if unsaved: save / discard / cancel) |

The current key bindings are always shown in the footer; there is no separate help screen.

Overview: `↑/↓` or `j/k`, `enter` edit (or add a schema section the prompt lacks), `p` preview, `s` save, `q` quit.
Preview: scroll, `c` copy (OSC 52), `ctrl+s` save, `esc` back.

## Persistence rules

- Atomic writes: temp file in same dir + rename.
- On save: if the content changed, move the old file to `history/<slug>/` first.
- Name collisions on `new`/`cp`/`mv` are rejected.
- No locking (single user, local tool); last write wins.
- Saving unchanged content is a no-op (no history entry).

## Offline guarantee

No code path opens network connections. The binary links the `net` package only
because `spf13/pflag` uses it to parse IP-typed flags; verified by running the
binary in a network namespace without interfaces (`unshare -rn`).

## Package layout

```
cmd/promptfix/        main
internal/cli/         cobra commands
internal/schema/      default schema (embedded), config load + validation
internal/prompt/      model, render
internal/store/       paths, load/save, history, prune
internal/clipboard/   OSC 52
internal/tui/         wizard, overview, preview
scripts/install.sh    build + install to ~/.local/bin
Makefile              build, test, dist (linux/darwin/windows × amd64/arm64)
```

## Acceptance

- **A1** `new` → fill sections → save → `show` emits all tags correctly in schema order.
- **A2** Abort wizard after step 3 and save → `edit` resumes the draft with all input intact.
- **A3** Jump from the overview to any section, change and save it without walking the other steps.
- **A4** Binaries for Linux, macOS and Windows start without network and without runtime dependencies.
- **A5** A section containing `<`, `>` or tag-like text survives save + load unchanged.

## Implementation order (AP1)

1. Module, Makefile, `prompt` model + render (A5 via unit tests)
2. `store`: paths, atomic save, history, prune
3. `schema`: embedded default, config load/validate, `config` commands
4. Non-interactive commands: list/show/export/copy/cp/mv/rm/history/restore/prune
5. TUI: section editor, overview, preview, external editor (A1–A3, teatest)
6. `make dist`, `install.sh`, offline check (A4)

---

# AP2 – Claude Code rules

Manage Claude Code rule files (`.claude/rules/**/*.md`) in the user scope and in
the current project. Only rule files – no `CLAUDE.md`, no `settings.json`.

## Scopes

| Scope | Location |
|---|---|
| User | `$CLAUDE_CONFIG_DIR/rules`, else `~/.claude/rules` (Windows: `%USERPROFILE%\.claude\rules`) |
| Project | `<git-root>/.claude/rules`; git root = first directory upwards from cwd containing `.git` (dir or file, so worktrees/submodules work); override with `--root DIR` |

No git root found → only the user scope is available; TUI and `rules list` say
so, `rules new` without `--user` fails. promptfix never creates `.claude/rules`
outside a git root unless `--root DIR` is given.

## Rule files

- Free Markdown body, optional YAML frontmatter delimited by `---` lines.
- promptfix edits only two things: the body and the `paths` key (string or list of strings).
- **Byte preservation:** frontmatter is never re-serialized. Editing `paths` replaces
  only the lines of the top-level `paths:` block (or appends it at the end of the
  frontmatter, or removes it when emptied; a frontmatter that becomes empty is
  dropped). Other keys, order and comments stay byte-identical.
- The TUI text fields normalize tabs and CR; a field the user did not change keeps
  its original bytes. Changed bodies use the file's line ending (taken from the
  first line).
- Unclosed or invalid frontmatter, or an unsupported `paths` value → read-only in
  the TUI; editable only as a whole file in the external editor (`ctrl+e`).
- Subdirectories are listed and can be created (`security/tenant-id`); `.md` is
  appended when no extension is given; paths must stay inside the rules directory.
- Symlinks are read-only (no edit, delete, move) and show their target.
- Deleting removes subdirectories that became empty.

## History

Raw file bytes (not YAML), so restore is exact:

```
<data>/rules-history/<scope-id>/<url-escaped rel-path>/<revision>.md
```

`scope-id` = `user` or `project-<slug-of-root-dir>-<8 hex of sha256(abs path)>`.
Every save, delete, move and overwrite archives the previous bytes first.
Writing identical content is a no-op.

## Cross-scope

- Copy / move a rule between user and project. Existing target → confirm in the
  TUI, `--force` in the CLI; the old target content goes to history.
- A rule whose relative path exists in both scopes is flagged in both lists
  (Claude Code loads both; project wins).

## TUI

- Bare `promptfix` (in a terminal) opens a start menu: **Prompts** (list: edit,
  new, delete) and **Rules**. Not in a terminal → help.
- `promptfix rules [--user | --project] [--root DIR]` opens the rules manager;
  starts in the project scope if available.
- List: relative path, `paths` summary ("always" when absent), line count, flags
  (read-only, "also in …"). Keys: `enter` edit · `n` new · `d` delete · `c` copy /
  `m` move to other scope · `h` history (enter restores) · `tab` switch scope · `q` quit.
- Editor: `paths` field (one glob per line) + Markdown field; `tab` switches,
  `ctrl+s` save, `ctrl+e` whole file in external editor, `esc` back (asks if
  unsaved), `ctrl+c` quit.
- Prompt editor: `ctrl+x` exports the current section as a new rule (path input,
  `tab` switches scope; existing rules are not overwritten).

## CLI

`promptfix rules list | show | new | edit | rm | cp | mv | history | restore | prune | from-prompt`

- Scope flags: `--user`, `--project`, `--root DIR`.
- Commands on an existing rule pick the scope that has it; present in both →
  error asking for `--user`/`--project`. Deleted rules are found via their history.
- `new` opens the TUI in a terminal; with piped stdin it writes the body from stdin
  (`--paths a,b`, `--force`). New rules go to the project unless `--user`.
- `from-prompt <prompt> <tag> <path>` creates a rule from a prompt section.

## Not done

- Match count of `paths` globs (left open).

## Acceptance

- **R1** Create a project rule with `paths` → file at `.claude/rules/<name>.md` with valid frontmatter.
- **R2** Switch between user and project → both lists correct, each with its path.
- **R3** Edit a hand-written rule with unknown frontmatter fields → only the content changes, everything else byte-identical.
- **R4** Delete a rule → restorable from history.
- **R5** No git repository → clear message, only the user scope, no file in a wrong place.

---

# AP3 – Start an agent

Build the prompt, then start **claude** or **opencode** with it. The user picks
the agent. No headless mode, no embedded terminal.

Amended non-goal: promptfix itself opens no network connections and calls no AI
API; it only starts locally installed agent CLIs.

## Detection

- Supported: `claude`, `opencode` – nothing else for now. The picker states this
  ("currently only claude and opencode are supported").
- Lookup: `PATH`, then the default install locations
  (`~/.local/bin/claude`, `~/.claude/local/claude`, `~/.opencode/bin/opencode`;
  `.exe` on Windows).
- Not found → shown greyed out with install hint, not selectable.
- No version calls, no `opencode models` (may hit the network).

## Modes

| Mode | Behaviour | Availability |
|---|---|---|
| **Handoff** | promptfix exits; the agent runs in the same terminal (Unix: `exec` replaces the process; Windows: child with inherited stdio, exit code passed through) | always |
| **Split** | agent opens in a new pane to the right; promptfix keeps running | inside tmux (`$TMUX`), zellij (`$ZELLIJ`) or WezTerm (`$WEZTERM_PANE`); otherwise greyed out with reason |

Working directory: the directory promptfix was started in (also for split panes).

### Split without quoting problems

Multiplexers pass commands through a shell, prompts contain arbitrary text. So the
pane runs promptfix itself as launcher:

```
tmux split-window -h -c <cwd> -- <promptfix> agent-exec --file <tmp>
zellij run --direction right --cwd <cwd> -- <promptfix> agent-exec --file <tmp>
wezterm cli split-pane --right --cwd <cwd> -- <promptfix> agent-exec --file <tmp>
```

`<tmp>` (mode 0600) holds agent, options and prompt; the hidden `agent-exec`
command reads it, deletes it and `exec`s the agent. Only safe path arguments
cross the shell.

## Passing the prompt

As argument (rendered prompt, see `show`):

- claude: `claude [--model M] [--permission-mode P] -- "<prompt>"`
- opencode: `opencode [--auto] --prompt "<prompt>"`

Windows command lines are limited to ~32k characters; longer prompts are rejected
with a hint (copy/export instead).

## Picker

Opened with `ctrl+g` (prompt editor and overview), `r` (prompt list) or
`promptfix run <name>`. The prompt is saved before the agent starts.
Keys: `↑/↓` field, `←/→` (or space) change value, `tab` completes the model,
`enter` start, `esc` cancel.

1. Agent: claude / opencode (detected state shown)
2. Mode: Handoff / Split
3. Options per agent
   - claude: model (free text, suggestions `opus`, `sonnet`, `haiku`, `fable`; empty = default),
     permission mode (`default`, `plan`, `acceptEdits`, `auto`, …)
   - opencode: auto-approve (`--auto`) on/off; no model option (the interactive
     CLI has none – it uses the opencode config)

The last selection is remembered per user (`<data>/launch.yaml`).
`promptfix run <name> --agent claude --mode split --model opus --permission-mode plan`
skips the picker.

Agents not installed are listed but not selectable; with no agent at all,
`ctrl+g` shows the support note instead of the picker.

## Verification

Unit tests cover argument building, detection, launch file round trip and the
picker flows. End to end in tmux 3.8 with fake `claude`/`opencode` scripts on
`PATH` (recording argv and cwd): split from the editor, handoff via `run` and via
the picker; the prompt arrived byte-identical (quotes, `$()`, backticks,
newlines, tags), no shell injection, cwd = start directory, the handoff pane's
PID runs the agent (promptfix replaced). zellij and WezTerm are covered by
argument tests only.

## Acceptance

- **S1** Picker shows both agents with detection state; a missing agent is not selectable and the "only claude/opencode" note is shown.
- **S2** Handoff starts the chosen agent in the start directory with the rendered prompt; promptfix is gone.
- **S3** Split (in tmux) opens the agent in a new pane; promptfix stays usable.
- **S4** A prompt with quotes, `$()`, backticks, newlines and `<tags>` arrives at the agent unchanged in both modes.
- **S5** claude options (model, permission mode) and opencode `--auto` end up on the command line exactly as chosen.

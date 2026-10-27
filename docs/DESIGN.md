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

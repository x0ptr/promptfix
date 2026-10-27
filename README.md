# promptfix

Build structured prompts step by step in the terminal. Offline only.

A prompt consists of tagged sections (`<product>`, `<technik>`, `<aufgabe>`, …).
promptfix walks you through them, saves drafts with history, and renders the
final prompt:

```
<product>
Short description
</product>

<glossar></glossar>
...
```

## Install

Requires Go 1.27+.

```sh
scripts/install.sh            # builds and installs to ~/.local/bin
scripts/install.sh --prefix /usr/local/bin
make dist                     # binaries for linux/darwin/windows × amd64/arm64 in dist/
```

## Usage

```sh
promptfix new "My API"         # wizard
promptfix edit my-api          # overview, jump to any section
promptfix list
promptfix show my-api          # render to stdout
promptfix export my-api -o prompt.txt
promptfix copy my-api          # clipboard via OSC 52 (terminal must support it)
promptfix cp | mv | rm
promptfix history my-api
promptfix restore my-api <revision>
promptfix prune [name] --all | --keep N | --older-than 30d
promptfix config init | path | validate
```

Editor keys: `ctrl+n`/`ctrl+p` next/previous section, `ctrl+o` overview,
`ctrl+e` external editor (`$VISUAL`/`$EDITOR`), `ctrl+r` preview, `ctrl+s` save,
`ctrl+c` quit.

Shell completion: `promptfix completion bash|zsh|fish|powershell`.

## Claude Code rules

`promptfix rules` manages rule files in `~/.claude/rules` (or `$CLAUDE_CONFIG_DIR/rules`)
and in `<git-root>/.claude/rules` of the current project:

```sh
promptfix rules                     # interactive manager (tab switches user/project)
promptfix rules list
promptfix rules new security/tenant-id          # opens the editor
echo "Use gofmt." | promptfix rules new style --user --paths '**/*.go'
promptfix rules cp|mv <rule>        # to the other scope
promptfix rules history|restore|prune
promptfix rules from-prompt my-api arbeitsweise workflow
```

Only `paths` and the body are edited; other frontmatter stays byte-identical.
Every change is kept in the history. In the prompt editor, `ctrl+x` exports the
current section as a rule. Bare `promptfix` opens a start menu (Prompts / Rules).

## Files

| | Linux / macOS | Windows |
|---|---|---|
| Prompts + history | `~/.local/share/promptfix` (`$XDG_DATA_HOME`) | `%LOCALAPPDATA%\promptfix` |
| Config | `~/.config/promptfix/config.yaml` (`$XDG_CONFIG_HOME`) | `%APPDATA%\promptfix\config.yaml` |
| Rule history | `<data>/rules-history` | `<data>\rules-history` |

Override with `PROMPTFIX_HOME` and `PROMPTFIX_CONFIG`.

## Custom schemas

`promptfix config init` writes the default config with all options documented.
Add schemas under `schemas:`; a schema named `default` replaces the built-in one.

```yaml
default_schema: short
schemas:
  - name: short
    sections:
      - tag: task
        title: Task
        help: What to do.
        required: true
      - tag: done
        title: Definition of done
```

See [docs/DESIGN.md](docs/DESIGN.md) for the design.

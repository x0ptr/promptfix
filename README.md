# promptfix

Build structured prompts step by step in the terminal, then hand them to
claude or opencode. promptfix itself works offline.

A prompt consists of tagged sections (`<product>`, `<technology>`, `<task>`, …).
promptfix walks you through them, saves drafts with history, and renders the
final prompt:

```
<product>
Short description
</product>

<glossary></glossary>
...
```

## Install

Linux and macOS (installs to `~/.local/bin`):

```sh
curl -fsSL https://raw.githubusercontent.com/x0ptr/promptfix/main/install.sh | bash
```

Windows (PowerShell; installs to `%LOCALAPPDATA%\Programs\promptfix` and adds it
to the user PATH):

```powershell
irm https://raw.githubusercontent.com/x0ptr/promptfix/main/install.ps1 | iex
```

Options (environment variables, both installers):

| Variable | Effect |
|---|---|
| `PROMPTFIX_VERSION=v0.1.0` | install this release instead of the latest |
| `PROMPTFIX_INSTALL_DIR=DIR` | install to DIR |
| `PROMPTFIX_VERIFY_ATTESTATION=1` | `install.sh` only: also verify the build provenance with `gh attestation verify` |

```sh
curl -fsSL https://raw.githubusercontent.com/x0ptr/promptfix/main/install.sh | PROMPTFIX_VERSION=v0.1.0 bash
```

The installers verify the SHA-256 checksum of every download. To update, run the
installer again (promptfix has no self-update; it stays offline).

**Uninstall:** delete the binary (`~/.local/bin/promptfix`, or
`%LOCALAPPDATA%\Programs\promptfix` on Windows, plus its user PATH entry).
Prompts, history and config stay in the directories listed under [Files](#files);
delete them too if you want.

### From source

Requires Go 1.27+.

```sh
scripts/install-from-source.sh   # builds and installs to ~/.local/bin
make dist                        # binaries for linux/darwin/windows × amd64/arm64 in dist/
```

### Releases

Pushing a tag `v*` runs `.github/workflows/release.yml`: tests on Linux, macOS
and Windows, then GoReleaser publishes archives (`promptfix_<os>_<arch>.tar.gz`,
`.zip` for Windows) and `checksums.txt`. Tags with a suffix (`v0.2.0-rc.1`) become
pre-releases.

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

## Start an agent

When the prompt is ready, start **claude** or **opencode** with it (currently the
only supported agents; both are detected automatically):

- `ctrl+g` in the prompt editor, `r` in the prompt list, or `promptfix run <name>`
- **Handoff**: promptfix exits, the agent takes over the terminal
- **Split**: the agent opens in a pane to the right (inside tmux, zellij or WezTerm)
- claude: model and permission mode; opencode: `--auto`

```sh
promptfix run my-api                                   # picker
promptfix run my-api --agent claude --model opus --permission-mode plan
promptfix run my-api --agent opencode --mode split --auto
```

The agent runs in the directory promptfix was started in. promptfix itself
stays offline; it only starts locally installed agent CLIs.

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
promptfix rules from-prompt my-api way_of_working workflow
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

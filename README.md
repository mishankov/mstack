# mstack

Reusable agent skills and a Go CLI for managing backlogs, implementing features and UIs, running structured reviews, and delivering pull requests through merge.

Skills install directly into `.agents/skills/` inside any Git repository, track their exact provenance in `.agents/mstack.lock.json`, and adapt to your project's own issue tracker, test suite, and conventions.

## Why mstack

- **Composable workflows.** Install only the skills you want—from single backlog helpers to full autonomous PR delivery—and `mstack` automatically resolves their dependencies.
- **Portable across agents.** Standard `.agents/skills/` folders work with any coding agent configured to load Agent Skills from `.agents/skills/`.
- **Safe by default.** Every installed skill is SHA-256 fingerprinted. `mstack` never overwrites local edits or unmanaged folders, applies updates atomically with rollback, and tolerates Git CRLF checkouts.
- **Bundled + upstream skills.** Ships a curated catalog of workflow skills alongside versioned upstream dependencies such as [Impeccable](https://github.com/pbakaus/impeccable).

## Quick start

### 1. Install the CLI

Install `mstack` once in the environment where your coding agent runs (requires **Go 1.23+** and **Git**). Paste this into your coding agent:

```text
Install the mstack CLI using the command below.

go install github.com/mishankov/mstack/cmd/mstack@latest

Verify the installation with mstack --version. If mstack isn't on PATH, use
the executable in Go's binary directory (GOBIN, or bin in the first GOPATH
entry) and tell me where it was installed.
```

Rerun the same instruction at any time to upgrade the CLI.

<details>
<summary>Or install from the terminal</summary>

Make sure Go's binary directory (`GOBIN` or `$GOPATH/bin`) is on your `PATH`:

```sh
go install github.com/mishankov/mstack/cmd/mstack@latest
mstack --version
```

</details>

### 2. Install skills in a project

Open your coding agent inside the target Git repository (it needs Git, the `mstack` CLI, and support for loading skills from `.agents/skills/`). Paste this to install all bundled skills and their required dependencies:

```text
Use the mstack CLI to install the bundled skills in my current Git project. Run
mstack install --all, then use mstack list to verify what was installed.
Preserve existing skill files if the installer reports a collision or local
edits, and tell me what blocked setup.
```

For a smaller install, ask your agent to install only the skills you want—for example, `capture-issue pick-next-work` for backlog management, `implement-ui` for UI work, or `stage-pr` for the full review-to-PR pipeline. `mstack` automatically installs required dependencies.

<details>
<summary>Or install from the terminal</summary>

Run these commands from any directory inside the target Git repository:

```sh
# Install all bundled skills and their required dependencies
mstack install --all
mstack list

# Or install a focused subset (dependencies are added automatically)
mstack install capture-issue pick-next-work   # Backlog management
mstack install implement-ui                   # UI design, implementation, and Impeccable
mstack install stage-pr                       # Full review-to-PR delivery pipeline
```

</details>

If your agent loads skills only at startup, open a new session after installing or updating skills.

## Skill catalog

Most users interact with `mstack` through **`stage-pr`** and **`ship-pr`**. Those two entry points orchestrate the supporting implementation, review, PR, and monitoring skills automatically, though every skill can also be invoked on its own.

### Primary delivery workflows

| Skill | Summary | Dependencies |
| --- | --- | --- |
| [`stage-pr`](skills/stage-pr/SKILL.md) | Implement, prove, review, file, and babysit a PR, then hand off ready to merge without merging. | `ship-pr` |
| [`ship-pr`](skills/ship-pr/SKILL.md) | Implement, prove, review, file, babysit, and merge a PR unattended once the merge gate passes. | `babysit-pr`, `file-pr`, `implement`, `review-loop`, `start-thread` |

### Supporting skills (called by `stage-pr` and `ship-pr`)

| Skill | Summary | Dependencies |
| --- | --- | --- |
| [`start-thread`](skills/start-thread/SKILL.md) | Run at the start of every new top-level thread to upgrade the mstack CLI and apply pending skill updates by default. | — |
| [`implement`](skills/implement/SKILL.md) | Confirm implementation intent and parallelize independent work. | `start-thread` |
| [`review-loop`](skills/review-loop/SKILL.md) | Run a bounded findings loop with Astra (including Impeccable audit/critique checks on UI changes). | `impeccable` |
| [`file-pr`](skills/file-pr/SKILL.md) | Prepare and open a concise, problem-first pull request. | `babysit-pr`, `github-comments` |
| [`babysit-pr`](skills/babysit-pr/SKILL.md) | Monitor PR checks, reviews, and base-branch drift without writing a custom polling loop. | `github-comments` |
| [`github-comments`](skills/github-comments/SKILL.md) | Format agent-authored GitHub comments with explicit model/user attribution. | — |

### Additional workflows

| Skill | Summary | Dependencies |
| --- | --- | --- |
| [`capture-issue`](skills/capture-issue/SKILL.md) | Record a request, bug, or deferred follow-up in the project backlog. | — |
| [`pick-next-work`](skills/pick-next-work/SKILL.md) | Inspect backlog candidates and recommend the next slice of work. | — |
| [`implement-ui`](skills/implement-ui/SKILL.md) | Design, review, implement, and visually verify interfaces across platforms (includes Birman's UI/UX principles and Vercel UI SDK guidance). | `impeccable` |
| [`impeccable`](https://github.com/pbakaus/impeccable) *(external)* | Critique UX, audit accessibility/responsiveness, and refine visual polish. Installed from latest stable upstream release (`skill-v*`). | — |
| [`review-panel`](skills/review-panel/SKILL.md) | Run a bounded, scored review with GPT Astra and Gemini 4 Neon at high effort. | `review-loop` |
| [`manage-mstack-skills`](skills/manage-mstack-skills/SKILL.md) | Guide an agent to install, update, or remove mstack skills and upgrade the CLI. | — |

> **Note on dependencies:** `mstack install` resolves transitive dependencies automatically. Installing `stage-pr`, `ship-pr`, `review-panel`, `review-loop`, `implement-ui`, or `--all` installs `impeccable` from its latest stable upstream release. You can also install `impeccable` standalone with `mstack install impeccable`.

## Managing skills & updates

### CLI cheat sheet

| Command | What it does |
| --- | --- |
| `mstack list` | Show catalog skills (bundled and external) and their local status. |
| `mstack install <skill>...` | Install named skills and all transitive dependencies. |
| `mstack install --all` | Install all bundled skills and their required dependencies. |
| `mstack update --check [<skill>...]` | Dry-run check for skill updates, renames, missing dependencies, and pending language attribute changes. |
| `mstack update [<skill>...]` | Update managed skills and migrate renamed skills (all still in catalog, or only named skills + their dependencies). |
| `mstack remove <skill>...` | Remove named managed skills and their lock entries (works offline). |
| `mstack --version` | Print the installed CLI version. |

Full flag semantics, lockfile format, safety guarantees, and recovery steps are documented in [CLI & Lockfile Reference](docs/cli.md).

### Automatic updates at thread start

When a project has `.agents/mstack.lock.json`, the `start-thread` skill (also installed as a dependency of `implement`, `ship-pr`, and `stage-pr`) runs once per project at the start of every new top-level thread. By default it upgrades the `mstack` CLI, runs `mstack update --check`, and applies pending skill, rename, dependency, and language attribute changes before other work.

To keep the check visible without auto-applying, tell your agent or add this line to your project's `AGENTS.md`:

```markdown
Defer mstack updates at thread start.
```

To skip the update check altogether, add:

```markdown
Skip mstack updates at thread start.
```

If you share installed skills via Git, commit `.agents/mstack.lock.json` and `.agents/.gitattributes` alongside `.agents/skills/`. The lockfile records the exact installed versions, and the attributes file keeps all `mstack`-managed skills out of GitHub's language statistics. Run `mstack update` in an existing installation to create the rules. [GitHub language statistics](docs/cli.md#github-language-statistics)

## Documentation

- [Workflows & Skills Guide](docs/workflows.md) — How each skill behaves, how they compose, and what harness capabilities they expect.
- [CLI & Lockfile Reference](docs/cli.md) — Command behavior, `.agents/mstack.lock.json`, safety/CRLF guarantees, and troubleshooting.
- [Authoring & Contributing Guide](docs/authoring.md) — Adding bundled skills, registering external skills, and running the validation suite.
- [AGENTS.md](AGENTS.md) — Repository conventions for agents maintaining `mstack`.

## Contributing & support

Report bugs, setup issues, or skill proposals in [GitHub Issues](https://github.com/mishankov/mstack/issues). Before opening a pull request, run:

```sh
go vet ./...
go test -race -count=1 ./...
```

See [Authoring & Contributing Guide](docs/authoring.md) for catalog schemas and validation rules.

---

Some skills originated in [tstack](https://github.com/t3dotgg/tstack).

<!--
MIT License

Copyright (c) 2026 Theo Browne

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
-->

# mstack

mstack gives your coding agent reusable workflows for managing a backlog,
implementing changes, and getting pull requests through review. The skills live
in your project and use its existing tracker and conventions.

## Install the CLI

Install the `mstack` CLI once in the environment where your coding agent runs.
You'll need Go 1.23 or later. Paste this into your agent.

```text
Install the mstack CLI using the command below.

go install github.com/mishankov/mstack/cmd/mstack@latest

Verify the installation with mstack --version. If mstack isn't on PATH, use
the executable in Go's binary directory (GOBIN, or bin in the first GOPATH
entry) and tell me where it was installed.
```

Rerun the same command to upgrade the CLI.

<details>
<summary>Install the CLI from the terminal</summary>

Make sure Go's binary directory is on your `PATH` before running `mstack`.

```sh
go install github.com/mishankov/mstack/cmd/mstack@latest
mstack --version
```

</details>

## Install skills in a project

Open your agent in the target Git project. It needs Git, the `mstack` CLI,
and support for loading skills from `.agents/skills/`. Paste this to install
the full catalog.

```text
Use the mstack CLI to install all skills in my current Git project. Run
mstack install --all, then use mstack list to verify what was installed.
Preserve existing skill files if the installer reports a collision or local
edits, and tell me what blocked setup.
```

For a smaller install, tell your agent which skills you want. For example,
`mstack install capture-issue pick-next-work` installs the backlog skills.
mstack automatically includes any required dependencies.

<details>
<summary>Install project skills from the terminal</summary>

Run these commands from the target Git project.

```sh
mstack install --all
mstack list
```

</details>

## Try it in your project

If your agent loads skills at startup, start a new session in the project
after installation. Try one of these prompts with the relevant skills installed.

- **Capture a follow-up.** “Capture an issue to document how to run our tests
  locally.”
- **Choose what to work on.** “Pick the next work from our backlog and explain
  your recommendation.”
- **Prepare a PR for handoff.** “Implement the selected issue and full stop.”
  The agent takes it through review and leaves the PR ready for you to merge.
- **Deliver through merge.** “Implement the selected issue and full send.” The
  agent takes it through review and merges once it meets the merge requirements.

The delivery workflows need an agent that can delegate review to Astra and
access the project's PR tooling. Release and deployment follow your project's
own workflow.

## Skill catalog

| Skill | What it does |
| --- | --- |
| [capture-issue](skills/capture-issue/SKILL.md) | Record a request or follow-up in the project backlog. |
| [pick-next-work](skills/pick-next-work/SKILL.md) | Compare backlog priorities and recommend what to work on next. |
| [implement](skills/implement/SKILL.md) | Check for skill updates and delegate independent implementation work. |
| [full-stop](skills/full-stop/SKILL.md) | Implement a change and leave the reviewed PR ready to merge. |
| [full-send](skills/full-send/SKILL.md) | Implement a change and carry the PR through review and merge. |
| [review-loop](skills/review-loop/SKILL.md) | Review a change with Astra and resolve findings in a bounded loop. |
| [review-group](skills/review-group/SKILL.md) | Get review feedback from three model providers. |
| [file-pr](skills/file-pr/SKILL.md) | Prepare and open a pull request. |
| [babysit-pr](skills/babysit-pr/SKILL.md) | Follow pull request checks and reviews. |
| [leaving-pr-comments](skills/leaving-pr-comments/SKILL.md) | Format GitHub comments written on your behalf. |
| [use-ui-sdk](skills/use-ui-sdk/SKILL.md) | Follow Vercel's UI SDK guidance for web UI work. |
| [manage-mstack-skills](skills/manage-mstack-skills/SKILL.md) | Install, update, and remove mstack skills or upgrade the CLI. |

## Manage skills

Ask your agent to check for updates, install more skills, or remove a skill.
The full catalog includes `manage-mstack-skills` to guide it through these tasks.

Before implementation, mstack's implementation and delivery skills check once
for updates to skills tracked in `.agents/mstack.lock.json`. The agent asks
before applying updates and defers them in unattended runs such as `full send`
and `full stop`. To allow automatic updates, including during unattended runs,
add this instruction to your project's `AGENTS.md`.

```markdown
Automatically apply mstack skill updates before implementation.
```

If you already use mstack, run `mstack update` once to install the skills that
perform this check. If your agent loads skills at startup, start a new session
to pick up the new instructions.

| Command | What it does |
| --- | --- |
| `mstack list` | Show the available skills and their status in your project. |
| `mstack install <skill>...` | Install the named skills and their dependencies. |
| `mstack update --check` | Preview updates without applying them. |
| `mstack update` | Update managed skills still in the catalog and report retired skills. |
| `mstack update <skill>...` | Update the named skills and their dependencies. |
| `mstack remove <skill>...` | Remove the named managed skills and their lock entries. Works offline. |

The CLI installs complete skill folders from mstack's `main` branch into
`.agents/skills/` at your project's Git root. If you share these folders through
Git, commit `.agents/mstack.lock.json` alongside them to record their source
commits and content fingerprints.

An update can reinstall a removed skill if another installed skill still
requires it.

<details>
<summary>Troubleshooting and recovery</summary>

- **Download fails.** Check that the agent's environment can reach GitHub
  and the Go module proxy.
- **A skill has local changes.** mstack preserves local edits and unmanaged
  folders. Review the reported paths before retrying.
- **A managed skill folder is missing.** Run `mstack remove <skill>` to clear
  its lock entry before reinstalling it.
- **A write was interrupted.** Keep `.agents/.mstack-write/` until you've
  checked the installed folders against the lock file. The `old/` subdirectory
  may contain the original folders.

</details>

## Help and contributions

Report setup problems or suggest a skill in
[GitHub Issues](https://github.com/mishankov/mstack/issues).

Edit skill sources in [`skills/`](skills/). For new skills, add the summary and
required dependencies to [`catalog.json`](catalog.json). Keep resource links
relative and include any helper scripts and agent metadata in the skill folder.
Run `go test -count=1 ./...` to validate the CLI and catalog before submitting a
change. See [AGENTS.md](AGENTS.md) for the repository conventions.

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

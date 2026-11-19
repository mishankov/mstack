# mstack

A portable development skill collection for backlog management, implementation,
review, and PR delivery.

Capture → pick and refine → `full-stop` or `full-send` → reconcile the backlog.

## Skills

| Skill | Role |
| --- | --- |
| [capture-issue](skills/capture-issue/SKILL.md) | Capture a request, agreed follow-up, or unfinished work in the project backlog. |
| [pick-next-work](skills/pick-next-work/SKILL.md) | Review remaining work, compare priorities, or choose the next useful item. |
| [implement](skills/implement/SKILL.md) | Delegate independent work to subagents when implementing a change. |
| [full-send](skills/full-send/SKILL.md) | Implement, prove, review, and carry a change through a merged PR. |
| [full-stop](skills/full-stop/SKILL.md) | Follow the same workflow, leaving a ready PR unmerged. |
| [review-loop](skills/review-loop/SKILL.md) | Review with Astra at high reasoning effort through a bounded findings loop. |
| [review-group](skills/review-group/SKILL.md) | Request feedback from three providers' models. |
| [use-ui-sdk](skills/use-ui-sdk/SKILL.md) | Use Vercel's UI SDK and its version-matched guidance when implementing web UI. |
| [manage-mstack-skills](skills/manage-mstack-skills/SKILL.md) | Manage mstack skills in a project and install or upgrade the CLI. |

Included helpers:

- [file-pr](skills/file-pr/SKILL.md) — prepare and open the PR.
- [babysit-pr](skills/babysit-pr/SKILL.md) — follow checks and reviews; includes
  the executable `scripts/pr-wait` helper for environments outside T3.
- [leaving-pr-comments](skills/leaving-pr-comments/SKILL.md) — format comments
  written on the user's behalf.

`full-send` calls `review-loop`, `file-pr`, and `babysit-pr`. `full-stop` builds on
`full-send`. The PR helpers use `leaving-pr-comments`. `review-group` is a separately
selected review mode; requesting it alongside a delivery mode should make clear
whether it replaces or supplements the usual Astra review.

## Working with the backlog

Keep the selected issue linked to its implementation and PR. Capture agreed
follow-ups and reconcile the item's actual state at handoff or completion.

The backlog skills discover the project's tracker, priorities, and conventions.
Keep project-specific commands and environment details in that project's existing
documentation. A planning request ends at a recommendation; an explicit
choose-and-implement request continues into the requested execution workflow.

The delivery modes end at PR readiness or merge. Release, installation, and
deployment continue through each project's established workflow.

## Installing and updating with the CLI

Install or upgrade the CLI with Go 1.23 or later. Make sure Git and the Go
binary directory are on your `PATH`. Go uses `GOBIN` if configured and
`$GOPATH/bin` by default.

Configure Git credentials with read access to the private `mstack` repository.
Set `GOPRIVATE` for the install so Go fetches the module directly from GitHub.

```sh
GOPRIVATE=github.com/mishankov/mstack \
  go install github.com/mishankov/mstack/cmd/mstack@latest
mstack --version
mstack --help
```

Run these commands in the Git project where you want to manage the skills.

```sh
mstack list
mstack install full-stop
mstack install --all
mstack update --check
mstack update full-stop
mstack update
mstack remove full-stop
```

The CLI reads the catalog from `mstack`'s `main` branch. It installs whole
folders and their required dependencies into `.agents/skills/` at your
project's Git root. `.agents/mstack.lock.json` records the source commit and
content fingerprint for each managed skill. If you share the skills through
Git, commit the lock file alongside them. The CLI accepts Git checkout
conversion from LF to CRLF for tracked text files.

`mstack update` updates the project's available managed skills and reports any
skills retired upstream. Pass names to update only those skills and their
dependencies. `--check` previews the changes without writing to the project.
Existing unmanaged folders, local edits, and missing managed folders block
installs and updates for the affected selection.

`mstack remove` deletes named managed skill folders and removes their lock
entries. It works offline, preserves locally edited folders, and can clear a
lock entry when the folder is already missing. It removes only the skills you
name. If an installed skill still requires a removed dependency, an update
can reinstall that dependency while it remains in the catalog.

If a process was forcibly interrupted during a write, keep
`.agents/.mstack-write/` until you have checked the installed folders against
the lock file. It may contain the original folders under `old/`.

Install the companion skill with `mstack install manage-mstack-skills`, or get
it as part of `mstack install --all`. It guides the agent through skill
management and CLI upgrades.

When adding a skill, add its summary and required skill dependencies to
`catalog.json`. Use relative Markdown links for local resources and
cross-skill references so CI can check that they ship with the skill.

This repository exposes its own catalog through the relative
`.agents/skills` symlink. Edit the files in `skills/` directly when working
here. Use the installer in other projects.

## Copying into projects manually

`skills/` is the source catalog. Copy complete skill folders into the destination
project's existing skill directory, commonly `.agents/skills/`. Copy the full
collection for the complete dependency set, or include all dependencies of any
selected subset. Keep helper scripts, executable permissions, and `agents/`
metadata with their skill folders.

Review name collisions before copying so existing customized skills are not
silently overwritten. Keep personal model preferences and project facts outside
the shared skill bodies. Preparing this repository does not install skills into
the user's global configuration or other projects.

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

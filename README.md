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
| [review-loop](skills/review-loop/SKILL.md) | Review with Sol through a bounded findings loop. |
| [review-group](skills/review-group/SKILL.md) | Request feedback from three providers' models. |
| [use-ui-sdk](skills/use-ui-sdk/SKILL.md) | Use Vercel's UI SDK and its version-matched guidance when implementing web UI. |

Included helpers:

- [file-pr](skills/file-pr/SKILL.md) — prepare and open the PR.
- [babysit-pr](skills/babysit-pr/SKILL.md) — follow checks and reviews; includes
  the executable `scripts/pr-wait` helper for environments outside T3.
- [leaving-pr-comments](skills/leaving-pr-comments/SKILL.md) — format comments
  written on the user's behalf.

`full-send` calls `review-loop`, `file-pr`, and `babysit-pr`. `full-stop` builds on
`full-send`. The PR helpers use `leaving-pr-comments`. `review-group` is a separately
selected review mode; requesting it alongside a delivery mode should make clear
whether it replaces or supplements the usual Sol review.

## Working with the backlog

Keep the selected issue linked to its implementation and PR. Capture agreed
follow-ups and reconcile the item's actual state at handoff or completion.

The backlog skills discover the project's tracker, priorities, and conventions.
Keep project-specific commands and environment details in that project's existing
documentation. A planning request ends at a recommendation; an explicit
choose-and-implement request continues into the requested execution workflow.

The delivery modes end at PR readiness or merge. Release, installation, and
deployment continue through each project's established workflow.

## Copying into projects

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

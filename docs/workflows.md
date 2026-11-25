# Workflows & Skills Guide

Most day-to-day work in `mstack` starts with **`stage-pr`** or **`ship-pr`**. Those two skills are the main entry points users call; they automatically orchestrate the supporting skills for implementation, review, PR creation, monitoring, and GitHub comments.

Every skill is **project-portable**: it adapts to whatever issue tracker, test runner, linter, and Git conventions are already configured in your repository, and any supporting skill can also be invoked directly when you only want that single step.

---

## 1. Primary workflows: `stage-pr` and `ship-pr`

Sources:
- [`skills/stage-pr/SKILL.md`](../skills/stage-pr/SKILL.md)
- [`skills/ship-pr/SKILL.md`](../skills/ship-pr/SKILL.md)

Use **`stage-pr`** when you want the agent to take a change all the way to a reviewed, green PR ready for you to merge. Use **`ship-pr`** when you are away from the thread and want the agent to carry the PR through review and merge autonomously.

Both workflows run the same end-to-end pipeline:

1. **Implement** the smallest change that fulfills the request (branching off the default branch if needed, and ensuring [`start-thread`](#start-thread) updates have run).
2. **Prove** every change with direct observed evidence (reproduction, CI checks, a focused test, or running the app and inspecting screenshots for UI changes).
3. **Review** using [`review-loop`](#review-loop) (which includes Impeccable audit/critique checks when the diff changes user-facing UI).
4. **File the PR** using [`file-pr`](#file-pr) after rebasing onto the latest default branch, adding a one-line proof summary to the description.
5. **Babysit the PR** using [`babysit-pr`](#babysit-pr) and [`github-comments`](#github-comments), fixing real findings and CI failures and re-proving each fix.
6. **Check the merge gate** on the latest commit: proof covers the diff, required checks are green, required reviews are complete with every finding fixed or dismissed with a written reason, no human review still requests changes, and the branch is up to date and mergeable.

| Aspect | `stage-pr` | `ship-pr` |
| --- | --- | --- |
| **Trigger** | `"stage PR"` or end-to-end delivery without merging | `"ship PR"` (explicit request required) |
| **Testing permission** | Authorized to run direct proof methods (app, browser, simulators, visual checks) without asking first | Same |
| **Branch push** | May commit, rebase, and `--force-with-lease` on the task branch | Same |
| **Merging** | **Never merges.** Disables auto-merge / removes from merge queue if present, and hands off at the gate. | **Merges** via the repo's configured merge method and branch protection (never `--admin`). |
| **Final output** | Handoff report: PR link, head SHA, verdict (`Ready to merge` / `Waiting for required approval` / `Blocked`), proof, reviewer outcome, risks. | Final report: PR link, merge commit, proof, and decisions made for the user. |

**Harness requirements:** `stage-pr` and `ship-pr` require an agent harness that can delegate `review-loop` to the selected reviewer (OpenAI GPT Astra at high reasoning effort by default, or an explicit model/effort override) and access your project's PR/GitHub tooling. Both ask the user only through the harness's interactive question tool when genuinely blocked (cannot prove the change, cannot satisfy the merge gate, destructive/external action required, ambiguous goal, or PR made obsolete); if no question tool is available or it returns no answer, the agent leaves the PR open and unmerged and starts its final reply with the question.

**Orchestrated runs & structured handoffs:** When an orchestrator or prompt requests a structured output schema, `stage-pr`, `ship-pr`, `discover`, and `pick-next-work` submit the final handoff matching that schema through the mechanism it specifies (for example, `t3_submit_output` in T3 Code or an `outputSchema` contract) before ending the turn alongside their concise reply—preserving exact factual fields (verdict, PR URL, head SHA, merge commit or artifact revision when applicable, summary, and any blocking questions or open decisions) and using `null` or empty values rather than fabricating missing fields. When genuinely blocked on a user decision, they prefer the harness's structured interactive question tool so a user or orchestrator can answer via pending-request tooling before falling back to a leading question in the final reply.

---

## 2. Supporting skills invoked by `stage-pr` and `ship-pr`

You normally get these automatically when running `stage-pr` or `ship-pr`, though you can also call any of them directly.

### `start-thread`
Source: [`skills/start-thread/SKILL.md`](../skills/start-thread/SKILL.md)

The lead agent runs this skill silently before other work, once per project at the start of every new top-level thread. Delegated children and subagents skip it.

- **Update mstack CLI and skills.** When `.agents/mstack.lock.json` exists, upgrades the `mstack` CLI via `go install github.com/mishankov/mstack/cmd/mstack@latest` (falling back to any already-installed `mstack` binary if Go or `go install` fails), runs `mstack update --check`, and by default runs `mstack update` when the check reports skill updates, renames, missing dependencies, or pending language attribute changes. Re-reads any updated or renamed skill before following it.
- To **defer** applying updates while still reporting pending skill changes, say so in your prompt or add `Defer mstack updates at thread start.` to your project's `AGENTS.md`.
- To **skip** updates entirely, say so in your prompt or add `Skip mstack updates at thread start.` to your project's `AGENTS.md`.
- If `mstack` is unavailable, times out, or hits local conflicts, the agent reports the issue, preserves local edits, and continues with the installed skills.

### `implement`
Source: [`skills/implement/SKILL.md`](../skills/implement/SKILL.md)  
Dependencies: `start-thread`, `diagnosing-bugs`

Handles implementation kickoff and parallelization:

1. **Confirm implementation intent:** Does not edit files or delegate during feasibility questions or design discussions; waits for a clear request to implement, then proceeds without re-confirming.
2. **Parallelize independent work:** Uses subagents where changes decompose cleanly.

Installing `implement`, `stage-pr`, `ship-pr`, or `--all` also installs Matt Pocock's [diagnosing-bugs skill](https://github.com/mattpocock/skills/tree/main/skills/engineering/diagnosing-bugs). Implementation uses it when a bug or performance regression needs investigation.

### `review-loop`
Source: [`skills/review-loop/SKILL.md`](../skills/review-loop/SKILL.md)  
Dependency: `impeccable`

Runs a bounded single-reviewer findings loop (default: OpenAI GPT Astra at high reasoning effort):

- Scopes the review strictly to the current change against its Git merge-base and runs covering checks before requesting review.
- Every reviewer checks the requested behavior and the applicable repository rules. Round 1 covers the full change. Later rounds apply both checks to fixes and open findings.
- For user-facing UI changes, automatically includes Impeccable's `audit` checks (accessibility, keyboard flow, responsive layout, theming) and `critique` rubric for new or substantially changed screens.
- Tracks findings in a ledger (`blocker` / `should` / `nit`), prefers the smallest fix (or deleting/narrowing code), and pushes back on scope creep.
- Defaults to **3 rounds** (extending to **5** only for security, permissions, auth, data loss, or state/concurrency issues, or when requested). Budget exhaustion is never treated as approval—disputed items are presented to the user.
- When pushing to a PR, appends `Reviewed with review-loop: <N> rounds with <model>. Verdict: approved`.

### `file-pr`
Source: [`skills/file-pr/SKILL.md`](../skills/file-pr/SKILL.md)  
Dependencies: `babysit-pr`, `github-comments`

Prepares and opens pull requests:

- Checks for an existing PR and reviews the local diff against the base branch.
- Writes a concise, human-readable title explaining *why* the change matters and a problem-first description (not an implementation inventory), ending with the public model and harness attribution.
- Scans title, description, quotes, and attachments for secrets before submission.
- Opens a real (non-draft) PR so review bots trigger, falling back to GitHub REST if GraphQL rate limits are exhausted.

### `babysit-pr`
Source: [`skills/babysit-pr/SKILL.md`](../skills/babysit-pr/SKILL.md)  
Dependency: `github-comments`

Monitors CI checks, review comments, and base-branch drift without ad-hoc polling or `sleep` loops:

- **In T3 Code:** Uses `watch_pull_request` (owned exclusively by the parent thread, never by delegated children) and calls `unwatch_pull_request` before handing off or asking a question.
- **Everywhere else:** Uses the bundled [`scripts/pr-wait`](../skills/babysit-pr/scripts/pr-wait) Python helper via its absolute path:
  ```sh
  /path/to/repo/.agents/skills/babysit-pr/scripts/pr-wait <pr> [--repo owner/name] [--until ready|green|any-change] [--interval 20]
  ```
  - Prints one line per real event (check failure, checks complete, new comment/review, CodeRabbit completion, base branch touching PR files, `BEHIND`/`DIRTY`, merge/close).
  - Exits `0` when the requested `--until` condition is met or the PR merged, `1` when action is needed, `2` on invalid input or duplicate watcher.
  - Persists state in `~/.local/state/pr-wait/` so events arriving between restarts are not lost.
- Verifies bot findings against source before changing code, declines scope-expanding suggestions with a written reason, and checks for `BEHIND`/`DIRTY` or overlapping base commits before calling a PR ready or merging.

### `github-comments`
Source: [`skills/github-comments/SKILL.md`](../skills/github-comments/SKILL.md)

Formats agent-authored GitHub comments left on the user's behalf (excluding PR descriptions, commit messages, and text the user explicitly asked to post verbatim without attribution):

```markdown
> [!NOTE]
> 🤖 **<model> responding on behalf of <user>**

Actual comment starts here.
```

- Uses the verified public model name (or harness name if model identity is unavailable—never guessed).
- Direct, conclusion-first prose backed by code/check evidence, checked for secrets before posting, and resolves review threads only after GitHub confirms the reply was posted.

---

## 3. Standalone & specialized workflows

Call these directly for discovery, backlog management, UI design and verification, multi-provider review, or CLI and skill maintenance.

### Discovery with `discover`

Ask “let’s scope this” or “let’s think this through” to explore a proposed change. The agent checks the project for facts, brings you decisions that affect the outcome, and summarizes the agreed scope, examples of success, and open questions in the conversation. Ask to implement when you are ready to act on that scope. The approach draws on Matt Pocock’s [grilling](https://github.com/mattpocock/skills/tree/main/skills/productivity/grilling) and [domain modeling](https://github.com/mattpocock/skills/tree/main/skills/engineering/domain-modeling) skills. See the [discover workflow](../skills/discover/SKILL.md).

### Backlog: `capture-issue` and `pick-next-work`

- **[`capture-issue`](../skills/capture-issue/SKILL.md):** Locates the project's canonical tracker/backlog, checks for duplicates first (reporting **Created**, **Updated**, **Already tracked**, or **Blocked**), writes a concise problem/outcome description without inventing an implementation plan, and verifies the saved record.
- **[`pick-next-work`](../skills/pick-next-work/SKILL.md):** Reads the backlog, checks top candidates against current code and open PRs to avoid already-active or superseded items, and recommends one concrete slice with tradeoffs and acceptance scenarios. For an explicit choose-and-implement request, it proceeds as requested; it hands off to `stage-pr` or `ship-pr` only when that workflow was explicitly requested (never inferring merge authority merely from selecting or implementing an item).

### UI design & verification: `implement-ui` + `impeccable`

Sources:
- [`skills/implement-ui/SKILL.md`](../skills/implement-ui/SKILL.md)
- [`skills/implement-ui/references/birman-ui-ux.md`](../skills/implement-ui/references/birman-ui-ux.md) ([source notes](../skills/implement-ui/references/birman-sources.md))
- [`skills/implement-ui/references/ui-sdk.md`](../skills/implement-ui/references/ui-sdk.md)
- External dependency: [Impeccable](https://github.com/pbakaus/impeccable)

Use when designing, reviewing, implementing, or updating a user interface on any platform or framework (or when requesting Vercel's UI SDK):

- **Interaction design:** Applies a synthesis of Ilya Birman's UI/UX principles—starting from real user scenarios, coherent form syntax, clear control labels/states, thoughtful layout width usage, and state completeness checks (empty, single, many, extreme, and live-changing states).
- **Impeccable passes:** Runs focused passes for UX feedback (`critique`), implementation/accessibility/responsive quality (`audit`), or visual refinement (`polish`)—for example, `$impeccable critique the settings screen`.
- **Web UI SDK:** For package-based web apps, integrates with `@ui-sdk/core` and the matching adapter (`@ui-sdk/react`, `@ui-sdk/vue`, `@ui-sdk/svelte`, `@ui-sdk/solid`, or `@ui-sdk/web`) and loads the SDK's version-matched skill and docs from `node_modules`. For plain HTML pages without a build step, follows the Web Components script and stylesheet setup instead of adding a package toolchain.
- **Visual verification:** Runs the updated UI, exercises changed interactions, and attaches screenshots demonstrating the change (or reports the specific blocker if capture isn't possible).

### Multi-provider review: `review-panel`
Source: [`skills/review-panel/SKILL.md`](../skills/review-panel/SKILL.md)

`review-panel` runs the [review loop](#review-loop) with GPT Astra and Gemini 4 Neon in parallel, both at high reasoning effort by default. You can override the reviewers or effort.

- The panel uses the same scoped diff, finding ledger, UI checks, three-round default, and bounded extension as `review-loop`. Approval requires each required reviewer to approve the current behavior diff and all blockers and shoulds to be resolved.
- The lead verifies findings as they arrive and applies fixes after the required reviews finish. The `Review panel results` table records each reviewer's score out of 10 per round in the PR description or, for local work, in the final reply.
- Installing `review-panel` also installs `review-loop` and its Impeccable dependency.

### Skill management: `manage-mstack-skills`
Source: [`skills/manage-mstack-skills/SKILL.md`](../skills/manage-mstack-skills/SKILL.md)

Guides your coding agent to install or upgrade the `mstack` CLI and run `list`, `install`, `update`, and `remove` safely while preserving local edits and warning before removing skills that other installed skills depend on.

## Related docs

- [README](../README.md)
- [CLI & Lockfile Reference](cli.md)
- [Authoring & Contributing Guide](authoring.md)

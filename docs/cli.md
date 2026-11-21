# CLI & Lockfile Reference

The `mstack` CLI installs, updates, and removes agent skills in any Git project. It manages complete skill directories inside `.agents/skills/` at the project's Git root and tracks state in `.agents/mstack.lock.json`.

## Requirements

- **Go 1.23+** (to install or upgrade the CLI via `go install`)
- **Git** on `PATH` (used to locate the project root and fetch skill archives)
- Network access to GitHub (and upstream skill repositories) for `list`, `install`, and `update`; `remove` works fully offline

## Installing and upgrading the CLI

Paste this prompt into your coding agent to install (or upgrade) the `mstack` CLI:

```text
Install the mstack CLI using the command below.

go install github.com/mishankov/mstack/cmd/mstack@latest

Verify the installation with mstack --version. If mstack isn't on PATH, use
the executable in Go's binary directory (GOBIN, or bin in the first GOPATH
entry) and tell me where it was installed.
```

<details>
<summary>Or install from the terminal</summary>

Ensure Go's binary directory (`GOBIN` or `$GOPATH/bin`) is on your `PATH`, then run:

```sh
go install github.com/mishankov/mstack/cmd/mstack@latest
mstack --version
```

</details>

## Commands

All project commands resolve the repository root using `git rev-parse --show-toplevel`, so you can run `mstack` from any subdirectory inside a Git checkout.

### `mstack list`

Fetches the upstream catalog and prints a table of all available bundled skills, external skills (tagged `(external)`), and any locally managed skills that have been retired upstream.

Each skill is reported with one of four statuses:

| Status | Meaning |
| --- | --- |
| `installed` | Tracked in `.agents/mstack.lock.json` and present in `.agents/skills/<skill>`. |
| `available` | Present in the upstream catalog and not installed locally. |
| `missing` | Tracked in `.agents/mstack.lock.json`, but `.agents/skills/<skill>` is absent on disk. |
| `unmanaged` | A directory or file exists at `.agents/skills/<skill>`, but it is not tracked by `mstack`. |

### `mstack install <skill>...` / `mstack install --all`

Installs one or more skills into `.agents/skills/` along with their complete transitive dependency graph.

- `mstack install <skill>...` accepts one or more skill names (bundled or external).
- `mstack install --all` installs every **bundled** skill in `catalog.json` plus any required dependencies (including external dependencies like `impeccable`, while skipping external skills that nothing requires).
- Passing both `--all` and explicit skill names is rejected.
- Already-current managed skills are left untouched and counted in `Already current: <n>`.
- If an unmanaged directory already exists at `.agents/skills/<skill>`, installation aborts without modifying any files or the lockfile.

### `mstack update [--check] [<skill>...]`

Updates managed skills and installs any newly added dependencies.

- `mstack update` updates every managed skill that still exists in the upstream catalog. If a managed skill was removed upstream, `mstack` reports `Retired upstream (<n>): ...`, skips it, and continues updating the remaining skills.
- `mstack update <skill>...` updates only the named managed skills and their transitive dependencies. Naming an unmanaged skill fails and suggests `mstack install <skill>`. Naming a retired skill fails because it is unavailable upstream.
- `mstack update --check [<skill>...]` previews what would change (`Updates available`, `Dependencies to install`, `Already current`, and any local conflicts) without modifying `.agents/skills/` or `.agents/mstack.lock.json`.
- If a previously removed skill is still required by another installed skill, `mstack update` reinstalls it as a dependency.

### `mstack remove <skill>...`

Removes one or more managed skills from `.agents/skills/` and deletes their entries from `.agents/mstack.lock.json`.

- **Works offline:** never contacts GitHub or upstream repositories.
- **Protects local edits:** refuses to delete a skill whose files differ from the lockfile fingerprint.
- **Repairs missing folders:** if a managed skill's directory was already deleted on disk, `mstack remove <skill>` still cleans up its lockfile entry.
- **No cascading removal:** removes only the named skills. Removing a dependency does not remove skills that depend on it (and a future `mstack update` may reinstall it if still required).

### `mstack --version` and `mstack --help`

- `mstack --version` prints the build version (or `(devel)` for local source builds).
- `mstack --help`, `mstack -h`, `mstack help`, or running `mstack` with no arguments prints usage text.

## Sources & release resolution

- **Bundled skills** are exported from the `main` branch of `https://github.com/mishankov/mstack.git` via a shallow bare clone and `git archive`.
- **External skills** (defined in `external-skills.json`) are fetched from their upstream HTTPS Git repositories:
  1. `mstack` queries `git ls-remote --tags --sort=-version:refname` for tags matching `<tag_prefix>*`.
  2. It selects the highest strict stable semantic version `<tag_prefix>MAJOR.MINOR.PATCH` (prereleases such as `-rc.1` are ignored; both lightweight and annotated tags are supported).
  3. It fetches the target commit with `fetch.fsckObjects=true` and `fetch.fsck.hasDotgit=error`, exports the configured skill subdirectory, and includes any configured root-level files (such as `LICENSE` and `NOTICE.md`).

## Lockfile (`.agents/mstack.lock.json`)

Every managed project stores lock metadata at `.agents/mstack.lock.json`:

```json
{
  "version": 1,
  "skills": {
    "implement-ui": {
      "commit": "6e8e289069bb1b5cce8283a32d678fb5be3062d9",
      "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
    },
    "impeccable": {
      "commit": "0123456789abcdef0123456789abcdef01234567",
      "sha256": "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
      "release": "skill-v1.2.0",
      "source": {
        "repository": "https://github.com/pbakaus/impeccable.git",
        "path": ".agents/skills/impeccable",
        "include": ["LICENSE", "NOTICE.md"]
      }
    }
  }
}
```

| Field | Description |
| --- | --- |
| `version` | Lockfile format version (currently `1`). |
| `commit` | Full 40-character Git commit SHA the skill was exported from. |
| `sha256` | Deterministic SHA-256 fingerprint of all directory entries, regular file contents, and executable bits. |
| `release` | *(External skills only)* Selected upstream release tag. |
| `source` | *(External skills only)* Upstream repository URL, subdirectory path, and included root files. |

**Should you commit `.agents/mstack.lock.json`?** Yes—whenever you commit `.agents/skills/` to your project repository, commit `.agents/mstack.lock.json` alongside it so teammates and agents verify against the exact same installed versions and fingerprints.

## Safety guarantees

### Local edit & collision protection

Before modifying or deleting any skill folder, `mstack` recomputes its SHA-256 fingerprint and compares it to `.agents/mstack.lock.json`. During `install` and `update` planning, if managed skills have local edits or an install targets unmanaged folders, `mstack` aborts and reports every conflicting path without touching any files. During `remove`, `mstack` validates and moves each named folder in turn, stopping at the first local-edit conflict and attempting to roll back any earlier moves.

### CRLF checkout compatibility

If your repository checks out text files with CRLF line endings (for example, `core.autocrlf=true` on Windows), `mstack` checks `git ls-files --eol` and `git cat-file --filters`. Files whose working-tree bytes match Git's clean filtered checkout are normalized to LF during verification so clean checkouts do not trigger false `local changes` errors—while actual edits, binary (`-text`) CRLF changes, and dirty working trees are still caught.

### Atomic writes & rollback

All write commands (`install`, `update`, `remove`) acquire an exclusive transaction directory at `.agents/.mstack-write/`:

1. Staged skills are copied into `.agents/.mstack-write/new/` and re-fingerprinted to confirm contents and executable permissions survived the copy.
2. The next lockfile is written to `.agents/.mstack-write/mstack.lock.json`.
3. Local targets are re-validated right before swapping.
4. Existing folders being replaced or removed are moved to `.agents/.mstack-write/old/`, new folders are moved into `.agents/skills/`, and the staged lockfile is atomically renamed over `.agents/mstack.lock.json`.
5. If any step fails, `mstack` attempts to reverse every completed rename in reverse order. If rollback succeeds, it removes `.agents/.mstack-write/`; if any rollback rename fails, it retains `.agents/.mstack-write/` (including backups in `old/`) for manual recovery.

Concurrent `mstack` invocations—including read-only `list` and `update --check`—refuse to run while `.agents/.mstack-write/` exists.

## Troubleshooting & recovery

| Symptom | Cause & fix |
| --- | --- |
| **Download / clone fails** | Verify `git` is installed and the environment can reach GitHub (and upstream skill repos) plus the Go module proxy for `go install`. |
| **`unmanaged skill already exists at ...`** | A folder already exists in `.agents/skills/<name>` outside `mstack`. Back up, rename, or remove that folder before running `mstack install`. |
| **`local changes at ...`** | Files inside a managed skill differ from the recorded fingerprint. Review your edits with `git diff .agents/skills/<name>`, revert or back them up, then retry. |
| **`managed skill is missing at ...`** | The skill is listed in `.agents/mstack.lock.json` but its folder was deleted. Run `mstack remove <skill>` to clear the stale lock entry, then `mstack install <skill>` if you still want it. |
| **`another mstack write is in progress or was interrupted`** | Either another `mstack` process is running, or a previous write was interrupted / failed rollback. Confirm no `mstack` writer is still running, then reconcile `.agents/skills/` against `.agents/mstack.lock.json`—restoring originals from `.agents/.mstack-write/old/` where needed—before deleting `.agents/.mstack-write/` and retrying. |

## Related docs

- [README](../README.md)
- [Workflows & Skills Guide](workflows.md)
- [Authoring & Contributing Guide](authoring.md)

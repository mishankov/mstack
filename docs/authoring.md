# Authoring & Contributing Guide

This guide covers how `mstack` is structured, how to add or modify bundled and external skills, and how changes are validated.

See also [AGENTS.md](../AGENTS.md) for the short-form repository rules.

## Repository layout

```text
.
├── .agents/skills -> ../skills    # Relative symlink exposing the catalog to local agents
├── .github/workflows/ci.yml       # CI: go vet + go test -race -count=1 ./...
├── AGENTS.md                      # Maintainer rules for coding agents
├── README.md                      # Main project overview
├── catalog.json                   # Bundled skill registry (summaries & dependencies)
├── external-skills.json           # Upstream external skill registry
├── cmd/mstack/                    # Go CLI implementation & validation tests
│   ├── main.go                    # CLI argument parsing & entrypoint
│   ├── catalog.go                 # Upstream catalog fetching, archive extraction & selection
│   ├── external.go                # External skill validation, tag resolution & fetching
│   ├── project.go                 # Lockfile, SHA-256 fingerprinting, CRLF check & planning
│   ├── transaction.go             # Exclusive writer lock, staging & atomic rollback
│   ├── catalog_test.go            # Validates live catalog, frontmatter, links & Git modes
│   ├── external_test.go           # End-to-end external skill lifecycle tests
│   └── main_test.go               # End-to-end install/update/remove/CRLF/rollback tests
├── docs/                          # Project documentation
└── skills/                        # Source catalog of bundled skills
```

> **Note:** When `mstack` installs skills in downstream projects, it archives only `catalog.json`, `external-skills.json`, and `skills/`. Files in `docs/` and `cmd/` are never copied into user repositories.

## Bundled skills

### Folder structure

Every bundled skill lives in `skills/<name>/`:

- `<name>` must match `^[a-z0-9]+(-[a-z0-9]+)*$` and be at most 64 characters.
- `SKILL.md` is required and must be valid UTF-8 with a single YAML frontmatter document:
  ```markdown
  ---
  name: my-skill
  description: Use when the user asks to ...
  ---

  # My Skill

  Direct procedure here.
  ```
- Optional subdirectories:
  - `agents/openai.yaml` — agent UI display metadata (`display_name`, `short_description`, `default_prompt`).
  - `references/` — detailed supporting guides linked from `SKILL.md`.
  - `scripts/` — executable helper scripts. Any file under `skills/<name>/scripts/` starting with `#!` **must** be committed with executable permissions (`git update-index --chmod=+x` / mode `100755`).
- All tracked files under `skills/` must be regular files (`100644` or `100755`)—no symlinks inside skill folders.

### Frontmatter & portability rules

- `name` must exactly match the skill folder name.
- `description` must be 1–1024 characters and describe **when** the skill activates. Quote values that YAML 1.1 parsers could misread as booleans (`yes`, `no`, `on`, `off`) or sexagesimal numbers (`12:30`).
- Keep skill bodies portable: describe procedures and concrete handoffs without hardcoding project-specific build commands, trackers, or environments.
- Keep all local links relative. Links inside `SKILL.md` may point to files within the same skill folder (e.g., `references/guide.md`, `scripts/helper`) or to another skill (e.g., `../github-comments/SKILL.md`), **provided that skill is declared in `requires`** (directly or transitively).

### Registering in `catalog.json`

Add an entry to [`catalog.json`](../catalog.json):

```json
{
  "my-skill": {
    "summary": "Short one-line description shown in mstack list",
    "requires": ["other-skill"],
    "replaces": ["old-skill-name"]
  }
}
```

- Keys must be lowercase (`summary`, `requires`, `replaces`).
- `summary` is required and non-empty.
- `requires` is optional; when present, it must be an array of valid skill names defined in `catalog.json` or `external-skills.json`.
- `replaces` is optional; when present, it must be an array of retired skill names that this skill replaces so `mstack update` migrates existing projects automatically. Replaced names must be valid skill names, cannot match an active skill, and can be claimed by only one skill.
- Do not put `source` in `catalog.json`—external skills belong in `external-skills.json`.

## External skills

External skills live in upstream Git repositories and are registered in [`external-skills.json`](../external-skills.json):

```json
{
  "impeccable": {
    "summary": "Critique, audit, and refine user interfaces with Impeccable",
    "required_by": ["implement-ui", "review-loop"],
    "requires": [],
    "source": {
      "repository": "https://github.com/pbakaus/impeccable.git",
      "tag_prefix": "skill-v",
      "path": ".agents/skills/impeccable",
      "include": ["LICENSE", "NOTICE.md"]
    }
  }
}
```

| Field | Rules |
| --- | --- |
| `summary` | Required non-empty description (displayed with `(external)` suffix in `mstack list`). |
| `required_by` | Optional array of catalog skills that automatically install this external skill as a dependency. |
| `requires` | Optional array of skills that this external skill depends on. |
| `source.repository` | Required HTTPS Git URL with no embedded credentials, query string, or fragment. |
| `source.tag_prefix` | Required tag prefix matching `^[a-zA-Z0-9][a-zA-Z0-9._-]*$`. `mstack` selects the highest stable `<tag_prefix>MAJOR.MINOR.PATCH` tag (prereleases are ignored). |
| `source.path` | Required clean relative path inside the upstream repo pointing to the skill folder (must contain a regular `SKILL.md`). |
| `source.include` | Optional array of root-level regular files (no `/` in paths) to copy into the installed skill folder alongside the skill directory (for example, `LICENSE` and `NOTICE.md`). |

An external skill cannot share a name with a bundled skill or have a folder under `skills/`.

## Retiring a skill

To retire a bundled or external skill:

1. Remove its entry from `catalog.json` (or `external-skills.json`) and remove its folder from `skills/` (if bundled).
2. Remove any `requires` or `required_by` references pointing to it, along with any cross-skill Markdown links.
3. Update `README.md` and `docs/workflows.md`.

Downstream projects that already installed the retired skill are handled gracefully: `mstack update` reports `Retired upstream` and skips it while continuing to update other skills, and users can clean it up offline with `mstack remove <skill>`.

## Validation & testing

Run the full validation suite before opening a pull request (this matches [`.github/workflows/ci.yml`](../.github/workflows/ci.yml)):

```sh
go vet ./...
go test -race -count=1 ./...
```

### What `go test` validates

[`cmd/mstack/catalog_test.go`](../cmd/mstack/catalog_test.go) automatically enforces:

1. `.agents/skills` is a symlink pointing to `../skills`.
2. `catalog.json` is valid UTF-8 JSON with required `summary` and lowercase keys, and both `catalog.json` and `external-skills.json` parse as valid JSON with valid skill names, summaries, source URLs/paths, and dependency references.
3. Every folder in `skills/` is registered in `catalog.json` and contains a regular `SKILL.md` (and no external skill has a bundled folder).
4. Every `SKILL.md` has valid single-document YAML frontmatter, matching `name`, valid `description` (1–1024 chars), and no unsupported YAML tags.
5. Every relative Markdown link and image in `SKILL.md` with a local path resolves to an existing destination inside `skills/` within the skill's own folder or its transitive `requires` closure.
6. Every Git-tracked file in `skills/` is a regular file (`100644` or `100755`), and every `#!` script under `skills/*/scripts/` is committed as executable (`100755`).

In addition, `main_test.go` and `external_test.go` exercise full install/update/remove lifecycles, dependency resolution, external release selection, local-edit protection, CRLF checkout handling, retired skill handling, and atomic rollback.

### Running the CLI locally

To test CLI changes directly from the repository without installing globally:

```sh
go run ./cmd/mstack --help
```

## Documentation checklist

When changing skills or CLI behavior, keep these files aligned:

- [`catalog.json`](../catalog.json) / [`external-skills.json`](../external-skills.json) — machine-readable registry
- [`README.md`](../README.md) — overview, quick start, catalog table, and CLI cheat sheet
- [`docs/workflows.md`](workflows.md) — skill behavior and workflow composition
- [`docs/cli.md`](cli.md) — CLI flags, lockfile schema, safety model, and troubleshooting
- [`AGENTS.md`](../AGENTS.md) — repository rules if conventions change

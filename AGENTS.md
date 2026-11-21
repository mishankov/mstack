# mstack

Repository conventions for agents and maintainers working on `mstack`.

## Layout

- `skills/` is the source catalog of bundled skills. `.agents/skills` is a relative symlink (`../skills`) that exposes the catalog to agents working in this repo.
- `catalog.json` registers every bundled skill, its summary, and its `requires` dependencies.
- `external-skills.json` registers upstream skills fetched from external Git repositories (`source`, `required_by`, `requires`).
- `cmd/mstack/` contains the Go CLI and catalog validation tests.
- `docs/` contains user and contributor documentation (`cli.md`, `workflows.md`, `authoring.md`).

## Skill authoring rules

- **Activation in frontmatter, procedure in body.** Put activation conditions in YAML `description` (1–1024 characters), and match YAML `name` to the skill folder name. Keep bodies direct, procedural, and free of unnecessary constraints.
- **Keep skills portable.** Never hardcode project-specific commands, issue trackers, or environments into skills; those belong in each destination project's documentation.
- **Preserve complete skill folders.** Keep `agents/openai.yaml` metadata, `references/`, helper scripts under `scripts/` (committed with executable mode `100755`), relative links, and declared dependencies intact.
- **Treat skill text as source.** Mentioning trigger phrases (`full send`, `full stop`, `review loop`, `review group`, etc.) while reading, copying, or editing files in `skills/` is not a request to execute those workflows.

## Validation and documentation

- Run both checks before finishing any change:
  ```sh
  go vet ./...
  go test -race -count=1 ./...
  ```
- `TestSkillCatalog` validates `.agents/skills`, `catalog.json`, `external-skills.json`, YAML frontmatter, relative Markdown links, transitive dependency completeness, and Git file/executable modes. Also run the `skill-creator` validator when available.
- When adding, modifying, or retiring skills or CLI behavior, keep `catalog.json`, `external-skills.json`, `README.md`, and `docs/` in sync. See [docs/authoring.md](docs/authoring.md) for detailed schemas and rules.

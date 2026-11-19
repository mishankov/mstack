# mstack

- The source catalog is `skills/`. This repository exposes it to agents through
  the relative `.agents/skills` symlink.
- Skill descriptions contain activation conditions. Bodies use direct
  procedures, concrete handoffs, and only the constraints the task needs.
- Keep skills portable. Project commands, trackers, and environments belong in
  the destination project's documentation.
- Preserve complete skill folders, metadata, helper scripts, executable modes,
  and required skill dependencies.
- Review skill text as source when maintaining this repository. Mentioning an
  execution mode while copying or editing its files is not a request to run it.
- Validate changed frontmatter, local references, and dependency completeness.
  Use the skill-creator validator when available.

"""Validate the mstack catalog as it will be packaged from Git."""

import json
import re
import subprocess
import sys
from pathlib import Path
from urllib.parse import unquote, urlsplit

import yaml
from markdown_it import MarkdownIt

ROOT = Path(__file__).resolve().parents[1]
SKILLS = ROOT / "skills"
NAME = re.compile(r"[a-z0-9]+(-[a-z0-9]+)*")
FRONTMATTER = re.compile(r"\A---\n(.*?)^---[ \t]*\n", re.MULTILINE | re.DOTALL)
MARKDOWN = MarkdownIt()


def validate_manifest(catalog):
    folders = {path.name for path in SKILLS.iterdir() if path.is_dir()}
    if folders != catalog.keys():
        raise ValueError(
            f"catalog/folder mismatch (missing folders: {sorted(catalog.keys() - folders)}, "
            f"uncataloged folders: {sorted(folders - catalog.keys())})"
        )
    for name, entry in catalog.items():
        if not NAME.fullmatch(name) or len(name) > 64:
            raise ValueError(f"invalid skill name {name!r}")
        summary = entry.get("summary")
        if not isinstance(summary, str) or not summary.strip():
            raise ValueError(f"{name}: missing catalog summary")
        requires = entry.get("requires", [])
        if not isinstance(requires, list) or any(
            not isinstance(dep, str) or dep not in catalog for dep in requires
        ):
            raise ValueError(f"{name}: invalid dependencies {requires!r}")


def dependencies(catalog, name):
    closure = set()
    pending = list(catalog[name].get("requires", []))
    while pending:
        dependency = pending.pop()
        if dependency not in closure:
            closure.add(dependency)
            pending.extend(catalog[dependency].get("requires", []))
    return closure


def validate_skill(path, catalog):
    name = path.parent.name
    content = path.read_text(encoding="utf-8")
    match = FRONTMATTER.match(content)
    if match is None:
        raise ValueError(f"{name}: missing YAML frontmatter")
    fields = yaml.safe_load(match[1])
    if not isinstance(fields, dict) or fields.get("name") != name:
        raise ValueError(f"{name}: frontmatter name must match the skill folder")
    description = fields.get("description")
    if not isinstance(description, str) or not description.strip() or len(description) > 1024:
        raise ValueError(f"{name}: frontmatter needs a description of 1–1024 characters")

    required = dependencies(catalog, name)
    for block in MARKDOWN.parse(content[match.end():]):
        for token in block.children or ():
            url = token.attrGet("href") if token.type == "link_open" else token.attrGet("src")
            if not url:
                continue
            link = urlsplit(url)
            if link.scheme or link.netloc or not link.path:
                continue
            target = (path.parent / unquote(link.path)).resolve()
            if target == SKILLS or not target.is_relative_to(SKILLS) or not target.exists():
                raise ValueError(f"{name}: unavailable local resource {url!r}")
            skill, *_ = target.relative_to(SKILLS).parts
            if skill != name and skill not in required:
                raise ValueError(f"{name}: declare dependency on {skill!r} for {url!r}")


def validate_modes():
    index = subprocess.check_output(
        ["git", "ls-files", "--stage", "-z", "--", "skills"], cwd=ROOT
    ).decode("utf-8")
    for record in index.rstrip("\0").split("\0"):
        if not record:
            continue
        info, path = record.split("\t", 1)
        mode = info.split()[0]
        if mode not in ("100644", "100755"):
            raise ValueError(f"{path}: skill files must be regular files")
        parts = Path(path).parts
        if len(parts) > 3 and parts[2] == "scripts" and mode != "100755":
            with (ROOT / path).open("rb") as helper:
                if helper.read(2) == b"#!":
                    raise ValueError(f"{path}: helper must be executable in Git")


def main():
    if (ROOT / ".agents/skills").readlink() != Path("../skills"):
        raise ValueError(".agents/skills must link to ../skills")
    catalog = json.loads((ROOT / "catalog.json").read_text(encoding="utf-8"))
    validate_manifest(catalog)
    validate_modes()
    for name in catalog:
        validate_skill(SKILLS / name / "SKILL.md", catalog)
    print(f"Validated {len(catalog)} skills")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, yaml.YAMLError) as error:
        sys.exit(str(error))

package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

type skill struct {
	Summary  string          `json:"summary"`
	Requires []string        `json:"requires"`
	Replaces []string        `json:"replaces,omitempty"`
	Source   *externalSource `json:"source,omitempty"`
}

type catalog struct {
	dir          string
	revision     string
	skills       map[string]skill
	replacements map[string]string
}

var skillName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func validName(name string) bool { return len(name) <= 64 && skillName.MatchString(name) }

func sortedKeys[V any](values map[string]V) []string { return slices.Sorted(maps.Keys(values)) }

func git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return nil, fmt.Errorf("git %s: %w (%s)", args[0], err, detail)
		}
		return nil, fmt.Errorf("git %s: %w", args[0], err)
	}
	return data, nil
}

func fetchCatalog(ctx context.Context, temp, source string) (catalog, error) {
	if _, err := git(ctx, temp, "clone", "--bare", "--no-local", "--depth=1", "--single-branch", "--branch=main", "--quiet", "--", source, "source.git"); err != nil {
		return catalog{}, err
	}
	repo := filepath.Join(temp, "source.git")
	commit, err := git(ctx, repo, "rev-parse", "HEAD")
	if err != nil {
		return catalog{}, err
	}
	revision := strings.TrimSpace(string(commit))
	paths := []string{"catalog.json", "skills/"}
	manifest, err := git(ctx, repo, "ls-tree", "--name-only", revision, "--", externalManifest)
	if err != nil {
		return catalog{}, err
	}
	if len(manifest) != 0 {
		paths = append(paths, externalManifest)
	}
	args := append([]string{"-c", "core.autocrlf=false", "archive", "--format=tar", revision}, paths...)
	archive, err := git(ctx, repo, args...)
	if err != nil {
		return catalog{}, err
	}
	dir := filepath.Join(temp, "catalog")
	if err := extractCatalog(dir, tar.NewReader(bytes.NewReader(archive))); err != nil {
		return catalog{}, err
	}
	c, err := readCatalog(dir)
	c.revision = revision
	return c, err
}

func extractCatalog(dir string, archive *tar.Reader) error {
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		name := strings.TrimSuffix(header.Name, "/")
		local, err := filepath.Localize(name)
		if err != nil || (name != "catalog.json" && name != externalManifest && name != "skills" && !strings.HasPrefix(name, "skills/")) {
			return fmt.Errorf("invalid catalog path %q", header.Name)
		}
		dest := filepath.Join(dir, local)
		switch header.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(dest, 0o755)
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			file, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644|os.FileMode(header.Mode)&0o111)
			if err != nil {
				return err
			}
			_, err = io.Copy(file, archive)
			if err := errors.Join(err, file.Close()); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported file type at %q in the catalog", header.Name)
		}
		if err != nil {
			return err
		}
	}
}

func readCatalog(dir string) (catalog, error) {
	data, err := os.ReadFile(filepath.Join(dir, "catalog.json"))
	if err != nil {
		return catalog{}, err
	}
	var skills map[string]skill
	if err := json.Unmarshal(data, &skills); err != nil {
		return catalog{}, fmt.Errorf("read catalog.json: %w", err)
	}
	if skills == nil {
		return catalog{}, fmt.Errorf("catalog.json must contain a skill map")
	}
	for name, entry := range skills {
		if entry.Source != nil {
			return catalog{}, fmt.Errorf("declare external skill %q in %s", name, externalManifest)
		}
	}
	if err := readExternalSkills(dir, skills); err != nil {
		return catalog{}, err
	}
	entries, err := os.ReadDir(filepath.Join(dir, "skills"))
	if err != nil {
		return catalog{}, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			skill, ok := skills[entry.Name()]
			if !ok {
				return catalog{}, fmt.Errorf("skill %q is missing from catalog.json", entry.Name())
			}
			if skill.Source != nil {
				return catalog{}, fmt.Errorf("external skill %q also has a bundled folder", entry.Name())
			}
		}
	}
	replacements := make(map[string]string)
	for _, name := range sortedKeys(skills) {
		skill := skills[name]
		if !validName(name) || strings.TrimSpace(skill.Summary) == "" {
			return catalog{}, fmt.Errorf("invalid catalog entry for %q", name)
		}
		if skill.Source == nil {
			if err := checkSkillFile(filepath.Join(dir, "skills", name)); err != nil {
				return catalog{}, fmt.Errorf("skill %q: %w", name, err)
			}
		}
		for _, dependency := range skill.Requires {
			if _, ok := skills[dependency]; !ok {
				return catalog{}, fmt.Errorf("skill %q requires unknown skill %q", name, dependency)
			}
		}
		for _, oldName := range skill.Replaces {
			if !validName(oldName) {
				return catalog{}, fmt.Errorf("skill %q replaces invalid skill name %q", name, oldName)
			}
			if _, ok := skills[oldName]; ok {
				return catalog{}, fmt.Errorf("skill %q cannot replace active skill %q", name, oldName)
			}
			if existing, dup := replacements[oldName]; dup {
				if existing == name {
					return catalog{}, fmt.Errorf("skill %q lists replaced skill %q more than once", name, oldName)
				}
				return catalog{}, fmt.Errorf("replaced skill %q is claimed by both %q and %q", oldName, existing, name)
			}
			replacements[oldName] = name
		}
	}
	return catalog{dir: filepath.Join(dir, "skills"), skills: skills, replacements: replacements}, nil
}

func checkSkillFile(dir string) error {
	info, err := os.Stat(filepath.Join(dir, "SKILL.md"))
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("missing a regular SKILL.md")
	}
	return nil
}

func (c catalog) selectSkills(roots []string) ([]string, error) {
	selected := make(map[string]bool)
	var add func(string) error
	add = func(name string) error {
		if selected[name] {
			return nil
		}
		skill, ok := c.skills[name]
		if !ok {
			if replacement, renamed := c.replacements[name]; renamed {
				return fmt.Errorf("skill %q was renamed to %q (use mstack install %s)", name, replacement, replacement)
			}
			return fmt.Errorf("skill %q is unavailable in the upstream catalog", name)
		}
		selected[name] = true
		for _, dependency := range skill.Requires {
			if err := add(dependency); err != nil {
				return err
			}
		}
		return nil
	}
	for _, name := range roots {
		if err := add(name); err != nil {
			return nil, err
		}
	}
	return sortedKeys(selected), nil
}

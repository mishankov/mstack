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
	Summary  string   `json:"summary"`
	Requires []string `json:"requires"`
}

type catalog struct {
	dir      string
	revision string
	skills   map[string]skill
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
	archive, err := git(ctx, repo, "-c", "core.autocrlf=false", "archive", "--format=tar", revision, "catalog.json", "skills/")
	if err != nil {
		return catalog{}, err
	}
	dir := filepath.Join(temp, "catalog")
	if err := extractCatalog(dir, tar.NewReader(bytes.NewReader(archive))); err != nil {
		return catalog{}, err
	}
	skills, err := readCatalog(dir)
	return catalog{dir: filepath.Join(dir, "skills"), revision: revision, skills: skills}, err
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
		if err != nil || (name != "catalog.json" && name != "skills" && !strings.HasPrefix(name, "skills/")) {
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

func readCatalog(dir string) (map[string]skill, error) {
	data, err := os.ReadFile(filepath.Join(dir, "catalog.json"))
	if err != nil {
		return nil, err
	}
	var skills map[string]skill
	if err := json.Unmarshal(data, &skills); err != nil {
		return nil, fmt.Errorf("read catalog.json: %w", err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "skills"))
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			if _, ok := skills[entry.Name()]; !ok {
				return nil, fmt.Errorf("skill %q is missing from catalog.json", entry.Name())
			}
		}
	}
	for _, name := range sortedKeys(skills) {
		skill := skills[name]
		if !validName(name) || strings.TrimSpace(skill.Summary) == "" {
			return nil, fmt.Errorf("invalid catalog entry for %q", name)
		}
		info, err := os.Stat(filepath.Join(dir, "skills", name, "SKILL.md"))
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("skill %q is missing a regular SKILL.md", name)
		}
		for _, dependency := range skill.Requires {
			if _, ok := skills[dependency]; !ok {
				return nil, fmt.Errorf("skill %q requires unknown skill %q", name, dependency)
			}
		}
	}
	return skills, nil
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

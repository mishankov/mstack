package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const externalManifest = "external-skills.json"

var (
	gitCommit     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	tagPrefix     = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)
	stableVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
)

type sourceLocation struct {
	Repository string   `json:"repository"`
	Path       string   `json:"path"`
	Include    []string `json:"include,omitempty"`
}

type externalSource struct {
	sourceLocation
	TagPrefix string `json:"tag_prefix"`
}

type externalRelease struct {
	tag    string
	commit string
}

func readExternalSkills(dir string, skills map[string]skill) error {
	data, err := os.ReadFile(filepath.Join(dir, externalManifest))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var external map[string]struct {
		skill
		RequiredBy []string `json:"required_by"`
	}
	if err := json.Unmarshal(data, &external); err != nil {
		return fmt.Errorf("read %s: %w", externalManifest, err)
	}
	if external == nil {
		return fmt.Errorf("%s must contain a skill map", externalManifest)
	}
	for _, name := range sortedKeys(external) {
		if _, exists := skills[name]; exists {
			return fmt.Errorf("external skill %q is already declared in catalog.json", name)
		}
		entry := external[name]
		if entry.Source == nil {
			return fmt.Errorf("external skill %q needs a source", name)
		}
		if err := entry.Source.validate(); err != nil {
			return fmt.Errorf("external skill %q: %w", name, err)
		}
		skills[name] = entry.skill
	}
	for _, name := range sortedKeys(external) {
		for _, dependent := range external[name].RequiredBy {
			entry, ok := skills[dependent]
			if !ok {
				return fmt.Errorf("external skill %q is required by unknown skill %q", name, dependent)
			}
			entry.Requires = append(entry.Requires, name)
			skills[dependent] = entry
		}
	}
	return nil
}

func (s externalSource) validate() error {
	repo, err := url.Parse(s.Repository)
	if err != nil || repo.Scheme != "https" || repo.Hostname() == "" || repo.User != nil || repo.RawQuery != "" || repo.Fragment != "" {
		return fmt.Errorf("source repository must be an HTTPS Git URL without credentials, query, or fragment")
	}
	if !tagPrefix.MatchString(s.TagPrefix) {
		return fmt.Errorf("invalid release tag prefix %q", s.TagPrefix)
	}
	if !validSourcePath(s.Path) {
		return fmt.Errorf("invalid source path %q", s.Path)
	}
	for _, file := range s.Include {
		if !validSourcePath(file) || strings.Contains(file, "/") {
			return fmt.Errorf("included file %q must be at the repository root", file)
		}
	}
	return nil
}

func validSourcePath(path string) bool {
	_, err := filepath.Localize(path)
	return err == nil && path != "." && !strings.ContainsAny(path, `\:`)
}

func (s externalSource) latest(ctx context.Context, dir string) (externalRelease, error) {
	refs, err := git(ctx, dir, "ls-remote", "--tags", "--sort=-version:refname", "--", s.Repository, "refs/tags/"+s.TagPrefix+"*")
	if err != nil {
		return externalRelease{}, err
	}
	var release externalRelease
	for _, line := range strings.Split(string(refs), "\n") {
		commit, ref, _ := strings.Cut(line, "\t")
		ref, peeled := strings.CutSuffix(ref, "^{}")
		version, ok := strings.CutPrefix(ref, "refs/tags/"+s.TagPrefix)
		if !ok || !stableVersion.MatchString(version) || !gitCommit.MatchString(commit) {
			continue
		}
		tag := s.TagPrefix + version
		if release.tag != "" && release.tag != tag {
			break
		}
		release = externalRelease{tag: tag, commit: commit}
		// Annotated tags also list the commit their tag object points to.
		if peeled {
			break
		}
	}
	if release.tag == "" {
		return externalRelease{}, fmt.Errorf("no stable releases found with tag prefix %q", s.TagPrefix)
	}
	return release, nil
}

func (s externalSource) matches(record installedSkill, release externalRelease) bool {
	return record.Source != nil && record.Release == release.tag && record.Commit == release.commit &&
		record.Source.Repository == s.Repository && record.Source.Path == s.Path && slices.Equal(record.Source.Include, s.Include)
}

func (c catalog) fetchExternal(ctx context.Context, name string, source externalSource, release externalRelease) error {
	repo, err := os.MkdirTemp("", "mstack-external-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(repo)
	if _, err := git(ctx, repo, "init", "--bare", "--quiet", "--template="); err != nil {
		return err
	}
	if _, err := git(ctx, repo, "-c", "fetch.fsckObjects=true", "-c", "fetch.fsck.hasDotgit=error",
		"fetch", "--quiet", "--depth=1", "--no-tags", "--", source.Repository, release.commit); err != nil {
		return err
	}

	prefix := "--prefix=skills/" + name + "/"
	args := []string{"-c", "core.autocrlf=false", "--literal-pathspecs", "archive", "--format=tar", prefix}
	archive, err := git(ctx, repo, append(args, release.commit+":"+source.Path)...)
	if err != nil {
		return err
	}
	if err := extractCatalog(filepath.Dir(c.dir), tar.NewReader(bytes.NewReader(archive))); err != nil {
		return err
	}
	dir := filepath.Join(c.dir, name)
	if err := checkSkillFile(dir); err != nil {
		return err
	}
	if len(source.Include) == 0 {
		return nil
	}
	archive, err = git(ctx, repo, append(append(args, release.commit, "--"), source.Include...)...)
	if err != nil {
		return err
	}
	if err := extractCatalog(filepath.Dir(c.dir), tar.NewReader(bytes.NewReader(archive))); err != nil {
		return err
	}
	for _, file := range source.Include {
		info, err := os.Stat(filepath.Join(dir, file))
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("included path %q must export a regular file", file)
		}
	}
	return nil
}

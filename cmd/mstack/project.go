package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"
)

type project struct {
	agents      string
	skills      string
	lock        string
	transaction string
}

func openProject(root string) project {
	agents := filepath.Join(root, ".agents")
	return project{
		agents:      agents,
		skills:      filepath.Join(agents, "skills"),
		lock:        filepath.Join(agents, "mstack.lock.json"),
		transaction: filepath.Join(agents, ".mstack-write"),
	}
}

type installedSkill struct {
	Commit string `json:"commit"`
	SHA256 string `json:"sha256"`
}

type lockFile struct {
	Version int                       `json:"version"`
	Skills  map[string]installedSkill `json:"skills"`
}

func (p project) readLock() (lockFile, error) {
	if _, err := directoryExists(p.agents); err != nil {
		return lockFile{}, err
	}
	if _, err := directoryExists(p.skills); err != nil {
		return lockFile{}, err
	}
	info, err := os.Lstat(p.lock)
	if errors.Is(err, fs.ErrNotExist) {
		return lockFile{Version: 1, Skills: make(map[string]installedSkill)}, nil
	}
	if err != nil {
		return lockFile{}, err
	}
	if !info.Mode().IsRegular() {
		return lockFile{}, fmt.Errorf("expected a regular lock file at %s", p.lock)
	}
	data, err := os.ReadFile(p.lock)
	if err != nil {
		return lockFile{}, err
	}
	var lock lockFile
	if err := json.Unmarshal(data, &lock); err != nil {
		return lockFile{}, fmt.Errorf("read %s: %w", p.lock, err)
	}
	if lock.Version != 1 || lock.Skills == nil {
		return lockFile{}, fmt.Errorf("unsupported lock file format in %s", p.lock)
	}
	for name, record := range lock.Skills {
		_, digestErr := hex.DecodeString(record.SHA256)
		if !validName(name) || record.Commit == "" || len(record.SHA256) != 64 || digestErr != nil {
			return lockFile{}, fmt.Errorf("invalid record for %q in %s", name, p.lock)
		}
	}
	return lock, nil
}

func directoryExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("expected a directory at %s", path)
	}
	return true, nil
}

func fingerprint(dir string) (string, error) {
	hash := sha256.New()
	err := fs.WalkDir(os.DirFS(dir), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			fmt.Fprintf(hash, "dir %q\n", path)
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%q is not a regular file", path)
		}
		fmt.Fprintf(hash, "file %q %t %d\n", path, info.Mode().Perm()&0o100 != 0, info.Size())
		file, err := os.Open(filepath.Join(dir, filepath.FromSlash(path)))
		if err != nil {
			return err
		}
		_, err = io.Copy(hash, file)
		return errors.Join(err, file.Close())
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (p project) validateLocal(name string, record installedSkill, managed bool) error {
	dest := filepath.Join(p.skills, name)
	info, err := os.Lstat(dest)
	if errors.Is(err, fs.ErrNotExist) {
		if managed {
			return fmt.Errorf("managed skill is missing at %s", dest)
		}
		return nil
	}
	if err != nil {
		return err
	}
	if !managed {
		return fmt.Errorf("unmanaged skill already exists at %s", dest)
	}
	if !info.IsDir() {
		return fmt.Errorf("local changes at %s (expected a skill directory)", dest)
	}
	digest, err := fingerprint(dest)
	if err != nil {
		return fmt.Errorf("inspect managed skill at %s: %w", dest, err)
	}
	if digest != record.SHA256 {
		return fmt.Errorf("local changes at %s", dest)
	}
	return nil
}

type changes struct {
	install []string
	update  []string
	current int
	records map[string]installedSkill
}

func (p project) plan(c catalog, names []string, lock lockFile) (changes, error) {
	plan := changes{records: make(map[string]installedSkill)}
	var conflicts []error
	for _, name := range names {
		local, managed := lock.Skills[name]
		localErr := p.validateLocal(name, local, managed)
		if localErr != nil {
			conflicts = append(conflicts, localErr)
		}
		digest, err := fingerprint(filepath.Join(c.dir, name))
		if err != nil {
			return changes{}, fmt.Errorf("read upstream skill %q: %w", name, err)
		}
		switch {
		case !managed:
			plan.install = append(plan.install, name)
		case digest != local.SHA256:
			plan.update = append(plan.update, name)
		default:
			if localErr == nil {
				plan.current++
			}
			continue
		}
		plan.records[name] = installedSkill{Commit: c.revision, SHA256: digest}
	}
	return plan, errors.Join(conflicts...)
}

func updateTargets(names []string, lock lockFile) ([]string, error) {
	if len(names) == 0 {
		return sortedKeys(lock.Skills), nil
	}
	for _, name := range names {
		if _, ok := lock.Skills[name]; !ok {
			return nil, fmt.Errorf("skill %q is not managed by mstack (use mstack install %s)", name, name)
		}
	}
	return names, nil
}

func (p project) execute(ctx context.Context, source string, r request, out io.Writer) error {
	lock, err := p.readLock()
	if err != nil {
		return err
	}
	names := r.names
	if r.command == "update" {
		names, err = updateTargets(names, lock)
		if err != nil {
			return err
		}
		if len(names) == 0 {
			fmt.Fprintln(out, "No mstack-managed skills installed.")
			return nil
		}
	}
	temp, err := os.MkdirTemp("", "mstack-catalog-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	c, err := fetchCatalog(ctx, temp, source)
	if err != nil {
		return fmt.Errorf("read upstream catalog: %w", err)
	}
	if r.command == "list" {
		return p.list(c, lock, out)
	}
	if r.all {
		names = sortedKeys(c.skills)
	}
	names, err = c.selectSkills(names)
	if err != nil {
		return err
	}
	plan, err := p.plan(c, names, lock)
	if r.check {
		plan.report(out, "Updates available", "Dependencies to install")
		return err
	}
	if err != nil {
		return err
	}
	if len(plan.records) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := p.apply(c, plan, lock); err != nil {
			return err
		}
	}
	plan.report(out, "Updated", "Installed")
	return nil
}

func (p project) list(c catalog, lock lockFile, out io.Writer) error {
	names := sortedKeys(c.skills)
	for name := range lock.Skills {
		if _, ok := c.skills[name]; !ok {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "SKILL\tSTATUS\tDESCRIPTION")
	for _, name := range names {
		_, managed := lock.Skills[name]
		_, err := os.Lstat(filepath.Join(p.skills, name))
		present := err == nil
		if !present && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		status := "available"
		switch {
		case managed && present:
			status = "installed"
		case managed:
			status = "missing"
		case present:
			status = "unmanaged"
		}
		summary := c.skills[name].Summary
		if summary == "" {
			summary = "Removed from the upstream catalog"
		}
		fmt.Fprintf(table, "%s\t%s\t%s\n", name, status, summary)
	}
	return table.Flush()
}

func (plan changes) report(out io.Writer, updateLabel, installLabel string) {
	if len(plan.install) != 0 {
		fmt.Fprintf(out, "%s (%d): %s\n", installLabel, len(plan.install), strings.Join(plan.install, ", "))
	}
	if len(plan.update) != 0 {
		fmt.Fprintf(out, "%s (%d): %s\n", updateLabel, len(plan.update), strings.Join(plan.update, ", "))
	}
	if plan.current != 0 {
		fmt.Fprintf(out, "Already current: %d\n", plan.current)
	}
}

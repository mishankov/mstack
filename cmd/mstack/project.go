package main

import (
	"bytes"
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

func fingerprint(dir string, checkoutBytes map[string][]byte) (string, error) {
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
		filename := filepath.Join(dir, filepath.FromSlash(path))
		executable := info.Mode().Perm()&0o100 != 0
		if expected, ok := checkoutBytes[path]; ok {
			data, err := os.ReadFile(filename)
			if err != nil {
				return err
			}
			if bytes.Equal(data, expected) {
				data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
			}
			fmt.Fprintf(hash, "file %q %t %d\n", path, executable, len(data))
			_, err = hash.Write(data)
			return err
		}
		fmt.Fprintf(hash, "file %q %t %d\n", path, executable, info.Size())
		file, err := os.Open(filename)
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

func checkoutCRLFFiles(ctx context.Context, dir string) (map[string][]byte, error) {
	index, err := git(ctx, dir, "ls-files", "--eol", "-z", "--", ".")
	if err != nil {
		return nil, err
	}
	files := make(map[string][]byte)
	for _, record := range bytes.Split(index, []byte{0}) {
		info, path, ok := strings.Cut(string(record), "\t")
		fields := strings.Fields(info)
		if ok && len(fields) >= 3 && fields[0] == "i/lf" && fields[1] == "w/crlf" && fields[2] != "attr/-text" {
			data, err := git(ctx, dir, "cat-file", "--filters", ":./"+path)
			if err != nil {
				return nil, err
			}
			files[path] = data
		}
	}
	return files, nil
}

var errMissingSkill = errors.New("managed skill is missing")

func (p project) validateLocal(ctx context.Context, name string, record installedSkill, managed bool) error {
	dest := filepath.Join(p.skills, name)
	info, err := os.Lstat(dest)
	if errors.Is(err, fs.ErrNotExist) {
		if managed {
			return fmt.Errorf("%w at %s", errMissingSkill, dest)
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
	digest, err := fingerprint(dest, nil)
	if err == nil && digest != record.SHA256 {
		// Keep the raw lockfile hash. Only normalize tracked CRLF files whose
		// bytes match what Git checks out under the active attributes and config.
		var files map[string][]byte
		files, err = checkoutCRLFFiles(ctx, dest)
		if err == nil && len(files) > 0 {
			digest, err = fingerprint(dest, files)
		}
	}
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
	remove  []string
	current int
	records map[string]installedSkill
}

func (p project) plan(ctx context.Context, c catalog, names []string, lock lockFile) (changes, error) {
	plan := changes{records: make(map[string]installedSkill)}
	var conflicts []error
	for _, name := range names {
		local, managed := lock.Skills[name]
		localErr := p.validateLocal(ctx, name, local, managed)
		if localErr != nil {
			conflicts = append(conflicts, localErr)
		}
		digest, err := fingerprint(filepath.Join(c.dir, name), nil)
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

func (p project) remove(ctx context.Context, names []string, lock lockFile, out io.Writer) error {
	names = slices.Compact(slices.Sorted(slices.Values(names)))
	for _, name := range names {
		if _, ok := lock.Skills[name]; !ok {
			return fmt.Errorf("skill %q is not managed by mstack", name)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.apply(ctx, catalog{}, changes{remove: names}, lock); err != nil {
		return err
	}
	fmt.Fprintf(out, "Removed (%d): %s\n", len(names), strings.Join(names, ", "))
	return nil
}

func (p project) execute(ctx context.Context, source string, r request, out io.Writer) error {
	lock, err := p.readLock()
	if err != nil {
		return err
	}
	if r.command == "remove" {
		return p.remove(ctx, r.names, lock, out)
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
	if r.command == "update" && len(r.names) == 0 {
		available := names[:0]
		var retired []string
		for _, name := range names {
			if _, ok := c.skills[name]; ok {
				available = append(available, name)
			} else {
				retired = append(retired, name)
			}
		}
		names = available
		if len(retired) > 0 {
			fmt.Fprintf(out, "Retired upstream (%d): %s\n", len(retired), strings.Join(retired, ", "))
		}
	}
	names, err = c.selectSkills(names)
	if err != nil {
		return err
	}
	plan, err := p.plan(ctx, c, names, lock)
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
		if err := p.apply(ctx, c, plan, lock); err != nil {
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

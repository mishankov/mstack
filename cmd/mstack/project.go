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
	Commit  string          `json:"commit"`
	SHA256  string          `json:"sha256"`
	Release string          `json:"release,omitempty"`
	Source  *sourceLocation `json:"source,omitempty"`
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

type skillRename struct {
	from string
	to   string
}

type changes struct {
	install []string
	update  []string
	remove  []string
	renames []skillRename
	current int
	records map[string]installedSkill
}

func (p project) plan(ctx context.Context, c catalog, names []string, lock lockFile, preview bool) (changes, error) {
	plan := changes{records: make(map[string]installedSkill)}
	var conflicts []error
	removable := p.removableRenames(c, names, lock)
	for _, oldName := range sortedKeys(removable) {
		plan.renames = append(plan.renames, skillRename{from: oldName, to: removable[oldName]})
		plan.remove = append(plan.remove, oldName)
		if err := p.validateLocal(ctx, oldName, lock.Skills[oldName], true); err != nil && !errors.Is(err, errMissingSkill) {
			conflicts = append(conflicts, err)
		}
	}
	for _, name := range names {
		local, managed := lock.Skills[name]
		localErr := p.validateLocal(ctx, name, local, managed)
		if localErr != nil {
			conflicts = append(conflicts, localErr)
		}
		source := c.skills[name].Source
		record := installedSkill{Commit: c.revision}
		if source != nil {
			release, err := source.latest(ctx, c.dir)
			if err != nil {
				return changes{}, fmt.Errorf("check releases for external skill %q: %w", name, err)
			}
			if source.matches(local, release) {
				if localErr == nil {
					plan.current++
				}
				continue
			}
			if !preview {
				if err := c.fetchExternal(ctx, name, *source, release); err != nil {
					return changes{}, fmt.Errorf("fetch external skill %q at %s: %w", name, release.tag, err)
				}
			}
			record.Commit = release.commit
			record.Release = release.tag
			record.Source = &source.sourceLocation
		}
		if source == nil || !preview {
			digest, err := fingerprint(filepath.Join(c.dir, name), nil)
			if err != nil {
				return changes{}, fmt.Errorf("read upstream skill %q: %w", name, err)
			}
			record.SHA256 = digest
		}
		switch {
		case !managed:
			plan.install = append(plan.install, name)
		// Record a changed external release or source even when the installed bytes match.
		case record.SHA256 != local.SHA256 || source != nil || local.Source != nil:
			plan.update = append(plan.update, name)
		default:
			if localErr == nil {
				plan.current++
			}
			continue
		}
		if !preview {
			plan.records[name] = record
		}
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
			if r.check {
				return p.checkAttributes(nil, out)
			}
			return p.apply(ctx, catalog{}, changes{}, lock)
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
		for _, name := range sortedKeys(c.skills) {
			if c.skills[name].Source == nil {
				names = append(names, name)
			}
		}
	}
	if r.command == "update" {
		if len(r.names) == 0 {
			available := names[:0]
			var retired []string
			for _, name := range names {
				if _, ok := c.skills[name]; ok {
					available = append(available, name)
				} else if target, renamed := c.replacements[name]; renamed {
					available = append(available, target)
				} else {
					retired = append(retired, name)
				}
			}
			names = available
			if len(retired) > 0 {
				fmt.Fprintf(out, "Retired upstream (%d): %s\n", len(retired), strings.Join(retired, ", "))
			}
		} else {
			for i, name := range names {
				if _, ok := c.skills[name]; !ok {
					if target, renamed := c.replacements[name]; renamed {
						names[i] = target
					}
				}
			}
		}
	}
	names, err = c.selectSkills(names)
	if err != nil {
		return err
	}
	plan, err := p.plan(ctx, c, names, lock, r.check)
	if r.check {
		plan.report(out, "Updates available", "Dependencies to install", "Renames available")
		return errors.Join(err, p.checkAttributes(nextManagedSkills(lock.Skills, plan), out))
	}
	if err != nil {
		return err
	}
	if err := p.apply(ctx, c, plan, lock); err != nil {
		return err
	}
	plan.report(out, "Updated", "Installed", "Renamed")
	return nil
}

func (p project) removableRenames(c catalog, selected []string, lock lockFile) map[string]string {
	removable := make(map[string]string)
	for oldName := range lock.Skills {
		if target, ok := c.replacements[oldName]; ok && slices.Contains(selected, target) {
			removable[oldName] = target
		}
	}
	for changed := true; changed; {
		changed = false
		for oldName, target := range removable {
			if p.hasRetainedDependent(c, oldName, target, selected, removable, lock) {
				delete(removable, oldName)
				changed = true
			}
		}
	}
	return removable
}

func (p project) hasRetainedDependent(c catalog, oldName, target string, selected []string, removable map[string]string, lock lockFile) bool {
	for other, record := range lock.Skills {
		if other == oldName || slices.Contains(selected, other) {
			continue
		}
		if _, removing := removable[other]; removing {
			continue
		}
		if skillReferences(filepath.Join(p.skills, other), oldName) {
			return true
		}
		active := other
		outdated := false
		if _, ok := c.skills[other]; !ok {
			if replacement, renamed := c.replacements[other]; renamed {
				active = replacement
			} else {
				active = ""
			}
			outdated = true
		}
		if active == "" {
			continue
		}
		if !outdated && c.skills[active].Source == nil {
			if digest, err := fingerprint(filepath.Join(c.dir, active), nil); err == nil && digest == record.SHA256 {
				continue
			}
		}
		if deps, err := c.selectSkills([]string{active}); err == nil && slices.Contains(deps, target) {
			return true
		}
	}
	return false
}

func skillReferences(dir, name string) bool {
	if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		return false
	}
	found := false
	err := fs.WalkDir(os.DirFS(dir), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
		if err != nil {
			return err
		}
		if referencesSkill(data, name) {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found || err != nil
}

func referencesSkill(data []byte, name string) bool {
	needle := []byte(name)
	for offset := 0; offset <= len(data)-len(needle); {
		idx := bytes.Index(data[offset:], needle)
		if idx < 0 {
			return false
		}
		pos := offset + idx
		end := pos + len(needle)
		leftOK := pos == 0 || !isSkillTokenByte(data[pos-1])
		rightOK := end == len(data) || !isSkillTokenByte(data[end])
		if leftOK && rightOK {
			return true
		}
		offset = pos + 1
	}
	return false
}

func isSkillTokenByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '-' || b == '_'
}

func nextManagedSkills(skills map[string]installedSkill, plan changes) []string {
	managed := make(map[string]struct{}, len(skills)+len(plan.install))
	for name := range skills {
		managed[name] = struct{}{}
	}
	for _, name := range plan.install {
		managed[name] = struct{}{}
	}
	for _, name := range plan.remove {
		delete(managed, name)
	}
	return sortedKeys(managed)
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
			if target, ok := c.replacements[name]; ok {
				summary = fmt.Sprintf("Renamed to %s upstream", target)
			} else {
				summary = "Removed from the upstream catalog"
			}
		}
		if c.skills[name].Source != nil {
			summary += " (external)"
		}
		fmt.Fprintf(table, "%s\t%s\t%s\n", name, status, summary)
	}
	return table.Flush()
}

func (plan changes) report(out io.Writer, updateLabel, installLabel, renameLabel string) {
	renamedTargets := make(map[string]bool, len(plan.renames))
	if len(plan.renames) != 0 {
		pairs := make([]string, len(plan.renames))
		for i, r := range plan.renames {
			pairs[i] = r.from + " -> " + r.to
			renamedTargets[r.to] = true
		}
		fmt.Fprintf(out, "%s (%d): %s\n", renameLabel, len(pairs), strings.Join(pairs, ", "))
	}
	var installed []string
	for _, name := range plan.install {
		if !renamedTargets[name] {
			installed = append(installed, name)
		}
	}
	if len(installed) != 0 {
		fmt.Fprintf(out, "%s (%d): %s\n", installLabel, len(installed), strings.Join(installed, ", "))
	}
	if len(plan.update) != 0 {
		fmt.Fprintf(out, "%s (%d): %s\n", updateLabel, len(plan.update), strings.Join(plan.update, ", "))
	}
	if plan.current != 0 {
		fmt.Fprintf(out, "Already current: %d\n", plan.current)
	}
}

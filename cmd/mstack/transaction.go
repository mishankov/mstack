package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
)

var errRecoveryRequired = errors.New("could not roll back every skill move")

func (p project) busy() error {
	return fmt.Errorf("another mstack write is in progress or was interrupted (see %s)", p.transaction)
}

func (p project) checkWriter() error {
	_, err := os.Lstat(p.transaction)
	switch {
	case err == nil:
		return p.busy()
	case errors.Is(err, fs.ErrNotExist):
		return nil
	default:
		return err
	}
}

func (p project) withWriter(execute func() error) (result error) {
	present, err := directoryExists(p.agents)
	if err != nil {
		return err
	}
	if !present {
		if err := os.Mkdir(p.agents, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		if _, err := directoryExists(p.agents); err != nil {
			return err
		}
	}
	if err := os.Mkdir(p.transaction, 0o700); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return p.busy()
		}
		return err
	}
	defer func() {
		if !errors.Is(result, errRecoveryRequired) {
			result = errors.Join(result, os.RemoveAll(p.transaction))
		}
	}()
	return execute()
}

func (p project) apply(c catalog, plan changes, lock lockFile) (result error) {
	staged := filepath.Join(p.transaction, "new")
	backup := filepath.Join(p.transaction, "old")
	for _, dir := range []string{staged, backup} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			return err
		}
	}
	names := slices.Concat(plan.install, plan.update)
	for _, name := range names {
		dest := filepath.Join(staged, name)
		if err := os.CopyFS(dest, os.DirFS(filepath.Join(c.dir, name))); err != nil {
			return fmt.Errorf("stage %s: %w", name, err)
		}
		digest, err := fingerprint(dest)
		if err != nil {
			return fmt.Errorf("verify staged skill %q: %w", name, err)
		}
		if digest != plan.records[name].SHA256 {
			return fmt.Errorf("copying skill %q changed its contents or executable permissions", name)
		}
	}
	nextLock := lock
	nextLock.Skills = maps.Clone(lock.Skills)
	for name, record := range plan.records {
		nextLock.Skills[name] = record
	}
	data, err := json.MarshalIndent(nextLock, "", "  ")
	if err != nil {
		return err
	}
	stagedLock := filepath.Join(p.transaction, "mstack.lock.json")
	if err := os.WriteFile(stagedLock, append(data, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.MkdirAll(p.skills, 0o755); err != nil {
		return err
	}

	type move struct{ from, to string }
	var moved []move
	rename := func(from, to string) error {
		if err := os.Rename(from, to); err != nil {
			return err
		}
		moved = append(moved, move{from, to})
		return nil
	}
	// Keep the previous lock and skill folders available until every move succeeds.
	defer func() {
		if result == nil {
			return
		}
		for _, m := range slices.Backward(moved) {
			if err := os.Rename(m.to, m.from); err != nil {
				result = errors.Join(result, fmt.Errorf("%w at %s: %w", errRecoveryRequired, p.transaction, err))
			}
		}
	}()
	for _, name := range plan.install {
		if err := p.validateLocal(name, installedSkill{}, false); err != nil {
			return err
		}
		if err := rename(filepath.Join(staged, name), filepath.Join(p.skills, name)); err != nil {
			return err
		}
	}
	for _, name := range plan.update {
		if err := p.validateLocal(name, lock.Skills[name], true); err != nil {
			return err
		}
		dest := filepath.Join(p.skills, name)
		if err := rename(dest, filepath.Join(backup, name)); err != nil {
			return err
		}
		if err := rename(filepath.Join(staged, name), dest); err != nil {
			return err
		}
	}
	return os.Rename(stagedLock, p.lock)
}

package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	attributesName  = ".gitattributes"
	attributesStart = "# BEGIN mstack managed skill attributes"
	attributesEnd   = "# END mstack managed skill attributes"
)

type attributesChange struct {
	info    fs.FileInfo
	current string
	updated string
}

func readAttributes(path string) (string, fs.FileInfo, error) {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", nil, nil
	case err != nil:
		return "", nil, err
	case !info.Mode().IsRegular():
		return "", nil, fmt.Errorf("expected a regular attributes file at %s", path)
	}
	content, err := os.ReadFile(path)
	return string(content), info, err
}

func planAttributes(path string, names []string) (attributesChange, error) {
	current, info, err := readAttributes(path)
	if err != nil {
		return attributesChange{}, err
	}
	updated, err := skillAttributes(current, names)
	if err != nil {
		return attributesChange{}, fmt.Errorf("read %s: %w", path, err)
	}
	return attributesChange{info: info, current: current, updated: updated}, nil
}

func (change attributesChange) pending() bool { return change.current != change.updated }

func (change attributesChange) validate(path string) error {
	current, info, err := readAttributes(path)
	if err != nil {
		return err
	}
	if current != change.current || (info == nil) != (change.info == nil) ||
		(info != nil && info.Mode() != change.info.Mode()) {
		return fmt.Errorf("local changes at %s during write", path)
	}
	return nil
}

func (change attributesChange) stage(path string) error {
	if err := os.WriteFile(path, []byte(change.updated), 0o644); err != nil {
		return err
	}
	if change.info != nil {
		return os.Chmod(path, change.info.Mode().Perm())
	}
	return nil
}

func (p project) checkAttributes(names []string, out io.Writer) error {
	attributes, err := planAttributes(filepath.Join(p.agents, attributesName), names)
	if err == nil && attributes.pending() {
		fmt.Fprintf(out, "Skill attributes need updating: .agents/%s\n", attributesName)
	}
	return err
}

func skillAttributes(content string, names []string) (string, error) {
	start, end := -1, -1
	offset := 0
	for _, line := range strings.SplitAfter(content, "\n") {
		switch strings.TrimRight(line, "\r\n") {
		case attributesStart:
			if start >= 0 {
				return "", errors.New("duplicate mstack attributes block")
			}
			start = offset
		case attributesEnd:
			if start < 0 || end >= 0 {
				return "", errors.New("unexpected end of mstack attributes block")
			}
			end = offset + len(line)
		}
		offset += len(line)
	}
	if start >= 0 && end < 0 {
		return "", errors.New("missing end of mstack attributes block")
	}
	if start < 0 {
		start, end = len(content), len(content)
	}
	before, after := content[:start], content[end:]
	if len(names) == 0 {
		return before + after, nil
	}

	newline := "\n"
	if firstLine, _, ok := strings.Cut(content, "\n"); ok && strings.HasSuffix(firstLine, "\r") {
		newline = "\r\n"
	}
	var updated strings.Builder
	updated.WriteString(before)
	if before != "" && !strings.HasSuffix(before, "\n") {
		updated.WriteString(newline)
	}
	updated.WriteString(attributesStart + newline)
	for _, name := range names {
		fmt.Fprintf(&updated, "skills/%s/** linguist-vendored%s", name, newline)
	}
	updated.WriteString(attributesEnd + newline)
	updated.WriteString(after)
	return updated.String(), nil
}

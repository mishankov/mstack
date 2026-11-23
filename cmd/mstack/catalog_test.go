package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"go.yaml.in/yaml/v3"
)

var frontmatter = regexp.MustCompile("(?ms)\\A---\\n(.*?)^---[ \\t]*\\n")
var yamlBase60Number = regexp.MustCompile("^[-+]?(?:[1-9][0-9_]*(?::[0-5]?[0-9])+|[0-9][0-9_]*(?::[0-5]?[0-9])+\\.[0-9_]*)$")

func TestSkillCatalog(t *testing.T) {
	root := filepath.Join("..", "..")
	t.Run("Local skills link", func(t *testing.T) {
		if err := validateLocalSkillLink(root); err != nil {
			t.Fatal(err)
		}
	})

	if err := validateCatalogJSON(root); err != nil {
		t.Fatal(err)
	}
	c, err := readCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := filepath.EvalSymlinks(c.dir)
	if err != nil {
		t.Fatal(err)
	}
	c.dir = dir
	t.Run("Implement UI dependencies", func(t *testing.T) {
		selected, err := c.selectSkills([]string{"implement-ui"})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(selected, "impeccable") {
			t.Fatal("implement-ui requires Impeccable")
		}
	})
	for _, name := range sortedKeys(c.skills) {
		if c.skills[name].Source != nil {
			continue
		}
		t.Run(name, func(t *testing.T) {
			if err := validateSkillDocument(c, name); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("Git modes", func(t *testing.T) {
		if err := validateSkillModes(root); err != nil {
			t.Fatal(err)
		}
	})
}

func TestSkillFrontmatter(t *testing.T) {
	c := catalog{dir: t.TempDir(), skills: map[string]skill{"example": {}}}
	doc := filepath.Join(c.dir, "example", "SKILL.md")
	for _, tc := range []struct {
		fields    string
		wantError string
	}{
		{fields: "description: 12:30", wantError: "needs a description"},
		{fields: "description: 12:30.5", wantError: "needs a description"},
		{fields: "description: off", wantError: "needs a description"},
		{fields: "description: !!binary ZGVtbw==", wantError: "needs a description"},
		{fields: "description: \"12:30\""},
		{fields: "description: Example\nmetadata: {count: !!int oops}", wantError: "invalid YAML frontmatter"},
		{fields: "description: Example\nmetadata: {count: !!int [2]}", wantError: "invalid YAML frontmatter"},
		{fields: "description: Example\nmetadata: {count: !innt 2}", wantError: "invalid YAML frontmatter"},
		{fields: "description: Example\nmetadata: !!pairs [2]", wantError: "invalid YAML frontmatter"},
		{fields: "description: Example\nmetadata: !!omap [{a: 1, b: 2}]", wantError: "invalid YAML frontmatter"},
		{fields: "description: Example\nmetadata: {count: !!int 2}"},
		{fields: "description: !!str off\nmetadata: !!map {items: !!seq [one]}"},
		{fields: "description: Example\nmetadata: {item: &item {a: 1}, ordered: !!omap [*item], pairs: !!pairs [{b: 2}]}"},
		{fields: "description: Example\n--- # second document\nname: wrong", wantError: "one YAML document"},
	} {
		t.Run(tc.fields, func(t *testing.T) {
			writeFile(t, doc, []byte("---\nname: example\n"+tc.fields+"\n---\n"))
			err := validateSkillDocument(c, "example")
			if tc.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("got %v, want %q", err, tc.wantError)
			}
		})
	}
}

func TestCatalogReplacesValidation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		manifest  string
		wantError string
	}{
		{
			name:      "invalid name",
			manifest:  `{"alpha":{"summary":"Alpha","replaces":["Bad_Name"]}}`,
			wantError: "replaces invalid skill name",
		},
		{
			name:      "active skill",
			manifest:  `{"alpha":{"summary":"Alpha","replaces":["beta"]},"beta":{"summary":"Beta"}}`,
			wantError: "cannot replace active skill",
		},
		{
			name:      "duplicate on same skill",
			manifest:  `{"alpha":{"summary":"Alpha","replaces":["old","old"]}}`,
			wantError: "more than once",
		},
		{
			name:      "claimed by two skills",
			manifest:  `{"alpha":{"summary":"Alpha","replaces":["old"]},"beta":{"summary":"Beta","replaces":["old"]}}`,
			wantError: "claimed by both",
		},
		{
			name:     "valid",
			manifest: `{"alpha":{"summary":"Alpha","replaces":["old-one","old-two"]}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "catalog.json"), []byte(tc.manifest))
			writeFile(t, filepath.Join(dir, "skills", "alpha", "SKILL.md"), []byte("---\nname: alpha\ndescription: Alpha.\n---\n"))
			if strings.Contains(tc.manifest, `"beta"`) {
				writeFile(t, filepath.Join(dir, "skills", "beta", "SKILL.md"), []byte("---\nname: beta\ndescription: Beta.\n---\n"))
			}
			_, err := readCatalog(dir)
			if tc.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("got %v, want %q", err, tc.wantError)
			}
		})
	}
}

func validateCatalogJSON(root string) error {
	data, err := os.ReadFile(filepath.Join(root, "catalog.json"))
	if err != nil {
		return err
	}
	if !utf8.Valid(data) {
		return fmt.Errorf("catalog.json must be UTF-8")
	}
	var entries map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("read catalog.json: %w", err)
	}
	for _, name := range sortedKeys(entries) {
		entry := entries[name]
		if _, ok := entry["summary"]; !ok {
			return fmt.Errorf("%q needs a summary in catalog.json", name)
		}
		if requires, ok := entry["requires"]; ok && !bytes.HasPrefix(bytes.TrimSpace(requires), []byte("[")) {
			return fmt.Errorf("%q: requires must be an array", name)
		}
		if replaces, ok := entry["replaces"]; ok && !bytes.HasPrefix(bytes.TrimSpace(replaces), []byte("[")) {
			return fmt.Errorf("%q: replaces must be an array", name)
		}
		for key := range entry {
			if key != "summary" && strings.EqualFold(key, "summary") ||
				key != "requires" && strings.EqualFold(key, "requires") ||
				key != "replaces" && strings.EqualFold(key, "replaces") {
				return fmt.Errorf("%q: use the lowercase spelling of %q in catalog.json", name, key)
			}
		}
	}
	return nil
}

func validateLocalSkillLink(root string) error {
	link := filepath.Join(root, ".agents", "skills")
	target, err := os.Readlink(link)
	if err != nil && runtime.GOOS == "windows" {
		// Git can check out a tracked symlink as a plain file on Windows.
		entry, gitErr := git(context.Background(), root, "ls-files", "--stage", "--", ".agents/skills")
		if gitErr == nil && bytes.HasPrefix(entry, []byte("120000 ")) {
			var content []byte
			content, err = os.ReadFile(link)
			target = string(content)
		}
	}
	if err != nil {
		return err
	}
	if filepath.ToSlash(target) != "../skills" {
		return fmt.Errorf("%s must link to ../skills, got %q", link, target)
	}
	return nil
}

func validateSkillDocument(c catalog, name string) error {
	doc := filepath.Join(c.dir, name, "SKILL.md")
	content, err := os.ReadFile(doc)
	if err != nil {
		return err
	}
	if !utf8.Valid(content) {
		return fmt.Errorf("SKILL.md must be UTF-8")
	}
	content = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	match := frontmatter.FindSubmatchIndex(content)
	if match == nil {
		return fmt.Errorf("missing YAML frontmatter")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(content[match[2]:match[3]]))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return fmt.Errorf("invalid YAML frontmatter: %w", err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return fmt.Errorf("invalid YAML frontmatter: %w", err)
		}
		return fmt.Errorf("frontmatter must contain one YAML document")
	}
	if err := validateYAMLTags(&document); err != nil {
		return fmt.Errorf("invalid YAML frontmatter: %w", err)
	}
	// Decode the values as well as the nodes to check the contents of typed values.
	var values map[string]any
	if err := document.Decode(&values); err != nil {
		return fmt.Errorf("invalid YAML frontmatter: %w", err)
	}
	var fields map[string]yaml.Node
	if err := document.Decode(&fields); err != nil {
		return fmt.Errorf("invalid YAML frontmatter: %w", err)
	}
	if frontmatterString(fields["name"]) != name {
		return fmt.Errorf("frontmatter name must match the skill folder")
	}
	description := frontmatterString(fields["description"])
	if strings.TrimSpace(description) == "" || utf8.RuneCountInString(description) > 1024 {
		return fmt.Errorf("frontmatter needs a description of 1–1024 characters")
	}

	required, err := c.selectSkills([]string{name})
	if err != nil {
		return err
	}
	body := goldmark.DefaultParser().Parse(text.NewReader(content[match[1]:]))
	return ast.Walk(body, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var destination []byte
		switch node := node.(type) {
		case *ast.Link:
			destination = node.Destination
		case *ast.Image:
			destination = node.Destination
		default:
			return ast.WalkContinue, nil
		}
		link := string(util.URLEscape(destination, true))
		return ast.WalkContinue, validateSkillLink(c.dir, name, link, required)
	})
}

func validateYAMLTags(node *yaml.Node) error {
	if node.Style&yaml.TaggedStyle != 0 {
		var kind yaml.Kind
		switch node.ShortTag() {
		case "!!null", "!!bool", "!!int", "!!float", "!!binary", "!!timestamp", "!!str", "!!merge":
			kind = yaml.ScalarNode
		case "!!seq":
			kind = yaml.SequenceNode
		case "!!omap", "!!pairs":
			kind = yaml.SequenceNode
			for _, item := range node.Content {
				if item.Kind == yaml.AliasNode {
					item = item.Alias
				}
				if item.Kind != yaml.MappingNode || len(item.Content) != 2 {
					return fmt.Errorf("tag %q requires one mapping entry per item on line %d", node.Tag, item.Line)
				}
			}
		case "!!map", "!!set":
			kind = yaml.MappingNode
		}
		if kind != node.Kind {
			return fmt.Errorf("unsupported tag %q for value on line %d", node.Tag, node.Line)
		}
	}
	for _, child := range node.Content {
		if err := validateYAMLTags(child); err != nil {
			return err
		}
	}
	return nil
}

func frontmatterString(field yaml.Node) string {
	if field.Kind == yaml.AliasNode {
		field = *field.Alias
	}
	if field.Kind != yaml.ScalarNode || field.ShortTag() != "!!str" {
		return ""
	}
	if field.Style == 0 {
		// Preserve PyYAML's handling of plain YAML 1.1 scalars.
		if yamlBase60Number.MatchString(field.Value) {
			return ""
		}
		switch field.Value {
		case "yes", "Yes", "YES", "no", "No", "NO", "on", "On", "ON", "off", "Off", "OFF":
			return ""
		}
	}
	return field.Value
}

func validateSkillLink(skillsDir, name, link string, required []string) error {
	ref, err := url.Parse(link)
	if err != nil {
		return fmt.Errorf("invalid link %q: %w", link, err)
	}
	if ref.IsAbs() || ref.Host != "" || ref.Path == "" {
		return nil
	}
	path := filepath.FromSlash(ref.Path)
	if filepath.IsAbs(path) {
		return fmt.Errorf("unavailable local resource %q", link)
	}
	target, err := filepath.EvalSymlinks(filepath.Join(skillsDir, name, path))
	if err != nil {
		return fmt.Errorf("unavailable local resource %q: %w", link, err)
	}
	relative, err := filepath.Rel(skillsDir, target)
	if err != nil || !filepath.IsLocal(relative) || relative == "." {
		return fmt.Errorf("unavailable local resource %q", link)
	}
	dependency, _, _ := strings.Cut(relative, string(filepath.Separator))
	if !slices.Contains(required, dependency) {
		return fmt.Errorf("declare dependency on %q for %q", dependency, link)
	}
	return nil
}

func validateSkillModes(root string) error {
	index, err := git(context.Background(), root, "ls-files", "--stage", "-z", "--", "skills")
	if err != nil {
		return err
	}
	for _, entry := range bytes.Split(bytes.TrimSuffix(index, []byte{0}), []byte{0}) {
		info, path, ok := strings.Cut(string(entry), "\t")
		if !ok {
			return fmt.Errorf("invalid skill entry in Git index: %q", entry)
		}
		mode, _, _ := strings.Cut(info, " ")
		if mode != "100644" && mode != "100755" {
			return fmt.Errorf("%s: skill files must be regular files", path)
		}
		parts := strings.SplitN(path, "/", 4)
		if len(parts) == 4 && parts[2] == "scripts" && mode != "100755" {
			content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
			if err != nil {
				return err
			}
			if bytes.HasPrefix(content, []byte("#!")) {
				return fmt.Errorf("%s: helper must be executable in Git", path)
			}
		}
	}
	return nil
}

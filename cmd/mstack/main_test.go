package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestProjectSkillLifecycle(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git is required for the CLI")
	}
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.autocrlf")
	t.Setenv("GIT_CONFIG_VALUE_0", "true")
	ctx := context.Background()
	root := filepath.Join("..", "..")
	source := t.TempDir()
	manifest := readFile(t, filepath.Join(root, "catalog.json"))
	writeFile(t, filepath.Join(source, "catalog.json"), manifest)
	if err := os.CopyFS(filepath.Join(source, "skills"), os.DirFS(filepath.Join(root, "skills"))); err != nil {
		t.Fatal(err)
	}
	initRepo(t, source)
	oldGuide := filepath.Join("babysit-pr", "extra guide.md")
	writeFile(t, filepath.Join(source, "skills", oldGuide), []byte("This guide will be removed.\n"))
	first := commitRepo(t, source)

	dest := t.TempDir()
	initRepo(t, dest)
	p := openProject(dest)
	subdir := filepath.Join(dest, "a nested directory")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	command := func(wantError string, args ...string) string {
		t.Helper()
		var out bytes.Buffer
		err := run(ctx, subdir, source, args, &out)
		if wantError == "" && err != nil || wantError != "" && (err == nil || !strings.Contains(err.Error(), wantError)) {
			t.Fatalf("mstack %v: got error %v, want %q\n%s", args, err, wantError, out.String())
		}
		return out.String()
	}

	if out := command("", "list"); !strings.Contains(out, "manage-mstack-skills") || !strings.Contains(out, "available") {
		t.Fatalf("list omitted available skills:\n%s", out)
	}
	command("", "update", "--check")
	command("", "update")
	command("not managed by mstack", "remove", "stage-pr")
	assertMissing(t, p.agents)

	collision := filepath.Join(p.skills, "babysit-pr", "SKILL.md")
	writeFile(t, collision, []byte("User's existing copy\n"))
	command("unmanaged skill already exists", "install", "stage-pr")
	if got := string(readFile(t, collision)); got != "User's existing copy\n" {
		t.Fatalf("overwrote unmanaged skill: %q", got)
	}
	assertMissing(t, filepath.Join(p.skills, "stage-pr"))
	assertMissing(t, p.lock)
	if err := os.RemoveAll(filepath.Dir(collision)); err != nil {
		t.Fatal(err)
	}

	command("", "install", "stage-pr")
	lock, err := p.readLock()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"babysit-pr", "file-pr", "github-comments", "implement", "review-loop", "ship-pr", "stage-pr", "start-thread"}
	if got := sortedKeys(lock.Skills); !slices.Equal(got, want) {
		t.Fatalf("installed %v, want %v", got, want)
	}
	if lock.Skills["stage-pr"].Commit != first {
		t.Fatalf("wrong source commit in lock: %+v", lock.Skills["stage-pr"])
	}
	const helper = "babysit-pr/scripts/pr-wait"
	installedHelper := filepath.Join(p.skills, filepath.FromSlash(helper))
	committedHelper, err := git(ctx, source, "show", first+":skills/"+helper)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(readFile(t, installedHelper), committedHelper) {
		t.Fatal("pr-wait differs from the committed bytes")
	}
	mode, err := os.Stat(installedHelper)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && mode.Mode().Perm()&0o100 == 0 {
		t.Fatal("pr-wait lost its executable permission")
	}
	readFile(t, filepath.Join(p.skills, "stage-pr", "agents", "openai.yaml"))
	command("", "install", "review-panel")

	attributes := filepath.Join(p.agents, attributesName)
	if err := os.Remove(attributes); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, p.lock)
	const attributesNotice = "Skill attributes need updating: .agents/.gitattributes"
	if out := command("", "update", "--check"); !strings.Contains(out, "Already current") || !strings.Contains(out, attributesNotice) {
		t.Fatalf("check did not report missing attributes for current skills:\n%s", out)
	}
	assertMissing(t, attributes)

	customAttributes := []byte("*.md text\r\n")
	staleAttributes := append(bytes.Clone(customAttributes), []byte(attributesStart+"\r\nskills/old/** linguist-vendored\r\n"+attributesEnd+"\r\n")...)
	writeFile(t, attributes, staleAttributes)
	if out := command("", "update", "--check", "implement"); !strings.Contains(out, attributesNotice) {
		t.Fatalf("scoped check did not report stale attributes:\n%s", out)
	}
	if !bytes.Equal(readFile(t, attributes), staleAttributes) || !bytes.Equal(readFile(t, p.lock), before) {
		t.Fatal("--check changed the attributes or lockfile")
	}
	command("", "update")
	if !bytes.Equal(readFile(t, p.lock), before) {
		t.Fatal("backfilling attributes changed the lockfile")
	}
	if out := command("", "update", "--check"); strings.Contains(out, attributesNotice) {
		t.Fatalf("check reported an update after attributes were fixed:\n%s", out)
	}
	managedAttributes := readFile(t, attributes)
	if !bytes.HasPrefix(managedAttributes, customAttributes) || !bytes.Contains(managedAttributes, []byte("skills/babysit-pr/** linguist-vendored\r\n")) || bytes.Contains(managedAttributes, []byte("skills/old/")) {
		t.Fatalf("expected generated rules after the custom CRLF line, got %q", managedAttributes)
	}
	const managedScript = ".agents/skills/babysit-pr/scripts/pr-wait"
	const unmanagedScript = ".agents/skills/local/scripts/main.js"
	const appScript = "web/main.js"
	got, err := git(ctx, dest, "check-attr", "linguist-vendored", "--", managedScript, unmanagedScript, appScript)
	if want := managedScript + ": linguist-vendored: set\n" + unmanagedScript + ": linguist-vendored: unspecified\n" + appScript + ": linguist-vendored: unspecified\n"; err != nil || string(got) != want {
		t.Fatalf("Git attributes = %q, want %q: %v", got, want, err)
	}

	var entries map[string]skill
	if err := json.Unmarshal(manifest, &entries); err != nil {
		t.Fatal(err)
	}
	entry := entries["ship-pr"]
	entry.Requires = append(entry.Requires, "capture-issue")
	entries["ship-pr"] = entry
	next, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(source, "catalog.json"), next)
	if err := os.Remove(filepath.Join(source, "skills", oldGuide)); err != nil {
		t.Fatal(err)
	}
	shipPR := filepath.Join("ship-pr", "SKILL.md")
	reviewPanel := filepath.Join("review-panel", "SKILL.md")
	original := readFile(t, filepath.Join(p.skills, shipPR))
	upstreamUpdate := append(slices.Clone(original), []byte("\nUpstream change.\n")...)
	writeFile(t, filepath.Join(source, "skills", shipPR), upstreamUpdate)
	writeFile(t, filepath.Join(source, "skills", reviewPanel), []byte("New review guidance.\n"))
	second := commitRepo(t, source)

	before = readFile(t, p.lock)
	if out := command("", "update", "stage-pr", "--check"); !strings.Contains(out, "babysit-pr, ship-pr") || !strings.Contains(out, "capture-issue") || strings.Contains(out, "review-panel") {
		t.Fatalf("unexpected update check:\n%s", out)
	}
	assertMissing(t, filepath.Join(p.skills, "capture-issue"))
	if !bytes.Equal(before, readFile(t, p.lock)) {
		t.Fatal("--check wrote the lock file")
	}
	localEdit := []byte("Keep my changes.\n")
	writeFile(t, filepath.Join(p.skills, shipPR), localEdit)
	if out := command("local changes", "update", "stage-pr", "--check"); !strings.Contains(out, "Updates available") {
		t.Fatalf("check omitted available updates:\n%s", out)
	}
	command("local changes", "update", "stage-pr")
	if !bytes.Equal(localEdit, readFile(t, filepath.Join(p.skills, shipPR))) || !bytes.Equal(before, readFile(t, p.lock)) {
		t.Fatal("conflicting update modified local changes or the lock file")
	}
	readFile(t, filepath.Join(p.skills, oldGuide))
	assertMissing(t, filepath.Join(p.skills, "capture-issue"))

	writeFile(t, filepath.Join(p.skills, shipPR), original)
	command("", "update", "stage-pr")
	assertMissing(t, filepath.Join(p.skills, oldGuide))
	if !bytes.Equal(upstreamUpdate, readFile(t, filepath.Join(p.skills, shipPR))) {
		t.Fatal("update did not install the new skill file")
	}
	lock, err = p.readLock()
	if err != nil {
		t.Fatal(err)
	}
	if lock.Skills["capture-issue"].Commit != second || lock.Skills["review-panel"].Commit != first {
		t.Fatalf("scoped update recorded the wrong commits: %+v", lock.Skills)
	}
	command("", "update")
	if got := readFile(t, filepath.Join(p.skills, reviewPanel)); string(got) != "New review guidance.\n" {
		t.Fatalf("update omitted a managed skill: %q", got)
	}
	command("", "install", "--all")
	lock, err = p.readLock()
	if err != nil || len(lock.Skills) != len(entries) {
		t.Fatalf("full install recorded %d skills, want %d: %v", len(lock.Skills), len(entries), err)
	}
	readFile(t, filepath.Join(p.skills, "manage-mstack-skills", "SKILL.md"))
	command("", append([]string{"remove"}, sortedKeys(lock.Skills)...)...)
	if got := readFile(t, attributes); !bytes.Equal(got, customAttributes) {
		t.Fatalf("removing all skills left %q, want %q", got, customAttributes)
	}
	assertMissing(t, p.transaction)
}

func TestRollbackWhenNewCollisionAppears(t *testing.T) {
	p := openProject(t.TempDir())
	lock, err := p.readLock()
	if err != nil {
		t.Fatal(err)
	}
	c := catalog{dir: t.TempDir(), revision: "test-revision"}
	for _, name := range []string{"first", "second"} {
		writeFile(t, filepath.Join(c.dir, name, "SKILL.md"), []byte(name))
	}
	ctx := context.Background()
	plan, err := p.plan(ctx, c, []string{"first", "second"}, lock, false)
	if err != nil {
		t.Fatal(err)
	}
	collision := filepath.Join(p.skills, "second", "SKILL.md")
	writeFile(t, collision, []byte("local"))
	err = p.withWriter(func() error {
		if err := p.withWriter(func() error {
			t.Fatal("concurrent write entered")
			return nil
		}); err == nil {
			t.Fatal("concurrent write was accepted")
		}
		return p.apply(ctx, c, plan, lock)
	})
	if err == nil || !strings.Contains(err.Error(), "unmanaged skill already exists") {
		t.Fatalf("transaction did not report the new collision: %v", err)
	}
	assertMissing(t, filepath.Join(p.skills, "first"))
	assertMissing(t, p.lock)
	assertMissing(t, filepath.Join(p.agents, attributesName))
	assertMissing(t, p.transaction)
	if got := string(readFile(t, collision)); got != "local" {
		t.Fatalf("overwrote the conflicting skill: %q", got)
	}
}

func TestRollbackAttributesWhenLockReplacementFails(t *testing.T) {
	p := openProject(t.TempDir())
	lock, err := p.readLock()
	if err != nil {
		t.Fatal(err)
	}
	c := catalog{dir: t.TempDir(), revision: "test-revision"}
	writeFile(t, filepath.Join(c.dir, "demo", "SKILL.md"), []byte("demo"))
	ctx := context.Background()
	plan, err := p.plan(ctx, c, []string{"demo"}, lock, false)
	if err != nil {
		t.Fatal(err)
	}
	attributes := filepath.Join(p.agents, attributesName)
	custom := []byte("*.md text\n")
	writeFile(t, attributes, custom)
	if err := os.Mkdir(p.lock, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := p.withWriter(func() error { return p.apply(ctx, c, plan, lock) }); err == nil {
		t.Fatal("expected replacing the lockfile to fail")
	}
	if got := readFile(t, attributes); !bytes.Equal(got, custom) {
		t.Fatalf("rollback left attributes %q, want %q", got, custom)
	}
	assertMissing(t, filepath.Join(p.skills, "demo"))
	assertMissing(t, p.transaction)
}

func TestAttributesDetectEditsAfterPlanning(t *testing.T) {
	path := filepath.Join(t.TempDir(), attributesName)
	writeFile(t, path, []byte("*.md text\n"))
	change, err := planAttributes(path, []string{"demo"})
	if err != nil {
		t.Fatal(err)
	}
	edited := []byte("*.md text\n*.txt -text\n")
	writeFile(t, path, edited)
	if err := change.validate(path); err == nil || !strings.Contains(err.Error(), "local changes") {
		t.Fatalf("expected an attributes conflict, got %v", err)
	}
}

func TestUpdateAfterCRLFCheckout(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	initRepo(t, source)
	writeFile(t, filepath.Join(source, "catalog.json"), []byte(`{"demo":{"summary":"Demo"}}`))
	writeFile(t, filepath.Join(source, "skills", "demo", "SKILL.md"), []byte("Original text.\n"))
	writeFile(t, filepath.Join(source, "skills", "demo", "data.bin"), []byte("byte-exact\n"))
	commitRepo(t, source)

	dest := t.TempDir()
	initRepo(t, dest)
	writeFile(t, filepath.Join(dest, ".gitattributes"), []byte("*.bin -text\n"))
	if err := run(ctx, dest, source, []string{"install", "demo"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	commitRepo(t, dest)
	clone := filepath.Join(t.TempDir(), "clone")
	if _, err := git(ctx, "", "clone", "--quiet", "--config", "core.autocrlf=true", dest, clone); err != nil {
		t.Fatal(err)
	}
	status, err := git(ctx, clone, "status", "--porcelain")
	if err != nil || len(status) != 0 {
		t.Fatalf("expected a clean checkout, got %q, %v", status, err)
	}
	dir := filepath.Join(openProject(clone).skills, "demo")
	doc := filepath.Join(dir, "SKILL.md")
	if got := readFile(t, doc); !bytes.Equal(got, []byte("Original text.\r\n")) {
		t.Fatalf("expected Git to check out CRLF, got %q", got)
	}
	writeFile(t, filepath.Join(source, "skills", "demo", "SKILL.md"), []byte("Upstream change.\n"))
	commitRepo(t, source)

	var out bytes.Buffer
	check := func(wantError string) {
		t.Helper()
		err := run(ctx, clone, source, []string{"update", "--check"}, &out)
		if wantError == "" && err != nil || wantError != "" && (err == nil || !strings.Contains(err.Error(), wantError)) {
			t.Fatalf("got error %v, want %q", err, wantError)
		}
	}
	check("")
	writeFile(t, doc, []byte("Local edit.\r\n"))
	check("local changes")
	writeFile(t, doc, []byte("Original text.\r\n"))
	binary := filepath.Join(dir, "data.bin")
	writeFile(t, binary, []byte("byte-exact\r\n"))
	check("local changes")
	writeFile(t, binary, []byte("byte-exact\n"))
	if _, err := git(ctx, clone, "config", "core.autocrlf", "false"); err != nil {
		t.Fatal(err)
	}
	check("local changes")
	if err := run(ctx, clone, source, []string{"remove", "demo"}, &out); err == nil || !strings.Contains(err.Error(), "local changes") {
		t.Fatalf("expected remove to preserve the CRLF edit, got %v", err)
	}
	if _, err := git(ctx, clone, "config", "core.autocrlf", "true"); err != nil {
		t.Fatal(err)
	}
	if err := run(ctx, clone, source, []string{"update"}, &out); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, doc); !bytes.Equal(got, []byte("Upstream change.\n")) {
		t.Fatalf("expected the new skill, got %q", got)
	}
}

func TestRetiredSkillLifecycle(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	initRepo(t, source)
	manifest := filepath.Join(source, "catalog.json")
	writeFile(t, manifest, []byte(`{"current":{"summary":"Current"},"retired":{"summary":"Retired"}}`))
	current := filepath.Join(source, "skills", "current", "SKILL.md")
	retired := filepath.Join(source, "skills", "retired", "SKILL.md")
	writeFile(t, current, []byte("Original.\n"))
	writeFile(t, retired, []byte("Retiring.\n"))
	commitRepo(t, source)

	dest := t.TempDir()
	initRepo(t, dest)
	command := func(upstream, wantError string, args ...string) string {
		t.Helper()
		var out bytes.Buffer
		err := run(ctx, dest, upstream, args, &out)
		if wantError == "" && err != nil || wantError != "" && (err == nil || !strings.Contains(err.Error(), wantError)) {
			t.Fatalf("mstack %v: got error %v, want %q\n%s", args, err, wantError, out.String())
		}
		return out.String()
	}
	command(source, "", "install", "--all")
	writeFile(t, manifest, []byte(`{"current":{"summary":"Current"}}`))
	writeFile(t, current, []byte("Updated.\n"))
	if err := os.Remove(retired); err != nil {
		t.Fatal(err)
	}
	commitRepo(t, source)

	p := openProject(dest)
	before := readFile(t, p.lock)
	for _, args := range [][]string{{"update", "--check"}, {"update"}} {
		if out := command(source, "", args...); !strings.Contains(out, "Retired upstream (1): retired") ||
			!strings.Contains(out, "(1): current") {
			t.Fatalf("unexpected mstack %v output: %s", args, out)
		}
		if len(args) > 1 && !bytes.Equal(readFile(t, p.lock), before) {
			t.Fatal("--check modified the lockfile")
		}
	}
	installedCurrent := filepath.Join(p.skills, "current", "SKILL.md")
	installedRetired := filepath.Join(p.skills, "retired", "SKILL.md")
	if got := string(readFile(t, installedCurrent)); got != "Updated.\n" {
		t.Fatalf("the retired skill blocked an independent update: %q", got)
	}
	command(source, "unavailable in the upstream catalog", "update", "retired")
	offline := filepath.Join(t.TempDir(), "no-upstream")
	command(offline, "not managed by mstack", "remove", "unknown")
	writeFile(t, installedRetired, []byte("Local edit.\n"))
	before = readFile(t, p.lock)
	command(offline, "local changes", "remove", "current", "retired")
	if !bytes.Equal(readFile(t, p.lock), before) || string(readFile(t, installedCurrent)) != "Updated.\n" ||
		string(readFile(t, installedRetired)) != "Local edit.\n" {
		t.Fatal("failed removal changed installed files or the lock")
	}
	writeFile(t, installedRetired, []byte("Retiring.\n"))
	command(offline, "", "remove", "retired", "retired")
	assertMissing(t, filepath.Dir(installedRetired))
	if err := os.RemoveAll(filepath.Dir(installedCurrent)); err != nil {
		t.Fatal(err)
	}
	command(offline, "", "remove", "current")
	lock, err := p.readLock()
	if err != nil || len(lock.Skills) != 0 {
		t.Fatalf("expected empty lock after removal, got %+v, %v", lock, err)
	}
	attributes := filepath.Join(p.agents, attributesName)
	assertMissing(t, attributes)
	const custom = "*.md text\n"
	// An older mstack can remove the last skill without removing its rule.
	stale := custom + attributesStart + "\nskills/retired/** linguist-vendored\n" + attributesEnd + "\n"
	writeFile(t, attributes, []byte(stale))
	if out := command(offline, "", "update", "--check"); !strings.Contains(out, "Skill attributes need updating: .agents/.gitattributes") {
		t.Fatalf("check did not report leftover attributes with an empty lockfile:\n%s", out)
	}
	if got := string(readFile(t, attributes)); got != stale {
		t.Fatalf("read-only check changed the attributes: %q", got)
	}
	command(offline, "", "update")
	if got := string(readFile(t, attributes)); got != custom {
		t.Fatalf("update left %q, want %q", got, custom)
	}
	assertMissing(t, p.transaction)
}

func TestRenamedSkillLifecycle(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	initRepo(t, source)
	manifest := filepath.Join(source, "catalog.json")
	writeFile(t, manifest, []byte(`{
		"old-dep": {"summary": "Old dep"},
		"old-main": {"summary": "Old main", "requires": ["old-dep"]},
		"old-dropped": {"summary": "Old dropped", "requires": ["old-dep"]},
		"old-side": {"summary": "Old side"},
		"retired": {"summary": "Retired"}
	}`))
	writeFile(t, filepath.Join(source, "skills", "old-dep", "SKILL.md"), []byte("Old dep.\n"))
	writeFile(t, filepath.Join(source, "skills", "old-main", "SKILL.md"), []byte("Old main.\n"))
	writeFile(t, filepath.Join(source, "skills", "old-dropped", "SKILL.md"), []byte("See [old-dep](../old-dep/SKILL.md).\n"))
	writeFile(t, filepath.Join(source, "skills", "old-side", "SKILL.md"), []byte("Old side.\n"))
	writeFile(t, filepath.Join(source, "skills", "retired", "SKILL.md"), []byte("Retired.\n"))
	commitRepo(t, source)

	dest := t.TempDir()
	initRepo(t, dest)
	command := func(wantError string, args ...string) string {
		t.Helper()
		var out bytes.Buffer
		err := run(ctx, dest, source, args, &out)
		if wantError == "" && err != nil || wantError != "" && (err == nil || !strings.Contains(err.Error(), wantError)) {
			t.Fatalf("mstack %v: got error %v, want %q\n%s", args, err, wantError, out.String())
		}
		return out.String()
	}
	command("", "install", "--all")

	// Upstream renames old-dep -> new-dep, old-main -> new-main, old-dropped -> new-dropped (dropping new-dep),
	// old-side -> new-side, and retires retired.
	writeFile(t, manifest, []byte(`{
		"new-dep": {"summary": "New dep", "replaces": ["old-dep"]},
		"new-main": {"summary": "New main", "requires": ["new-dep"], "replaces": ["old-main"]},
		"new-dropped": {"summary": "New dropped", "replaces": ["old-dropped"]},
		"new-side": {"summary": "New side", "replaces": ["old-side"]}
	}`))
	for _, old := range []string{"old-dep", "old-main", "old-dropped", "old-side", "retired"} {
		if err := os.RemoveAll(filepath.Join(source, "skills", old)); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(source, "skills", "new-dep", "SKILL.md"), []byte("New dep.\n"))
	writeFile(t, filepath.Join(source, "skills", "new-main", "SKILL.md"), []byte("New main.\n"))
	writeFile(t, filepath.Join(source, "skills", "new-dropped", "SKILL.md"), []byte("New dropped.\n"))
	writeFile(t, filepath.Join(source, "skills", "new-side", "SKILL.md"), []byte("New side.\n"))
	commitRepo(t, source)

	p := openProject(dest)
	if out := command("", "list"); !strings.Contains(out, "Renamed to new-main upstream") || !strings.Contains(out, "Removed from the upstream catalog") {
		t.Fatalf("list did not distinguish renamed and retired skills:\n%s", out)
	}
	command("renamed to \"new-main\"", "install", "old-main")

	// Scoped update check previews only old-main (preserving old-dep while old-dropped still links to it).
	before := readFile(t, p.lock)
	if out := command("", "update", "--check", "old-main"); !strings.Contains(out, "Renames available (1): old-main -> new-main") ||
		!strings.Contains(out, "Dependencies to install (1): new-dep") ||
		strings.Contains(out, "old-side") || !strings.Contains(out, "Skill attributes need updating: .agents/.gitattributes") {
		t.Fatalf("unexpected scoped rename check:\n%s", out)
	}
	if !bytes.Equal(before, readFile(t, p.lock)) {
		t.Fatal("--check modified the lockfile")
	}

	// Local edit on the renamed skill blocks migration without modifying anything.
	oldMainDoc := filepath.Join(p.skills, "old-main", "SKILL.md")
	writeFile(t, oldMainDoc, []byte("Local edit.\n"))
	command("local changes", "update", "old-main")
	assertMissing(t, filepath.Join(p.skills, "new-main"))
	if !bytes.Equal(before, readFile(t, p.lock)) {
		t.Fatal("conflicting rename modified the lockfile")
	}
	writeFile(t, oldMainDoc, []byte("Old main.\n"))

	// Unmanaged folder at the destination blocks migration without touching either folder.
	unmanaged := filepath.Join(p.skills, "new-main", "SKILL.md")
	writeFile(t, unmanaged, []byte("Custom skill.\n"))
	command("unmanaged skill already exists", "update", "old-main")
	if got := string(readFile(t, unmanaged)); got != "Custom skill.\n" {
		t.Fatalf("overwrote unmanaged destination: %q", got)
	}
	if got := string(readFile(t, oldMainDoc)); got != "Old main.\n" {
		t.Fatalf("removed old skill after destination collision: %q", got)
	}
	if err := os.RemoveAll(filepath.Dir(unmanaged)); err != nil {
		t.Fatal(err)
	}

	// Scoped update of a renamed dependency preserves old-dep while old-main and old-dropped still depend on it.
	if out := command("", "update", "old-dep"); !strings.Contains(out, "Installed (1): new-dep") || strings.Contains(out, "Renamed") {
		t.Fatalf("scoped dependency update should preserve old-dep while callers are installed:\n%s", out)
	}
	if got := string(readFile(t, filepath.Join(p.skills, "old-dep", "SKILL.md"))); got != "Old dep.\n" {
		t.Fatalf("old-dep was removed while callers still depended on it: %q", got)
	}
	if got := string(readFile(t, filepath.Join(p.skills, "new-dep", "SKILL.md"))); got != "New dep.\n" {
		t.Fatalf("new-dep content = %q", got)
	}

	// Scoped update of old-main migrates old-main, but still preserves old-dep because installed old-dropped
	// links to ../old-dep/SKILL.md even though new-dropped dropped that dependency.
	if out := command("", "update", "old-main"); !strings.Contains(out, "Renamed (1): old-main -> new-main") {
		t.Fatalf("unexpected scoped rename output:\n%s", out)
	}
	assertMissing(t, filepath.Join(p.skills, "old-main"))
	if got := string(readFile(t, filepath.Join(p.skills, "old-dep", "SKILL.md"))); got != "Old dep.\n" {
		t.Fatalf("old-dep was removed while old-dropped still linked to it: %q", got)
	}
	if got := string(readFile(t, filepath.Join(p.skills, "new-main", "SKILL.md"))); got != "New main.\n" {
		t.Fatalf("new-main content = %q", got)
	}
	if got := string(readFile(t, filepath.Join(p.skills, "old-side", "SKILL.md"))); got != "Old side.\n" {
		t.Fatalf("unrelated old-side changed: %q", got)
	}

	// Simulate an already-missing old folder before bulk update, which now finishes migrating old-dropped,
	// old-side, and the previously preserved old-dep.
	if err := os.RemoveAll(filepath.Join(p.skills, "old-side")); err != nil {
		t.Fatal(err)
	}
	if out := command("", "update"); !strings.Contains(out, "Retired upstream (1): retired") ||
		!strings.Contains(out, "Renamed (3): old-dep -> new-dep, old-dropped -> new-dropped, old-side -> new-side") {
		t.Fatalf("unexpected bulk rename output:\n%s", out)
	}
	assertMissing(t, filepath.Join(p.skills, "old-dep"))
	assertMissing(t, filepath.Join(p.skills, "old-dropped"))
	assertMissing(t, filepath.Join(p.skills, "old-side"))
	if got := string(readFile(t, filepath.Join(p.skills, "new-side", "SKILL.md"))); got != "New side.\n" {
		t.Fatalf("new-side content = %q", got)
	}

	lock, err := p.readLock()
	if err != nil {
		t.Fatal(err)
	}
	wantSkills := []string{"new-dep", "new-dropped", "new-main", "new-side", "retired"}
	if got := sortedKeys(lock.Skills); !slices.Equal(got, wantSkills) {
		t.Fatalf("lock skills = %v, want %v", got, wantSkills)
	}
	attrs := string(readFile(t, filepath.Join(p.agents, attributesName)))
	for _, old := range []string{"old-dep", "old-dropped", "old-main", "old-side"} {
		if strings.Contains(attrs, "skills/"+old+"/") {
			t.Fatalf("attributes retained renamed skill %q:\n%s", old, attrs)
		}
	}
	for _, name := range wantSkills {
		if !strings.Contains(attrs, "skills/"+name+"/** linguist-vendored") {
			t.Fatalf("attributes missing %q:\n%s", name, attrs)
		}
	}

	// Subsequent check reports everything current with no pending renames or attribute changes.
	if out := command("", "update", "--check"); strings.Contains(out, "Rename") || strings.Contains(out, "Skill attributes") || !strings.Contains(out, "Already current: 4") {
		t.Fatalf("unexpected follow-up check:\n%s", out)
	}

	// Partial migration: if an older CLI installed new-side while leaving old-side in the lockfile,
	// mstack update cleans up the leftover old-side entry without disturbing new-side.
	lock.Skills["old-side"] = lock.Skills["new-side"]
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, p.lock, append(data, '\n'))
	if out := command("", "update"); !strings.Contains(out, "Renamed (1): old-side -> new-side") {
		t.Fatalf("partial migration did not clean up old-side:\n%s", out)
	}
	lock, err = p.readLock()
	if err != nil {
		t.Fatal(err)
	}
	if got := sortedKeys(lock.Skills); !slices.Equal(got, wantSkills) {
		t.Fatalf("partial migration left %v, want %v", got, wantSkills)
	}
	assertMissing(t, p.transaction)
}

func TestRenamedSkillRetentionPropagationAndLocalEdits(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	initRepo(t, source)
	manifest := filepath.Join(source, "catalog.json")
	writeFile(t, manifest, []byte(`{
		"old-dep": {"summary": "Old dep"},
		"old-main": {"summary": "Old main", "requires": ["old-dep"]},
		"active": {"summary": "Active"},
		"retired": {"summary": "Retired", "requires": ["old-main"]}
	}`))
	writeFile(t, filepath.Join(source, "skills", "old-dep", "SKILL.md"), []byte("Old dep.\n"))
	writeFile(t, filepath.Join(source, "skills", "old-main", "SKILL.md"), []byte("Use [old-dep](../old-dep/SKILL.md).\n"))
	writeFile(t, filepath.Join(source, "skills", "active", "SKILL.md"), []byte("Active.\n"))
	writeFile(t, filepath.Join(source, "skills", "retired", "SKILL.md"), []byte("Use [old-main](../old-main/SKILL.md).\n"))
	commitRepo(t, source)

	dest := t.TempDir()
	initRepo(t, dest)
	command := func(wantError string, args ...string) string {
		t.Helper()
		var out bytes.Buffer
		err := run(ctx, dest, source, args, &out)
		if wantError == "" && err != nil || wantError != "" && (err == nil || !strings.Contains(err.Error(), wantError)) {
			t.Fatalf("mstack %v: got error %v, want %q\n%s", args, err, wantError, out.String())
		}
		return out.String()
	}
	command("", "install", "--all")

	writeFile(t, manifest, []byte(`{
		"new-dep": {"summary": "New dep", "replaces": ["old-dep"]},
		"new-main": {"summary": "New main", "requires": ["new-dep"], "replaces": ["old-main"]},
		"active": {"summary": "Active"}
	}`))
	for _, old := range []string{"old-dep", "old-main", "retired"} {
		if err := os.RemoveAll(filepath.Join(source, "skills", old)); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(source, "skills", "new-dep", "SKILL.md"), []byte("New dep.\n"))
	writeFile(t, filepath.Join(source, "skills", "new-main", "SKILL.md"), []byte("Use [new-dep](../new-dep/SKILL.md).\n"))
	commitRepo(t, source)

	p := openProject(dest)

	// Three-skill transitive retention: retired -> old-main -> old-dep.
	// Updating old-main installs new-main and new-dep, but preserves both old-main and old-dep.
	if out := command("", "update", "old-main"); !strings.Contains(out, "Installed (2): new-dep, new-main") || strings.Contains(out, "Renamed") {
		t.Fatalf("expected transitive retention of old-main and old-dep:\n%s", out)
	}
	if got := string(readFile(t, filepath.Join(p.skills, "old-main", "SKILL.md"))); !strings.Contains(got, "../old-dep/SKILL.md") {
		t.Fatalf("old-main was not preserved: %q", got)
	}
	if got := string(readFile(t, filepath.Join(p.skills, "old-dep", "SKILL.md"))); got != "Old dep.\n" {
		t.Fatalf("old-dep was removed while retained old-main still linked to it: %q", got)
	}

	// Remove retired, and add a local link from the unselected current active skill to ../old-dep/SKILL.md.
	command("", "remove", "retired")
	activeDoc := filepath.Join(p.skills, "active", "SKILL.md")
	writeFile(t, activeDoc, []byte("Active now links to [old-dep](../old-dep/SKILL.md).\n"))
	if out := command("", "update", "old-main"); !strings.Contains(out, "Renamed (1): old-main -> new-main") || strings.Contains(out, "old-dep ->") {
		t.Fatalf("expected old-main to migrate while old-dep stays preserved for locally edited active skill:\n%s", out)
	}
	assertMissing(t, filepath.Join(p.skills, "old-main"))
	if got := string(readFile(t, filepath.Join(p.skills, "old-dep", "SKILL.md"))); got != "Old dep.\n" {
		t.Fatalf("old-dep was removed while locally edited active skill still linked to it: %q", got)
	}

	// Restore active skill; bulk update now finishes migrating old-dep.
	writeFile(t, activeDoc, []byte("Active.\n"))
	if out := command("", "update"); !strings.Contains(out, "Renamed (1): old-dep -> new-dep") {
		t.Fatalf("expected old-dep to finish migrating after active edit restored:\n%s", out)
	}
	assertMissing(t, filepath.Join(p.skills, "old-dep"))
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	if _, err := git(context.Background(), dir, "init", "--initial-branch=main", "--quiet"); err != nil {
		t.Fatal(err)
	}
}

func commitRepo(t *testing.T, dir string) string {
	t.Helper()
	ctx := context.Background()
	if _, err := git(ctx, dir, "add", "--all"); err != nil {
		t.Fatal(err)
	}
	if _, err := git(ctx, dir, "-c", "user.name=mstack test", "-c", "user.email=mstack@example.invalid", "-c", "commit.gpgSign=false", "commit", "--quiet", "-m", "catalog fixture"); err != nil {
		t.Fatal(err)
	}
	commit, err := git(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(commit))
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be absent, got %v", path, err)
	}
}

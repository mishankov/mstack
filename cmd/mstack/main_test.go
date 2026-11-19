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
	command("not managed by mstack", "remove", "full-stop")
	assertMissing(t, p.agents)

	collision := filepath.Join(p.skills, "babysit-pr", "SKILL.md")
	writeFile(t, collision, []byte("User's existing copy\n"))
	command("unmanaged skill already exists", "install", "full-stop")
	if got := string(readFile(t, collision)); got != "User's existing copy\n" {
		t.Fatalf("overwrote unmanaged skill: %q", got)
	}
	assertMissing(t, filepath.Join(p.skills, "full-stop"))
	assertMissing(t, p.lock)
	if err := os.RemoveAll(filepath.Dir(collision)); err != nil {
		t.Fatal(err)
	}

	command("", "install", "full-stop")
	lock, err := p.readLock()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"babysit-pr", "file-pr", "full-send", "full-stop", "implement", "leaving-pr-comments", "review-loop"}
	if got := sortedKeys(lock.Skills); !slices.Equal(got, want) {
		t.Fatalf("installed %v, want %v", got, want)
	}
	if lock.Skills["full-stop"].Commit != first {
		t.Fatalf("wrong source commit in lock: %+v", lock.Skills["full-stop"])
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
	readFile(t, filepath.Join(p.skills, "full-stop", "agents", "openai.yaml"))
	command("", "install", "review-group")

	var entries map[string]skill
	if err := json.Unmarshal(manifest, &entries); err != nil {
		t.Fatal(err)
	}
	entry := entries["full-send"]
	entry.Requires = append(entry.Requires, "capture-issue")
	entries["full-send"] = entry
	next, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(source, "catalog.json"), next)
	if err := os.Remove(filepath.Join(source, "skills", oldGuide)); err != nil {
		t.Fatal(err)
	}
	fullSend := filepath.Join("full-send", "SKILL.md")
	reviewGroup := filepath.Join("review-group", "SKILL.md")
	original := readFile(t, filepath.Join(p.skills, fullSend))
	upstreamUpdate := append(slices.Clone(original), []byte("\nUpstream change.\n")...)
	writeFile(t, filepath.Join(source, "skills", fullSend), upstreamUpdate)
	writeFile(t, filepath.Join(source, "skills", reviewGroup), []byte("New review guidance.\n"))
	second := commitRepo(t, source)

	before := readFile(t, p.lock)
	if out := command("", "update", "full-stop", "--check"); !strings.Contains(out, "babysit-pr, full-send") || !strings.Contains(out, "capture-issue") || strings.Contains(out, "review-group") {
		t.Fatalf("unexpected update check:\n%s", out)
	}
	assertMissing(t, filepath.Join(p.skills, "capture-issue"))
	if !bytes.Equal(before, readFile(t, p.lock)) {
		t.Fatal("--check wrote the lock file")
	}
	localEdit := []byte("Keep my changes.\n")
	writeFile(t, filepath.Join(p.skills, fullSend), localEdit)
	if out := command("local changes", "update", "full-stop", "--check"); !strings.Contains(out, "Updates available") {
		t.Fatalf("check omitted available updates:\n%s", out)
	}
	command("local changes", "update", "full-stop")
	if !bytes.Equal(localEdit, readFile(t, filepath.Join(p.skills, fullSend))) || !bytes.Equal(before, readFile(t, p.lock)) {
		t.Fatal("conflicting update modified local changes or the lock file")
	}
	readFile(t, filepath.Join(p.skills, oldGuide))
	assertMissing(t, filepath.Join(p.skills, "capture-issue"))

	writeFile(t, filepath.Join(p.skills, fullSend), original)
	command("", "update", "full-stop")
	assertMissing(t, filepath.Join(p.skills, oldGuide))
	if !bytes.Equal(upstreamUpdate, readFile(t, filepath.Join(p.skills, fullSend))) {
		t.Fatal("update did not install the new skill file")
	}
	lock, err = p.readLock()
	if err != nil {
		t.Fatal(err)
	}
	if lock.Skills["capture-issue"].Commit != second || lock.Skills["review-group"].Commit != first {
		t.Fatalf("scoped update recorded the wrong commits: %+v", lock.Skills)
	}
	command("", "update")
	if got := readFile(t, filepath.Join(p.skills, reviewGroup)); string(got) != "New review guidance.\n" {
		t.Fatalf("update omitted a managed skill: %q", got)
	}
	command("", "install", "--all")
	lock, err = p.readLock()
	if err != nil || len(lock.Skills) != len(entries) {
		t.Fatalf("full install recorded %d skills, want %d: %v", len(lock.Skills), len(entries), err)
	}
	readFile(t, filepath.Join(p.skills, "manage-mstack-skills", "SKILL.md"))
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
	plan, err := p.plan(ctx, c, []string{"first", "second"}, lock)
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
	assertMissing(t, p.transaction)
	if got := string(readFile(t, collision)); got != "local" {
		t.Fatalf("overwrote the conflicting skill: %q", got)
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
	assertMissing(t, p.transaction)
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

package main

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExternalSkillLifecycle(t *testing.T) {
	const repository = "https://example.invalid/external.git"
	const skillPath = "packages/extern"
	const firstRelease = "skill-v1.9.0"
	const secondRelease = "skill-v1.10.0"
	ctx := context.Background()
	upstream, source, dest := t.TempDir(), t.TempDir(), t.TempDir()
	for _, dir := range []string{upstream, source, dest} {
		initRepo(t, dir)
	}
	rewriteRepository := func(path string) {
		t.Helper()
		path = filepath.ToSlash(path)
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		fileURL := url.URL{Scheme: "file", Path: path}
		t.Setenv("GIT_CONFIG_COUNT", "2")
		t.Setenv("GIT_CONFIG_KEY_0", "url."+fileURL.String()+".insteadOf")
		t.Setenv("GIT_CONFIG_VALUE_0", repository)
		t.Setenv("GIT_CONFIG_KEY_1", "protocol.file.allow")
		t.Setenv("GIT_CONFIG_VALUE_1", "always")
	}
	upstreamSkill := filepath.Join(upstream, filepath.FromSlash(skillPath))
	const doc = "---\nname: extern\ndescription: External skill.\n---\nSee [the guide](reference/guide.md).\n"
	upstreamFiles := map[string]string{
		"SKILL.md":           doc,
		"agents/openai.yaml": "interface:\n  display_name: External skill\n",
		"reference/guide.md": "Original guidance.\n",
		"scripts/inspect":    "#!/bin/sh\nexit 1\n",
	}
	for path, content := range upstreamFiles {
		writeFile(t, filepath.Join(upstreamSkill, filepath.FromSlash(path)), []byte(content))
	}
	if err := os.Chmod(filepath.Join(upstreamSkill, "scripts", "inspect"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(upstream, "LICENSE"), []byte("License terms.\n"))
	writeFile(t, filepath.Join(upstream, "NOTICE.md"), []byte("License notices.\n"))
	writeFile(t, filepath.Join(upstream, ".codex", "hooks.json"), []byte("{}\n"))
	first := commitRepo(t, upstream)
	const updatedGuide = "Updated guidance.\n"
	writeFile(t, filepath.Join(upstreamSkill, "reference", "guide.md"), []byte(updatedGuide))
	second := commitRepo(t, upstream)
	tag := func(args ...string) {
		t.Helper()
		config := []string{"-c", "user.name=mstack test", "-c", "user.email=mstack@example.invalid", "-c", "tag.gpgSign=false", "tag"}
		if _, err := git(ctx, upstream, append(config, args...)...); err != nil {
			t.Fatal(err)
		}
	}
	tag("-a", "-m", "Stable release", firstRelease, first)
	tag("skill-v1.8.99", second)
	tag("skill-v2.0.0-rc.1", second)
	tag("engine-v9.0.0", second)

	writeFile(t, filepath.Join(source, "catalog.json"), []byte(`{"base":{"summary":"Built-in skill"},"consumer":{"summary":"Dependent skill"}}`))
	writeFile(t, filepath.Join(source, "skills", "base", "SKILL.md"), []byte("Built-in guidance.\n"))
	consumer := filepath.Join("consumer", "SKILL.md")
	const originalConsumer = "Dependent guidance.\n"
	writeFile(t, filepath.Join(source, "skills", consumer), []byte(originalConsumer))
	externalManifest := filepath.Join(source, "external-skills.json")
	externalEntry := fmt.Sprintf(`"summary":"External skill","source":{"repository":%q,"tag_prefix":"skill-v","path":%q,"include":["LICENSE","NOTICE.md"]}`, repository, skillPath)
	writeFile(t, externalManifest, []byte(`{"extern":{`+externalEntry+`}}`))
	commitRepo(t, source)

	p := openProject(dest)
	installed := filepath.Join(p.skills, "extern")
	command := func(catalog, wantError string, args ...string) string {
		t.Helper()
		var out bytes.Buffer
		err := run(ctx, dest, catalog, args, &out)
		if wantError == "" && err != nil || wantError != "" && (err == nil || !strings.Contains(err.Error(), wantError)) {
			t.Fatalf("mstack %v: got error %v, want %q\n%s", args, err, wantError, out.String())
		}
		return out.String()
	}
	assertContents := func(path, want string) {
		t.Helper()
		if got := string(readFile(t, path)); got != want {
			t.Fatalf("%s contains %q, want %q", path, got, want)
		}
	}
	assertRelease := func(wantTag, wantCommit string) {
		t.Helper()
		lock, err := p.readLock()
		if err != nil {
			t.Fatal(err)
		}
		record := lock.Skills["extern"]
		if record.Release != wantTag || record.Commit != wantCommit {
			t.Fatalf("external skill recorded release %q at %q, want %q at %q", record.Release, record.Commit, wantTag, wantCommit)
		}
		if !bytes.Contains(readFile(t, p.lock), []byte(repository)) {
			t.Fatal("lock file does not record the external repository")
		}
	}

	unavailable := filepath.Join(t.TempDir(), "unavailable")
	rewriteRepository(unavailable)
	if out := command(source, "", "list"); !strings.Contains(out, "extern") || !strings.Contains(out, "External skill") {
		t.Fatalf("list omitted the available external skill:\n%s", out)
	}
	command(source, "", "install", "--all")
	assertMissing(t, installed)
	assertContents(filepath.Join(p.skills, "base", "SKILL.md"), "Built-in guidance.\n")

	rewriteRepository(upstream)
	before := readFile(t, p.lock)
	installedDoc := filepath.Join(installed, "SKILL.md")
	writeFile(t, installedDoc, []byte("Unmanaged copy.\n"))
	command(source, "unmanaged skill", "install", "extern")
	assertContents(installedDoc, "Unmanaged copy.\n")
	assertContents(p.lock, string(before))
	if err := os.RemoveAll(installed); err != nil {
		t.Fatal(err)
	}
	command(source, "", "install", "extern")
	assertRelease(firstRelease, first)
	for path, content := range upstreamFiles {
		assertContents(filepath.Join(installed, filepath.FromSlash(path)), content)
	}
	assertContents(filepath.Join(installed, "LICENSE"), "License terms.\n")
	assertContents(filepath.Join(installed, "NOTICE.md"), "License notices.\n")
	assertMissing(t, filepath.Join(dest, ".codex"))
	if mode, err := os.Stat(filepath.Join(installed, "scripts", "inspect")); err != nil {
		t.Fatal(err)
	} else if runtime.GOOS != "windows" && mode.Mode().Perm()&0o100 == 0 {
		t.Fatal("external skill launcher lost its executable mode")
	}

	before = readFile(t, p.lock)
	tag(secondRelease, second)
	if out := command(source, "", "update", "--check"); !strings.Contains(out, "Updates available (1): extern") {
		t.Fatalf("update check omitted the new upstream release:\n%s", out)
	}
	guide := filepath.Join(installed, "reference", "guide.md")
	assertContents(guide, upstreamFiles["reference/guide.md"])
	assertContents(p.lock, string(before))
	writeFile(t, guide, []byte("Local changes.\n"))
	command(source, "local changes", "update", "extern")
	assertContents(guide, "Local changes.\n")
	assertContents(p.lock, string(before))
	writeFile(t, guide, []byte(upstreamFiles["reference/guide.md"]))
	command(source, "", "update", "extern")
	assertContents(guide, updatedGuide)
	assertRelease(secondRelease, second)

	if out := command(source, "", "update", "--check", "extern"); !strings.Contains(out, "Already current: 1") {
		t.Fatalf("update check did not recognize the installed release:\n%s", out)
	}
	rewriteRepository(unavailable)
	command(unavailable, "", "remove", "extern")
	assertMissing(t, installed)
	before = readFile(t, p.lock)
	const updatedConsumer = "Updated dependent guidance.\n"
	writeFile(t, externalManifest, []byte(`{"extern":{`+externalEntry+`,"required_by":["consumer"]}}`))
	writeFile(t, filepath.Join(source, "skills", consumer), []byte(updatedConsumer))
	commitRepo(t, source)
	command(source, "extern", "update", "consumer")
	assertContents(filepath.Join(p.skills, consumer), originalConsumer)
	assertMissing(t, installed)
	assertContents(p.lock, string(before))
	rewriteRepository(upstream)
	if out := command(source, "", "update", "--check", "consumer"); !strings.Contains(out, "Dependencies to install (1): extern") || !strings.Contains(out, "Skill attributes need updating: .agents/.gitattributes") {
		t.Fatalf("update check omitted the missing dependency:\n%s", out)
	}
	assertMissing(t, installed)
	assertContents(p.lock, string(before))
	command(source, "", "update", "consumer")
	assertContents(filepath.Join(p.skills, consumer), updatedConsumer)
	assertRelease(secondRelease, second)
	attributes := readFile(t, filepath.Join(p.agents, attributesName))
	if !bytes.Contains(attributes, []byte("skills/extern/** linguist-vendored\n")) {
		t.Fatalf("attributes omit the installed external dependency: %s", attributes)
	}
	assertMissing(t, p.transaction)
}

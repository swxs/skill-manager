package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/swxs/skill-manager/internal/cli"
	"github.com/swxs/skill-manager/internal/gitpack"
	"github.com/swxs/skill-manager/internal/jsonc"
	lib "github.com/swxs/skill-manager/internal/library"
	"github.com/swxs/skill-manager/internal/link"
	"github.com/swxs/skill-manager/internal/symlink"
)

func writeSkill(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	text := "---\nname: " + filepath.Base(dir) + "\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func withHome(t *testing.T) (root, home, library string) {
	t.Helper()
	root = t.TempDir()
	user := filepath.Join(root, "user")
	home = filepath.Join(user, ".agents")
	library = filepath.Join(root, "library")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", user)
	t.Setenv("USERPROFILE", user)
	return root, home, library
}

func runCLI(t *testing.T, library string, args ...string) (int, string) {
	t.Helper()
	code, out, _ := runCLIStreams(t, library, args...)
	return code, out
}

func runCLIStreams(t *testing.T, library string, args ...string) (int, string, string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = outW, errW
	code := cli.Main(append([]string{"--library", library}, args...))
	_ = outW.Close()
	_ = errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	var outBuf, errBuf bytes.Buffer
	_, _ = io.Copy(&outBuf, outR)
	_, _ = io.Copy(&errBuf, errR)
	_ = outR.Close()
	_ = errR.Close()
	return code, outBuf.String(), errBuf.String()
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestParseJSONCKeepsCommentMarkersInsideStrings(t *testing.T) {
	text := `
        // 行注释
        [
          "keep // inside",
          "keep, comma",
          /* 块
             注释 */
          "tail",
        ]
        `
	got, err := jsonc.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	items, ok := got.([]any)
	if !ok || len(items) != 3 || items[0] != "keep // inside" || items[1] != "keep, comma" || items[2] != "tail" {
		t.Fatalf("%#v", got)
	}
}

func TestStripTrailingCommasKeepsCommasInsideStrings(t *testing.T) {
	got := jsonc.StripTrailingCommas("[\"a,b\", {\"k\": \"x,\",},]\n")
	want := "[\"a,b\", {\"k\": \"x,\"}]\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestClassifySource(t *testing.T) {
	root, _, _ := withHome(t)
	single := filepath.Join(root, "single")
	writeSkill(t, single)
	pack := filepath.Join(root, "pack")
	writeSkill(t, filepath.Join(pack, "alpha"))
	writeSkill(t, filepath.Join(pack, "beta"))
	mixed := filepath.Join(root, "mixed")
	writeSkill(t, mixed)
	writeSkill(t, filepath.Join(mixed, "child"))
	if kind, _ := lib.ClassifySource(single); kind != "single" {
		t.Fatal(kind)
	}
	if kind, _ := lib.ClassifySource(pack); kind != "pack" {
		t.Fatal(kind)
	}
	if kind, _ := lib.ClassifySource(mixed); kind != "mixed" {
		t.Fatal(kind)
	}
}

func TestClassifySourceRejectsMissingAndSkipsHidden(t *testing.T) {
	root, _, _ := withHome(t)
	empty := filepath.Join(root, "empty")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	hidden := filepath.Join(root, "hidden")
	writeSkill(t, filepath.Join(hidden, ".secret"))
	visible := filepath.Join(root, "visible")
	writeSkill(t, filepath.Join(visible, "shown"))
	writeSkill(t, filepath.Join(visible, ".secret"))
	if _, err := lib.ClassifySource(empty); err == nil || !bytes.Contains([]byte(err.Error()), []byte("没有 SKILL.md")) {
		t.Fatal(err)
	}
	if _, err := lib.ClassifySource(hidden); err == nil || !bytes.Contains([]byte(err.Error()), []byte("没有 SKILL.md")) {
		t.Fatal(err)
	}
	if kind, err := lib.ClassifySource(visible); err != nil || kind != "pack" {
		t.Fatal(kind, err)
	}
	names, _, err := lib.DirectMembers(visible)
	if err != nil || len(names) != 1 || names[0] != "shown" {
		t.Fatal(names, err)
	}
}

func TestDirectMembersSortByCasefold(t *testing.T) {
	root, _, _ := withHome(t)
	pack := filepath.Join(root, "sorted")
	for _, name := range []string{"b", "A", "c"} {
		writeSkill(t, filepath.Join(pack, name))
	}
	names, _, err := lib.DirectMembers(pack)
	if err != nil {
		t.Fatal(err)
	}
	if stringsJoin(names) != "A,b,c" {
		t.Fatal(names)
	}
	info, err := lib.Inspect(pack)
	if err != nil || stringsJoin(info.Skills) != "A,b,c" {
		t.Fatal(info.Skills, err)
	}
}

func stringsJoin(items []string) string {
	return joinComma(items)
}

func joinComma(items []string) string {
	out := ""
	for i, item := range items {
		if i > 0 {
			out += ","
		}
		out += item
	}
	return out
}

func TestResolveSelectionReportsMissing(t *testing.T) {
	_, _, library := withHome(t)
	writeSkill(t, filepath.Join(library, "demo", "one"))
	missing, problems := link.Resolve(library, []string{"nope"})
	if len(missing) != 0 || stringsJoin(problems) != "! 库中没有包 nope" {
		t.Fatal(problems)
	}
	_, problems = link.Resolve(library, []string{"demo:missing"})
	if stringsJoin(problems) != "! 包 demo 里没有技能 missing" {
		t.Fatal(problems)
	}
}

func TestMissingPackHintOnAdd(t *testing.T) {
	_, home, library := withHome(t)
	_, output := runCLI(t, library, "add", "--global", "missing-pack")
	if !bytes.Contains([]byte(output), []byte("! 库中没有包 missing-pack")) || !bytes.Contains([]byte(output), []byte("请先 install")) {
		t.Fatal(output)
	}
	if _, err := os.Stat(filepath.Join(home, "skills.json")); !os.IsNotExist(err) {
		t.Fatal("skills.json should not exist", err)
	}
}

func TestResolveSelectionRejectsSplittingMixed(t *testing.T) {
	_, _, library := withHome(t)
	mixed := filepath.Join(library, "mix")
	writeSkill(t, mixed)
	writeSkill(t, filepath.Join(mixed, "child"))
	desired, problems := link.Resolve(library, []string{"mix:child"})
	if len(desired) != 0 || stringsJoin(problems) != "! 混合包不能单拆 mix:child" {
		t.Fatal(problems)
	}
}

func TestResolveSelectionSkipsSameName(t *testing.T) {
	_, _, library := withHome(t)
	writeSkill(t, filepath.Join(library, "pack-a", "shared"))
	writeSkill(t, filepath.Join(library, "pack-b", "shared"))
	desired, problems := link.Resolve(library, []string{"pack-a", "pack-b"})
	if len(desired) != 1 || desired["shared"] == "" {
		t.Fatal(desired)
	}
	if stringsJoin(problems) != "! 同名跳过 pack-b:shared（已占用 shared）" {
		t.Fatal(problems)
	}
}

func TestResolveSelectionExpandsPurePack(t *testing.T) {
	_, _, library := withHome(t)
	writeSkill(t, filepath.Join(library, "demo", "one"))
	writeSkill(t, filepath.Join(library, "demo", "two"))
	desired, problems := link.Resolve(library, []string{"demo"})
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	if desired["one"] == "" || desired["two"] == "" || len(desired) != 2 {
		t.Fatal(desired)
	}
}

func TestGlobalConfigPathFollowsHome(t *testing.T) {
	_, home, _ := withHome(t)
	got, err := lib.GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(home, "skills.json") {
		t.Fatalf("got %s", got)
	}
}

func TestInstallAddLinkStatusRemoveAndLF(t *testing.T) {
	root, _, library := withHome(t)
	source := filepath.Join(root, "sources", "demo")
	writeSkill(t, filepath.Join(source, "one"))
	writeSkill(t, filepath.Join(source, "two"))
	for _, rel := range []string{".git/config", "__pycache__/x.pyc", ".venv/pyvenv.cfg", "node_modules/pkg/index.js"} {
		path := filepath.Join(source, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("noise"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(source, ".DS_Store"), []byte("noise"), 0o644)
	_ = os.WriteFile(filepath.Join(source, ".skill-lock.json"), []byte("{}\n"), 0o644)

	if code, output := runCLI(t, library, "install", source); code != 0 {
		t.Fatal(output)
	}
	installed := filepath.Join(library, "demo")
	for _, name := range []string{"__pycache__", ".venv", "node_modules", ".DS_Store", ".skill-lock.json"} {
		if _, err := os.Stat(filepath.Join(installed, name)); !os.IsNotExist(err) {
			t.Fatal(name, err)
		}
	}
	lockBytes, err := os.ReadFile(filepath.Join(library, ".skill-lock.json"))
	if err != nil || bytes.Contains(lockBytes, []byte("\r")) || !bytes.HasSuffix(lockBytes, []byte("\n")) {
		t.Fatal(err, lockBytes)
	}
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q")
	if code, output := runCLI(t, library, "add", "--root", repo, "demo"); code != 0 {
		t.Fatal(output)
	}
	selection := filepath.Join(repo, ".agents", "skills.json")
	raw, err := os.ReadFile(selection)
	if err != nil || bytes.Contains(raw, []byte("\r")) {
		t.Fatal(err, raw)
	}
	got, err := jsonc.Parse(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	items := got.([]any)
	if len(items) != 1 || items[0] != "demo" {
		t.Fatal(got)
	}
	if code, output := runCLI(t, library, "sync", "--root", repo); code != 0 {
		t.Fatal(output)
	}
	if symlink.Target(filepath.Join(repo, ".agents", "skills", "one")) == "" || symlink.Target(filepath.Join(repo, ".agents", "skills", "two")) == "" {
		t.Fatal("links missing")
	}
	exclude, err := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if err != nil || bytes.Contains(exclude, []byte("\r")) || !bytes.Contains(exclude, []byte(".agents/skills/one")) || !bytes.Contains(exclude, []byte(".agents/skills/two")) {
		t.Fatal(err, string(exclude))
	}
	if code, output := runCLI(t, library, "status", "--root", repo); code != 0 || !bytes.Contains([]byte(output), []byte("已与配置一致")) {
		t.Fatal(code, output)
	}
	if code, output := runCLI(t, library, "remove", "--root", repo, "demo"); code != 0 {
		t.Fatal(output)
	}
	if _, err := os.Stat(installed); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(selection)
	if err != nil {
		t.Fatal(err)
	}
	got, err = jsonc.Parse(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	items = got.([]any)
	if len(items) != 0 {
		t.Fatal(got)
	}
	if code, output := runCLI(t, library, "sync", "--root", repo); code != 0 {
		t.Fatal(output)
	}
	if symlink.Target(filepath.Join(repo, ".agents", "skills", "one")) != "" || symlink.Target(filepath.Join(repo, ".agents", "skills", "two")) != "" {
		t.Fatal("links should be removed")
	}
}

func TestAddGlobalWritesSkillsJSONUnderHome(t *testing.T) {
	root, home, library := withHome(t)
	source := filepath.Join(root, "sources", "demo")
	writeSkill(t, filepath.Join(source, "one"))
	if code, output := runCLI(t, library, "install", source); code != 0 {
		t.Fatal(output)
	}
	if code, output := runCLI(t, library, "add", "--global", "demo"); code != 0 {
		t.Fatal(output)
	}
	path := filepath.Join(home, "skills.json")
	raw, err := os.ReadFile(path)
	if err != nil || bytes.Contains(raw, []byte("\r")) {
		t.Fatal(err, raw)
	}
	if _, err := os.Stat(filepath.Join(home, "skills", "skill-manager", "skills.json")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	got, err := jsonc.Parse(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	items := got.([]any)
	if len(items) != 1 || items[0] != "demo" {
		t.Fatal(got)
	}
}

func TestInstallKeepsGitAndLockMetadata(t *testing.T) {
	root, _, library := withHome(t)
	source := filepath.Join(root, "sources", "demo")
	writeSkill(t, filepath.Join(source, "one"))
	writeSkill(t, filepath.Join(source, "two"))
	git(t, source, "init", "-q")
	git(t, source, "config", "user.email", "t@example.com")
	git(t, source, "config", "user.name", "t")
	git(t, source, "add", ".")
	git(t, source, "commit", "-m", "init")
	git(t, source, "branch", "-M", "main")
	git(t, source, "remote", "add", "origin", "https://example.com/demo.git")
	if code, output := runCLI(t, library, "install", source); code != 0 {
		t.Fatal(output)
	}
	text, err := os.ReadFile(filepath.Join(library, ".skill-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	var lock map[string]any
	if err := json.Unmarshal(text, &lock); err != nil {
		t.Fatal(err)
	}
	pack := lock["packs"].(map[string]any)["demo"].(map[string]any)
	skills, _ := pack["skills"].(map[string]any)
	if len(skills) != 0 || pack["revision"] != nil || pack["ref"] != nil || pack["source"] != nil {
		t.Fatal(pack)
	}
}

func TestAddGitURLFetchesThenWritesConfig(t *testing.T) {
	root, _, library := withHome(t)
	upstream := filepath.Join(root, "upstream")
	writeSkill(t, filepath.Join(upstream, "alpha"))
	git(t, upstream, "init", "-q")
	git(t, upstream, "config", "user.email", "t@example.com")
	git(t, upstream, "config", "user.name", "t")
	git(t, upstream, "add", ".")
	git(t, upstream, "commit", "-m", "init")
	git(t, upstream, "branch", "-M", "main")
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	code, output := runCLI(t, library, "install", upstream)
	if code != 0 {
		t.Fatal(output)
	}
	if !lib.IsDir(filepath.Join(library, "upstream")) {
		t.Fatal("missing upstream")
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "skills.json")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	code, output = runCLI(t, library, "add", "--root", repo, "upstream")
	if code != 0 {
		t.Fatal(output)
	}
	raw, err := os.ReadFile(filepath.Join(repo, ".agents", "skills.json"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := jsonc.Parse(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	items := got.([]any)
	if len(items) != 1 || items[0] != "upstream" {
		t.Fatal(got)
	}
}

func TestInitGroupsGlobalLinksIntoPackEntry(t *testing.T) {
	_, home, library := withHome(t)
	writeSkill(t, filepath.Join(library, "demo", "one"))
	writeSkill(t, filepath.Join(library, "demo", "two"))
	if err := gitpack.WriteLock(library, nil); err != nil {
		t.Fatal(err)
	}
	skillsHome := filepath.Join(home, "skills")
	if err := os.MkdirAll(skillsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one", "two"} {
		if _, err := symlink.Create(filepath.Join(skillsHome, name), filepath.Join(library, "demo", name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(skillsHome, "skill-manager"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, output := runCLI(t, library, "init")
	if code != 0 {
		t.Fatal(output)
	}
	raw, err := os.ReadFile(filepath.Join(home, "skills.json"))
	if err != nil {
		t.Fatal(err, output)
	}
	got, err := jsonc.Parse(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	items := got.([]any)
	if len(items) != 1 || items[0] != "demo" {
		t.Fatal(got, output)
	}
	if symlink.Target(filepath.Join(skillsHome, "one")) == "" {
		t.Fatal("link missing", output)
	}
}

func TestInitInstallsRealSkillManager(t *testing.T) {
	_, home, library := withHome(t)
	skillsHome := filepath.Join(home, "skills")
	writeSkill(t, filepath.Join(skillsHome, "skill-manager"))
	code, output := runCLI(t, library, "init")
	if code != 0 {
		t.Fatal(output)
	}
	if _, err := os.Stat(filepath.Join(library, "skill-manager", "skill-manager", "SKILL.md")); err != nil {
		t.Fatal(err, output)
	}
	raw, err := os.ReadFile(filepath.Join(home, "skills.json"))
	if err != nil {
		t.Fatal(err, output)
	}
	got, err := jsonc.Parse(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	items := got.([]any)
	if len(items) != 1 || items[0] != "skill-manager" {
		t.Fatal(got, output)
	}
	if symlink.Target(filepath.Join(skillsHome, "skill-manager")) == "" {
		t.Fatal("still a real directory", output)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(bytes.TrimSpace(out))
}

func TestInstallUnlockSkipsLock(t *testing.T) {
	root, _, library := withHome(t)
	source := filepath.Join(root, "sources", "demo")
	writeSkill(t, filepath.Join(source, "one"))
	if code, output := runCLI(t, library, "install", "--unlock", source); code != 0 {
		t.Fatal(output)
	}
	if _, err := os.Stat(filepath.Join(library, "demo", "one", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(library, ".skill-lock.json")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestInstallExistingSkipsCopy(t *testing.T) {
	root, _, library := withHome(t)
	source := filepath.Join(root, "sources", "demo")
	writeSkill(t, filepath.Join(source, "one"))
	if code, output := runCLI(t, library, "install", source); code != 0 {
		t.Fatal(output)
	}
	marker := filepath.Join(library, "demo", "one", "SKILL.md")
	if err := os.WriteFile(marker, []byte("kept\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, output := runCLI(t, library, "install", source); code != 0 {
		t.Fatal(output)
	}
	text, err := os.ReadFile(marker)
	if err != nil || string(text) != "kept\n" {
		t.Fatalf("%q %v", text, err)
	}
}

func TestRemoveDeletesExactEntryOnly(t *testing.T) {
	root, _, library := withHome(t)
	source := filepath.Join(root, "sources", "demo")
	writeSkill(t, filepath.Join(source, "one"))
	writeSkill(t, filepath.Join(source, "two"))
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if code, output := runCLI(t, library, "install", source); code != 0 {
		t.Fatal(output)
	}
	if code, output := runCLI(t, library, "add", "--root", repo, "demo", "demo:one"); code != 0 {
		t.Fatal(output)
	}
	selection := filepath.Join(repo, ".agents", "skills.json")
	before, err := os.ReadFile(selection)
	if err != nil {
		t.Fatal(err)
	}
	if code, output := runCLI(t, library, "remove", "--root", repo, "demo:two"); code == 0 {
		t.Fatal(output)
	}
	after, err := os.ReadFile(selection)
	if err != nil || string(after) != string(before) {
		t.Fatalf("config changed\n%s", after)
	}
	if code, output := runCLI(t, library, "remove", "--root", repo, "demo"); code != 0 {
		t.Fatal(output)
	}
	raw, err := os.ReadFile(selection)
	if err != nil {
		t.Fatal(err)
	}
	got, err := jsonc.Parse(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	items := got.([]any)
	if len(items) != 1 || items[0] != "demo:one" {
		t.Fatal(got)
	}
	if _, err := os.Stat(filepath.Join(library, "demo")); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveDropsDeclarationWhenPackIsGone(t *testing.T) {
	root, _, library := withHome(t)
	source := filepath.Join(root, "sources", "tavily")
	writeSkill(t, filepath.Join(source, "search"))
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if code, output := runCLI(t, library, "install", source); code != 0 {
		t.Fatal(output)
	}
	if code, output := runCLI(t, library, "add", "--root", repo, "tavily"); code != 0 {
		t.Fatal(output)
	}
	if err := os.RemoveAll(filepath.Join(library, "tavily")); err != nil {
		t.Fatal(err)
	}
	code, output := runCLI(t, library, "remove", "--root", repo, "tavily")
	if code != 0 || bytes.Contains([]byte(output), []byte("库中没有包")) || bytes.Contains([]byte(output), []byte("未修改配置")) {
		t.Fatal(code, output)
	}
	raw, err := os.ReadFile(filepath.Join(repo, ".agents", "skills.json"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := jsonc.Parse(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	items, ok := got.([]any)
	if !ok || len(items) != 0 {
		t.Fatal(got)
	}
}

func TestAddRejectsGitURL(t *testing.T) {
	root, _, library := withHome(t)
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	code, output := runCLI(t, library, "add", "--root", repo, "https://example.com/demo.git")
	if code == 0 || !bytes.Contains([]byte(output), []byte("install")) {
		t.Fatal(code, output)
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "skills.json")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestLinkCommandIsGone(t *testing.T) {
	_, _, library := withHome(t)
	if code, _ := runCLI(t, library, "link"); code == 0 {
		t.Fatal(code)
	}
}

func TestInstallChecksOutRefAndRefusesDirty(t *testing.T) {
	root, _, library := withHome(t)
	source := filepath.Join(root, "sources", "demo")
	skill := filepath.Join(source, "one", "SKILL.md")
	writeSkill(t, filepath.Join(source, "one"))
	git(t, source, "init", "-q")
	git(t, source, "config", "user.email", "t@example.com")
	git(t, source, "config", "user.name", "t")
	git(t, source, "add", ".")
	git(t, source, "commit", "-m", "v1")
	git(t, source, "branch", "-M", "main")
	git(t, source, "tag", "v1")
	git(t, source, "remote", "add", "origin", source)
	if err := os.WriteFile(skill, []byte("---\nname: one\n---\nv2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, source, "add", ".")
	git(t, source, "commit", "-m", "v2")
	if code, output := runCLI(t, library, "install", source); code != 0 {
		t.Fatal(output)
	}
	installed := filepath.Join(library, "demo")
	headV2 := gitOutput(t, installed, "rev-parse", "HEAD")
	if code, output := runCLI(t, library, "install", source+"#v1"); code != 0 {
		t.Fatal(output)
	}
	headV1 := gitOutput(t, installed, "rev-parse", "HEAD")
	tag := gitOutput(t, source, "rev-parse", "v1")
	if headV1 != tag || headV1 == headV2 {
		t.Fatalf("head %s tag %s previous %s", headV1, tag, headV2)
	}
	if err := os.WriteFile(filepath.Join(installed, "one", "SKILL.md"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, output := runCLI(t, library, "install", source+"#main")
	if code == 0 || !bytes.Contains([]byte(output), []byte("不干净")) {
		t.Fatal(code, output)
	}
	if gitOutput(t, installed, "rev-parse", "HEAD") != headV1 {
		t.Fatal("dirty checkout changed HEAD")
	}
}

func TestAddHelpUsesTerms(t *testing.T) {
	_, _, library := withHome(t)
	code, output := runCLI(t, library, "add", "--help")
	if code != 0 || !bytes.Contains([]byte(output), []byte("添加技能声明")) || !bytes.Contains([]byte(output), []byte("--global")) {
		t.Fatal(code, output)
	}
	if bytes.Contains([]byte(output), []byte("初始化技能管理体系")) {
		t.Fatal("subcommand help included the overview", output)
	}
}

func TestStatusPrintsGlobalBeforeWorkspace(t *testing.T) {
	root, _, library := withHome(t)
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	code, output := runCLI(t, library, "status", "--root", repo)
	if code != 0 && code != 2 {
		t.Fatal(code, output)
	}
	configAt := bytes.Index([]byte(output), []byte("全局技能声明:"))
	workspaceAt := bytes.Index([]byte(output), []byte("全局工作区:"))
	selectionAt := bytes.Index([]byte(output), []byte("\n工作区技能声明:"))
	rootAt := bytes.Index([]byte(output), []byte("\n工作区:"))
	if configAt < 0 || workspaceAt < 0 || selectionAt < 0 || rootAt < 0 || !(configAt < workspaceAt && workspaceAt < selectionAt && selectionAt < rootAt) {
		t.Fatal(output)
	}
}

func TestInitReplacesRealDirectoryWithLink(t *testing.T) {
	_, home, library := withHome(t)
	skills := filepath.Join(home, "skills")
	writeSkill(t, filepath.Join(skills, "extra"))
	code, output := runCLI(t, library, "init")
	if code != 0 {
		t.Fatal(output)
	}
	if symlink.Target(filepath.Join(skills, "extra")) == "" {
		t.Fatal("extra should be a link", output)
	}
	if _, err := os.Stat(filepath.Join(library, "extra", "extra", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
}

func TestInitDoesNotOverwriteExistingPack(t *testing.T) {
	_, home, library := withHome(t)
	writeSkill(t, filepath.Join(library, "extra", "extra"))
	marker := filepath.Join(library, "extra", "extra", "SKILL.md")
	if err := os.WriteFile(marker, []byte("library\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skills := filepath.Join(home, "skills", "extra")
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skills, "SKILL.md"), []byte("fresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, output := runCLI(t, library, "init")
	if code != 0 {
		t.Fatal(output)
	}
	text, err := os.ReadFile(marker)
	if err != nil || string(text) != "library\n" {
		t.Fatalf("%q %v", text, err)
	}
	if symlink.Target(filepath.Join(home, "skills", "extra")) == "" {
		t.Fatal("extra should be a link", output)
	}
}

func TestLockAlignsKindAndListsEachSkill(t *testing.T) {
	root, _, library := withHome(t)
	writeSkill(t, filepath.Join(root, "sources", "ab"))
	writeSkill(t, filepath.Join(root, "sources", "longer-name", "one"))
	writeSkill(t, filepath.Join(root, "sources", "longer-name", "two"))
	for _, name := range []string{"ab", "longer-name"} {
		if code, output := runCLI(t, library, "install", filepath.Join(root, "sources", name)); code != 0 {
			t.Fatal(output)
		}
	}
	code, output := runCLI(t, library, "lock")
	if code != 0 {
		t.Fatal(output)
	}
	cols := map[string]int{}
	for _, line := range bytes.Split([]byte(output), []byte("\n")) {
		for _, kind := range []string{"single", "pack"} {
			if bytes.Contains(line, []byte("  "+kind)) {
				cols[kind] = bytes.Index(line, []byte(kind))
			}
		}
	}
	if _, ok := cols["single"]; !ok || cols["single"] != cols["pack"] {
		t.Fatal(output)
	}
	for _, skill := range []string{"    ab", "    one", "    two"} {
		if !bytes.Contains([]byte(output), []byte(skill+"\n")) {
			t.Fatal(output)
		}
	}
	if bytes.Contains([]byte(output), []byte(" +")) || bytes.Contains([]byte(output), []byte(",")) {
		t.Fatal(output)
	}
}

func TestListAlignsKindColumn(t *testing.T) {
	root, _, library := withHome(t)
	for _, name := range []string{"ab", "longer-name"} {
		writeSkill(t, filepath.Join(root, "sources", name, "one"))
		if code, output := runCLI(t, library, "install", filepath.Join(root, "sources", name)); code != 0 {
			t.Fatal(output)
		}
	}
	code, output := runCLI(t, library, "list")
	if code != 0 {
		t.Fatal(output)
	}
	cols := []int{}
	for _, line := range bytes.Split([]byte(output), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		at := bytes.Index(line, []byte("pack"))
		if at < 0 {
			t.Fatal(output)
		}
		cols = append(cols, at)
	}
	if len(cols) != 2 || cols[0] != cols[1] {
		t.Fatal(output)
	}
}

func TestStatusUsesDeclarationAndPackSkill(t *testing.T) {
	root, _, library := withHome(t)
	source := filepath.Join(root, "sources", "demo")
	writeSkill(t, filepath.Join(source, "one"))
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if code, output := runCLI(t, library, "install", source); code != 0 {
		t.Fatal(output)
	}
	if code, output := runCLI(t, library, "add", "--global", "demo"); code != 0 {
		t.Fatal(output)
	}
	if code, output := runCLI(t, library, "sync", "--global"); code != 0 {
		t.Fatal(output)
	}
	if code, output := runCLI(t, library, "add", "--root", repo, "demo"); code != 0 {
		t.Fatal(output)
	}
	code, output := runCLI(t, library, "status", "--root", repo)
	if code != 0 {
		t.Fatal(output)
	}
	if !bytes.Contains([]byte(output), []byte("- demo\n")) || !bytes.Contains([]byte(output), []byte("= 未变化 demo:one")) {
		t.Fatal(output)
	}
	if !bytes.Contains([]byte(output), []byte("· 跳过 demo:one（global 已覆盖）")) {
		t.Fatal(output)
	}
	other := filepath.Join(root, "other")
	if err := os.Mkdir(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(other, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, ".agents", "skills.json"), []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, output = runCLI(t, library, "status", "--root", other)
	if code != 0 || bytes.Contains([]byte(output), []byte("跳过 demo:one")) {
		t.Fatal(code, output)
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	git(t, dir, "init", "-q")
	git(t, dir, "config", "user.email", "t@example.com")
	git(t, dir, "config", "user.name", "t")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "init")
	git(t, dir, "branch", "-M", "main")
}

func pointGitHubAt(t *testing.T, local, prefix string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url."+filepath.ToSlash(local)+".insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", prefix)
}

func TestPackageNameRules(t *testing.T) {
	_, _, library := withHome(t)
	cases := []struct{ url, want string }{
		{"https://github.com/cathrynlavery/diagram-design", "diagram-design"},
		{"https://github.com/axtonliu/axton-obsidian-visual-skills", "axton-obsidian-visual"},
		{"https://github.com/anthropics/skills", "anthropics"},
		{"https://github.com/anthropics/skills.git", "anthropics"},
		{"https://github.com/anthropics/skills/tree/main/skills/foo", "anthropics"},
	}
	for _, tc := range cases {
		code, out, errOut := runCLIStreams(t, library, "package-name", tc.url)
		if code != 0 || errOut != "" || out != tc.want+"\n" {
			t.Fatalf("%s code %d out %q err %q", tc.url, code, out, errOut)
		}
	}
	code, out, errOut := runCLIStreams(t, library, "package-name", "https://example.com/foo")
	if code != 2 || out != "" || errOut != "无法从 URL 推导包名: https://example.com/foo\n" {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
}

func TestInstallGitSkillsAndUpgrade(t *testing.T) {
	root, _, library := withHome(t)
	upstream := filepath.Join(root, "upstream")
	writeSkill(t, filepath.Join(upstream, "skills", "alpha"))
	writeSkill(t, filepath.Join(upstream, "skills", "beta"))
	initGitRepo(t, upstream)
	prefix := "https://github.com/cathrynlavery/diagram-design"
	pointGitHubAt(t, upstream, prefix)
	code, out, errOut := runCLIStreams(t, library, "install", prefix)
	if code != 0 || errOut != "" {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
	if lib.IsDir(filepath.Join(library, "diagram-design", ".git")) {
		t.Fatal("package kept .git")
	}
	if _, err := os.Stat(filepath.Join(library, "diagram-design", "alpha", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(library, ".skill-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	var lock map[string]any
	if err := json.Unmarshal(raw, &lock); err != nil {
		t.Fatal(err)
	}
	pack := lock["packs"].(map[string]any)["diagram-design"].(map[string]any)
	skills := pack["skills"].(map[string]any)
	want := prefix + "/tree/main/skills/alpha"
	if skills["alpha"] != want || skills["beta"] != prefix+"/tree/main/skills/beta" {
		t.Fatal(skills)
	}
	if err := os.WriteFile(filepath.Join(upstream, "skills", "alpha", "SKILL.md"), []byte("---\nname: alpha\n---\nnext\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, upstream, "add", ".")
	git(t, upstream, "commit", "-m", "next")
	code, out, errOut = runCLIStreams(t, library, "upgrade", "diagram-design:alpha")
	if code != 0 || errOut != "" || !bytes.Contains([]byte(out), []byte("已升级 diagram-design:alpha")) {
		t.Fatalf("upgrade code %d out %q err %q", code, out, errOut)
	}
	text, err := os.ReadFile(filepath.Join(library, "diagram-design", "alpha", "SKILL.md"))
	if err != nil || !bytes.Contains(text, []byte("next")) {
		t.Fatalf("%s %v", text, err)
	}
	beta, err := os.ReadFile(filepath.Join(library, "diagram-design", "beta", "SKILL.md"))
	if err != nil || bytes.Contains(beta, []byte("next")) {
		t.Fatalf("beta changed %s", beta)
	}
}

func TestInstallSingleSkillAndRejectsBadSkills(t *testing.T) {
	root, _, library := withHome(t)
	upstream := filepath.Join(root, "upstream")
	writeSkill(t, filepath.Join(upstream, "skills", "engineering", "tdd"))
	initGitRepo(t, upstream)
	prefix := "https://github.com/anthropics/skills"
	pointGitHubAt(t, upstream, prefix)
	url := prefix + "/tree/main/skills/engineering/tdd"
	code, _, errOut := runCLIStreams(t, library, "install", url)
	if code != 0 || errOut != "" {
		t.Fatalf("code %d err %q", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(library, "anthropics", "tdd", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(library, "anthropics", "engineering")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	missing := prefix + "/tree/main/skills/engineering"
	code, _, errOut = runCLIStreams(t, library, "install", missing)
	if code != 2 || errOut != "目录没有 SKILL.md: "+missing+"\n" {
		t.Fatalf("code %d err %q", code, errOut)
	}

	bad := filepath.Join(root, "bad")
	if err := os.MkdirAll(filepath.Join(bad, "skills", "nope"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "skills", "nope", "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, bad)
	badPrefix := "https://github.com/cathrynlavery/diagram-design"
	pointGitHubAt(t, bad, badPrefix)
	code, _, errOut = runCLIStreams(t, library, "install", badPrefix)
	if code != 2 || errOut != "skills 目录不合法: "+badPrefix+"\n" {
		t.Fatalf("code %d err %q", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(library, "diagram-design")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestInstallSameSourceOverwritesDifferentSourceErrors(t *testing.T) {
	root, _, library := withHome(t)
	first := filepath.Join(root, "first")
	writeSkill(t, filepath.Join(first, "skills", "alpha"))
	initGitRepo(t, first)
	prefix := "https://github.com/axtonliu/axton-obsidian-visual-skills"
	pointGitHubAt(t, first, prefix)
	if code, _, errOut := runCLIStreams(t, library, "install", prefix); code != 0 {
		t.Fatal(errOut)
	}
	if err := os.WriteFile(filepath.Join(first, "skills", "alpha", "SKILL.md"), []byte("---\nname: alpha\n---\nagain\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, first, "add", ".")
	git(t, first, "commit", "-m", "again")
	code, _, errOut := runCLIStreams(t, library, "install", prefix)
	if code != 0 || errOut != "" {
		t.Fatalf("reinstall code %d err %q", code, errOut)
	}
	text, err := os.ReadFile(filepath.Join(library, "axton-obsidian-visual", "alpha", "SKILL.md"))
	if err != nil || !bytes.Contains(text, []byte("again")) {
		t.Fatalf("%s %v", text, err)
	}

	other := filepath.Join(root, "other")
	writeSkill(t, filepath.Join(other, "skills", "alpha"))
	initGitRepo(t, other)
	otherPrefix := "https://github.com/cathrynlavery/diagram-design"
	pointGitHubAt(t, other, otherPrefix)
	code, _, errOut = runCLIStreams(t, library, "install", "--package", "axton-obsidian-visual", otherPrefix)
	if code != 2 || errOut != "同名技能来源不同: axton-obsidian-visual/alpha\n" {
		t.Fatalf("code %d err %q", code, errOut)
	}
	text, err = os.ReadFile(filepath.Join(library, "axton-obsidian-visual", "alpha", "SKILL.md"))
	if err != nil || !bytes.Contains(text, []byte("again")) {
		t.Fatalf("overwritten %s", text)
	}
}

func TestLocalInstallSkipsUpgradeAddress(t *testing.T) {
	root, _, library := withHome(t)
	source := filepath.Join(root, "sources", "demo")
	writeSkill(t, filepath.Join(source, "one"))
	code, _, errOut := runCLIStreams(t, library, "install", "--package", "other", source)
	if code != 2 || errOut != "本地路径不接受 --package\n" {
		t.Fatalf("code %d err %q", code, errOut)
	}
	if code, output := runCLI(t, library, "install", source); code != 0 {
		t.Fatal(output)
	}
	code, out, errOut := runCLIStreams(t, library, "upgrade", "demo")
	if code != 0 || errOut != "" || out != "没有安装地址: demo:one\n" {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
}

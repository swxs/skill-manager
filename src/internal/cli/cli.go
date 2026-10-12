package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/swxs/skill-manager/internal/gitpack"
	"github.com/swxs/skill-manager/internal/jsonc"
	lib "github.com/swxs/skill-manager/internal/library"
	"github.com/swxs/skill-manager/internal/link"
	"github.com/swxs/skill-manager/internal/symlink"
)

func writeInstallLock(library, packName string, urls, hashes map[string]string, source gitpack.SourceURL, unlock bool) int {
	if unlock {
		return 0
	}
	overrides := map[string]map[string]string{}
	hashOverrides := map[string]map[string]string{}
	sources := map[string]gitpack.SourceURL{}
	if packName != "" && len(urls) > 0 {
		overrides[packName] = urls
	}
	if packName != "" && len(hashes) > 0 {
		hashOverrides[packName] = hashes
	}
	if packName != "" && source.URL != "" {
		sources[packName] = source
	}
	if err := gitpack.WriteLock(library, overrides, hashOverrides, sources, false); err != nil {
		fmt.Println(err.Error())
		return 1
	}
	fmt.Printf("lock: %s\n", gitpack.LockPath(library))
	return 0
}

func printInstalled(dest, packName, headline string, list bool) int {
	info, err := lib.Inspect(dest)
	if err != nil {
		fmt.Println(err.Error())
		return 1
	}
	fmt.Printf("%s %s (%s)\n", headline, packName, info.Kind)
	if list {
		for _, skill := range info.Skills {
			fmt.Printf("  - %s\n", skill)
		}
	}
	return 0
}

func refusePackLink(dest string) int {
	if symlink.Target(dest) == "" {
		return 0
	}
	fmt.Printf("拒绝在链接上安装: %s\n", dest)
	return 2
}

func installLocal(library, src, ref string, unlock bool) int {
	src = lib.Resolve(lib.ExpandUser(src))
	if !lib.IsDir(src) {
		fmt.Printf("目录不存在: %s\n", src)
		return 2
	}
	libraryResolved := lib.Resolve(library)
	if src == libraryResolved || strings.HasPrefix(src, libraryResolved+string(os.PathSeparator)) {
		fmt.Println("不能把库目录本身或库内目录再装进库")
		return 2
	}
	kind, err := lib.ClassifySource(src)
	if err != nil {
		fmt.Println(err.Error())
		return 2
	}
	packName := filepath.Base(src)
	if !lib.ValidName(packName) {
		fmt.Printf("非法包名: %s\n", packName)
		return 2
	}
	dest := filepath.Join(library, packName)
	if code := refusePackLink(dest); code != 0 {
		return code
	}
	existed := lib.IsDir(dest)
	if !existed {
		if err := os.MkdirAll(library, 0o755); err != nil {
			fmt.Println(err.Error())
			return 1
		}
		var copyErr error
		if kind == "single" {
			if err := os.Mkdir(dest, 0o755); err != nil {
				fmt.Println(err.Error())
				return 1
			}
			copyErr = lib.CopyInstall(src, filepath.Join(dest, packName))
		} else {
			copyErr = lib.CopyInstall(src, dest)
		}
		if copyErr != nil {
			if lib.Exists(dest) && symlink.Target(dest) == "" {
				_ = lib.RemoveTree(dest)
			}
			fmt.Println(copyErr.Error())
			return 1
		}
	}
	if ref != "" {
		if err := gitpack.CheckoutRef(dest, ref); err != nil {
			fmt.Println(err.Error())
			if !existed && lib.Exists(dest) && symlink.Target(dest) == "" {
				_ = lib.RemoveTree(dest)
			}
			return 2
		}
	}
	headline := "已安装"
	if existed && ref == "" {
		headline = "库里已有包"
	} else if existed {
		headline = "已检出"
	}
	if code := printInstalled(dest, packName, headline, true); code != 0 {
		return code
	}
	if existed && ref == "" {
		fmt.Println("跳过复制")
	}
	return writeInstallLock(library, packName, nil, nil, gitpack.SourceURL{}, unlock)
}

func installGit(library, raw, packName string, unlock bool) int {
	remote, err := gitpack.ParseRemote(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "无法从 URL 推导包名: %s\n", strings.TrimSpace(raw))
		return 2
	}
	explicit := packName != ""
	base := packName
	if !explicit {
		base, err = gitpack.PackageName(remote.Owner, remote.Repo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "无法从 URL 推导包名: %s\n", strings.TrimSpace(raw))
			return 2
		}
		packName = base
	}
	if !lib.ValidName(packName) {
		fmt.Fprintf(os.Stderr, "非法包名: %s\n", packName)
		return 2
	}
	cleanup, drops, kind, line := gitpack.Fetch(raw)
	defer cleanup()
	if kind != "" {
		fmt.Fprintln(os.Stderr, line)
		gitpack.AppendInstallLog(line)
		if kind == "fetch" {
			return 1
		}
		return 2
	}
	if !explicit && skillDirectory(remote, drops) && remote.SkillRel != "skills" {
		packName = base + "-" + remote.SkillName()
		if !lib.ValidName(packName) {
			fmt.Fprintf(os.Stderr, "非法包名: %s\n", packName)
			return 2
		}
	}
	dest := filepath.Join(library, packName)
	if code := refusePackLink(dest); code != 0 {
		return code
	}
	for _, drop := range drops {
		skillDir := filepath.Join(dest, drop.Name)
		if !lib.IsDir(skillDir) {
			continue
		}
		old := gitpack.SkillURL(library, packName, drop.Name)
		if !gitpack.SameURL(old, drop.URL) {
			fmt.Fprintf(os.Stderr, "同名技能来源不同: %s/%s\n", packName, drop.Name)
			return 2
		}
	}
	created := !lib.IsDir(dest)
	if created {
		if err := os.MkdirAll(dest, 0o755); err != nil {
			gitpack.AppendInstallLog("无法建立包目录: " + err.Error())
			fmt.Fprintf(os.Stderr, "无法取得技能: %s\n", strings.TrimSpace(raw))
			return 1
		}
	}
	many := len(drops) > 1
	added, swapErr := swapDrops(dest, packName, drops, many)
	if swapErr != nil {
		for _, name := range added {
			_ = lib.RemoveTree(filepath.Join(dest, name))
		}
		if created {
			_ = lib.RemoveTree(dest)
		}
		gitpack.AppendInstallLog("换入技能文件夹失败: " + swapErr.Error())
		fmt.Fprintf(os.Stderr, "无法取得技能: %s\n", strings.TrimSpace(raw))
		return 1
	}
	urls := map[string]string{}
	hashes := map[string]string{}
	for _, drop := range drops {
		urls[drop.Name] = drop.URL
		if drop.Hash != "" {
			hashes[drop.Name] = drop.Hash
		}
	}
	source := sourceOf(remote, drops)
	if many {
		if code := writeInstallLock(library, packName, urls, hashes, source, unlock); code != 0 {
			return code
		}
		return printInstalled(dest, packName, "已安装", false)
	}
	if code := printInstalled(dest, packName, "已安装", true); code != 0 {
		return code
	}
	return writeInstallLock(library, packName, urls, hashes, source, unlock)
}

func skillDirectory(remote gitpack.Remote, drops []gitpack.Drop) bool {
	if !remote.Skill || len(drops) == 0 {
		return false
	}
	parsed, err := gitpack.ParseRemote(drops[0].URL)
	if err != nil {
		return false
	}
	return parsed.SkillRel != remote.SkillRel
}

func sourceOf(remote gitpack.Remote, drops []gitpack.Drop) gitpack.SourceURL {
	branch := remote.Branch
	if len(drops) > 0 {
		if parsed, err := gitpack.ParseRemote(drops[0].URL); err == nil && parsed.Branch != "" {
			branch = parsed.Branch
		}
	}
	rel := remote.SkillRel
	kind := "pick"
	if !remote.Skill {
		rel = "skills"
		kind = "pack"
	} else if skillDirectory(remote, drops) {
		kind = "pack"
	}
	return gitpack.SourceURL{URL: remote.At(branch, rel), Kind: kind}
}

func swapTree(src, dest string) error {
	tmp := dest + ".incoming"
	_ = lib.RemoveTree(tmp)
	if err := lib.CopyInstall(src, tmp); err != nil {
		_ = lib.RemoveTree(tmp)
		return err
	}
	backup := dest + ".backup"
	had := lib.Exists(dest)
	if had {
		_ = lib.RemoveTree(backup)
		if err := os.Rename(dest, backup); err != nil {
			_ = lib.RemoveTree(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, dest); err != nil {
		if had {
			_ = os.Rename(backup, dest)
		}
		_ = lib.RemoveTree(tmp)
		return err
	}
	if had {
		_ = lib.RemoveTree(backup)
	}
	return nil
}

type placedSkill struct {
	name    string
	existed bool
	err     error
}

func swapDrops(dest, packName string, drops []gitpack.Drop, progress bool) (added []string, err error) {
	placed := make([]placedSkill, len(drops))
	var printMu sync.Mutex
	var wg sync.WaitGroup
	for i, drop := range drops {
		wg.Add(1)
		go func(i int, drop gitpack.Drop) {
			defer wg.Done()
			skillDir := filepath.Join(dest, drop.Name)
			existed := lib.IsDir(skillDir)
			swapErr := swapTree(drop.Dir, skillDir)
			placed[i] = placedSkill{name: drop.Name, existed: existed, err: swapErr}
			if swapErr == nil && progress {
				printMu.Lock()
				fmt.Printf("已安装 %s:%s\n", packName, drop.Name)
				printMu.Unlock()
			}
		}(i, drop)
	}
	wg.Wait()
	for _, item := range placed {
		if item.err != nil && err == nil {
			err = item.err
		}
		if item.err == nil && !item.existed {
			added = append(added, item.name)
		}
	}
	return added, err
}

func installSpec(library, spec, packName string, unlock bool) (code int) {
	gitpack.BeginInstallLog()
	defer gitpack.EndInstallLog()
	gitpack.AppendInstallLog("安装 " + strings.TrimSpace(spec))
	defer func() {
		gitpack.AppendInstallLog(fmt.Sprintf("结束 %d", code))
	}()
	url, ref := gitpack.ParseSpec(spec)
	local := lib.ExpandUser(url)
	if lib.IsDir(local) {
		if packName != "" {
			fmt.Fprintln(os.Stderr, "本地路径不接受 --package")
			gitpack.AppendInstallLog("本地路径不接受 --package")
			return 2
		}
		return installLocal(library, local, ref, unlock)
	}
	if _, err := gitpack.ParseRemote(url); err == nil {
		return installGit(library, url, packName, unlock)
	}
	if gitpack.IsURL(spec) || ref != "" {
		fmt.Fprintf(os.Stderr, "无法从 URL 推导包名: %s\n", strings.TrimSpace(url))
		gitpack.AppendInstallLog("无法从 URL 推导包名: " + strings.TrimSpace(url))
		return 2
	}
	fmt.Printf("目录不存在: %s\n", lib.Resolve(local))
	gitpack.AppendInstallLog("目录不存在: " + lib.Resolve(local))
	return 2
}

func loadConfigFile(path string) ([]string, error) {
	text, err := lib.ReadText(path)
	if err != nil {
		return nil, err
	}
	items, err := jsonc.ParseStrings(text)
	if err != nil {
		return nil, fmt.Errorf("%s %s", path, "必须是字符串数组")
	}
	return items, nil
}

func loadSelection(root string) ([]string, bool, error) {
	path := filepath.Join(root, ".agents", lib.ConfigName)
	if !lib.IsFile(path) {
		return nil, false, nil
	}
	items, err := loadConfigFile(path)
	return items, true, err
}

func globalCoverage(library string) (map[string]string, []string, error) {
	path, err := lib.GlobalConfigPath()
	if err != nil {
		return map[string]string{}, nil, err
	}
	if !lib.IsFile(path) {
		return map[string]string{}, nil, nil
	}
	entries, err := loadConfigFile(path)
	if err != nil {
		return nil, nil, err
	}
	desired, problems := link.Resolve(library, entries)
	return desired, problems, nil
}

func leadingComment(text string) string {
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		stripped := strings.TrimSpace(line)
		if stripped == "" || strings.HasPrefix(stripped, "//") {
			kept = append(kept, line)
			continue
		}
		break
	}
	return strings.TrimRight(strings.Join(kept, "\n"), "\n")
}

func writeSelection(path string, entries []string, createdComment string) error {
	comment := ""
	if lib.IsFile(path) {
		text, err := lib.ReadText(path)
		if err != nil {
			return err
		}
		comment = leadingComment(text)
	}
	if comment == "" {
		comment = createdComment
	}
	var body strings.Builder
	body.WriteString("[\n")
	for _, item := range entries {
		body.WriteString("  ")
		body.WriteString(jsonc.Quote(item))
		body.WriteString(",\n")
	}
	body.WriteString("]\n")
	text := body.String()
	if comment != "" {
		text = comment + "\n" + text
	}
	return lib.WriteLF(path, text)
}

func workspacePlan(library string, entries []string) (map[string]string, []string, map[string]string, error) {
	desired, problems := link.Resolve(library, entries)
	covered, globalProblems, err := globalCoverage(library)
	if err != nil {
		return nil, nil, nil, err
	}
	problems = append(globalProblems, problems...)
	overlap := map[string]string{}
	for name, target := range desired {
		if _, ok := covered[name]; ok {
			overlap[name] = target
			delete(desired, name)
		}
	}
	return desired, problems, overlap, nil
}

func printLibrary(library string) {
	fmt.Printf("库: %s\n", library)
	packs, _ := gitpack.PackDirs(library)
	if len(packs) == 0 {
		fmt.Println("  （空）")
		return
	}
	type row struct {
		name, kind, err string
		skills          []string
	}
	rows := make([]row, 0, len(packs))
	nameW, kindW := 0, 0
	for _, name := range packs {
		item := row{name: name}
		info, err := lib.Inspect(filepath.Join(library, name))
		if err != nil {
			item.err = err.Error()
		} else {
			item.kind = shownKind(library, name, info)
			item.skills = info.Skills
		}
		rows = append(rows, item)
		nameW = max(nameW, displayWidth(item.name))
		kindW = max(kindW, displayWidth(item.kind))
	}
	for _, item := range rows {
		if item.err != "" {
			fmt.Printf("  %s  %s\n", padRight(item.name, nameW), item.err)
			continue
		}
		fmt.Printf("  %s  %s\n", padRight(item.name, nameW), padRight(item.kind, kindW))
		if len(item.skills) == 0 {
			fmt.Println("    （无技能）")
			continue
		}
		for _, skill := range item.skills {
			fmt.Printf("    %s\n", skill)
		}
	}
}

func shownKind(library, name string, info lib.Pack) string {
	if kind := gitpack.EffectiveKind(library, name); kind != "" {
		return kind
	}
	return info.Kind
}

func displayWidth(s string) int {
	n := 0
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hangul, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || (r >= 0xFF01 && r <= 0xFF60) {
			n += 2
			continue
		}
		n++
	}
	return n
}

func padRight(s string, width int) string {
	if n := displayWidth(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}

func padLeft(s string, width int) string {
	if n := displayWidth(s); n < width {
		return strings.Repeat(" ", width-n) + s
	}
	return s
}

func cmdList(library, packName string) int {
	if !lib.IsDir(library) {
		fmt.Printf("skill 库不存在: %s\n", library)
		return 2
	}
	if packName == "" {
		packs, _ := gitpack.PackDirs(library)
		if len(packs) == 0 {
			fmt.Println("（空）")
			return 0
		}
		type row struct {
			name, kind, count string
		}
		rows := make([]row, 0, len(packs))
		nameW, kindW, countW := 0, 0, 0
		for _, name := range packs {
			info, _ := lib.Inspect(filepath.Join(library, name))
			item := row{name: name, kind: shownKind(library, name, info), count: strconv.Itoa(len(info.Skills))}
			rows = append(rows, item)
			nameW = max(nameW, displayWidth(item.name))
			kindW = max(kindW, displayWidth(item.kind))
			countW = max(countW, displayWidth(item.count))
		}
		for _, item := range rows {
			fmt.Printf("%s  %s  %s\n", padRight(item.name, nameW), padRight(item.kind, kindW), padLeft(item.count, countW))
		}
		return 0
	}
	if !lib.ValidName(packName) {
		fmt.Printf("非法包名: %s\n", packName)
		return 2
	}
	pack := filepath.Join(library, packName)
	if !lib.IsDir(pack) || symlink.Target(pack) != "" {
		fmt.Printf("库里没有包: %s\n", packName)
		return 1
	}
	info, _ := lib.Inspect(pack)
	fmt.Printf("%s  %s\n", packName, shownKind(library, packName, info))
	if len(info.Skills) == 0 {
		fmt.Println("  （无技能）")
		return 0
	}
	for _, skill := range info.Skills {
		fmt.Printf("  %s\n", skill)
	}
	return 0
}

func printDeclaration(title, path string) int {
	fmt.Printf("%s: %s\n", title, path)
	if !lib.IsFile(path) {
		fmt.Println("未找到技能声明")
		return 0
	}
	entries, err := loadConfigFile(path)
	if err != nil {
		fmt.Printf("配置无法读取: %s\n", err.Error())
		return 2
	}
	if len(entries) == 0 {
		fmt.Println("  （空）")
		return 0
	}
	for _, entry := range entries {
		fmt.Printf("- %s\n", entry)
	}
	return 0
}

func reportGlobal(library string, write bool) int {
	path, err := lib.GlobalConfigPath()
	if err != nil {
		fmt.Println(err.Error())
		return 2
	}
	target, _ := lib.GlobalSkillsDir()
	home, _ := lib.AgentHome()
	fmt.Printf("全局工作区: %s\n", home)
	if !lib.IsFile(path) {
		return 0
	}
	entries, err := loadConfigFile(path)
	if err != nil {
		fmt.Printf("配置无法读取: %s\n", err.Error())
		return 2
	}
	desired, problems := link.Resolve(library, entries)
	return link.Apply(home, desired, problems, write, nil, target, false, library)
}

func cmdStatus(library string, roots []string) int {
	path, err := lib.GlobalConfigPath()
	if err != nil {
		fmt.Println(err.Error())
		return 2
	}
	code := printDeclaration("全局技能声明", path)
	fmt.Println()
	if next := reportGlobal(library, false); next > code {
		code = next
	}
	for _, root := range roots {
		if isUserHome(root) {
			continue
		}
		fmt.Println()
		resolved := lib.Resolve(root)
		config := filepath.Join(resolved, ".agents", lib.ConfigName)
		if next := printDeclaration("工作区技能声明", config); next != 0 {
			if next > code {
				code = next
			}
			continue
		}
		entries, ok, err := loadSelection(resolved)
		if err != nil {
			fmt.Printf("配置无法读取: %s\n", err.Error())
			code = 2
			continue
		}
		fmt.Println()
		fmt.Printf("工作区: %s\n", filepath.Join(resolved, ".agents"))
		if !ok {
			continue
		}
		desired, problems, covered, err := workspacePlan(library, entries)
		if err != nil {
			fmt.Printf("global 配置无法读取: %s\n", err.Error())
			code = 2
			continue
		}
		if next := link.Apply(resolved, desired, problems, false, covered, "", true, library); next > code {
			code = next
		}
	}
	return code
}

func isUserHome(root string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	return lib.SamePath(root, home)
}

func cmdSync(library string, roots []string) int {
	code := 0
	syncedGlobal := false
	for index, root := range roots {
		if isUserHome(root) {
			if syncedGlobal {
				continue
			}
			syncedGlobal = true
			if index > 0 {
				fmt.Println()
			}
			if next := cmdSyncGlobal(library); next > code {
				code = next
			}
			continue
		}
		if index > 0 {
			fmt.Println()
		}
		fmt.Printf("根目录: %s\n", lib.Resolve(root))
		if !lib.IsDir(root) {
			fmt.Printf("根目录不存在: %s\n", root)
			code = 2
			continue
		}
		config := filepath.Join(root, ".agents", lib.ConfigName)
		fmt.Printf("配置: %s\n", config)
		entries, ok, err := loadSelection(root)
		if err != nil {
			fmt.Printf("配置无法读取: %s\n", err.Error())
			code = 2
			continue
		}
		if !ok {
			fmt.Println("未找到 .agents/skills.json。请写入字符串数组后再 sync。")
			code = 2
			continue
		}
		desired, problems, covered, err := workspacePlan(library, entries)
		if err != nil {
			fmt.Printf("global 配置无法读取: %s\n", err.Error())
			code = 2
			continue
		}
		if next := link.Apply(root, desired, problems, true, covered, "", true, library); next > code {
			code = next
		}
	}
	return code
}

func cmdSyncGlobal(library string) int {
	path, err := lib.GlobalConfigPath()
	if err != nil {
		fmt.Println(err.Error())
		return 2
	}
	target, _ := lib.GlobalSkillsDir()
	fmt.Printf("global 配置: %s\n", path)
	fmt.Printf("global 目标: %s\n", target)
	if !lib.IsFile(path) {
		fmt.Println("未找到 global skills.json。请先 add --global。")
		return 2
	}
	entries, err := loadConfigFile(path)
	if err != nil {
		fmt.Printf("配置无法读取: %s\n", err.Error())
		return 2
	}
	desired, problems := link.Resolve(library, entries)
	home, _ := lib.AgentHome()
	return link.Apply(home, desired, problems, true, nil, target, false, library)
}

func printProblems(problems []string) {
	for _, line := range problems {
		if strings.HasPrefix(line, "!") {
			fmt.Println(line)
		} else {
			fmt.Println("! " + line)
		}
	}
	if link.Contains(problems, "! 库中没有包") {
		fmt.Println(lib.MissingPackHint)
	}
}

func rejectRemoteSpecs(names []string) int {
	for _, name := range names {
		if gitpack.IsURL(name) {
			fmt.Printf("不接收 Git URL: %s\n请改用 install\n", name)
			return 2
		}
	}
	return 0
}

func validateNames(library string, names []string) int {
	if code := rejectRemoteSpecs(names); code != 0 {
		fmt.Println("未修改配置")
		return code
	}
	var problems []string
	for _, name := range names {
		_, more := link.Resolve(library, []string{name})
		problems = append(problems, more...)
	}
	if len(problems) > 0 {
		printProblems(problems)
		fmt.Println("未修改配置")
		return 1
	}
	return 0
}

func selectionFile(root string, useGlobal bool) (string, string, []string, int) {
	if useGlobal {
		path, err := lib.GlobalConfigPath()
		if err != nil {
			fmt.Println(err.Error())
			return "", "", nil, 1
		}
		comment := "// global 技能。运行 /skill-manager sync --global。"
		if !lib.IsFile(path) {
			return path, comment, nil, 0
		}
		existing, err := loadConfigFile(path)
		if err != nil {
			fmt.Println(err.Error())
			return "", "", nil, 1
		}
		return path, comment, existing, 0
	}
	if !lib.IsDir(root) {
		fmt.Printf("根目录不存在: %s\n", root)
		return "", "", nil, 2
	}
	path := filepath.Join(root, ".agents", lib.ConfigName)
	comment := "// 启用的技能包。包名加载整包，包名:技能名 只加载一个。运行 /skill-manager sync。"
	entries, ok, err := loadSelection(root)
	if err != nil {
		fmt.Println(err.Error())
		return "", "", nil, 1
	}
	if !ok {
		return path, comment, nil, 0
	}
	return path, comment, entries, 0
}

func cmdAdd(library string, names []string, root string, useGlobal bool) int {
	if code := validateNames(library, names); code != 0 {
		return code
	}
	path, comment, existing, code := selectionFile(root, useGlobal)
	if code != 0 {
		return code
	}
	current := append([]string{}, existing...)
	seen := map[string]bool{}
	for _, item := range current {
		seen[lib.CaseFold(item)] = true
	}
	var added []string
	for _, name := range names {
		if seen[lib.CaseFold(name)] {
			fmt.Printf("已存在 %s\n", name)
			continue
		}
		current = append(current, name)
		seen[lib.CaseFold(name)] = true
		added = append(added, name)
	}
	fmt.Printf("配置: %s\n", path)
	if len(added) == 0 {
		fmt.Println("没有新条目")
		return 0
	}
	if err := writeSelection(path, current, comment); err != nil {
		fmt.Println(err.Error())
		return 1
	}
	for _, name := range added {
		fmt.Printf("  + %s\n", name)
	}
	return 0
}

func cmdRemove(names []string, root string, useGlobal bool) int {
	if code := rejectRemoteSpecs(names); code != 0 {
		fmt.Println("未修改配置")
		return code
	}
	path, comment, existing, code := selectionFile(root, useGlobal)
	if code != 0 {
		return code
	}
	present := map[string]bool{}
	for _, item := range existing {
		present[lib.CaseFold(item)] = true
	}
	drop := map[string]bool{}
	var missing []string
	for _, name := range names {
		key := lib.CaseFold(name)
		if !present[key] {
			missing = append(missing, name)
			continue
		}
		drop[key] = true
	}
	if len(missing) > 0 {
		for _, name := range missing {
			fmt.Printf("没有该条目 %s\n", name)
		}
		fmt.Println("未修改配置")
		return 1
	}
	var kept []string
	for _, item := range existing {
		if drop[lib.CaseFold(item)] {
			continue
		}
		kept = append(kept, item)
	}
	if err := writeSelection(path, kept, comment); err != nil {
		fmt.Println(err.Error())
		return 1
	}
	fmt.Printf("配置: %s\n", path)
	for _, name := range names {
		fmt.Printf("  - %s\n", name)
	}
	return 0
}

func cmdLock(library string) int {
	if !lib.IsDir(library) {
		fmt.Printf("skill 库不存在: %s\n", library)
		return 2
	}
	if err := gitpack.WriteLock(library, nil, nil, nil, true); err != nil {
		fmt.Println(err.Error())
		return 1
	}
	fmt.Printf("已按磁盘重写 %s\n", gitpack.LockPath(library))
	printLibrary(library)
	return 0
}

func upgradePack(library, spec string) int {
	packName, skillName, one := splitUpgrade(spec)
	if !lib.ValidName(packName) || (one && !lib.ValidName(skillName)) {
		fmt.Printf("非法包名: %s\n", spec)
		return 2
	}
	dest := filepath.Join(library, packName)
	if !lib.IsDir(dest) || symlink.Target(dest) != "" {
		fmt.Fprintf(os.Stderr, "没有这个技能: %s\n", spec)
		return 2
	}
	info, err := lib.Inspect(dest)
	if err != nil {
		fmt.Println(err.Error())
		return 1
	}
	targets := info.Skills
	if one {
		found := false
		for _, name := range info.Skills {
			if name == skillName {
				found = true
				break
			}
		}
		if !found {
			fmt.Fprintf(os.Stderr, "没有这个技能: %s\n", spec)
			return 2
		}
		targets = []string{skillName}
	}
	type upgradeOutcome struct {
		label     string
		url       string
		hash      string
		missing   bool
		unchanged bool
		failed    bool
	}
	outcomes := make([]upgradeOutcome, len(targets))
	for i, name := range targets {
		url := gitpack.SkillURL(library, packName, name)
		outcomes[i] = upgradeOutcome{label: packName + ":" + name, url: url, missing: url == ""}
	}
	for i := range outcomes {
		if outcomes[i].missing {
			fmt.Printf("没有安装地址: %s\n", outcomes[i].label)
		}
	}
	grouped := map[string][]gitpack.SkillWant{}
	var refOrder []gitpack.CloneRef
	for i, name := range targets {
		if outcomes[i].missing {
			continue
		}
		remote, err := gitpack.ParseRemote(outcomes[i].url)
		if err != nil || remote.SkillRel == "" {
			outcomes[i].failed = true
			fmt.Fprintf(os.Stderr, "无法取得技能: %s\n", outcomes[i].url)
			continue
		}
		ref, ok := gitpack.RefOf(outcomes[i].url)
		if !ok {
			outcomes[i].failed = true
			fmt.Fprintf(os.Stderr, "无法取得技能: %s\n", outcomes[i].url)
			continue
		}
		key := ref.Clone + "\n" + ref.Branch
		if _, seen := grouped[key]; !seen {
			refOrder = append(refOrder, ref)
		}
		grouped[key] = append(grouped[key], gitpack.SkillWant{
			Name:    name,
			Rel:     remote.SkillRel,
			URL:     outcomes[i].url,
			OldHash: gitpack.SkillHash(library, packName, name),
		})
	}
	discover := map[string][]string{}
	if !one {
		for _, source := range gitpack.PackSources(library, packName) {
			if source.Kind != "pack" {
				continue
			}
			remote, err := gitpack.ParseRemote(source.URL)
			if err != nil {
				continue
			}
			ref, ok := gitpack.RefOf(source.URL)
			if !ok {
				continue
			}
			key := ref.Clone + "\n" + ref.Branch
			if _, seen := grouped[key]; !seen {
				grouped[key] = nil
				refOrder = append(refOrder, ref)
			}
			discover[key] = append(discover[key], remote.SkillRel)
		}
	}
	var printMu sync.Mutex
	var wg sync.WaitGroup
	failed := false
	updated := 0
	freshHashes := map[string]string{}
	newURLs := map[string]string{}
	var tallyMu sync.Mutex
	for _, ref := range refOrder {
		wg.Add(1)
		go func(ref gitpack.CloneRef) {
			defer wg.Done()
			key := ref.Clone + "\n" + ref.Branch
			cleanup, unchanged, brought, err := gitpack.Bring(ref, grouped[key], discover[key])
			defer cleanup()
			printMu.Lock()
			defer printMu.Unlock()
			if err != nil {
				for _, want := range grouped[key] {
					fmt.Fprintf(os.Stderr, "无法取得技能: %s\n", want.URL)
				}
				tallyMu.Lock()
				failed = true
				tallyMu.Unlock()
				return
			}
			for _, name := range unchanged {
				fmt.Printf("未变化 %s:%s\n", packName, name)
			}
			tallyMu.Lock()
			defer tallyMu.Unlock()
			for _, item := range brought {
				if err := swapTree(item.Dir, filepath.Join(dest, item.Name)); err != nil {
					fmt.Fprintf(os.Stderr, "无法取得技能: %s\n", item.URL)
					failed = true
					continue
				}
				updated++
				if item.Hash != "" {
					freshHashes[item.Name] = item.Hash
				}
				if item.Fresh {
					newURLs[item.Name] = item.URL
					fmt.Printf("已安装 %s:%s\n", packName, item.Name)
					continue
				}
				if packName+":"+item.Name != spec {
					fmt.Printf("已升级 %s:%s\n", packName, item.Name)
				}
			}
		}(ref)
	}
	wg.Wait()
	if updated > 0 {
		if code := writeInstallLock(library, packName, newURLs, freshHashes, gitpack.SourceURL{}, false); code != 0 {
			return code
		}
	}
	if failed {
		return 1
	}
	if updated > 0 {
		fmt.Printf("已升级 %s\n", spec)
	}
	return 0
}

func splitUpgrade(spec string) (string, string, bool) {
	pack, skill, ok := strings.Cut(spec, ":")
	if !ok {
		return spec, "", false
	}
	return pack, skill, true
}

func refreshSkill(pack, name, raw string) (string, error) {
	cleanup, drops, kind, _ := gitpack.Fetch(raw)
	defer cleanup()
	if kind != "" || len(drops) != 1 {
		return "", fmt.Errorf("fetch")
	}
	if err := swapTree(drops[0].Dir, filepath.Join(pack, name)); err != nil {
		return "", err
	}
	return drops[0].Hash, nil
}

func initSelectionEntries(library, skillsFolder string) ([]string, []string, error) {
	libraryResolved := lib.Resolve(library)
	byPack := map[string]map[string]bool{}
	var realInstalls []looseDir
	if lib.IsDir(skillsFolder) {
		entries, err := os.ReadDir(skillsFolder)
		if err != nil {
			return nil, nil, err
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		lib.SortFold(names)
		for _, name := range names {
			if strings.HasPrefix(name, ".") {
				continue
			}
			entryPath := filepath.Join(skillsFolder, name)
			target := symlink.Target(entryPath)
			if target != "" {
				resolvedTarget := lib.Resolve(target)
				rel, err := filepath.Rel(libraryResolved, resolvedTarget)
				if err != nil || strings.HasPrefix(rel, "..") {
					if lib.IsDir(resolvedTarget) {
						realInstalls = append(realInstalls, looseDir{src: resolvedTarget})
					}
					continue
				}
				parts := strings.Split(rel, string(os.PathSeparator))
				packName := parts[0]
				skillKey := packName
				if len(parts) >= 2 {
					skillKey = parts[1]
				}
				if byPack[packName] == nil {
					byPack[packName] = map[string]bool{}
				}
				byPack[packName][skillKey] = true
				continue
			}
			realInstalls = append(realInstalls, looseDir{src: lib.Resolve(entryPath), replace: entryPath})
		}
	}
	var replace []string
	for _, item := range realInstalls {
		if !lib.IsDir(item.src) {
			continue
		}
		if _, err := lib.ClassifySource(item.src); err != nil {
			continue
		}
		packName := filepath.Base(item.src)
		if !lib.ValidName(packName) {
			continue
		}
		dest := filepath.Join(library, packName)
		if !lib.IsDir(dest) && symlink.Target(dest) == "" {
			if code := installLocal(library, item.src, "", false); code != 0 {
				return nil, nil, fmt.Errorf("未能收进技能库: %s", packName)
			}
		}
		if byPack[packName] == nil {
			byPack[packName] = map[string]bool{}
		}
		if item.replace != "" {
			replace = append(replace, item.replace)
		}
	}
	packNames := make([]string, 0, len(byPack))
	for name := range byPack {
		packNames = append(packNames, name)
	}
	lib.SortFold(packNames)
	var entries []string
	for _, packName := range packNames {
		pack := filepath.Join(library, packName)
		if !lib.IsDir(pack) {
			continue
		}
		info, err := lib.Inspect(pack)
		if err != nil {
			return nil, nil, err
		}
		linked := byPack[packName]
		if info.Kind == "mixed" {
			entries = append(entries, packName)
			continue
		}
		skillNames := map[string]bool{}
		for _, skill := range info.Skills {
			skillNames[skill] = true
		}
		coversAll := len(linked) > 0
		for skill := range skillNames {
			if !linked[skill] {
				coversAll = false
				break
			}
		}
		if coversAll {
			entries = append(entries, packName)
			continue
		}
		if len(linked) > 0 {
			skills := make([]string, 0, len(linked))
			for skill := range linked {
				skills = append(skills, skill)
			}
			lib.SortFold(skills)
			for _, skill := range skills {
				entries = append(entries, packName+":"+skill)
			}
			continue
		}
		entries = append(entries, packName)
	}
	return entries, replace, nil
}

type looseDir struct {
	src     string
	replace string
}

func cmdInit(library string) int {
	skillsFolder, err := lib.GlobalSkillsDir()
	if err != nil {
		fmt.Println(err.Error())
		return 1
	}
	fmt.Printf("init global: %s\n", skillsFolder)
	fmt.Printf("库: %s\n", library)
	entries, replace, err := initSelectionEntries(library, skillsFolder)
	if err != nil {
		fmt.Println(err.Error())
		return 1
	}
	path, err := lib.GlobalConfigPath()
	if err != nil {
		fmt.Println(err.Error())
		return 1
	}
	comment := "// global 技能。运行 /skill-manager sync --global。"
	if err := writeSelection(path, entries, comment); err != nil {
		fmt.Println(err.Error())
		return 1
	}
	fmt.Printf("配置: %s\n", path)
	for _, entry := range entries {
		fmt.Printf("  + %s\n", entry)
	}
	if err := gitpack.WriteLock(library, nil, nil, nil, false); err != nil {
		fmt.Println(err.Error())
		return 1
	}
	fmt.Printf("lock: %s\n", gitpack.LockPath(library))
	for _, dir := range replace {
		if err := lib.RemoveTree(dir); err != nil {
			fmt.Println(err.Error())
			return 1
		}
	}
	return cmdSyncGlobal(library)
}

func decodeLockPacks(path string) (map[string]any, error) {
	text, err := lib.ReadText(path)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		return nil, err
	}
	packs, _ := data["packs"].(map[string]any)
	return packs, nil
}

func Main(argv []string) int {
	library := ""
	var rest []string
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if arg == "--library" {
			if i+1 >= len(argv) {
				fmt.Fprintln(os.Stderr, "缺少 --library 的值")
				return 2
			}
			library = argv[i+1]
			i++
			continue
		}
		rest = append(rest, arg)
	}
	if library == "" {
		var err error
		library, err = lib.DefaultLibrary()
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			return 2
		}
	}
	library = lib.ExpandUser(library)
	if len(rest) == 0 {
		return cmdStatus(library, []string{getwd()})
	}
	cmd := rest[0]
	args := rest[1:]
	if cmd == "-h" || cmd == "--help" {
		printHelp()
		return 0
	}
	if wantsHelp(args) {
		return printCommandHelp(cmd)
	}
	switch cmd {
	case "install":
		unlock, packName, spec, errCode := parseInstall(args)
		if errCode != 0 {
			return errCode
		}
		return installSpec(library, spec, packName, unlock)
	case "package-name":
		return cmdPackageName(args)
	case "remove":
		names, root, global, errCode := parseSelection(args)
		if errCode != 0 {
			return errCode
		}
		if root == "" {
			root = getwd()
		}
		return cmdRemove(names, root, global)
	case "lock":
		return cmdLock(library)
	case "list":
		name := ""
		if len(args) == 1 {
			name = args[0]
		} else if len(args) > 1 {
			fmt.Fprintln(os.Stderr, "list 最多一个包名")
			return 2
		}
		return cmdList(library, name)
	case "search":
		return cmdSearch(library, args)
	case "sync":
		roots, global, errCode := parseRoots(args, true)
		if errCode != 0 {
			return errCode
		}
		if global {
			if len(roots) > 0 {
				fmt.Println("sync --global 不使用 --root")
			}
			return cmdSyncGlobal(library)
		}
		if len(roots) == 0 {
			roots = []string{getwd()}
		}
		return cmdSync(library, roots)
	case "add":
		names, root, global, errCode := parseSelection(args)
		if errCode != 0 {
			return errCode
		}
		if root == "" {
			root = getwd()
		}
		return cmdAdd(library, names, root, global)
	case "status":
		roots, _, errCode := parseRoots(args, false)
		if errCode != 0 {
			return errCode
		}
		if len(roots) == 0 {
			roots = []string{getwd()}
		}
		return cmdStatus(library, roots)
	case "upgrade":
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "upgrade 需要一个包名或包名:技能名")
			return 2
		}
		return upgradePack(library, args[0])
	case "init":
		return cmdInit(library)
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n", cmd)
		return 2
	}
}

func parseRoots(args []string, allowGlobal bool) ([]string, bool, int) {
	var roots []string
	global := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--root":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "缺少 --root 的值")
				return nil, false, 2
			}
			roots = append(roots, args[i+1])
			i++
		case "--global":
			if !allowGlobal {
				fmt.Fprintln(os.Stderr, "该命令不接受 --global")
				return nil, false, 2
			}
			global = true
		default:
			fmt.Fprintf(os.Stderr, "无法识别的参数: %s\n", args[i])
			return nil, false, 2
		}
	}
	return roots, global, 0
}

func parseInstall(args []string) (bool, string, string, int) {
	unlock := false
	packName := ""
	var specs []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--unlock":
			unlock = true
		case "--package":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				fmt.Fprintln(os.Stderr, "缺少 --package 的值")
				return false, "", "", 2
			}
			packName = args[i+1]
			i++
		default:
			if strings.HasPrefix(args[i], "-") {
				fmt.Fprintf(os.Stderr, "无法识别的参数: %s\n", args[i])
				return false, "", "", 2
			}
			specs = append(specs, args[i])
		}
	}
	if len(specs) != 1 {
		fmt.Fprintln(os.Stderr, "install 需要一个目录或 Git URL")
		return false, "", "", 2
	}
	return unlock, packName, specs[0], 0
}

func cmdPackageName(args []string) int {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(os.Stderr, "package-name 需要一个 Git 地址")
		return 2
	}
	remote, err := gitpack.ParseRemote(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "无法从 URL 推导包名: %s\n", strings.TrimSpace(args[0]))
		return 2
	}
	name, err := gitpack.PackageName(remote.Owner, remote.Repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "无法从 URL 推导包名: %s\n", strings.TrimSpace(args[0]))
		return 2
	}
	fmt.Println(name)
	return 0
}

func parseSelection(args []string) ([]string, string, bool, int) {
	var names []string
	root := ""
	global := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--root":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "缺少 --root 的值")
				return nil, "", false, 2
			}
			root = args[i+1]
			i++
		case "--global":
			global = true
		default:
			if strings.HasPrefix(args[i], "-") {
				fmt.Fprintf(os.Stderr, "无法识别的参数: %s\n", args[i])
				return nil, "", false, 2
			}
			names = append(names, args[i])
		}
	}
	if len(names) == 0 {
		fmt.Fprintln(os.Stderr, "至少需要一个包名或包名:技能名")
		return nil, "", false, 2
	}
	return names, root, global, 0
}

func getwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func wantsHelp(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

func printHelp() {
	fmt.Println(`用法:
  skill-manager [--library <路径>] <命令>

命令:
  init          初始化技能管理体系
  list          查看当前技能库
  search        从收集站查找技能
  install       安装技能到技能库
  package-name  从 Git 地址推导包名
  upgrade       更新技能库中的技能
  lock          锁定技能库信息
  status        查看全局工作区, 工作区技能状态
  add           添加技能声明
  remove        移除技能声明
  sync          按技能声明同步技能`)
}

func printCommandHelp(cmd string) int {
	text, ok := commandHelp[cmd]
	if !ok {
		fmt.Fprintf(os.Stderr, "未知命令: %s\n", cmd)
		return 2
	}
	fmt.Println(text)
	return 0
}

var commandHelp = map[string]string{
	"init": `初始化技能管理体系

扫描全局工作区里已有的技能，收进技能库，写好技能声明，再链回全局工作区。库里已有同名包时不覆盖技能库，链接指向库里原来的包。

用法:
  skill-manager init`,
	"list": `查看当前技能库

列出技能库里的包。写出包名时，列出该包里的技能。

用法:
  skill-manager list [包名]`,
	"search": `从收集站查找技能

先问 SkillsMP。没有收录、检索词过短、配额用尽或不可用时再问 ModelScope。只列出，不安装。

用法:
  skill-manager search <检索词>`,
	"install": `安装技能到技能库

本机目录按原样复制。Git 地址在 /tree/<分支>/ 或 /-/tree/<分支>/ 之后还有路径时，只复制那一个技能文件夹。只到分支时，只取仓库 skills/ 的直接子目录。本机有 git 时浅克隆并只检出这些目录，也可以装 GitHub 以外的 https 仓库。没有 git 时只从 GitHub 取这些目录。包里不留下 .git。过程追加写到系统临时目录的 skill-manager-install.log。

用法:
  skill-manager install [--unlock] [--package <包名>] <本机目录或 Git URL>

--package
  指定包名。省略时从 Git 地址推导。本机目录不接受此旗标。

--unlock
  跳过锁定。本机目录带 #ref 时检出仍然做。`,
	"package-name": `从 Git 地址推导包名

只把包名写到标准输出。不安装，不建目录，不写锁定。

用法:
  skill-manager package-name <Git地址>`,
	"upgrade": `更新技能库中的技能

按锁定里的安装地址比较 Git 树哈希。相同则不下载文件。没有哈希或树哈希不同则下载并覆盖。没有地址的跳过。不读包里的 .git。

用法:
  skill-manager upgrade <包名>
  skill-manager upgrade <包名:技能名>`,
	"lock": `锁定技能库信息

按磁盘写下技能库索引。有安装地址的技能文件夹写上 Git 树哈希。

用法:
  skill-manager lock`,
	"status": `查看全局工作区, 工作区技能状态

按这个顺序查看：全局技能声明，全局工作区，工作区技能声明，工作区。根目录是用户主目录时，后两节不重复打印。

用法:
  skill-manager status [--root <工作区>]...

--root <工作区>
  查看该工作区的技能状态。可重复。不写则用当前目录。不接受 --global。`,
	"add": `添加技能声明

向指定工作区的技能声明中添加技能。技能须已在技能库中。可写多个；有一条不合法则全部不写。不创建链接。

用法:
  skill-manager add [--root <工作区>] [--global] <包名或包名:技能名>...

--root <工作区>
  写入该工作区的技能声明。不写则用当前目录。

--global
  写入全局工作区的技能声明。与 --root 同时出现时以全局工作区为准。`,
	"remove": `移除技能声明

从指定工作区的技能声明中移除技能。不连带删掉「包名:技能名」。库里没有该包也能移除。可写多个；有一条对不上或不合法则全部不改。不删技能库里的包，也不改链接。

用法:
  skill-manager remove [--root <工作区>] [--global] <包名或包名:技能名>...

--root <工作区>
  从该工作区的技能声明中移除。不写则用当前目录。

--global
  从全局工作区的技能声明中移除。与 --root 同时出现时以全局工作区为准。`,
	"sync": `按技能声明同步技能

按指定工作区的技能声明，把技能库中的技能链到该工作区。技能声明不存在则失败，不建链接。根目录是用户主目录时，按全局工作区同步。

用法:
  skill-manager sync [--root <工作区>]... [--global]

--root <工作区>
  按该工作区的技能声明同步技能。可重复。不写则用当前目录。

--global
  按全局工作区的技能声明同步技能。与 --root 同时出现时以全局工作区为准。`,
}

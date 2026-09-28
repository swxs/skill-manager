package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/swxs/skill-manager/internal/gitpack"
	"github.com/swxs/skill-manager/internal/jsonc"
	lib "github.com/swxs/skill-manager/internal/library"
	"github.com/swxs/skill-manager/internal/link"
	"github.com/swxs/skill-manager/internal/symlink"
)

func installPack(library, src string) int {
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
	if lib.Exists(dest) || symlink.Target(dest) != "" {
		fmt.Printf("库里已有包: %s\n", packName)
		return 1
	}
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
	fallback := src
	if gitRoot := gitpack.Root(dest); gitRoot != "" {
		meta := gitpack.Metadata(gitRoot, fallback)
		if srcMeta, ok := meta["source"]; ok {
			fallback = srcMeta
		}
	}
	if err := gitpack.WriteLock(library, map[string]string{packName: fallback}); err != nil {
		fmt.Println(err.Error())
		return 1
	}
	info, err := lib.Inspect(dest)
	if err != nil {
		fmt.Println(err.Error())
		return 1
	}
	fmt.Printf("已安装 %s (%s)\n", packName, info.Kind)
	for _, skill := range info.Skills {
		fmt.Printf("  - %s\n", skill)
	}
	fmt.Printf("lock: %s\n", gitpack.LockPath(library))
	return 0
}

func removePack(library, packName string) int {
	if !lib.ValidName(packName) {
		fmt.Printf("非法包名: %s\n", packName)
		return 2
	}
	dest := filepath.Join(library, packName)
	if symlink.Target(dest) != "" {
		fmt.Printf("拒绝删除链接: %s\n", dest)
		return 2
	}
	if !lib.IsDir(dest) {
		fmt.Printf("库里没有包: %s\n", packName)
		return 1
	}
	resolved := lib.Resolve(dest)
	libraryResolved := lib.Resolve(library)
	sep := string(os.PathSeparator)
	if !strings.HasPrefix(resolved, libraryResolved+sep) {
		fmt.Printf("拒绝删除库以外的路径: %s\n", dest)
		return 2
	}
	if err := lib.RemoveTree(dest); err != nil {
		fmt.Println(err.Error())
		return 1
	}
	if err := gitpack.WriteLock(library, nil); err != nil {
		fmt.Println(err.Error())
		return 1
	}
	fmt.Printf("已从库中删除 %s\n", packName)
	fmt.Println("仓库里的链接要再跑 link 才会去掉")
	return 0
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

func globalCoverage(library string) (map[string]bool, []string, error) {
	path, err := lib.GlobalConfigPath()
	if err != nil {
		return map[string]bool{}, nil, err
	}
	if !lib.IsFile(path) {
		return map[string]bool{}, nil, nil
	}
	entries, err := loadConfigFile(path)
	if err != nil {
		return nil, nil, err
	}
	desired, problems := link.Resolve(library, entries)
	covered := map[string]bool{}
	for name := range desired {
		covered[name] = true
	}
	return covered, problems, nil
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

func workspacePlan(library string, entries []string) (map[string]string, []string, map[string]bool, error) {
	desired, problems := link.Resolve(library, entries)
	covered, globalProblems, err := globalCoverage(library)
	if err != nil {
		return nil, nil, nil, err
	}
	problems = append(globalProblems, problems...)
	for name := range desired {
		if covered[name] {
			delete(desired, name)
		}
	}
	return desired, problems, covered, nil
}

func printLibrary(library string) {
	fmt.Printf("库: %s\n", library)
	packs, _ := gitpack.PackDirs(library)
	if len(packs) == 0 {
		fmt.Println("  （空）")
		return
	}
	for _, name := range packs {
		info, err := lib.Inspect(filepath.Join(library, name))
		if err != nil {
			fmt.Printf("  %s  %s\n", name, err.Error())
			continue
		}
		preview := info.Skills
		extra := ""
		if len(preview) > 8 {
			extra = fmt.Sprintf(" +%d", len(preview)-8)
			preview = preview[:8]
		}
		fmt.Printf("  %s  %s  %s%s\n", name, info.Kind, strings.Join(preview, ", "), extra)
	}
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
		for _, name := range packs {
			info, _ := lib.Inspect(filepath.Join(library, name))
			fmt.Printf("%s  %s  %d\n", name, info.Kind, len(info.Skills))
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
	fmt.Printf("%s  %s\n", packName, info.Kind)
	if len(info.Skills) == 0 {
		fmt.Println("  （无技能）")
		return 0
	}
	for _, skill := range info.Skills {
		fmt.Printf("  %s\n", skill)
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
	fmt.Println()
	fmt.Printf("global 配置: %s\n", path)
	fmt.Printf("global 目标: %s\n", target)
	if !lib.IsFile(path) {
		fmt.Println("未找到 global skills.json")
		return 0
	}
	entries, err := loadConfigFile(path)
	if err != nil {
		fmt.Printf("配置无法读取: %s\n", err.Error())
		return 2
	}
	desired, problems := link.Resolve(library, entries)
	home, _ := lib.AgentHome()
	return link.Apply(home, desired, problems, write, nil, target, false)
}

func cmdStatus(library string, roots []string) int {
	printLibrary(library)
	code := reportGlobal(library, false)
	for _, root := range roots {
		fmt.Println()
		resolved := lib.Resolve(root)
		fmt.Printf("根目录: %s\n", resolved)
		config := filepath.Join(root, ".agents", lib.ConfigName)
		fmt.Printf("配置: %s\n", config)
		entries, ok, err := loadSelection(root)
		if err != nil {
			fmt.Printf("配置无法读取: %s\n", err.Error())
			code = 2
			continue
		}
		if !ok {
			fmt.Println("未找到 .agents/skills.json")
			continue
		}
		desired, problems, covered, err := workspacePlan(library, entries)
		if err != nil {
			fmt.Printf("global 配置无法读取: %s\n", err.Error())
			code = 2
			continue
		}
		if next := link.Apply(root, desired, problems, false, covered, "", true); next > code {
			code = next
		}
	}
	return code
}

func cmdLink(library string, roots []string) int {
	code := 0
	for index, root := range roots {
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
			fmt.Println("未找到 .agents/skills.json。请写入字符串数组后再 link。")
			code = 2
			continue
		}
		desired, problems, covered, err := workspacePlan(library, entries)
		if err != nil {
			fmt.Printf("global 配置无法读取: %s\n", err.Error())
			code = 2
			continue
		}
		if next := link.Apply(root, desired, problems, true, covered, "", true); next > code {
			code = next
		}
	}
	return code
}

func cmdLinkGlobal(library string) int {
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
	return link.Apply(home, desired, problems, true, nil, target, false)
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

func resolveAddNames(library string, names []string) ([]string, int) {
	var resolved []string
	for _, name := range names {
		if gitpack.IsURL(name) {
			url, ref := gitpack.ParseSpec(name)
			packName, err := gitpack.Fetch(library, url, ref, "")
			if err != nil {
				fmt.Println(err.Error())
				return nil, 1
			}
			fmt.Printf("已获取远程包 %s\n", packName)
			resolved = append(resolved, packName)
			continue
		}
		resolved = append(resolved, name)
	}
	return resolved, 0
}

func cmdAdd(library string, names []string, root string, useGlobal bool) int {
	resolved, code := resolveAddNames(library, names)
	if code != 0 {
		fmt.Println("未修改配置")
		return code
	}
	var problems []string
	for _, name := range resolved {
		_, more := link.Resolve(library, []string{name})
		problems = append(problems, more...)
	}
	if len(problems) > 0 {
		printProblems(problems)
		fmt.Println("未修改配置")
		return 1
	}
	var path, comment string
	var existing []string
	if useGlobal {
		var err error
		path, err = lib.GlobalConfigPath()
		if err != nil {
			fmt.Println(err.Error())
			return 1
		}
		comment = "// global 技能。运行 /skill-manager link --global。"
		if lib.IsFile(path) {
			existing, err = loadConfigFile(path)
			if err != nil {
				fmt.Println(err.Error())
				return 1
			}
		}
	} else {
		if !lib.IsDir(root) {
			fmt.Printf("根目录不存在: %s\n", root)
			return 2
		}
		path = filepath.Join(root, ".agents", lib.ConfigName)
		comment = "// 启用的技能包。包名加载整包，包名:技能名 只加载一个。运行 /skill-manager link。"
		entries, ok, err := loadSelection(root)
		if err != nil {
			fmt.Println(err.Error())
			return 1
		}
		if ok {
			existing = entries
		}
	}
	current := append([]string{}, existing...)
	seen := map[string]bool{}
	for _, item := range current {
		seen[lib.CaseFold(item)] = true
	}
	var added []string
	for _, name := range resolved {
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

func cmdLock(library string) int {
	if !lib.IsDir(library) {
		fmt.Printf("skill 库不存在: %s\n", library)
		return 2
	}
	if err := gitpack.WriteLock(library, nil); err != nil {
		fmt.Println(err.Error())
		return 1
	}
	fmt.Printf("已按磁盘重写 %s\n", gitpack.LockPath(library))
	printLibrary(library)
	return 0
}

func upgradePack(library, packName string) int {
	if !lib.ValidName(packName) {
		fmt.Printf("非法包名: %s\n", packName)
		return 2
	}
	dest := filepath.Join(library, packName)
	if !lib.IsDir(dest) || symlink.Target(dest) != "" {
		fmt.Printf("库里没有包: %s\n", packName)
		return 1
	}
	gitRoot := gitpack.Root(dest)
	if gitRoot == "" {
		fmt.Printf("包 %s 不是 git 工作区，无法 upgrade\n", packName)
		return 2
	}
	if text, err := gitpack.Combined("-C", gitRoot, "fetch", "origin"); err != nil {
		if text == "" {
			text = "git fetch 失败"
		}
		fmt.Println(text)
		return 2
	}
	branch := gitpack.OriginDefaultBranch(gitRoot)
	if text, err := gitpack.Combined("-C", gitRoot, "checkout", branch); err != nil {
		if text == "" {
			text = "无法切换到 " + branch
		}
		fmt.Println(text)
		return 2
	}
	if text, err := gitpack.Combined("-C", gitRoot, "pull", "--ff-only", "origin", branch); err != nil {
		if text == "" {
			text = "git pull 失败"
		}
		fmt.Println(text)
		return 2
	}
	if err := gitpack.WriteLock(library, nil); err != nil {
		fmt.Println(err.Error())
		return 1
	}
	meta := gitpack.Metadata(gitRoot, "")
	rev := meta["revision"]
	if rev == "" {
		rev = "?"
	}
	fmt.Printf("已升级 %s (%s @ %s)\n", packName, branch, rev)
	return 0
}

func initSelectionEntries(library, skillsFolder string) ([]string, error) {
	libraryResolved := lib.Resolve(library)
	byPack := map[string]map[string]bool{}
	var realInstalls []string
	if lib.IsDir(skillsFolder) {
		entries, err := os.ReadDir(skillsFolder)
		if err != nil {
			return nil, err
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
			if name == "skill-manager" && symlink.Target(entryPath) == "" {
				continue
			}
			target := symlink.Target(entryPath)
			if target != "" {
				resolvedTarget := lib.Resolve(target)
				rel, err := filepath.Rel(libraryResolved, resolvedTarget)
				if err != nil || strings.HasPrefix(rel, "..") {
					if lib.IsDir(resolvedTarget) {
						realInstalls = append(realInstalls, resolvedTarget)
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
			realInstalls = append(realInstalls, lib.Resolve(entryPath))
		}
	}
	for _, src := range realInstalls {
		if !lib.IsDir(src) {
			continue
		}
		if _, err := lib.ClassifySource(src); err != nil {
			continue
		}
		packName := filepath.Base(src)
		if !lib.ValidName(packName) {
			continue
		}
		dest := filepath.Join(library, packName)
		if !lib.Exists(dest) && symlink.Target(dest) == "" {
			installPack(library, src)
		}
		if byPack[packName] == nil {
			byPack[packName] = map[string]bool{}
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
			return nil, err
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
	return entries, nil
}

func cmdInit(library string) int {
	skillsFolder, err := lib.GlobalSkillsDir()
	if err != nil {
		fmt.Println(err.Error())
		return 1
	}
	fmt.Printf("init global: %s\n", skillsFolder)
	fmt.Printf("库: %s\n", library)
	entries, err := initSelectionEntries(library, skillsFolder)
	if err != nil {
		fmt.Println(err.Error())
		return 1
	}
	path, err := lib.GlobalConfigPath()
	if err != nil {
		fmt.Println(err.Error())
		return 1
	}
	comment := "// global 技能。运行 /skill-manager link --global。"
	if err := writeSelection(path, entries, comment); err != nil {
		fmt.Println(err.Error())
		return 1
	}
	fmt.Printf("配置: %s\n", path)
	for _, entry := range entries {
		fmt.Printf("  + %s\n", entry)
	}
	if err := gitpack.WriteLock(library, nil); err != nil {
		fmt.Println(err.Error())
		return 1
	}
	fmt.Printf("lock: %s\n", gitpack.LockPath(library))
	return cmdLinkGlobal(library)
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
		if arg == "-h" || arg == "--help" {
			printHelp()
			return 0
		}
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
	switch cmd {
	case "install":
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "install 需要一个目录")
			return 2
		}
		return installPack(library, args[0])
	case "remove":
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "remove 需要一个包名")
			return 2
		}
		return removePack(library, args[0])
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
	case "link":
		roots, global, errCode := parseRoots(args, true)
		if errCode != 0 {
			return errCode
		}
		if global {
			if len(roots) > 0 {
				fmt.Println("link --global 不使用 --root")
			}
			return cmdLinkGlobal(library)
		}
		if len(roots) == 0 {
			roots = []string{getwd()}
		}
		return cmdLink(library, roots)
	case "add":
		names, root, global, errCode := parseAdd(args)
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
			fmt.Fprintln(os.Stderr, "upgrade 需要一个包名")
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

func parseAdd(args []string) ([]string, string, bool, int) {
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
		fmt.Fprintln(os.Stderr, "add 至少需要一个包名或 Git URL")
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

func printHelp() {
	fmt.Println(`管理 skill 库与仓库链接

用法:
  skill-manager [--library 路径] [命令]

命令:
  install 路径     把本机目录装进 skill 库
  remove 包名      从 skill 库删除一个包
  lock            按磁盘重写 .skill-lock.json
  list [包名]      列出包；指定包名时列出包内技能
  link            按仓库配置链接技能
  add             把技能写入 skills.json，不创建链接
  status          只查看，不修改
  upgrade 包名     从 origin 默认分支拉取 git 包
  init            将 global ~/.agents/skills 迁入 skill-library 并 link`)
}

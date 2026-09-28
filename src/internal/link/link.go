package link

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/swxs/skill-manager/internal/gitpack"
	lib "github.com/swxs/skill-manager/internal/library"
	"github.com/swxs/skill-manager/internal/symlink"
)

func gitExcludeFile(root string) string {
	text := gitpack.Command(root, "rev-parse", "--git-path", "info/exclude")
	if text == "" {
		return ""
	}
	path := text
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	return path
}

func updateExclude(root string, relPaths []string) error {
	exclude := gitExcludeFile(root)
	if exclude == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(exclude), 0o755); err != nil {
		return err
	}
	existing := ""
	if lib.IsFile(exclude) {
		var err error
		existing, err = lib.ReadText(exclude)
		if err != nil {
			return err
		}
	}
	begin := strings.Index(existing, lib.ExcludeBegin)
	end := strings.Index(existing, lib.ExcludeEnd)
	if begin != -1 && end != -1 && end > begin {
		existing = existing[:begin] + existing[end+len(lib.ExcludeEnd):]
	}
	existing = strings.TrimSpace(existing)
	block := ""
	if len(relPaths) > 0 {
		lines := append([]string{lib.ExcludeBegin}, relPaths...)
		lines = append(lines, lib.ExcludeEnd)
		block = strings.Join(lines, "\n")
	}
	var parts []string
	if existing != "" {
		parts = append(parts, existing)
	}
	if block != "" {
		parts = append(parts, block)
	}
	text := strings.Join(parts, "\n\n")
	if text != "" {
		text += "\n"
	}
	return lib.WriteLF(exclude, text)
}

func skillsDir(root string) string {
	return filepath.Join(root, ".agents", lib.SkillsDirName)
}

func Resolve(library string, entries []string) (map[string]string, []string) {
	desired := map[string]string{}
	var problems []string
	order := []string{}
	for _, entry := range entries {
		packName, skillName := entry, ""
		single := false
		if i := strings.Index(entry, ":"); i >= 0 {
			packName = strings.TrimSpace(entry[:i])
			skillName = strings.TrimSpace(entry[i+1:])
		} else {
			packName = strings.TrimSpace(entry)
		}
		if !lib.ValidName(packName) || (skillName != "" && !lib.ValidName(skillName)) {
			problems = append(problems, "! 非法配置 "+entry)
			continue
		}
		pack := filepath.Join(library, packName)
		if !lib.IsDir(pack) {
			problems = append(problems, "! 库中没有包 "+packName)
			continue
		}
		info, err := lib.Inspect(pack)
		if err != nil {
			problems = append(problems, "! "+err.Error())
			continue
		}
		links := map[string]string{}
		if info.Kind == "mixed" {
			if skillName != "" {
				problems = append(problems, "! 混合包不能单拆 "+entry)
				continue
			}
			links[packName] = pack
		} else if skillName != "" {
			target := info.Members[skillName]
			if target == "" {
				problems = append(problems, fmt.Sprintf("! 包 %s 里没有技能 %s", packName, skillName))
				continue
			}
			links[skillName] = target
			single = true
		} else {
			for k, v := range info.Members {
				links[k] = v
			}
			if len(links) == 0 {
				problems = append(problems, fmt.Sprintf("! 包 %s 里没有可链接的技能", packName))
				continue
			}
		}
		names := make([]string, 0, len(links))
		for name := range links {
			names = append(names, name)
		}
		lib.SortFold(names)
		for _, linkName := range names {
			target := links[linkName]
			if current, ok := desired[linkName]; ok && !lib.SamePath(current, target) {
				label := entry
				if !single && skillName == "" && info.Kind != "mixed" {
					label = packName + ":" + linkName
				}
				problems = append(problems, fmt.Sprintf("! 同名跳过 %s（已占用 %s）", label, linkName))
				continue
			}
			if _, ok := desired[linkName]; !ok {
				order = append(order, linkName)
			}
			desired[linkName] = target
		}
	}
	_ = order
	return desired, problems
}

func Apply(root string, desired map[string]string, problems []string, write bool, covered map[string]bool, folder string, manageExclude bool) int {
	root = lib.Resolve(root)
	if folder == "" {
		folder = skillsDir(root)
	}
	if covered == nil {
		covered = map[string]bool{}
	}
	var created, updated, removed, unchanged, owned, skipped []string
	seenCovered := map[string]bool{}
	linked := map[string]string{}

	if lib.IsDir(folder) {
		entries, _ := os.ReadDir(folder)
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		lib.SortFold(names)
		for _, name := range names {
			if strings.HasPrefix(name, ".") {
				continue
			}
			entryPath := filepath.Join(folder, name)
			current := symlink.Target(entryPath)
			if covered[name] {
				seenCovered[name] = true
				if current == "" {
					problems = append(problems, fmt.Sprintf("! 冲突 %s: global 已覆盖，但 .agents/skills/%s 是真实目录，未改动", name, name))
				} else {
					if write {
						_ = symlink.Remove(entryPath)
					}
					removed = append(removed, name+"（global 已覆盖）")
				}
				continue
			}
			wanted, hasWanted := desired[name]
			if current == "" {
				if hasWanted {
					problems = append(problems, fmt.Sprintf("! 冲突 %s: .agents/skills/%s 已是真实目录，未改动", name, name))
				} else if lib.Exists(entryPath) {
					owned = append(owned, name)
				}
				continue
			}
			if !hasWanted {
				if write {
					_ = symlink.Remove(entryPath)
				}
				removed = append(removed, name)
				continue
			}
			if lib.SamePath(entryPath, wanted) {
				unchanged = append(unchanged, name)
				linked[name] = wanted
				continue
			}
			if write {
				_ = symlink.Remove(entryPath)
				kind, err := symlink.Create(entryPath, wanted)
				if err != nil {
					problems = append(problems, "! "+err.Error())
					continue
				}
				updated = append(updated, fmt.Sprintf("%s (%s)", name, kind))
			} else {
				updated = append(updated, name)
			}
			linked[name] = wanted
		}
	}

	names := make([]string, 0, len(desired))
	for name := range desired {
		names = append(names, name)
	}
	lib.SortFold(names)
	for _, name := range names {
		target := desired[name]
		if _, ok := linked[name]; ok {
			continue
		}
		conflict := false
		for _, item := range problems {
			if strings.HasPrefix(item, "! 冲突 "+name+":") {
				conflict = true
				break
			}
		}
		if conflict {
			continue
		}
		dest := filepath.Join(folder, name)
		if lib.Exists(dest) && symlink.Target(dest) == "" {
			problems = append(problems, fmt.Sprintf("! 冲突 %s: .agents/skills/%s 已是真实目录，未改动", name, name))
			continue
		}
		if write {
			kind, err := symlink.Create(dest, target)
			if err != nil {
				problems = append(problems, "! "+err.Error())
				continue
			}
			created = append(created, fmt.Sprintf("%s (%s)", name, kind))
		} else {
			created = append(created, name)
		}
		linked[name] = target
	}

	coveredNames := make([]string, 0, len(covered))
	for name := range covered {
		coveredNames = append(coveredNames, name)
	}
	lib.SortFold(coveredNames)
	for _, name := range coveredNames {
		if !seenCovered[name] {
			skipped = append(skipped, name)
		}
	}

	if write && manageExclude {
		rel := make([]string, 0, len(linked))
		for name := range linked {
			rel = append(rel, ".agents/skills/"+name)
		}
		lib.SortFold(rel)
		_ = updateExclude(root, rel)
	}

	if write {
		if len(created)+len(updated)+len(removed) > 0 {
			fmt.Println("已按配置同步")
		} else if len(problems) > 0 {
			fmt.Println("未完全对齐")
		} else {
			fmt.Println("已与配置一致")
		}
	} else {
		if len(created)+len(updated)+len(removed)+len(problems) > 0 {
			fmt.Println("未对齐")
		} else {
			fmt.Println("已与配置一致")
		}
	}
	for _, line := range created {
		if write {
			fmt.Printf("  + %s\n", line)
		} else {
			fmt.Printf("  + 缺少 %s\n", line)
		}
	}
	for _, line := range updated {
		if write {
			fmt.Printf("  ~ 更新 %s\n", line)
		} else {
			fmt.Printf("  ~ 指向不同 %s\n", line)
		}
	}
	for _, line := range removed {
		if write {
			fmt.Printf("  - 移除 %s\n", line)
		} else {
			fmt.Printf("  - 多余 %s\n", line)
		}
	}
	for _, line := range unchanged {
		fmt.Printf("  = 未变化 %s\n", line)
	}
	for _, line := range problems {
		if strings.HasPrefix(line, "!") {
			fmt.Printf("  %s\n", line)
		} else {
			fmt.Printf("  ! %s\n", line)
		}
	}
	if Contains(problems, "! 库中没有包") {
		fmt.Printf("  %s\n", lib.MissingPackHint)
	}
	for _, line := range owned {
		fmt.Printf("  · 仓库自有目录，未改动 %s\n", line)
	}
	for _, line := range skipped {
		fmt.Printf("  · 跳过 %s（global 已覆盖）\n", line)
	}
	if len(created)+len(updated)+len(removed)+len(unchanged)+len(problems)+len(owned)+len(skipped) == 0 {
		fmt.Println("  （配置的列表为空，未链入任何技能）")
	}
	if len(problems) > 0 {
		return 1
	}
	return 0
}

func Contains(items []string, sub string) bool {
	for _, item := range items {
		if strings.Contains(item, sub) {
			return true
		}
	}
	return false
}

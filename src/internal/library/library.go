package library

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const (
	LockName        = ".skill-lock.json"
	ConfigName      = "skills.json"
	SkillsDirName   = "skills"
	ExcludeBegin    = "# begin skill-manager"
	ExcludeEnd      = "# end skill-manager"
	MissingPackHint = "? 请先 install 本机目录或 Git 仓库 URL（可选 #ref），例如：install https://github.com/org/repo.git"
)

var installIgnore = map[string]bool{
	"__pycache__":  true,
	".venv":        true,
	"node_modules": true,
	".DS_Store":    true,
	LockName:       true,
}

func AgentHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".agents"), nil
}

func DefaultLibrary() (string, error) {
	home, err := AgentHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "skill-library"), nil
}

func GlobalConfigPath() (string, error) {
	home, err := AgentHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ConfigName), nil
}

func GlobalSkillsDir() (string, error) {
	home, err := AgentHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "skills"), nil
}

func ValidName(name string) bool {
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, ".") {
		return false
	}
	return !strings.ContainsAny(name, `/\`) && !strings.Contains(name, ":")
}

func CaseFold(s string) string {
	return strings.ToLower(s)
}

func SortFold(names []string) {
	sort.Slice(names, func(i, j int) bool {
		return CaseFold(names[i]) < CaseFold(names[j])
	})
}

func skillFiles(root string) ([]string, error) {
	var found []string
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return found, nil
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		for _, part := range strings.Split(rel, string(os.PathSeparator)) {
			if strings.HasPrefix(part, ".") && part != "." {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if d.IsDir() || d.Name() != "SKILL.md" {
			return nil
		}
		found = append(found, path)
		return nil
	})
	return found, err
}

func ClassifySource(src string) (string, error) {
	files, err := skillFiles(src)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", fmt.Errorf("没有 SKILL.md: %s", src)
	}
	rootHas := IsFile(filepath.Join(src, "SKILL.md"))
	others := false
	for _, path := range files {
		if filepath.Dir(path) != src {
			others = true
			break
		}
	}
	if rootHas && !others {
		return "single", nil
	}
	if rootHas && others {
		return "mixed", nil
	}
	return "pack", nil
}

func DirectMembers(pack string) ([]string, map[string]string, error) {
	members := map[string]string{}
	info, err := os.Stat(pack)
	if err != nil || !info.IsDir() {
		return nil, members, nil
	}
	entries, err := os.ReadDir(pack)
	if err != nil {
		return nil, nil, err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		child := filepath.Join(pack, entry.Name())
		if IsFile(filepath.Join(child, "SKILL.md")) {
			members[entry.Name()] = child
			names = append(names, entry.Name())
		}
	}
	SortFold(names)
	return names, members, nil
}

type Pack struct {
	Kind    string
	Skills  []string
	Members map[string]string
}

func Inspect(pack string) (Pack, error) {
	rootHas := IsFile(filepath.Join(pack, "SKILL.md"))
	files, err := skillFiles(pack)
	if err != nil {
		return Pack{}, err
	}
	var descendants []string
	for _, path := range files {
		if filepath.Dir(path) != pack {
			descendants = append(descendants, path)
		}
	}
	if rootHas && len(descendants) > 0 {
		return Pack{
			Kind:    "mixed",
			Skills:  mixedSkillNames(pack, files),
			Members: map[string]string{filepath.Base(pack): pack},
		}, nil
	}
	if rootHas {
		name := filepath.Base(pack)
		return Pack{Kind: "single", Skills: []string{name}, Members: map[string]string{name: pack}}, nil
	}
	names, members, err := DirectMembers(pack)
	if err != nil {
		return Pack{}, err
	}
	kind := "pack"
	if len(members) == 1 && members[filepath.Base(pack)] != "" {
		kind = "single"
	}
	return Pack{Kind: kind, Skills: names, Members: members}, nil
}

func mixedSkillNames(pack string, files []string) []string {
	var names []string
	seen := map[string]bool{}
	base := filepath.Base(pack)
	for _, path := range files {
		name := base
		if filepath.Dir(path) != pack {
			name = filepath.Base(filepath.Dir(path))
		}
		if seen[CaseFold(name)] {
			rel, _ := filepath.Rel(pack, filepath.Dir(path))
			name = filepath.ToSlash(rel)
		}
		if seen[CaseFold(name)] {
			continue
		}
		seen[CaseFold(name)] = true
		names = append(names, name)
	}
	return names
}

func IsFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func IsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func WriteLF(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

func ReadText(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	s := string(b)
	s = strings.TrimPrefix(s, "\uFEFF")
	return s, nil
}

func CopyInstall(src, dest string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		base := d.Name()
		if path != src && installIgnore[base] {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := dest
		if rel != "." {
			target = filepath.Join(dest, rel)
		}
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if d.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func RemoveTree(path string) error {
	return os.RemoveAll(path)
}

func SamePath(left, right string) bool {
	lr := Resolve(left)
	rr := Resolve(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(lr, rr)
	}
	return lr == rr
}

func Resolve(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	eval, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(eval)
}

func ExpandUser(p string) string {
	if p == "~" || strings.HasPrefix(p, "~"+string(os.PathSeparator)) || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		if p == "~" {
			return home
		}
		return filepath.Join(home, p[2:])
	}
	return p
}

func Exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

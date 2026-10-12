package gitpack

import (
	"os"
	"path/filepath"
	"strings"

	lib "github.com/swxs/skill-manager/internal/library"
)

type SkillWant struct {
	Name    string
	Rel     string
	URL     string
	OldHash string
}

type Brought struct {
	Name  string
	Dir   string
	URL   string
	Hash  string
	Fresh bool
}

// Bring 按一个克隆地址取树哈希。需要文件时在同一次克隆里检出。discover 列出要补新技能的目录。
func Bring(ref CloneRef, wants []SkillWant, discover []string) (cleanup func(), unchanged []string, brought []Brought, err error) {
	cleanup = func() {}
	if !gitAvailable() {
		return cleanup, nil, nil, errNoGit
	}
	dir, err := os.MkdirTemp("", "skill-manager-hash-")
	if err != nil {
		return cleanup, nil, nil, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	args := []string{
		"-c", "core.autocrlf=false",
		"-c", "advice.detachedHead=false",
		"clone", "--depth", "1", "--filter=blob:none", "--sparse", "--single-branch",
		"--branch", ref.Branch, ref.Clone, dir,
	}
	if _, err = Combined(args...); err != nil {
		return cleanup, nil, nil, err
	}
	have := map[string]bool{}
	var need []Brought
	sample := ""
	for _, want := range wants {
		have[want.Name] = true
		if sample == "" {
			sample = want.URL
		}
		hash := Command(dir, "rev-parse", "HEAD:"+want.Rel)
		if hash != "" && want.OldHash != "" && hash == want.OldHash {
			unchanged = append(unchanged, want.Name)
			continue
		}
		need = append(need, Brought{Name: want.Name, URL: want.URL, Hash: hash})
	}
	for _, parent := range discover {
		for _, name := range skillNamesAt(dir, parent) {
			if have[name] {
				continue
			}
			have[name] = true
			rel := name
			if parent != "" {
				rel = parent + "/" + name
			}
			page := sample
			if page == "" {
				page = ref.Clone
			}
			need = append(need, Brought{
				Name:  name,
				URL:   siblingURL(page, rel, ref.Branch),
				Hash:  Command(dir, "rev-parse", "HEAD:"+rel),
				Fresh: true,
			})
		}
	}
	if len(need) == 0 {
		return cleanup, unchanged, nil, nil
	}
	rels := make([]string, 0, len(need))
	for _, item := range need {
		rel := relOf(item)
		if rel != "" {
			rels = append(rels, rel)
		}
	}
	checkout := []string{"-C", dir, "sparse-checkout", "set", "--no-cone", "--"}
	checkout = append(checkout, rels...)
	if _, err = Combined(checkout...); err != nil {
		return cleanup, unchanged, nil, err
	}
	for i := range need {
		rel := relOf(need[i])
		need[i].Dir = filepath.Join(dir, filepath.FromSlash(rel))
		if need[i].Hash == "" {
			need[i].Hash = Command(dir, "rev-parse", "HEAD:"+rel)
		}
	}
	return cleanup, unchanged, need, nil
}

var errNoGit = errString("没有 git")

type errString string

func (e errString) Error() string { return string(e) }

func relOf(item Brought) string {
	remote, err := ParseRemote(item.URL)
	if err != nil {
		return ""
	}
	return remote.SkillRel
}

func siblingURL(sample, rel, branch string) string {
	remote, err := ParseRemote(sample)
	if err != nil {
		return ""
	}
	if branch == "" {
		branch = remote.Branch
	}
	if branch == "" {
		branch = "HEAD"
	}
	return remote.At(branch, rel)
}

func skillNamesAt(dir, parent string) []string {
	spec := "HEAD"
	if parent != "" {
		spec = "HEAD:" + parent
	}
	var names []string
	for _, name := range strings.Split(Command(dir, "ls-tree", "--name-only", spec), "\n") {
		name = strings.TrimSpace(name)
		if name == "" || strings.Contains(name, "/") || !lib.ValidName(name) {
			continue
		}
		rel := name
		if parent != "" {
			rel = parent + "/" + name
		}
		ok := false
		for _, line := range strings.Split(Command(dir, "ls-tree", "--name-only", "HEAD:"+rel), "\n") {
			if strings.TrimSpace(line) == "SKILL.md" {
				ok = true
				break
			}
		}
		if ok {
			names = append(names, name)
		}
	}
	return names
}

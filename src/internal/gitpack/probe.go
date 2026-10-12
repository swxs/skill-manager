package gitpack

import (
	"os"
	"sync"

	lib "github.com/swxs/skill-manager/internal/library"
)

// treeAt 优先用克隆里的 Git 树对象。没有对象库时按磁盘上的文件计算。
func treeAt(repo, rel, disk string) string {
	if repo != "" && rel != "" {
		if sha := Command(repo, "rev-parse", "HEAD:"+rel); sha != "" {
			return sha
		}
	}
	hash, err := lib.TreeHash(disk)
	if err != nil {
		return ""
	}
	return hash
}

// ProbeTrees 按克隆地址分组，只取树对象，返回每条安装地址的树哈希。
func ProbeTrees(raws []string) map[string]string {
	type group struct {
		remote Remote
		raws   []string
	}
	groups := map[string]*group{}
	var keys []string
	for _, raw := range raws {
		remote, err := ParseRemote(raw)
		if err != nil || !remote.Skill || remote.SkillRel == "" {
			continue
		}
		key := remote.Clone + "\n" + remote.Branch
		item := groups[key]
		if item == nil {
			item = &group{remote: remote}
			groups[key] = item
			keys = append(keys, key)
		}
		item.raws = append(item.raws, raw)
	}
	out := map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, key := range keys {
		wg.Add(1)
		go func(item *group) {
			defer wg.Done()
			got := probeGroup(item.remote, item.raws)
			mu.Lock()
			for raw, hash := range got {
				out[raw] = hash
			}
			mu.Unlock()
		}(groups[key])
	}
	wg.Wait()
	return out
}

func probeGroup(remote Remote, raws []string) map[string]string {
	out := map[string]string{}
	if !gitAvailable() {
		return out
	}
	dir, err := os.MkdirTemp("", "skill-manager-hash-")
	if err != nil {
		return out
	}
	defer os.RemoveAll(dir)
	args := []string{
		"-c", "core.autocrlf=false",
		"-c", "advice.detachedHead=false",
		"clone", "--depth", "1", "--filter=blob:none", "--single-branch",
	}
	if remote.Branch != "" {
		args = append(args, "--branch", remote.Branch)
	}
	args = append(args, remote.Clone, dir)
	if _, err := Combined(args...); err != nil {
		return out
	}
	for _, raw := range raws {
		parsed, err := ParseRemote(raw)
		if err != nil || parsed.SkillRel == "" {
			continue
		}
		if sha := Command(dir, "rev-parse", "HEAD:"+parsed.SkillRel); sha != "" {
			out[raw] = sha
		}
	}
	return out
}

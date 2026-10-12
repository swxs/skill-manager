package gitpack

import (
	"bytes"
	"encoding/json"
	"fmt"
	lib "github.com/swxs/skill-manager/internal/library"
	"github.com/swxs/skill-manager/internal/symlink"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func Command(cwd string, args ...string) string {
	cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return ""
	}
	text := strings.TrimSpace(stdout.String())
	if text == "" {
		text = strings.TrimSpace(stderr.String())
	}
	return text
}

func Combined(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return text, err
	}
	return text, nil
}

func Root(pack string) string {
	if lib.IsDir(filepath.Join(pack, ".git")) {
		return pack
	}
	info, err := lib.Inspect(pack)
	if err != nil {
		return ""
	}
	if info.Kind == "single" {
		nested := filepath.Join(pack, filepath.Base(pack))
		if lib.IsDir(filepath.Join(nested, ".git")) {
			return nested
		}
	}
	return ""
}

func Metadata(gitRoot, fallback string) map[string]string {
	meta := map[string]string{}
	if remote := Command(gitRoot, "remote", "get-url", "origin"); remote != "" {
		meta["source"] = remote
	} else if fallback != "" {
		meta["source"] = fallback
	}
	if revision := Command(gitRoot, "rev-parse", "HEAD"); revision != "" {
		meta["revision"] = revision
	}
	ref := Command(gitRoot, "symbolic-ref", "-q", "--short", "HEAD")
	if ref == "" {
		ref = Command(gitRoot, "describe", "--tags", "--always")
	}
	if ref != "" {
		meta["ref"] = ref
	}
	return meta
}

func IsURL(text string) bool {
	stripped := strings.TrimSpace(text)
	if stripped == "" {
		return false
	}
	if strings.HasPrefix(stripped, "git@") || strings.Contains(stripped, "://") || strings.HasSuffix(stripped, ".git") {
		return true
	}
	candidate := lib.ExpandUser(stripped)
	if lib.IsDir(candidate) && lib.Exists(filepath.Join(candidate, ".git")) {
		return true
	}
	return false
}

func ParseSpec(text string) (string, string) {
	stripped := strings.TrimSpace(text)
	ref := ""
	if i := strings.LastIndex(stripped, "#"); i >= 0 {
		ref = strings.TrimSpace(stripped[i+1:])
		stripped = strings.TrimSpace(stripped[:i])
	}
	return stripped, ref
}

func NameFromURL(url string) (string, error) {
	cleaned := strings.TrimRight(url, "/")
	if strings.HasSuffix(strings.ToLower(cleaned), ".git") {
		cleaned = cleaned[:len(cleaned)-4]
	}
	cleaned = strings.ReplaceAll(cleaned, ":", "/")
	cleaned = strings.ReplaceAll(cleaned, "\\", "/")
	name := cleaned
	if i := strings.LastIndex(cleaned, "/"); i >= 0 {
		name = cleaned[i+1:]
	}
	if !lib.ValidName(name) {
		return "", fmt.Errorf("无法从 URL 推导包名: %s", url)
	}
	return name, nil
}

func Clone(url, dest, ref string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if text, err := Combined("clone", url, dest); err != nil {
		if text == "" {
			text = "git clone 失败"
		}
		return fmt.Errorf("%s", text)
	}
	if ref == "" {
		return nil
	}
	if text, err := Combined("-C", dest, "checkout", ref); err != nil {
		_ = lib.RemoveTree(dest)
		if text == "" {
			text = "无法检出 ref: " + ref
		}
		return fmt.Errorf("%s", text)
	}
	return nil
}

func CheckoutRef(pack, ref string) error {
	if strings.TrimSpace(ref) == "" {
		return fmt.Errorf("缺少 ref")
	}
	root := Root(pack)
	if root == "" {
		return fmt.Errorf("包不是 git 工作区，无法检出 %s", ref)
	}
	if text, err := Combined("-C", root, "fetch", "origin"); err != nil {
		if text == "" {
			text = "git fetch 失败"
		}
		return fmt.Errorf("%s", text)
	}
	text, err := Combined("-C", root, "status", "--porcelain")
	if err != nil {
		if text == "" {
			text = "无法读取工作区状态"
		}
		return fmt.Errorf("%s", text)
	}
	if text != "" {
		return fmt.Errorf("工作区不干净，未检出 %s", ref)
	}
	if text, err = Combined("-C", root, "checkout", ref); err != nil {
		if text == "" {
			text = "无法检出 ref: " + ref
		}
		return fmt.Errorf("%s", text)
	}
	return nil
}

const lockVersion = 2

type skillLock struct {
	URL  string `json:"url"`
	Hash string `json:"hash"`
}

func (s *skillLock) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var url string
		if err := json.Unmarshal(data, &url); err != nil {
			return err
		}
		s.URL = url
		s.Hash = ""
		return nil
	}
	var raw struct {
		URL  string `json:"url"`
		Hash string `json:"hash"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.URL = raw.URL
	s.Hash = raw.Hash
	return nil
}

type lockEntry struct {
	Kind   string               `json:"kind"`
	Skills map[string]skillLock `json:"skills"`
}

func (e *lockEntry) UnmarshalJSON(data []byte) error {
	var raw struct {
		Kind   string          `json:"kind"`
		Skills json.RawMessage `json:"skills"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	e.Kind = raw.Kind
	e.Skills = map[string]skillLock{}
	if len(raw.Skills) == 0 || string(raw.Skills) == "null" {
		return nil
	}
	if raw.Skills[0] == '[' {
		return nil
	}
	return json.Unmarshal(raw.Skills, &e.Skills)
}

type lockFile struct {
	Version int                  `json:"version"`
	Packs   map[string]lockEntry `json:"packs"`
}

func LockPath(library string) string {
	return filepath.Join(library, lib.LockName)
}

func readLock(library string) lockFile {
	path := LockPath(library)
	raw, err := lib.ReadText(path)
	if err != nil {
		return lockFile{Version: lockVersion, Packs: map[string]lockEntry{}}
	}
	var data lockFile
	if err := json.Unmarshal([]byte(raw), &data); err != nil || data.Packs == nil {
		return lockFile{Version: lockVersion, Packs: map[string]lockEntry{}}
	}
	return data
}

func PackDirs(library string) ([]string, error) {
	if !lib.IsDir(library) {
		return nil, nil
	}
	entries, err := os.ReadDir(library)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		full := filepath.Join(library, entry.Name())
		if symlink.Target(full) != "" {
			continue
		}
		names = append(names, entry.Name())
	}
	lib.SortFold(names)
	return names, nil
}

// WriteLock 按磁盘重写锁定。overrides 是这次要写下的 包名 → 技能名 → 安装地址。
// 没出现在 overrides 里的技能沿用上次的地址。没有地址的不写入。不读包里的 .git。
func WriteLock(library string, overrides map[string]map[string]string, hashes map[string]map[string]string, recompute bool) error {
	if err := os.MkdirAll(library, 0o755); err != nil {
		return err
	}
	previous := readLock(library)
	names, err := PackDirs(library)
	if err != nil {
		return err
	}
	packs := map[string]lockEntry{}
	for _, name := range names {
		pack := filepath.Join(library, name)
		info, err := lib.Inspect(pack)
		if err != nil {
			return err
		}
		old := map[string]skillLock{}
		if prev, ok := previous.Packs[name]; ok && prev.Skills != nil {
			old = prev.Skills
		}
		fresh := overrides[name]
		skills := map[string]skillLock{}
		for _, skill := range info.Skills {
			url := ""
			if fresh != nil {
				if u, ok := fresh[skill]; ok {
					url = u
				} else if prev, ok := old[skill]; ok {
					url = prev.URL
				}
			} else if prev, ok := old[skill]; ok {
				url = prev.URL
			}
			if url == "" {
				continue
			}
			hash := ""
			if !recompute {
				if packHashes := hashes[name]; packHashes != nil {
					hash = packHashes[skill]
				}
				if hash == "" {
					if prev, ok := old[skill]; ok {
						hash = prev.Hash
					}
				}
			}
			if hash == "" {
				dir := info.Members[skill]
				if dir == "" {
					dir = filepath.Join(pack, skill)
				}
				var err error
				hash, err = lib.TreeHash(dir)
				if err != nil {
					return err
				}
			}
			skills[skill] = skillLock{URL: url, Hash: hash}
		}
		packs[name] = lockEntry{Kind: info.Kind, Skills: skills}
	}
	payload, err := json.MarshalIndent(lockFile{Version: lockVersion, Packs: packs}, "", "  ")
	if err != nil {
		return err
	}
	return lib.WriteLF(LockPath(library), string(payload)+"\n")
}

func SkillURL(library, pack, skill string) string {
	data := readLock(library)
	entry, ok := data.Packs[pack]
	if !ok || entry.Skills == nil {
		return ""
	}
	return entry.Skills[skill].URL
}

func SkillHash(library, pack, skill string) string {
	data := readLock(library)
	entry, ok := data.Packs[pack]
	if !ok || entry.Skills == nil {
		return ""
	}
	return entry.Skills[skill].Hash
}

func OriginDefaultBranch(gitRoot string) string {
	if ref := Command(gitRoot, "symbolic-ref", "refs/remotes/origin/HEAD"); ref != "" {
		if i := strings.LastIndex(ref, "/"); i >= 0 {
			return ref[i+1:]
		}
		return ref
	}
	for _, branch := range []string{"main", "master"} {
		if Command(gitRoot, "show-ref", "--verify", "refs/remotes/origin/"+branch) != "" {
			return branch
		}
	}
	if remote := Command(gitRoot, "branch", "-r"); remote != "" {
		for _, line := range strings.Split(remote, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "origin/") && !strings.Contains(line, "HEAD") {
				return strings.SplitN(line, "/", 2)[1]
			}
		}
	}
	return "main"
}

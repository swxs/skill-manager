package gitpack

import (
	"fmt"
	lib "github.com/swxs/skill-manager/internal/library"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// GitHub 是一条 https://github.com/{owner}/{repo} 地址，可以带 /tree/<分支> 和仓库内路径。
type GitHub struct {
	Owner    string
	Repo     string
	Branch   string
	SkillRel string
	Skill    bool
}

// ParseGitHub 去掉片段和末尾斜杠。推不出 owner 与仓库名时失败。
func ParseGitHub(raw string) (GitHub, error) {
	text := strings.TrimSpace(raw)
	if i := strings.Index(text, "#"); i >= 0 {
		text = strings.TrimSpace(text[:i])
	}
	text = strings.TrimRight(text, "/")
	u, err := url.Parse(text)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || !strings.EqualFold(u.Host, "github.com") {
		return GitHub{}, fmt.Errorf("无法从 URL 推导包名: %s", raw)
	}
	parts := splitPath(u.Path)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return GitHub{}, fmt.Errorf("无法从 URL 推导包名: %s", raw)
	}
	repo := parts[1]
	if strings.HasSuffix(strings.ToLower(repo), ".git") {
		repo = repo[:len(repo)-4]
	}
	if repo == "" {
		return GitHub{}, fmt.Errorf("无法从 URL 推导包名: %s", raw)
	}
	gh := GitHub{Owner: parts[0], Repo: repo}
	if len(parts) == 2 {
		return gh, nil
	}
	if parts[2] != "tree" || len(parts) < 4 || parts[3] == "" {
		return GitHub{}, fmt.Errorf("无法从 URL 推导包名: %s", raw)
	}
	gh.Branch = parts[3]
	if len(parts) > 4 {
		gh.Skill = true
		gh.SkillRel = strings.Join(parts[4:], "/")
	}
	return gh, nil
}

func splitPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

// PackageName 只认小写的 skills 与 -skills。
func PackageName(owner, repo string) (string, error) {
	name := repo
	if repo == "skills" {
		name = owner
	} else if strings.HasSuffix(repo, "-skills") {
		name = strings.TrimSuffix(repo, "-skills")
	}
	if !lib.ValidName(name) {
		return "", fmt.Errorf("无法从 URL 推导包名")
	}
	return name, nil
}

func (g GitHub) CloneURL() string {
	return "https://github.com/" + g.Owner + "/" + g.Repo
}

func (g GitHub) SkillName() string {
	if g.SkillRel == "" {
		return ""
	}
	i := strings.LastIndex(g.SkillRel, "/")
	if i < 0 {
		return g.SkillRel
	}
	return g.SkillRel[i+1:]
}

// Recorded 是写入锁定的地址。branch 是取文件时实际用的分支。
func (g GitHub) Recorded(branch, skillName string) string {
	rel := g.SkillRel
	if !g.Skill {
		rel = "skills/" + skillName
	}
	return "https://github.com/" + g.Owner + "/" + g.Repo + "/tree/" + branch + "/" + rel
}

// SameURL 比较将要记下的地址，忽略末尾斜杠和仓库名上的 .git。
func SameURL(left, right string) bool {
	return canonURL(left) == canonURL(right)
}

func canonURL(raw string) string {
	text := strings.TrimSpace(raw)
	if i := strings.Index(text, "#"); i >= 0 {
		text = text[:i]
	}
	text = strings.TrimRight(text, "/")
	text = strings.ReplaceAll(text, ".git/tree/", "/tree/")
	text = strings.TrimSuffix(text, ".git")
	return text
}

// Drop 是临时克隆里的一个技能文件夹。
type Drop struct {
	Name string
	Dir  string
	URL  string
}

// Fetch 克隆到临时目录并列出要复制的技能文件夹。调用方负责 cleanup。
func Fetch(raw string) (cleanup func(), drops []Drop, kind string, line string) {
	gh, err := ParseGitHub(raw)
	if err != nil {
		return func() {}, nil, "name", fmt.Sprintf("无法从 URL 推导包名: %s", strings.TrimSpace(raw))
	}
	if _, err := PackageName(gh.Owner, gh.Repo); err != nil {
		return func() {}, nil, "name", fmt.Sprintf("无法从 URL 推导包名: %s", strings.TrimSpace(raw))
	}
	dir, err := os.MkdirTemp("", "skill-manager-")
	if err != nil {
		return func() {}, nil, "fetch", fmt.Sprintf("无法取得技能: %s", strings.TrimSpace(raw))
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	if text, err := Combined("clone", gh.CloneURL(), dir); err != nil {
		cleanup()
		if text == "" {
			text = "无法取得技能: " + strings.TrimSpace(raw)
		} else {
			text = fmt.Sprintf("无法取得技能: %s", strings.TrimSpace(raw))
		}
		return func() {}, nil, "fetch", text
	}
	branch := gh.Branch
	if branch != "" {
		if _, err := Combined("-C", dir, "checkout", branch); err != nil {
			cleanup()
			return func() {}, nil, "fetch", fmt.Sprintf("无法取得技能: %s", strings.TrimSpace(raw))
		}
	} else {
		branch = Command(dir, "rev-parse", "--abbrev-ref", "HEAD")
		if branch == "" || branch == "HEAD" {
			cleanup()
			return func() {}, nil, "fetch", fmt.Sprintf("无法取得技能: %s", strings.TrimSpace(raw))
		}
	}
	if gh.Skill {
		skillDir := filepath.Join(dir, filepath.FromSlash(gh.SkillRel))
		if !lib.IsFile(filepath.Join(skillDir, "SKILL.md")) {
			cleanup()
			return func() {}, nil, "noskill", fmt.Sprintf("目录没有 SKILL.md: %s", strings.TrimSpace(raw))
		}
		name := gh.SkillName()
		if !lib.ValidName(name) {
			cleanup()
			return func() {}, nil, "name", fmt.Sprintf("无法从 URL 推导包名: %s", strings.TrimSpace(raw))
		}
		return cleanup, []Drop{{
			Name: name,
			Dir:  skillDir,
			URL:  gh.Recorded(branch, name),
		}}, "", ""
	}
	children, bad := skillChildren(dir)
	if bad {
		cleanup()
		return func() {}, nil, "badskills", fmt.Sprintf("skills 目录不合法: %s", strings.TrimSpace(raw))
	}
	drops = make([]Drop, 0, len(children))
	for _, name := range children {
		drops = append(drops, Drop{
			Name: name,
			Dir:  filepath.Join(dir, "skills", name),
			URL:  gh.Recorded(branch, name),
		})
	}
	return cleanup, drops, "", ""
}

func skillChildren(root string) ([]string, bool) {
	dir := filepath.Join(root, "skills")
	if !lib.IsDir(dir) {
		return nil, true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, true
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if !lib.IsFile(filepath.Join(dir, entry.Name(), "SKILL.md")) {
			return nil, true
		}
		if !lib.ValidName(entry.Name()) {
			return nil, true
		}
		names = append(names, entry.Name())
	}
	if len(names) == 0 {
		return nil, true
	}
	lib.SortFold(names)
	return names, false
}

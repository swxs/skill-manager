package gitpack

import (
	"fmt"
	"net/url"
	"strings"
)

// Remote 是一条 https Git 地址。GitHub 以外的地址只有本机有 git 时才能安装。
type Remote struct {
	Clone     string
	Web       string
	Owner     string
	Repo      string
	Branch    string
	SkillRel  string
	Skill     bool
	GitHub    bool
	SlashTree bool
}

// ParseRemote 去掉片段和末尾斜杠。github.com 仍按 GitHub 规则解析。
// 其它主机认 /-/tree/<分支> 和 /tree/<分支>。推不出拥有者和仓库名时失败。
func ParseRemote(raw string) (Remote, error) {
	u, ok := parseHTTPS(raw)
	if !ok {
		return Remote{}, fmt.Errorf("无法从 URL 推导包名: %s", raw)
	}
	if strings.EqualFold(u.Host, "github.com") {
		gh, err := ParseGitHub(raw)
		if err != nil {
			return Remote{}, err
		}
		clone := gh.CloneURL()
		return Remote{
			Clone:    clone,
			Web:      clone,
			Owner:    gh.Owner,
			Repo:     gh.Repo,
			Branch:   gh.Branch,
			SkillRel: gh.SkillRel,
			Skill:    gh.Skill,
			GitHub:   true,
		}, nil
	}
	return parseNonGitHub(raw, u)
}

func parseHTTPS(raw string) (*url.URL, bool) {
	text := strings.TrimSpace(raw)
	if i := strings.Index(text, "#"); i >= 0 {
		text = strings.TrimSpace(text[:i])
	}
	text = strings.TrimRight(text, "/")
	u, err := url.Parse(text)
	if err != nil || u.Host == "" || !strings.EqualFold(u.Scheme, "https") {
		return nil, false
	}
	return u, true
}

func parseNonGitHub(raw string, u *url.URL) (Remote, error) {
	fail := func() (Remote, error) {
		return Remote{}, fmt.Errorf("无法从 URL 推导包名: %s", raw)
	}
	parts := splitPath(u.Path)
	for _, part := range parts {
		if part == "" {
			return fail()
		}
	}
	project := parts
	var branch, rel string
	skill := false
	slashTree := true
	if dash, gitlab, bad := gitlabTree(parts); bad {
		return fail()
	} else if gitlab {
		project = parts[:dash]
		branch = parts[dash+2]
		if len(parts) > dash+3 {
			skill = true
			rel = strings.Join(parts[dash+3:], "/")
		}
	} else if tree := githubTree(parts); tree >= 0 {
		project = parts[:tree]
		branch = parts[tree+1]
		slashTree = false
		if len(parts) > tree+2 {
			skill = true
			rel = strings.Join(parts[tree+2:], "/")
		}
	}
	if len(project) < 2 {
		return fail()
	}
	repo := project[len(project)-1]
	if strings.HasSuffix(strings.ToLower(repo), ".git") {
		repo = repo[:len(repo)-4]
	}
	if repo == "" {
		return fail()
	}
	owner := project[len(project)-2]
	project[len(project)-1] = repo
	web := "https://" + u.Host + "/" + strings.Join(project, "/")
	clone := web
	if u.User != nil {
		clone = "https://" + u.User.String() + "@" + u.Host + "/" + strings.Join(project, "/")
	}
	return Remote{
		Clone:     clone,
		Web:       web,
		Owner:     owner,
		Repo:      repo,
		Branch:    branch,
		SkillRel:  rel,
		Skill:     skill,
		SlashTree: slashTree,
	}, nil
}

// gitlabTree 找到 GitLab 的 /-/tree/<分支>。中间有单独的 "-" 但不是这条路径时，bad 为真。
func gitlabTree(parts []string) (index int, ok bool, bad bool) {
	for i, part := range parts {
		if part != "-" {
			continue
		}
		if i < 2 || i+2 >= len(parts) || parts[i+1] != "tree" || parts[i+2] == "" {
			return 0, false, true
		}
		return i, true, false
	}
	return 0, false, false
}

// githubTree 找到 /tree/<分支>，并且前面至少有拥有者和仓库名。
func githubTree(parts []string) int {
	for i := 2; i < len(parts)-1; i++ {
		if parts[i] == "tree" && parts[i+1] != "" {
			return i
		}
	}
	return -1
}

func (r Remote) SkillName() string {
	return GitHub{SkillRel: r.SkillRel}.SkillName()
}

func (r Remote) Recorded(branch, skillName string) string {
	if r.GitHub {
		return GitHub{
			Owner:    r.Owner,
			Repo:     r.Repo,
			SkillRel: r.SkillRel,
			Skill:    r.Skill,
		}.Recorded(branch, skillName)
	}
	rel := r.SkillRel
	if !r.Skill {
		rel = "skills/" + skillName
	}
	sep := "/tree/"
	if r.SlashTree {
		sep = "/-/tree/"
	}
	return r.Web + sep + branch + "/" + rel
}

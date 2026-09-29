package search

import (
	"net/url"
	"strings"
)

// RepoRoot 把收集站给出的 GitHub 链接截成 https://github.com/{owner}/{repo}。
// 截不出仓库根时返回空，调用方按非 Git 条目处理。
func RepoRoot(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "git@github.com:") {
		return githubRoot(strings.TrimPrefix(raw, "git@github.com:"))
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Host)
	if host != "github.com" && host != "www.github.com" {
		return ""
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return ""
	}
	return githubRoot(u.Path)
}

func githubRoot(path string) string {
	path = strings.TrimSuffix(path, ".git")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	repo := strings.TrimSuffix(parts[1], ".git")
	if repo == "" {
		return ""
	}
	return "https://github.com/" + parts[0] + "/" + repo
}

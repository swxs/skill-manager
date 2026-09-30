package gitpack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	githubAPIBase = "https://api.github.com"
	githubRawBase = "https://raw.githubusercontent.com"
	errNotInTree  = fmt.Errorf("not in tree")
)

// SetFetchForTest 替换 git 探测和 GitHub 接口地址。gitOK 为 false 时当作本机没有 git。地址传空字符串恢复默认。
func SetFetchForTest(gitOK bool, apiBase, rawBase string) {
	if gitOK {
		lookPath = exec.LookPath
	} else {
		lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	}
	if apiBase == "" {
		githubAPIBase = "https://api.github.com"
	} else {
		githubAPIBase = apiBase
	}
	if rawBase == "" {
		githubRawBase = "https://raw.githubusercontent.com"
	} else {
		githubRawBase = rawBase
	}
}

type ghTree struct {
	Truncated bool      `json:"truncated"`
	Tree      []ghEntry `json:"tree"`
}

type ghEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

type ghBlob struct {
	rel  string
	mode string
}

func populateWithHTTP(dir string, gh GitHub, raw string) (string, string, string) {
	client := &http.Client{Timeout: 60 * time.Second}
	branch, commit, root, err := resolveTip(client, gh)
	if err != nil {
		return "", "fetch", fmt.Sprintf("无法取得技能: %s", strings.TrimSpace(raw))
	}
	rel := "skills"
	if gh.Skill {
		rel = gh.SkillRel
	}
	treeSHA, err := findTree(client, gh, root, rel)
	if err == errNotInTree {
		return branch, "", ""
	}
	if err != nil {
		return "", "fetch", fmt.Sprintf("无法取得技能: %s", strings.TrimSpace(raw))
	}
	var files []ghBlob
	if err := collectBlobs(client, gh, treeSHA, rel, &files); err != nil {
		return "", "fetch", fmt.Sprintf("无法取得技能: %s", strings.TrimSpace(raw))
	}
	if err := downloadBlobs(client, gh, commit, dir, files); err != nil {
		return "", "fetch", fmt.Sprintf("无法取得技能: %s", strings.TrimSpace(raw))
	}
	return branch, "", ""
}

func resolveTip(client *http.Client, gh GitHub) (string, string, string, error) {
	branch := gh.Branch
	if branch == "" {
		var repo struct {
			DefaultBranch string `json:"default_branch"`
		}
		if err := getJSON(client, apiPath(gh, ""), &repo); err != nil {
			return "", "", "", err
		}
		if repo.DefaultBranch == "" {
			return "", "", "", fmt.Errorf("没有默认分支")
		}
		branch = repo.DefaultBranch
	}
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := getJSON(client, apiPath(gh, "/git/ref/heads/"+url.PathEscape(branch)), &ref); err != nil {
		return "", "", "", err
	}
	if ref.Object.SHA == "" {
		return "", "", "", fmt.Errorf("没有提交")
	}
	var commit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := getJSON(client, apiPath(gh, "/git/commits/"+url.PathEscape(ref.Object.SHA)), &commit); err != nil {
		return "", "", "", err
	}
	if commit.Tree.SHA == "" {
		return "", "", "", fmt.Errorf("没有树")
	}
	return branch, ref.Object.SHA, commit.Tree.SHA, nil
}

func findTree(client *http.Client, gh GitHub, root, rel string) (string, error) {
	sha := root
	if rel == "" {
		return sha, nil
	}
	for _, part := range strings.Split(rel, "/") {
		tree, err := getTree(client, gh, sha)
		if err != nil {
			return "", err
		}
		next := ""
		for _, entry := range tree.Tree {
			if entry.Path == part && entry.Type == "tree" {
				next = entry.SHA
				break
			}
		}
		if next == "" {
			return "", errNotInTree
		}
		sha = next
	}
	return sha, nil
}

func collectBlobs(client *http.Client, gh GitHub, sha, prefix string, out *[]ghBlob) error {
	tree, err := getTree(client, gh, sha)
	if err != nil {
		return err
	}
	for _, entry := range tree.Tree {
		if entry.Path == "" || entry.Path == "." || entry.Path == ".." || strings.Contains(entry.Path, "/") || strings.Contains(entry.Path, "\\") {
			return fmt.Errorf("非法路径")
		}
		rel := entry.Path
		if prefix != "" {
			rel = prefix + "/" + entry.Path
		}
		switch entry.Type {
		case "blob":
			*out = append(*out, ghBlob{rel: rel, mode: entry.Mode})
		case "tree":
			if err := collectBlobs(client, gh, entry.SHA, rel, out); err != nil {
				return err
			}
		}
	}
	return nil
}

func downloadBlobs(client *http.Client, gh GitHub, commit, dest string, files []ghBlob) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var first error
	for _, file := range files {
		wg.Add(1)
		go func(file ghBlob) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			if err := downloadBlob(ctx, client, gh, commit, dest, file); err != nil {
				mu.Lock()
				if first == nil {
					first = err
					cancel()
				}
				mu.Unlock()
			}
		}(file)
	}
	wg.Wait()
	return first
}

func downloadBlob(ctx context.Context, client *http.Client, gh GitHub, commit, dest string, file ghBlob) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawFileURL(gh, commit, file.rel), nil)
	if err != nil {
		return err
	}
	applyGitHubHeaders(req, "application/octet-stream")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	target := filepath.Join(dest, filepath.FromSlash(file.rel))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fileMode(file.mode))
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, res.Body)
	return err
}

func fileMode(mode string) os.FileMode {
	if mode == "100755" {
		return 0o755
	}
	return 0o644
}

func getTree(client *http.Client, gh GitHub, sha string) (ghTree, error) {
	var tree ghTree
	if err := getJSON(client, apiPath(gh, "/git/trees/"+url.PathEscape(sha)), &tree); err != nil {
		return ghTree{}, err
	}
	if tree.Truncated {
		return ghTree{}, fmt.Errorf("树被截断")
	}
	return tree, nil
}

func getJSON(client *http.Client, rawURL string, dest any) error {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	applyGitHubHeaders(req, "application/vnd.github+json")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	return json.Unmarshal(body, dest)
}

func applyGitHubHeaders(req *http.Request, accept string) {
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "skill-manager")
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func apiPath(gh GitHub, suffix string) string {
	return strings.TrimRight(githubAPIBase, "/") + "/repos/" + url.PathEscape(gh.Owner) + "/" + url.PathEscape(gh.Repo) + suffix
}

func rawFileURL(gh GitHub, commit, rel string) string {
	u := strings.TrimRight(githubRawBase, "/") + "/" + url.PathEscape(gh.Owner) + "/" + url.PathEscape(gh.Repo) + "/" + url.PathEscape(commit)
	for _, part := range strings.Split(rel, "/") {
		u += "/" + url.PathEscape(part)
	}
	return u
}

package tests

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/swxs/skill-manager/internal/gitpack"
)

func TestFetchUsesGitHubAPIWhenGitMissing(t *testing.T) {
	const commit = "cafebabe"
	files := map[string]string{
		"/repos/acme/widgets":                                     `{"default_branch":"main"}`,
		"/repos/acme/widgets/git/ref/heads/main":                  `{"object":{"sha":"` + commit + `"}}`,
		"/repos/acme/widgets/git/commits/" + commit:               `{"tree":{"sha":"root"}}`,
		"/repos/acme/widgets/git/trees/root":                      `{"tree":[{"path":"README.md","mode":"100644","type":"blob","sha":"r"},{"path":"skills","mode":"040000","type":"tree","sha":"skills"}]}`,
		"/repos/acme/widgets/git/trees/skills":                    `{"tree":[{"path":"alpha","mode":"040000","type":"tree","sha":"alpha"},{"path":"beta","mode":"040000","type":"tree","sha":"beta"}]}`,
		"/repos/acme/widgets/git/trees/alpha":                     `{"tree":[{"path":"SKILL.md","mode":"100644","type":"blob","sha":"a"}]}`,
		"/repos/acme/widgets/git/trees/beta":                      `{"tree":[{"path":"SKILL.md","mode":"100644","type":"blob","sha":"b"},{"path":"run.sh","mode":"100755","type":"blob","sha":"s"}]}`,
		"/" + "acme/widgets/" + commit + "/skills/alpha/SKILL.md": "---\nname: alpha\n---\n",
		"/" + "acme/widgets/" + commit + "/skills/beta/SKILL.md":  "---\nname: beta\n---\n",
		"/" + "acme/widgets/" + commit + "/skills/beta/run.sh":    "echo beta\n",
		"/" + "acme/widgets/" + commit + "/README.md":             "root\n",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	gitpack.SetFetchForTest(false, srv.URL, srv.URL)
	t.Cleanup(func() { gitpack.SetFetchForTest(true, "", "") })

	cleanup, drops, kind, line := gitpack.Fetch("https://github.com/acme/widgets")
	defer cleanup()
	if kind != "" || line != "" {
		t.Fatalf("kind %s line %s", kind, line)
	}
	if len(drops) != 2 || drops[0].Name != "alpha" || drops[1].Name != "beta" {
		t.Fatalf("drops %+v", drops)
	}
	if drops[0].URL != "https://github.com/acme/widgets/tree/main/skills/alpha" {
		t.Fatal(drops[0].URL)
	}
	text, err := os.ReadFile(filepath.Join(drops[1].Dir, "run.sh"))
	if err != nil || string(text) != "echo beta\n" {
		t.Fatalf("%q %v", text, err)
	}
	root := filepath.Dir(filepath.Dir(drops[0].Dir))
	if _, err := os.Stat(filepath.Join(root, "README.md")); !os.IsNotExist(err) {
		t.Fatalf("readme err %v", err)
	}

	cleanupOne, one, kind, line := gitpack.Fetch("https://github.com/acme/widgets/tree/main/skills/beta")
	defer cleanupOne()
	if kind != "" || len(one) != 1 || one[0].Name != "beta" {
		t.Fatalf("kind %s line %s drops %+v", kind, line, one)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(one[0].Dir), "alpha")); !os.IsNotExist(err) {
		t.Fatalf("sibling err %v", err)
	}

	cleanupMiss, _, kind, line := gitpack.Fetch("https://github.com/acme/widgets/tree/main/skills/missing")
	defer cleanupMiss()
	if kind != "noskill" || line != "目录没有 SKILL.md: https://github.com/acme/widgets/tree/main/skills/missing" {
		t.Fatalf("kind %s line %s", kind, line)
	}
}

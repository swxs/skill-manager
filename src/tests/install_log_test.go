package tests

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swxs/skill-manager/internal/gitpack"
)

func TestInstallLogFileUsesTemp(t *testing.T) {
	gitpack.SetInstallLogForTest("")
	t.Cleanup(func() { gitpack.SetInstallLogForTest("") })
	want := filepath.Join(os.TempDir(), "skill-manager-install.log")
	if gitpack.InstallLogFile() != want {
		t.Fatalf("got %s", gitpack.InstallLogFile())
	}
}

func TestInstallLogRecordsLocalGitAndRateLimit(t *testing.T) {
	t.Run("local", func(t *testing.T) {
		root, _, library := withHome(t)
		source := filepath.Join(root, "sources", "demo")
		writeSkill(t, filepath.Join(source, "one"))
		code, _, errOut := runCLIStreams(t, library, "install", source)
		if code != 0 || errOut != "" {
			t.Fatalf("code %d err %q", code, errOut)
		}
		text := installLog(t, root)
		if !strings.Contains(text, "安装 "+source) || !strings.Contains(text, "结束 0") {
			t.Fatal(text)
		}
		if strings.Contains(text, "使用 git") || strings.Contains(text, "使用 GitHub 接口") {
			t.Fatal(text)
		}
	})

	t.Run("git", func(t *testing.T) {
		root, _, library := withHome(t)
		upstream := filepath.Join(root, "upstream")
		writeSkill(t, filepath.Join(upstream, "skills", "alpha"))
		initGitRepo(t, upstream)
		prefix := "https://github.com/acme/widgets"
		pointGitHubAt(t, upstream, prefix)
		url := prefix + "/tree/main/skills/alpha"
		code, _, errOut := runCLIStreams(t, library, "install", url)
		if code != 0 || errOut != "" {
			t.Fatalf("code %d err %q", code, errOut)
		}
		text := installLog(t, root)
		for _, want := range []string{"安装 " + url, "使用 git", "浅克隆完成", "已检出 skills/alpha 分支 main", "结束 0"} {
			if !strings.Contains(text, want) {
				t.Fatalf("missing %q\n%s", want, text)
			}
		}
	})

	t.Run("rate limit", func(t *testing.T) {
		root, _, library := withHome(t)
		const token = "secret-token-value"
		t.Setenv("GITHUB_TOKEN", token)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-RateLimit-Limit", "60")
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", "1700000000")
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"API rate limit exceeded ` + token + `"}`))
		}))
		defer srv.Close()
		gitpack.SetFetchForTest(false, srv.URL, srv.URL)
		t.Cleanup(func() { gitpack.SetFetchForTest(true, "", "") })

		url := "https://github.com/acme/widgets/tree/main/skills/alpha"
		code, _, errOut := runCLIStreams(t, library, "install", url)
		if code != 1 || errOut != "无法取得技能: "+url+"\n" {
			t.Fatalf("code %d err %q", code, errOut)
		}
		text := installLog(t, root)
		for _, want := range []string{
			"安装 " + url,
			"使用 GitHub 接口",
			"HTTP 403",
			"X-RateLimit-Remaining: 0",
			"Retry-After: 30",
			"API rate limit exceeded <REDACTED>",
			"结束 1",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("missing %q\n%s", want, text)
			}
		}
		if strings.Contains(text, token) {
			t.Fatal(text)
		}
	})
}

func installLog(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "install.log"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

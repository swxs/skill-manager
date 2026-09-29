package tests

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/swxs/skill-manager/internal/cli"
	"github.com/swxs/skill-manager/internal/search"
)

func useSearch(t *testing.T, handler http.Handler, install func(string, string) int) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	search.Configure(search.Config{
		Client: srv.Client(),
		Bases: map[string]string{
			"skillsmp":   srv.URL,
			"modelscope": srv.URL,
		},
	})
	if install == nil {
		install = func(library, url string) int {
			t.Errorf("unexpected install %s", url)
			return 1
		}
	}
	cli.SetSearchInstallForTest(install)
	t.Cleanup(func() {
		search.ResetConfig()
		cli.SetSearchInstallForTest(nil)
	})
}

func jsonHandler(paths map[string]string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := paths[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	})
}

func TestSearchListsSkillsMPAndStops(t *testing.T) {
	var paths []string
	useSearch(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path != "/api/v1/skills/search" {
			t.Errorf("path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("q") != "react native" {
			t.Errorf("q %q", r.URL.Query().Get("q"))
		}
		_, _ = w.Write([]byte(`{"data":{"skills":[{"id":"abc","name":"Grill","description":"问清楚","githubUrl":"https://github.com/o/r/tree/main/a"},{"id":"def","name":"Other"}]}}`))
	}), nil)
	_, _, library := withHome(t)
	code, out, errOut := runCLIStreams(t, library, "search", "react", "native")
	if code != 0 || errOut != "" {
		t.Fatalf("code %d stderr %q stdout %q", code, errOut, out)
	}
	want := "skillsmp.abc\n问清楚\nhttps://github.com/o/r/tree/main/a\n\nskillsmp.def\nOther\n没有 Git 地址\n"
	if out != want {
		t.Fatalf("%q", out)
	}
	if len(paths) != 1 {
		t.Fatalf("paths %v", paths)
	}
}

func TestSearchEmptyQueryStillRequests(t *testing.T) {
	var got string
	useSearch(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("q")
		_, _ = w.Write([]byte(`{"data":{"skills":[]}}`))
	}), nil)
	_, _, library := withHome(t)
	code, out, errOut := runCLIStreams(t, library, "search")
	if code != 0 || out != "" || errOut != "" || got != "" {
		t.Fatalf("code %d out %q err %q q %q", code, out, errOut, got)
	}
}

func TestSearchBothTooShortAndBothDown(t *testing.T) {
	_, _, library := withHome(t)
	useSearch(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"query too short"}`))
	}), nil)
	code, out, errOut := runCLIStreams(t, library, "search", "x")
	if code != 2 || out != "" || errOut != "检索词过短\n" {
		t.Fatalf("short code %d out %q err %q", code, out, errOut)
	}

	useSearch(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}), nil)
	code, out, errOut = runCLIStreams(t, library, "search", "grill")
	if code != 1 || out != "" || errOut != "收集站不可用\n" {
		t.Fatalf("down code %d out %q err %q", code, out, errOut)
	}
}

func TestSearchSecondEmptyIsSilent(t *testing.T) {
	useSearch(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/skills/search" {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"skills":[]}}`))
	}), nil)
	_, _, library := withHome(t)
	code, out, errOut := runCLIStreams(t, library, "search", "none")
	if code != 0 || out != "" || errOut != "" {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
}

func TestSearchInstallUsage(t *testing.T) {
	deny := &denyTransport{t: t}
	search.Configure(search.Config{Client: &http.Client{Transport: deny}})
	cli.SetSearchInstallForTest(func(library, url string) int {
		t.Errorf("unexpected install %s", url)
		return 1
	})
	t.Cleanup(func() {
		search.ResetConfig()
		cli.SetSearchInstallForTest(nil)
	})
	_, _, library := withHome(t)
	code, _, errOut := runCLIStreams(t, library, "search", "--install")
	if code != 2 || errOut != "缺少 --install 的值\n" {
		t.Fatalf("missing code %d err %q", code, errOut)
	}
	code, _, errOut = runCLIStreams(t, library, "search", "--install", "skillsmp.abc", "extra")
	if code != 2 || errOut != "search --install 只接受一个点号串\n" {
		t.Fatalf("extra code %d err %q", code, errOut)
	}
	code, _, errOut = runCLIStreams(t, library, "search", "--install", "nodot")
	if code != 2 || errOut != "点号串无法切开: nodot\n" {
		t.Fatalf("split code %d err %q", code, errOut)
	}
	code, _, errOut = runCLIStreams(t, library, "search", "--install", "other.abc")
	if code != 2 || errOut != "未知收集站: other\n" {
		t.Fatalf("unknown code %d err %q", code, errOut)
	}
}

type denyTransport struct{ t *testing.T }

func (d denyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	d.t.Errorf("unexpected request %s", r.URL)
	return nil, errors.New("unexpected")
}

func TestSearchInstallMissAndDown(t *testing.T) {
	_, _, library := withHome(t)
	useSearch(t, jsonHandler(map[string]string{
		"/api/v1/skills/search": `{"data":{"skills":[{"id":"other","name":"N","githubUrl":"https://github.com/o/r"}]}}`,
	}), nil)
	code, out, errOut := runCLIStreams(t, library, "search", "--install", "skillsmp.missing")
	if code != 1 || out != "" || errOut != "没有这条收录: skillsmp.missing\n" {
		t.Fatalf("miss code %d out %q err %q", code, out, errOut)
	}

	useSearch(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}), nil)
	code, out, errOut = runCLIStreams(t, library, "search", "--install", "skillsmp.abc")
	if code != 1 || out != "" || errOut != "没有这条收录: skillsmp.abc\n" {
		t.Fatalf("down code %d out %q err %q", code, out, errOut)
	}
}

func TestSearchInstallNonGitDoesNotInstall(t *testing.T) {
	useSearch(t, jsonHandler(map[string]string{
		"/openapi/v1/skills": `{"data":{"skills":[{"id":"@ajoslin/grill-me","display_name":"Grill Me","source_url":""}]}}`,
	}), nil)
	_, _, library := withHome(t)
	code, out, errOut := runCLIStreams(t, library, "search", "--install", "modelscope.@ajoslin/grill-me")
	if code != 0 || errOut != "" || out != "未安装，没有 Git 地址: modelscope.@ajoslin/grill-me\n" {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
}

func TestSearchInstallExistingPack(t *testing.T) {
	body := `{"data":{"skills":[{"id":"abc","name":"N","githubUrl":"https://github.com/octocat/Hello-World/tree/master/src"}]}}`
	_, _, library := withHome(t)
	useSearch(t, jsonHandler(map[string]string{"/api/v1/skills/search": body}), nil)
	if err := os.MkdirAll(filepath.Join(library, "Hello-World", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runCLIStreams(t, library, "search", "--install", "skillsmp.abc")
	if code != 0 || errOut != "" || out != "库里已有 git 包: Hello-World\n" {
		t.Fatalf("git code %d out %q err %q", code, out, errOut)
	}

	plain := filepath.Join(library, "Hello-World")
	if err := os.RemoveAll(filepath.Join(plain, ".git")); err != nil {
		t.Fatal(err)
	}
	code, out, errOut = runCLIStreams(t, library, "search", "--install", "skillsmp.abc")
	if code != 0 || errOut != "" || out != "库里已有非 git 目录: Hello-World\n" {
		t.Fatalf("dir code %d out %q err %q", code, out, errOut)
	}
}

func TestSearchInstallReceivesRepoRoot(t *testing.T) {
	var got string
	useSearch(t, jsonHandler(map[string]string{
		"/api/v1/skills/search": `{"data":{"skills":[{"id":"abc","name":"N","githubUrl":"https://github.com/octocat/Hello-World/tree/master/src"}]}}`,
	}), func(library, url string) int {
		got = url
		return 0
	})
	_, _, library := withHome(t)
	code, out, errOut := runCLIStreams(t, library, "search", "--install", "skillsmp.abc")
	if code != 0 || out != "" || errOut != "" {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
	if got != "https://github.com/octocat/Hello-World" {
		t.Fatalf("install url %s", got)
	}
}

func TestSearchInstallRejectsBadPackName(t *testing.T) {
	useSearch(t, jsonHandler(map[string]string{
		"/api/v1/skills/search": `{"data":{"skills":[{"id":"abc","name":"N","githubUrl":"https://github.com/owner/.hidden"}]}}`,
	}), nil)
	_, _, library := withHome(t)
	code, out, errOut := runCLIStreams(t, library, "search", "--install", "skillsmp.abc")
	if code != 2 || errOut != "" || out != "无法从 URL 推导包名: https://github.com/owner/.hidden\n" {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(library, ".hidden")); !os.IsNotExist(err) {
		t.Fatalf("disk changed: %v", err)
	}
}

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

func isolateSearchCache(t *testing.T) {
	t.Helper()
	search.SetCacheFileForTest(filepath.Join(t.TempDir(), "search.json"))
	t.Cleanup(func() { search.SetCacheFileForTest("") })
}

func rememberSearch(t *testing.T, records ...search.Record) {
	t.Helper()
	if err := search.Remember(records); err != nil {
		t.Fatal(err)
	}
}

func useSearch(t *testing.T, handler http.Handler, install func(string, string) int) {
	t.Helper()
	isolateSearchCache(t)
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
	isolateSearchCache(t)
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

func TestSearchInstallMissDoesNotQuery(t *testing.T) {
	isolateSearchCache(t)
	search.Configure(search.Config{Client: &http.Client{Transport: denyTransport{t: t}}})
	cli.SetSearchInstallForTest(func(library, url string) int {
		t.Errorf("unexpected install %s", url)
		return 1
	})
	t.Cleanup(func() {
		search.ResetConfig()
		cli.SetSearchInstallForTest(nil)
	})
	_, _, library := withHome(t)
	token := "skillsmp.mattpocock-skills-skills-engineering-tdd-skill-md"
	code, out, errOut := runCLIStreams(t, library, "search", "--install", token)
	if code != 1 || out != "" || errOut != "没找到对应技能: "+token+"。请重新查询\n" {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
}

func TestSearchInstallNonGitDoesNotInstall(t *testing.T) {
	isolateSearchCache(t)
	cli.SetSearchInstallForTest(func(library, url string) int {
		t.Errorf("unexpected install %s", url)
		return 1
	})
	t.Cleanup(func() { cli.SetSearchInstallForTest(nil) })
	rememberSearch(t, search.Record{Catalog: "modelscope", ID: "@ajoslin/grill-me", DisplayName: "Grill Me"})
	_, _, library := withHome(t)
	code, out, errOut := runCLIStreams(t, library, "search", "--install", "modelscope.@ajoslin/grill-me")
	if code != 0 || errOut != "" || out != "未安装，没有 Git 地址: modelscope.@ajoslin/grill-me\n" {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
}

func TestSearchInstallExistingPack(t *testing.T) {
	isolateSearchCache(t)
	cli.SetSearchInstallForTest(func(library, url string) int {
		t.Errorf("unexpected install %s", url)
		return 1
	})
	t.Cleanup(func() { cli.SetSearchInstallForTest(nil) })
	rememberSearch(t, search.Record{
		Catalog: "skillsmp",
		ID:      "abc",
		RawURL:  "https://github.com/octocat/Hello-World/tree/master/src",
	})
	_, _, library := withHome(t)
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
	isolateSearchCache(t)
	var got string
	cli.SetSearchInstallForTest(func(library, url string) int {
		got = url
		return 0
	})
	t.Cleanup(func() { cli.SetSearchInstallForTest(nil) })
	rememberSearch(t, search.Record{
		Catalog: "skillsmp",
		ID:      "abc",
		RawURL:  "https://github.com/octocat/Hello-World/tree/master/src",
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
	isolateSearchCache(t)
	cli.SetSearchInstallForTest(func(library, url string) int {
		t.Errorf("unexpected install %s", url)
		return 1
	})
	t.Cleanup(func() { cli.SetSearchInstallForTest(nil) })
	rememberSearch(t, search.Record{
		Catalog: "skillsmp",
		ID:      "abc",
		RawURL:  "https://github.com/owner/.hidden",
	})
	_, _, library := withHome(t)
	code, out, errOut := runCLIStreams(t, library, "search", "--install", "skillsmp.abc")
	if code != 2 || errOut != "" || out != "无法从 URL 推导包名: https://github.com/owner/.hidden\n" {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(library, ".hidden")); !os.IsNotExist(err) {
		t.Fatalf("disk changed: %v", err)
	}
}

func TestSearchInstallReadsCacheNotCatalog(t *testing.T) {
	var calls int
	var got string
	useSearch(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/v1/skills/search" {
			t.Errorf("path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"skills":[{"id":"mattpocock-skills-skills-engineering-tdd-skill-md","name":"tdd","description":"Test-driven development.","githubUrl":"https://github.com/mattpocock/skills/tree/main/skills/engineering/tdd"}]}}`))
	}), func(library, url string) int {
		got = url
		return 0
	})
	_, _, library := withHome(t)
	token := "skillsmp.mattpocock-skills-skills-engineering-tdd-skill-md"
	code, out, errOut := runCLIStreams(t, library, "search", "tdd")
	if code != 0 || errOut != "" || calls != 1 {
		t.Fatalf("list code %d err %q calls %d out %q", code, errOut, calls, out)
	}
	code, out, errOut = runCLIStreams(t, library, "search", "--install", token)
	if code != 0 || out != "" || errOut != "" || calls != 1 {
		t.Fatalf("install code %d out %q err %q calls %d", code, out, errOut, calls)
	}
	if got != "https://github.com/mattpocock/skills" {
		t.Fatalf("install url %s", got)
	}
}

func TestEmptySearchDoesNotDropCache(t *testing.T) {
	useSearch(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"skills":[]}}`))
	}), nil)
	rememberSearch(t, search.Record{Catalog: "skillsmp", ID: "keep", DisplayName: "Keep", RawURL: "https://github.com/o/r"})
	_, _, library := withHome(t)
	code, out, errOut := runCLIStreams(t, library, "search", "none")
	if code != 0 || out != "" || errOut != "" {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
	rec, found, err := search.Find("skillsmp.keep")
	if err != nil || !found || rec.RawURL != "https://github.com/o/r" {
		t.Fatalf("found %v err %v rec %+v", found, err, rec)
	}
}

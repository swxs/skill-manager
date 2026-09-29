package tests

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

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

func useSearch(t *testing.T, handler http.Handler) {
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
	t.Cleanup(func() {
		search.ResetConfig()
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
	}))
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
	}))
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
	}))
	code, out, errOut := runCLIStreams(t, library, "search", "x")
	if code != 2 || out != "" || errOut != "检索词过短\n" {
		t.Fatalf("short code %d out %q err %q", code, out, errOut)
	}

	useSearch(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
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
	}))
	_, _, library := withHome(t)
	code, out, errOut := runCLIStreams(t, library, "search", "none")
	if code != 0 || out != "" || errOut != "" {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
}

func TestSearchInstallRejected(t *testing.T) {
	isolateSearchCache(t)
	search.Configure(search.Config{Client: &http.Client{Transport: denyTransport{t: t}}})
	t.Cleanup(search.ResetConfig)
	_, _, library := withHome(t)
	for _, args := range [][]string{
		{"search", "--install"},
		{"search", "--install", "skillsmp.abc"},
		{"search", "tdd", "--install"},
	} {
		code, out, errOut := runCLIStreams(t, library, args...)
		if code != 2 || out != "" || errOut != "不再接受 search --install\n" {
			t.Fatalf("%v code %d out %q err %q", args, code, out, errOut)
		}
	}
}

type denyTransport struct{ t *testing.T }

func (d denyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	d.t.Errorf("unexpected request %s", r.URL)
	return nil, errors.New("unexpected")
}

func TestEmptySearchDoesNotDropCache(t *testing.T) {
	useSearch(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"skills":[]}}`))
	}))
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

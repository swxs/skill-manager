package tests

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swxs/skill-manager/internal/search"
)

func TestSkillsMPMapsFields(t *testing.T) {
	var gotQ string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/skills/search" {
			t.Errorf("path %s", r.URL.Path)
		}
		gotQ = r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"skills":[{"id":"abc-id","name":"Grill","description":"问清楚","githubUrl":"https://github.com/o/r/tree/main/skills/grill"}]}}`))
	}))
	t.Cleanup(srv.Close)
	cat, ok := search.New("skillsmp", search.Config{Client: srv.Client(), Bases: map[string]string{"skillsmp": srv.URL}})
	if !ok {
		t.Fatal("factory")
	}
	out := cat.Search("react native")
	if gotQ != "react native" {
		t.Fatalf("q %q", gotQ)
	}
	if out.Kind != search.KindHits || len(out.Records) != 1 {
		t.Fatalf("%+v", out)
	}
	rec := out.Records[0]
	if rec.Catalog != "skillsmp" || rec.ID != "abc-id" || rec.DisplayName != "Grill" || rec.Description != "问清楚" {
		t.Fatalf("%+v", rec)
	}
	if rec.RawURL != "https://github.com/o/r/tree/main/skills/grill" {
		t.Fatalf("raw %s", rec.RawURL)
	}
}

func TestModelScopeMapsFields(t *testing.T) {
	var gotSearch string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openapi/v1/skills" {
			t.Errorf("path %s", r.URL.Path)
		}
		gotSearch = r.URL.Query().Get("search")
		_, _ = w.Write([]byte(`{"data":{"skills":[{"id":"@ajoslin/grill-me","display_name":"Grill Me","source_url":"https://github.com/ajoslin/dot/tree/main/config/grill-me"},{"id":"plain","display_name":"No Git","source_url":""}]}}`))
	}))
	t.Cleanup(srv.Close)
	cat, ok := search.New("modelscope", search.Config{Client: srv.Client(), Bases: map[string]string{"modelscope": srv.URL}})
	if !ok {
		t.Fatal("factory")
	}
	out := cat.Search("@ajoslin/grill-me")
	if gotSearch != "@ajoslin/grill-me" {
		t.Fatalf("search %q", gotSearch)
	}
	if out.Kind != search.KindHits || len(out.Records) != 2 {
		t.Fatalf("%+v", out)
	}
	rec := out.Records[0]
	if rec.Catalog != "modelscope" || rec.ID != "@ajoslin/grill-me" || rec.DisplayName != "Grill Me" || rec.Description != "" {
		t.Fatalf("%+v", rec)
	}
	if rec.RawURL != "https://github.com/ajoslin/dot/tree/main/config/grill-me" {
		t.Fatalf("raw %s", rec.RawURL)
	}
	if out.Records[1].RawURL != "" || out.Records[1].Blurb() != "No Git" {
		t.Fatalf("%+v", out.Records[1])
	}
}

func TestHTTPOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		header map[string]string
		body   string
		kind   search.Kind
	}{
		{name: "too short", status: 400, body: `{"error":"query too short"}`, kind: search.KindTooShort},
		{name: "bad request", status: 400, body: `{"error":"bad request"}`, kind: search.KindUnavailable},
		{name: "quota", status: 429, body: `{"data":{"skills":[{"id":"a","name":"n"}]}}`, kind: search.KindQuota},
		{name: "down", status: 500, body: `nope`, kind: search.KindUnavailable},
		{name: "bad json", status: 200, body: `not-json`, kind: search.KindUnavailable},
		{name: "empty", status: 200, body: `{"data":{"skills":[]}}`, kind: search.KindEmpty},
		{name: "remaining zero", status: 200, header: map[string]string{"X-RateLimit-Remaining": "0"}, body: `{"data":{"skills":[]}}`, kind: search.KindQuota},
		{name: "hits despite remaining", status: 200, header: map[string]string{"X-RateLimit-Daily-Remaining": "0"}, body: `{"skills":[{"id":"a","name":"n","githubUrl":"https://github.com/o/r"}]}`, kind: search.KindHits},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for key, value := range tc.header {
					w.Header().Set(key, value)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)
			cat, _ := search.New("skillsmp", search.Config{Client: srv.Client(), Bases: map[string]string{"skillsmp": srv.URL}})
			out := cat.Search("q")
			if out.Kind != tc.kind {
				t.Fatalf("got %v", out.Kind)
			}
		})
	}
}

func TestClosedServerIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := srv.Client()
	base := srv.URL
	srv.Close()
	cat, _ := search.New("skillsmp", search.Config{Client: client, Bases: map[string]string{"skillsmp": base}})
	if cat.Search("q").Kind != search.KindUnavailable {
		t.Fatal("expected unavailable")
	}
}

func TestUnknownCatalogIsNotBuilt(t *testing.T) {
	if _, ok := search.New("skills.sh", search.Config{}); ok {
		t.Fatal("unknown catalog was built")
	}
}

func TestDefaultPipelineUsesSkillsMPFirst(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if strings.Contains(r.URL.Path, "modelscope") || r.URL.Path == "/openapi/v1/skills" {
			t.Errorf("asked modelscope")
		}
		_, _ = w.Write([]byte(`{"data":{"skills":[{"id":"abc","name":"N","githubUrl":"https://github.com/o/r/tree/main/x"}]}}`))
	}))
	t.Cleanup(srv.Close)
	out := search.DefaultPipeline(search.Config{
		Client: srv.Client(),
		Bases:  map[string]string{"skillsmp": srv.URL, "modelscope": srv.URL},
	}).List("grill")
	if out.Kind != search.KindHits || len(paths) != 1 || paths[0] != "/api/v1/skills/search" {
		t.Fatalf("out %+v paths %v", out, paths)
	}
	if out.Records[0].RawURL != "https://github.com/o/r/tree/main/x" {
		t.Fatalf("raw %s", out.Records[0].RawURL)
	}
}

func TestRepoRoot(t *testing.T) {
	cases := map[string]string{
		"https://github.com/o/r/tree/main/skills/grill": "https://github.com/o/r",
		"https://github.com/o/r/blob/main/SKILL.md":     "https://github.com/o/r",
		"https://github.com/o/r.git":                    "https://github.com/o/r",
		"https://www.github.com/o/r":                    "https://github.com/o/r",
		"git@github.com:o/r.git":                        "https://github.com/o/r",
		"https://gitlab.com/o/r":                        "",
		"":                                              "",
		"https://github.com/only":                       "",
	}
	for raw, want := range cases {
		if got := search.RepoRoot(raw); got != want {
			t.Errorf("%s -> %s, want %s", raw, got, want)
		}
	}
}

func TestFormatListAndToken(t *testing.T) {
	text := search.FormatList([]search.Record{
		{Catalog: "skillsmp", ID: "a", Description: "一", RawURL: "https://github.com/o/r/tree/main/a"},
		{Catalog: "skillsmp", ID: "b", DisplayName: "乙"},
	})
	want := "skillsmp.a\n一\nhttps://github.com/o/r/tree/main/a\n\nskillsmp.b\n乙\n没有 Git 地址\n"
	if text != want {
		t.Fatalf("%q", text)
	}
}

func TestSplitToken(t *testing.T) {
	catalog, id, ok := search.SplitToken("modelscope.@ajoslin/grill-me")
	if !ok || catalog != "modelscope" || id != "@ajoslin/grill-me" {
		t.Fatalf("%s %s %v", catalog, id, ok)
	}
	for _, token := range []string{"nodot", ".id", "skillsmp.", ""} {
		if _, _, ok := search.SplitToken(token); ok {
			t.Fatalf("split %q", token)
		}
	}
}

func TestExactRequiresSameID(t *testing.T) {
	out := search.Outcome{Kind: search.KindHits, Records: []search.Record{{ID: "other"}}}
	if _, ok := search.Exact(out, "wanted"); ok {
		t.Fatal("matched other id")
	}
	if _, ok := search.Exact(search.Outcome{Kind: search.KindEmpty}, "wanted"); ok {
		t.Fatal("matched empty")
	}
	rec, ok := search.Exact(search.Outcome{Kind: search.KindHits, Records: []search.Record{{ID: "wanted", RawURL: "u"}}}, "wanted")
	if !ok || rec.RawURL != "u" {
		t.Fatalf("%+v %v", rec, ok)
	}
}

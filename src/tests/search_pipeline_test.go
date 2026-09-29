package tests

import (
	"testing"

	"github.com/swxs/skill-manager/internal/search"
)

type fakeCatalog struct {
	name  string
	out   search.Outcome
	calls int
	query string
}

func (f *fakeCatalog) Name() string { return f.name }

func (f *fakeCatalog) Search(query string) search.Outcome {
	f.calls++
	f.query = query
	return f.out
}

func searchHit(id string) search.Outcome {
	return search.Outcome{Kind: search.KindHits, Records: []search.Record{{Catalog: "x", ID: id, DisplayName: id}}}
}

func TestPipelineStopsWhenFirstHasHits(t *testing.T) {
	first := &fakeCatalog{name: "a", out: searchHit("one")}
	second := &fakeCatalog{name: "b", out: searchHit("two")}
	out := search.NewPipeline([]search.Catalog{first, second}).List("grill")
	if out.Kind != search.KindHits || len(out.Records) != 1 || out.Records[0].ID != "one" {
		t.Fatalf("got %+v", out)
	}
	if first.calls != 1 || second.calls != 0 {
		t.Fatalf("calls %d %d", first.calls, second.calls)
	}
	if first.query != "grill" {
		t.Fatalf("query %q", first.query)
	}
}

func TestPipelineContinuesAfterEmptyQuotaAndUnavailable(t *testing.T) {
	cases := []search.Kind{search.KindEmpty, search.KindQuota, search.KindUnavailable}
	for _, kind := range cases {
		first := &fakeCatalog{name: "a", out: search.Outcome{Kind: kind}}
		second := &fakeCatalog{name: "b", out: searchHit("two")}
		out := search.NewPipeline([]search.Catalog{first, second}).List("q")
		if out.Kind != search.KindHits || second.calls != 1 || out.Records[0].ID != "two" {
			t.Fatalf("kind %v got %+v calls %d", kind, out, second.calls)
		}
	}
}

func TestPipelineBothEmptyIsEmpty(t *testing.T) {
	first := &fakeCatalog{out: search.Outcome{Kind: search.KindEmpty}}
	second := &fakeCatalog{out: search.Outcome{Kind: search.KindEmpty}}
	out := search.NewPipeline([]search.Catalog{first, second}).List("")
	if out.Kind != search.KindEmpty || first.query != "" {
		t.Fatalf("got %+v query %q", out, first.query)
	}
}

func TestPipelineSecondEmptyAfterFailureIsEmpty(t *testing.T) {
	for _, kind := range []search.Kind{search.KindQuota, search.KindUnavailable} {
		out := search.NewPipeline([]search.Catalog{
			&fakeCatalog{out: search.Outcome{Kind: kind}},
			&fakeCatalog{out: search.Outcome{Kind: search.KindEmpty}},
		}).List("q")
		if out.Kind != search.KindEmpty {
			t.Fatalf("kind %v got %v", kind, out.Kind)
		}
	}
}

func TestPipelineBothTooShort(t *testing.T) {
	out := search.NewPipeline([]search.Catalog{
		&fakeCatalog{out: search.Outcome{Kind: search.KindTooShort}},
		&fakeCatalog{out: search.Outcome{Kind: search.KindTooShort}},
	}).List("x")
	if out.Kind != search.KindTooShort {
		t.Fatalf("got %v", out.Kind)
	}
}

func TestPipelineTooShortYieldsToSuccess(t *testing.T) {
	hits := search.NewPipeline([]search.Catalog{
		&fakeCatalog{out: search.Outcome{Kind: search.KindTooShort}},
		&fakeCatalog{out: searchHit("ok")},
	}).List("x")
	if hits.Kind != search.KindHits {
		t.Fatalf("hits got %v", hits.Kind)
	}
	empty := search.NewPipeline([]search.Catalog{
		&fakeCatalog{out: search.Outcome{Kind: search.KindTooShort}},
		&fakeCatalog{out: search.Outcome{Kind: search.KindEmpty}},
	}).List("x")
	if empty.Kind != search.KindEmpty {
		t.Fatalf("empty got %v", empty.Kind)
	}
	kept := search.NewPipeline([]search.Catalog{
		&fakeCatalog{out: search.Outcome{Kind: search.KindEmpty}},
		&fakeCatalog{out: search.Outcome{Kind: search.KindTooShort}},
	}).List("x")
	if kept.Kind != search.KindEmpty {
		t.Fatalf("kept got %v", kept.Kind)
	}
}

func TestPipelineUnavailableWhenNothingSucceeded(t *testing.T) {
	cases := [][]search.Kind{
		{search.KindUnavailable, search.KindUnavailable},
		{search.KindQuota, search.KindQuota},
		{search.KindEmpty, search.KindUnavailable},
		{search.KindTooShort, search.KindUnavailable},
		{search.KindUnavailable, search.KindTooShort},
	}
	for _, kinds := range cases {
		out := search.NewPipeline([]search.Catalog{
			&fakeCatalog{out: search.Outcome{Kind: kinds[0]}},
			&fakeCatalog{out: search.Outcome{Kind: kinds[1]}},
		}).List("q")
		if out.Kind != search.KindUnavailable {
			t.Fatalf("%v got %v", kinds, out.Kind)
		}
	}
}

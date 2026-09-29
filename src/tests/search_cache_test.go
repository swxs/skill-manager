package tests

import (
	"path/filepath"
	"testing"

	"github.com/swxs/skill-manager/internal/search"
)

func TestRememberKeepsLastFiveAndNewestWins(t *testing.T) {
	search.SetCacheFileForTest(filepath.Join(t.TempDir(), "search.json"))
	t.Cleanup(func() { search.SetCacheFileForTest("") })

	for i := 1; i <= 5; i++ {
		id := string(rune('a' + i - 1))
		if err := search.Remember([]search.Record{{Catalog: "skillsmp", ID: id, RawURL: "https://github.com/o/" + id}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := search.Remember([]search.Record{{Catalog: "skillsmp", ID: "a", RawURL: "https://github.com/o/newer"}}); err != nil {
		t.Fatal(err)
	}
	if err := search.Remember([]search.Record{{Catalog: "skillsmp", ID: "f", RawURL: "https://github.com/o/f"}}); err != nil {
		t.Fatal(err)
	}
	if _, found, err := search.Find("skillsmp.b"); err != nil || found {
		t.Fatalf("old search still present: found %v err %v", found, err)
	}
	rec, found, err := search.Find("skillsmp.a")
	if err != nil || !found || rec.RawURL != "https://github.com/o/newer" {
		t.Fatalf("found %v err %v rec %+v", found, err, rec)
	}
	if _, found, err := search.Find("skillsmp.f"); err != nil || !found {
		t.Fatalf("newest missing: found %v err %v", found, err)
	}
}

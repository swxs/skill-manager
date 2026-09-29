package search

import (
	"net/http"
	"net/url"
)

const skillsMPName = "skillsmp"

type skillsMP struct {
	client *http.Client
	base   string
}

func (s *skillsMP) Name() string { return skillsMPName }

func (s *skillsMP) Search(query string) Outcome {
	raw := endpoint(s.base, "/api/v1/skills/search", url.Values{"q": {query}})
	kind, skills := interpret(doGET(s.client, raw))
	return mapRecords(kind, skillsMPName, skills, "githubUrl", "github_url")
}

func mapRecords(kind Kind, catalog string, skills []map[string]any, urlKeys ...string) Outcome {
	if kind != KindHits {
		return Outcome{Kind: kind}
	}
	records := make([]Record, 0, len(skills))
	for _, skill := range skills {
		id := stringField(skill, "id")
		if id == "" {
			continue
		}
		records = append(records, Record{
			Catalog:     catalog,
			ID:          id,
			DisplayName: stringField(skill, "display_name", "displayName", "name"),
			Description: stringField(skill, "description", "summary"),
			RawURL:      stringField(skill, urlKeys...),
		})
	}
	if len(records) == 0 {
		return Outcome{Kind: KindEmpty}
	}
	return Outcome{Kind: KindHits, Records: records}
}

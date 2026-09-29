package search

import (
	"net/http"
	"net/url"
)

const modelScopeName = "modelscope"

type modelScope struct {
	client *http.Client
	base   string
}

func (s *modelScope) Name() string { return modelScopeName }

func (s *modelScope) Search(query string) Outcome {
	raw := endpoint(s.base, "/openapi/v1/skills", url.Values{"search": {query}})
	kind, skills := interpret(doGET(s.client, raw))
	return mapRecords(kind, modelScopeName, skills, "source_url", "sourceUrl")
}

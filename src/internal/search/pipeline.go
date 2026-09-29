package search

// Pipeline 按给定顺序询问收集站。循环里不出现站名。
type Pipeline struct {
	catalogs []Catalog
}

// NewPipeline 用调用方给的顺序组装流水线。
func NewPipeline(catalogs []Catalog) *Pipeline {
	return &Pipeline{catalogs: catalogs}
}

// DefaultPipeline 先 SkillsMP，再 ModelScope。
func DefaultPipeline(cfg Config) *Pipeline {
	names := []string{skillsMPName, modelScopeName}
	catalogs := make([]Catalog, 0, len(names))
	for _, name := range names {
		catalog, ok := New(name, cfg)
		if ok {
			catalogs = append(catalogs, catalog)
		}
	}
	return NewPipeline(catalogs)
}

// List 按结局决定继续还是停止。有收录立即返回。
// 后一站的成功空列表盖过前面的太短、配额和不可用。
// 后一站的配额或不可用盖过前面的空列表。
// 太短不盖过空列表，也不盖过配额或不可用。
func (p *Pipeline) List(query string) Outcome {
	state := KindUnavailable
	seen := false
	for _, catalog := range p.catalogs {
		out := catalog.Search(query)
		switch out.Kind {
		case KindHits:
			if len(out.Records) == 0 {
				seen = true
				state = KindEmpty
				continue
			}
			return out
		case KindEmpty:
			seen = true
			state = KindEmpty
		case KindTooShort:
			if !seen || state == KindTooShort {
				seen = true
				state = KindTooShort
			}
		case KindQuota, KindUnavailable:
			seen = true
			state = KindUnavailable
		default:
			seen = true
			state = KindUnavailable
		}
	}
	if !seen {
		return Outcome{Kind: KindUnavailable}
	}
	return Outcome{Kind: state}
}

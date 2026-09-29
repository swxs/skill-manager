package search

// Record 是安装前的标准收录。仓库根在安装步骤里从 RawURL 截出，不放进收集站实现。
type Record struct {
	Catalog     string `json:"catalog"`
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	RawURL      string `json:"rawURL"`
}

// Kind 是一次查询的结局。
type Kind int

const (
	KindHits Kind = iota
	KindEmpty
	KindTooShort
	KindQuota
	KindUnavailable
)

// Outcome 是 Catalog.Search 的结局。有收录时 Records 非空。
type Outcome struct {
	Kind    Kind
	Records []Record
}

// Catalog 是一个收集站。Name 是点号串里的站名。
type Catalog interface {
	Name() string
	Search(query string) Outcome
}

// Blurb 是列出的第二行：短描述，没有则显示名，再没有则稳定身份。
func (r Record) Blurb() string {
	if r.Description != "" {
		return r.Description
	}
	if r.DisplayName != "" {
		return r.DisplayName
	}
	return r.ID
}

// LinkLine 是列出的第三行。
func (r Record) LinkLine() string {
	if r.RawURL != "" {
		return r.RawURL
	}
	return "没有 Git 地址"
}

// Token 是点号串。
func (r Record) Token() string {
	return r.Catalog + "." + r.ID
}

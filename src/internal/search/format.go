package search

import "strings"

// FormatList 把收录写成三行一块，块与块之间空一行。
func FormatList(records []Record) string {
	var b strings.Builder
	for i, rec := range records {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(rec.Token())
		b.WriteByte('\n')
		b.WriteString(rec.Blurb())
		b.WriteByte('\n')
		b.WriteString(rec.LinkLine())
		b.WriteByte('\n')
	}
	return b.String()
}

// SplitToken 从第一个点切开点号串。两侧都不能空。
func SplitToken(token string) (catalog, id string, ok bool) {
	i := strings.Index(token, ".")
	if i <= 0 || i >= len(token)-1 {
		return "", "", false
	}
	return token[:i], token[i+1:], true
}

// Exact 在同一次查询结果里按稳定身份精确匹配。其他结局视为没有这条收录。
func Exact(out Outcome, id string) (Record, bool) {
	if out.Kind != KindHits {
		return Record{}, false
	}
	for _, rec := range out.Records {
		if rec.ID == id {
			return rec, true
		}
	}
	return Record{}, false
}

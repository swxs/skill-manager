package jsonc

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

func Parse(text string) (any, error) {
	var v any
	err := json.Unmarshal([]byte(StripTrailingCommas(stripComments(text))), &v)
	return v, err
}

func stripComments(text string) string {
	var b strings.Builder
	inStr := false
	esc := false
	for i := 0; i < len(text); {
		c := text[i]
		if inStr {
			b.WriteByte(c)
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			i++
			continue
		}
		if c == '"' {
			inStr = true
			b.WriteByte(c)
			i++
			continue
		}
		if c == '/' && i+1 < len(text) && text[i+1] == '/' {
			i += 2
			for i < len(text) && text[i] != '\r' && text[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(text) && text[i+1] == '*' {
			i += 2
			for i+1 < len(text) && !(text[i] == '*' && text[i+1] == '/') {
				i++
			}
			if i+2 < len(text) {
				i += 2
			} else {
				i = len(text)
			}
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

func StripTrailingCommas(text string) string {
	var b strings.Builder
	inStr := false
	esc := false
	for i := 0; i < len(text); {
		c := text[i]
		if inStr {
			b.WriteByte(c)
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			i++
			continue
		}
		if c == '"' {
			inStr = true
			b.WriteByte(c)
			i++
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(text) && (text[j] == ' ' || text[j] == '\t' || text[j] == '\r' || text[j] == '\n') {
				j++
			}
			if j < len(text) && (text[j] == '}' || text[j] == ']') {
				i++
				continue
			}
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

func ParseStrings(text string) ([]string, error) {
	raw, err := Parse(text)
	if err != nil {
		return nil, err
	}
	switch v := raw.(type) {
	case []any:
		return stringList(v)
	case map[string]any:
		skills, _ := v["skills"].([]any)
		return stringList(skills)
	default:
		return nil, errNotStringArray
	}
}

var errNotStringArray = errString("必须是字符串数组")

type errString string

func (e errString) Error() string { return string(e) }

func stringList(items []any) ([]string, error) {
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, errNotStringArray
		}
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

func Quote(s string) string {
	b, _ := json.Marshal(s)
	if !utf8.Valid(b) {
		return `""`
	}
	return string(b)
}

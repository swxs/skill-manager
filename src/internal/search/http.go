package search

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func doGET(client *http.Client, rawURL string) (int, http.Header, []byte, error) {
	if rawURL == "" {
		return 0, nil, nil, errEmptyURL
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "skill-manager")
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return resp.StatusCode, resp.Header, nil, err
	}
	return resp.StatusCode, resp.Header, body, nil
}

type emptyURLError struct{}

func (emptyURLError) Error() string { return "empty url" }

var errEmptyURL emptyURLError

func interpret(status int, header http.Header, body []byte, err error) (Kind, []map[string]any) {
	if err != nil {
		return KindUnavailable, nil
	}
	if status == http.StatusTooManyRequests {
		return KindQuota, nil
	}
	if status == http.StatusBadRequest && bodyTooShort(body) {
		return KindTooShort, nil
	}
	if status < 200 || status >= 300 {
		return KindUnavailable, nil
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		if remainingZero(header) {
			return KindQuota, nil
		}
		return KindEmpty, nil
	}
	skills, ok := parseSkills(body)
	if !ok {
		return KindUnavailable, nil
	}
	if len(skills) == 0 && remainingZero(header) {
		return KindQuota, nil
	}
	if len(skills) == 0 {
		return KindEmpty, nil
	}
	return KindHits, skills
}

func bodyTooShort(body []byte) bool {
	text := strings.ToLower(string(body))
	if strings.Contains(text, "too short") || strings.Contains(text, "too_short") || strings.Contains(text, "过短") || strings.Contains(text, "太短") {
		return true
	}
	if strings.Contains(text, "at least") && (strings.Contains(text, "character") || strings.Contains(text, "length")) {
		return true
	}
	if strings.Contains(text, "长度") && (strings.Contains(text, "短") || strings.Contains(text, "小") || strings.Contains(text, "至少")) {
		return true
	}
	return false
}

func remainingZero(header http.Header) bool {
	if header == nil {
		return false
	}
	for key, values := range header {
		lower := strings.ToLower(key)
		if !strings.Contains(lower, "ratelimit") || !strings.Contains(lower, "remaining") {
			continue
		}
		for _, value := range values {
			if strings.TrimSpace(value) == "0" {
				return true
			}
		}
	}
	return false
}

func parseSkills(body []byte) ([]map[string]any, bool) {
	var v any
	if err := json.Unmarshal(body, &v); err != nil || v == nil {
		return nil, false
	}
	if skills, found := findSkills(v); found {
		return skills, true
	}
	if hasError(v) {
		return nil, false
	}
	return nil, true
}

func hasError(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	if _, ok := m["error"]; ok {
		return true
	}
	if _, ok := m["Error"]; ok {
		return true
	}
	return false
}

func findSkills(v any) ([]map[string]any, bool) {
	switch t := v.(type) {
	case []any:
		return asSkills(t)
	case map[string]any:
		for _, key := range []string{"skills", "Skills", "items", "Items", "list", "List"} {
			child, ok := t[key]
			if !ok {
				continue
			}
			arr, ok := child.([]any)
			if !ok {
				continue
			}
			skills, good := asSkills(arr)
			if good {
				return skills, true
			}
		}
		for _, key := range []string{"data", "Data", "result", "Result"} {
			child, ok := t[key]
			if !ok {
				continue
			}
			if skills, found := findSkills(child); found {
				return skills, true
			}
		}
	}
	return nil, false
}

func asSkills(arr []any) ([]map[string]any, bool) {
	if len(arr) == 0 {
		return nil, true
	}
	skills := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok || !isSkillObject(m) {
			return nil, false
		}
		skills = append(skills, m)
	}
	return skills, true
}

func isSkillObject(m map[string]any) bool {
	if stringField(m, "id") == "" {
		return false
	}
	return stringField(m, "githubUrl", "github_url", "source_url", "sourceUrl", "name", "display_name", "displayName", "description") != ""
}

func stringField(m map[string]any, keys ...string) string {
	for _, key := range keys {
		text, ok := m[key].(string)
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		if text != "" {
			return text
		}
	}
	return ""
}

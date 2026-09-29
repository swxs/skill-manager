package search

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Config 注入 HTTP 客户端和各站基址。零值走公网基址。
type Config struct {
	Client *http.Client
	Bases  map[string]string
}

var current Config

// Configure 替换后续 New 和 DefaultPipeline 使用的配置。测试结束要 ResetConfig。
func Configure(cfg Config) { current = cfg }

// ResetConfig 恢复公网基址。
func ResetConfig() { current = Config{} }

// Current 返回当前配置。
func Current() Config { return current }

// New 按站名造出收集站。未知名字不发请求。
func New(name string, cfg Config) (Catalog, bool) {
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	switch name {
	case skillsMPName:
		return &skillsMP{client: client, base: baseURL(cfg, skillsMPName, "https://skillsmp.com")}, true
	case modelScopeName:
		return &modelScope{client: client, base: baseURL(cfg, modelScopeName, "https://www.modelscope.cn")}, true
	default:
		return nil, false
	}
}

func baseURL(cfg Config, name, fallback string) string {
	if cfg.Bases != nil {
		if base, ok := cfg.Bases[name]; ok && strings.TrimSpace(base) != "" {
			return strings.TrimRight(strings.TrimSpace(base), "/")
		}
	}
	return fallback
}

func endpoint(base, path string, query url.Values) string {
	u, err := url.Parse(base + path)
	if err != nil {
		return ""
	}
	q := u.Query()
	for key, values := range query {
		for _, value := range values {
			q.Set(key, value)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

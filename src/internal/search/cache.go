package search

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const keepSearches = 5

type storedSearch struct {
	Records []Record `json:"records"`
}

type storedCache struct {
	Searches []storedSearch `json:"searches"`
}

var cacheFileOverride string

// CacheFile 是最近几次 search 的缓存。默认在系统临时目录。
func CacheFile() string {
	if cacheFileOverride != "" {
		return cacheFileOverride
	}
	return filepath.Join(os.TempDir(), "skill-manager-search.json")
}

// SetCacheFileForTest 替换缓存文件。传入空字符串恢复默认。
func SetCacheFileForTest(path string) {
	cacheFileOverride = path
}

// Remember 把一次打出过收录的 search 追加进缓存，只留最近 5 次。
// 同一点号串以最新一次为准。没有收录时不调用。
func Remember(records []Record) error {
	if len(records) == 0 {
		return nil
	}
	cache, err := loadCache()
	if err != nil {
		return err
	}
	cache.Searches = append(cache.Searches, storedSearch{Records: records})
	if len(cache.Searches) > keepSearches {
		cache.Searches = cache.Searches[len(cache.Searches)-keepSearches:]
	}
	return saveCache(cache)
}

// Find 在缓存里按点号串查找，同一身份取最新一次。
func Find(token string) (Record, bool, error) {
	cache, err := loadCache()
	if err != nil {
		return Record{}, false, err
	}
	for i := len(cache.Searches) - 1; i >= 0; i-- {
		for _, rec := range cache.Searches[i].Records {
			if rec.Token() == token {
				return rec, true, nil
			}
		}
	}
	return Record{}, false, nil
}

func loadCache() (storedCache, error) {
	body, err := os.ReadFile(CacheFile())
	if os.IsNotExist(err) {
		return storedCache{}, nil
	}
	if err != nil {
		return storedCache{}, err
	}
	var cache storedCache
	if err := json.Unmarshal(body, &cache); err != nil {
		return storedCache{}, err
	}
	return cache, nil
}

func saveCache(cache storedCache) error {
	path := CacheFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	_ = os.Remove(path)
	return os.Rename(tmp, path)
}

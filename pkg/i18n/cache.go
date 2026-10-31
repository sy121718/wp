package i18n

import "sync"

type I18nResult struct {
	Key      string
	Value    string
	Lang     string
	HttpCode int
	AllLangs map[string]string
}

type MemoryCache struct {
	mu sync.RWMutex

	data map[string]map[string]string

	httpCodes map[string]int

	version int64
}

var cache = &MemoryCache{
	data:      make(map[string]map[string]string),
	httpCodes: make(map[string]int),
}

func (c *MemoryCache) Get(key, lang string) *I18nResult {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := &I18nResult{
		Key:      key,
		HttpCode: 200,
	}

	if code, ok := c.httpCodes[key]; ok {
		result.HttpCode = code
	}

	if m, ok := c.data[key]; ok {
		result.AllLangs = cloneLangMap(m)

		if v, ok := m[lang]; ok {
			result.Value = v
			result.Lang = lang
			return result
		}

		defaultLang := GetDefaultLang()
		if v, ok := m[defaultLang]; ok {
			result.Value = v
			result.Lang = defaultLang
			return result
		}

		for availLang, value := range m {
			result.Value = value
			result.Lang = availLang
			return result
		}
	}

	result.Value = key
	result.Lang = lang
	return result
}

func (c *MemoryCache) Update(newData map[string]map[string]string, newHttpCodes map[string]int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data = newData
	c.httpCodes = newHttpCodes
	c.version++
}

func InjectForTest(data map[string]map[string]string, httpCodes map[string]int) {
	cache.Update(data, httpCodes)
}

func (c *MemoryCache) GetVersion() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.version
}

func cloneLangMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return map[string]string{}
	}

	result := make(map[string]string, len(source))
	for lang, value := range source {
		result[lang] = value
	}
	return result
}

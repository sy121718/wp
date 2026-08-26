package i18n

import "sync"

// I18nResult 多语言查询结果
type I18nResult struct {
	Key      string            // 原始键
	Value    string            // 文本内容
	Lang     string            // 实际使用的语言
	HttpCode int               // HTTP状态码
	AllLangs map[string]string // 所有语言版本
}

// MemoryCache 内存缓存结构
type MemoryCache struct {
	mu sync.RWMutex

	// 核心结构：key -> lang -> value
	data map[string]map[string]string

	// HTTP 响应码映射：key -> http_code
	httpCodes map[string]int

	version int64
}

var cache = &MemoryCache{
	data:      make(map[string]map[string]string),
	httpCodes: make(map[string]int),
}

// Get 获取多语言信息（一次性返回所有信息）
func (c *MemoryCache) Get(key, lang string) *I18nResult {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := &I18nResult{
		Key:      key,
		HttpCode: 200, // 默认 200
	}

	// 获取 HTTP 状态码
	if code, ok := c.httpCodes[key]; ok {
		result.HttpCode = code
	}

	// 获取多语言数据
	if m, ok := c.data[key]; ok {
		result.AllLangs = cloneLangMap(m)

		// 优先返回指定语言
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

		// 降级：遍历所有可用语言，返回第一个
		for availLang, value := range m {
			result.Value = value
			result.Lang = availLang
			return result
		}
	}

	// 找不到，返回 key 本身
	result.Value = key
	result.Lang = lang
	return result
}

// Update 更新缓存
func (c *MemoryCache) Update(newData map[string]map[string]string, newHttpCodes map[string]int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data = newData
	c.httpCodes = newHttpCodes
	c.version++
}

// GetVersion 获取缓存版本
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

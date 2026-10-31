package i18n

import "strings"

type frozenCache struct {
	data        map[string]map[string]string
	defaultLang string
}

func (c *MemoryCache) freeze() *frozenCache {
	c.mu.RLock()
	data := make(map[string]map[string]string, len(c.data))
	for k, langs := range c.data {
		m := make(map[string]string, len(langs))
		for l, v := range langs {
			m[l] = v
		}
		data[k] = m
	}
	c.mu.RUnlock()

	return &frozenCache{data: data, defaultLang: GetDefaultLang()}
}

func (f *frozenCache) translate(key, fallback, lang string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		if fallback != "" {
			return fallback
		}
		return key
	}
	if m, ok := f.data[key]; ok {
		if v, ok := m[lang]; ok && v != "" {
			return v
		}
		if v, ok := m[f.defaultLang]; ok && v != "" {
			return v
		}
	}
	if fallback != "" {
		return fallback
	}
	return key
}

func Snapshot(lang string) func(key, fallback string) string {
	f := cache.freeze()
	lang = strings.TrimSpace(lang)
	if lang == "" {
		lang = f.defaultLang
	}
	return func(key, fallback string) string {
		return f.translate(key, fallback, lang)
	}
}

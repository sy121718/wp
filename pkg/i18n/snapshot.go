package i18n

// snapshot.go — 构建期冻结快照（docs/06-D §2.3 第 2 条 / §12 / §7.11 防线 4）。
//
// 读取时机分两类（docs/06-D §12）：
//   - 请求期（后台页面 / JSON 响应）读实时缓存，20s 自动刷新；
//   - 构建期必须读「构建开始时刻」的冻结副本——否则构建中途的自动刷新或
//     Reload 会让同一份输入产出不同字节，破坏确定性构建不变量
//     （AGENTS.md 不变量 5：同一 Page Document + BuildContext + Registry + Compiler
//     产生相同 Artifact 字节）。
//
// 本文件只新增只读路径：不修改 cache.go 的缓存结构、不修改 loader.go 的加载/刷新逻辑。

import "strings"

// frozenCache 词条缓存的冻结副本（与 MemoryCache.data 同构的只读深拷贝）。
type frozenCache struct {
	data        map[string]map[string]string
	defaultLang string
}

// freeze 在 RLock 内深拷贝当前缓存（与并发 Update 无竞态）。
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
	// GetDefaultLang 使用另一把锁（initMu），必须在释放 c.mu 之后调用，
	// 避免与 Init 路径形成反向锁序。
	return &frozenCache{data: data, defaultLang: GetDefaultLang()}
}

// translate 冻结副本取词：当前语言 → 默认语言 → fallback → key。
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

// Snapshot 冻结当前词条缓存，返回绑定语言的取词函数（签名与 TranslateFunc 一致）。
//
// 与 Translate 的差异（有意为之）：不做 cache.Get 的「遍历所有可用语言」兜底——
// 该兜底按 Go map 随机顺序取值，构建期会让产物字节抖动；冻结快照固定为
// 「当前语言 → 默认语言 → 原文」，结果可复现。未命中的 key 因此回退到调用方
// 写的中文原文（组件包内 fallback），绝不输出裸 key、绝不输出空串。
//
// 语言为空时取默认语言。返回的函数只读冻结副本，可安全并发调用。
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

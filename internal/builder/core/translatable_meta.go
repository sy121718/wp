package core

// translatable_meta.go — 可翻译字段的写入元数据（多语言 P5c，docs/06-D §7.8）。
//
// 工作台（P5c）在写入译文前必须按「字段上限 + 形态」校验，否则会出现
// 「译文超长/形态不符 → 构建期被归一或判空」的静默降级，比回退原文更糟。
// 上限与形态的唯一来源仍是组件自己的声明（ct tag → ParseControls）：
//
//   - 控件 Kind 为 richtext 的字段 → 构建期经 core.RichTextHTML 归一
//     （长度 > MaxRichLen 直接判空，见 richtext.go）；
//   - 控件声明 maxlen → 编辑期 ValidateSpec 的上限，工作台写入必须同样满足。
//
// 白名单之外（core.TranslatableFields 未声明）一律返回 ok=false：
// 工作台不允许写入「构建期根本不会取用」的字段。

import (
	"strings"
	"sync"
)

// FieldMeta 可翻译字段的写入元数据。
type FieldMeta struct {
	// Type 组件类型（如 core.button）。
	Type string
	// Field JSON 字段名（如 text）。
	Field string
	// Kind 控件类型；嵌套数组元素字段（如 core.table.headers）没有独立控件声明时为空串。
	Kind ControlKind
	// MaxLen 控件声明的长度上限（字节）；0 = 未声明上限。
	MaxLen int
}

// Rich 报告该字段是否走富文本渲染（构建期经 core.RichTextHTML 归一）。
func (m FieldMeta) Rich() bool { return m.Kind == ControlRichText }

// TranslatableFieldMeta 返回某组件某可翻译字段的元数据。
//
// ok=false 表示该字段不在组件白名单内（未注册类型 / 未声明字段）——
// 调用方据此拒绝写入，绝不「猜着翻」。
func TranslatableFieldMeta(typeName, field string) (m FieldMeta, ok bool) {
	typeName = strings.TrimSpace(typeName)
	field = strings.TrimSpace(field)
	if typeName == "" || field == "" {
		return FieldMeta{}, false
	}
	if !TranslatableFields(typeName)[field] {
		return FieldMeta{}, false
	}
	cacheKey := typeName + "." + field
	translatableMetaCacheMu.RLock()
	cached, hit := translatableMetaCache[cacheKey]
	translatableMetaCacheMu.RUnlock()
	if hit {
		return cached, true
	}

	m = FieldMeta{Type: typeName, Field: field}
	if comp, err := Lookup(typeName); err == nil {
		if sp, ok := comp.(SpecProvider); ok {
			if controls, cerr := ParseControls(sp.PropsSpec()); cerr == nil {
				for _, c := range controls {
					// 顶层字段直接同名；嵌套字段（items[].title / fields[].label）
					// 的控件 key 带点号路径，取「点号后缀同名」的第一条。
					if c.Key == field || strings.HasSuffix(c.Key, "."+field) {
						m.Kind, m.MaxLen = c.Kind, c.MaxLen
						break
					}
				}
			}
		}
	}
	translatableMetaCacheMu.Lock()
	translatableMetaCache[cacheKey] = m
	translatableMetaCacheMu.Unlock()
	return m, true
}

// translatableMetaCache 语境 → 字段元数据（见 TranslatableFieldMeta）。
// Register 会清空缓存，便于测试替换组件（与 translatableCache 同步）。
//
// 并发约束：与 translatableCache 同因——构建期并发查询下裸 map 写会进程级崩溃，
// 所有读写一律经 translatableMetaCacheMu。
var (
	translatableMetaCacheMu sync.RWMutex
	translatableMetaCache   = map[string]FieldMeta{}
)

// resetTranslatableMetaByType 失效某类型的全部字段元数据缓存（组件重注册时调用）。
func resetTranslatableMetaByType(typeName string) {
	prefix := typeName + "."
	translatableMetaCacheMu.Lock()
	for key := range translatableMetaCache {
		if strings.HasPrefix(key, prefix) {
			delete(translatableMetaCache, key)
		}
	}
	translatableMetaCacheMu.Unlock()
}

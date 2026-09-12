package source

// item.go — 集合项的类型安全访问（issue #35）。
//
// 背景：集合源的字段是**数据驱动**的（商品有 minPrice，文章有 title，插件表又有别的），
// 所以集合项天然是键值结构、不可能给每个源定义结构体。代价是组件侧写 item["minPrice"]
// 时既查不出拼写错误、也要各自写一遍类型断言 —— #28 就因此踩过
// 「同一个字段既是格式化字符串又是数值，按字符串排序得出 199 < 99」。
//
// 这里做两件事收敛它：
//
//	1. 字段键常量：组件写 ItemFieldMinPrice 而不是字面量 "minPrice"，拼错编译不过；
//	2. 类型安全访问器：断言与「缺失」语义集中一处，组件不再各写一遍 switch。
//
// 注意「缺失 ≠ 零值」是本项目的一条固定语义（没有启用变体 / 尚无评分都靠它区分），
// 所以访问器返回 (值, ok) 而不是零值。

import (
	"strings"
	"time"
)

// 集合项字段键常量（与各集合源的字段白名单一一对应）。
//
// 只收**组件会按类型取用**的那几个：其余字段走绑定（ItemFieldText）按字符串输出即可。
const (
	// ItemFieldID 项标识（商品 / 内容的 uuid）。
	ItemFieldID = "id"
	// ItemFieldSlug URL 段。
	ItemFieldSlug = "slug"
	// ItemFieldCreatedAt 创建时间（RFC3339 字符串）。
	ItemFieldCreatedAt = "createdAt"
	// ItemFieldMinPrice 最低启用变体价（**数值**；缺失 = 没有启用变体，与 0 元不同）。
	ItemFieldMinPrice = "minPrice"
	// ItemFieldRatingValue 评分**数值**（缺失 = 尚无评分，与 0 分不同）。
	ItemFieldRatingValue = "ratingValue"
)

// ItemFloat 取集合项里的数值字段。
//
// 兼容 json 解码与手写 map 可能产生的几种数值类型（float64 / int / int64）；
// 缺失或类型不符返回 ok=false —— 调用方据此走「没有这个值」的分支，而不是当 0 用。
func ItemFloat(item map[string]any, key string) (v float64, ok bool) {
	raw, exists := item[key]
	if !exists {
		return 0, false
	}
	switch n := raw.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// ItemString 取集合项里的字符串字段（缺失或类型不符返回空串）。
func ItemString(item map[string]any, key string) string {
	v, _ := item[key].(string)
	return v
}

// ItemTime 取集合项里的时间字段（按 RFC3339 解析）。
//
// 解析失败与缺失一律返回零值 + false：调用方据此判定「没有可排序的时间」，
// 而不是把零值当成 0001 年参与比较。
func ItemTime(item map[string]any, key string) (t time.Time, ok bool) {
	s := strings.TrimSpace(ItemString(item, key))
	if s == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

// Package core — 集合内容解析契约（docs/06-plugin-system.md §9，不变量 4）。
//
// CollectionResolver 与 ContentResolver（单字段）互补：前者解析「集合」
// （列表数据，如 product 列表 / 插件 campaigns 集合），后者解析「单字段」
// （如 product.name）。二者都遵守白名单：集合源的字段/过滤维度由声明方
// （content 模块 / 插件 manifest）封闭定义，Document 只保存白名单绑定，
// 不能保存 SQL、任意过滤表达式或 endpoint（不变量 4）。
package core

import (
	"fmt"
	"go_wp/internal/builder/source"
	"strconv"
	"strings"
)

// CollectionResolver — 定义已搬到 internal/builder/source（issue #35），
// 此处保留别名：既有引用（组件、构建管线、工作台）不必跟着改。
type CollectionResolver = source.CollectionResolver

// CollectionSchema — 定义已搬到 internal/builder/source（issue #35），
// 此处保留别名：既有引用（组件、构建管线、工作台）不必跟着改。
type CollectionSchema = source.CollectionSchema

// CollectionSchemaProvider — 定义已搬到 internal/builder/source（issue #35），
// 此处保留别名：既有引用（组件、构建管线、工作台）不必跟着改。
type CollectionSchemaProvider = source.CollectionSchemaProvider

// CollectionQuery / CollectionPage / CollectionPager — 同上口径（审计 PERF-019）：
// 形状定义在 internal/builder/source，此处保留别名。分页是**可选能力**，
// 调用方（片段渲染）按能力探测使用，没有它的集合源照旧走「取一批再截断」的退化路径。
type CollectionQuery = source.CollectionQuery
type CollectionPage = source.CollectionPage
type CollectionPager = source.CollectionPager

// ItemFieldPrefix 集合项字段前缀：绑定写成 item.title 表示「当前集合项的字段」，
// 由集合组件在展开每张卡时注入作用域（见 ItemScope）。
const ItemFieldPrefix = "item."

// ItemScope 把绑定解析限定到当前集合项：字段以 item. 开头时取当前项，
// 其余原样委托内层解析器（页面级 FieldBinding 照旧）。
//
// 这是「集合卡用任意组件做模板」的关键 —— 子节点组件完全不需要知道自己在集合里，
// 它们照旧调 ContentResolver.ResolveString，作用域由外层组件在递归渲染时换掉。
// 不在集合里（Item 为 nil）时 item.* 解析为空串，由组件的 fallback 兜底。
type ItemScope struct {
	Inner ContentResolver
	Item  map[string]any
}

// ResolveString 实现 ContentResolver。
func (s ItemScope) ResolveString(field string) (string, error) {
	if name, ok := strings.CutPrefix(field, ItemFieldPrefix); ok {
		return ItemFieldText(s.Item, name), nil
	}
	if s.Inner == nil {
		return "", nil
	}
	return s.Inner.ResolveString(field)
}

// ItemFieldText 取集合项字段的展示文本；数组取首元素（如 product.images），
// 整数数值去掉小数尾巴（价格 299 而非 299.000000）。
func ItemFieldText(item map[string]any, field string) string {
	if field == "" || item == nil {
		return ""
	}
	v, ok := item[field]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case []any:
		if len(t) == 0 {
			return ""
		}
		return ItemFieldText(map[string]any{"v": t[0]}, "v")
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "是"
		}
		return "否"
	default:
		return fmt.Sprint(t)
	}
}

// CollectionSource 集合源白名单声明（插件 manifest collections.json 投影，
// 由 plugin 模块注册，构建期经 CollectionResolver 消费）。
type CollectionSource struct {
	// Name 集合源标识（组件绑定时引用）。
	Name string `json:"name"`
	// Source 底层数据源（"content:{entityType}" 或 "plugin:{id}.{table}"）。
	Source string `json:"source"`
	// Fields 允许暴露的字段白名单（不变量 4：组件只能渲染声明字段）。
	Fields []string `json:"fields"`
	// Filters 允许的过滤维度白名单（键 + 枚举）。
	Filters []CollectionFilter `json:"filters,omitempty"`
	// OrderBy 允许的排序键白名单。
	OrderBy []string `json:"orderBy,omitempty"`
}

// CollectionFilter — 定义已搬到 internal/builder/source（issue #35），
// 此处保留别名：既有引用（组件、构建管线、工作台）不必跟着改。
type CollectionFilter = source.CollectionFilter

// Package core — 集合内容解析契约（docs/06-plugin-system.md §9，不变量 4）。
//
// CollectionResolver 与 ContentResolver（单字段）互补：前者解析「集合」
// （列表数据，如 product 列表 / 插件 campaigns 集合），后者解析「单字段」
// （如 product.name）。二者都遵守白名单：集合源的字段/过滤维度由声明方
// （content 模块 / 插件 manifest）封闭定义，Document 只保存白名单绑定，
// 不能保存 SQL、任意过滤表达式或 endpoint（不变量 4）。
package core

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// CollectionResolver 集合内容解析契约：集合源 → 静态列表数据（构建期填入）。
type CollectionResolver interface {
	// ResolveCollection 按集合源 + 白名单过滤解析为字段值列表（有序）。
	// source 格式："content:{entityType}"（content 实体集合，MVP）；
	// 后续扩展 "plugin:{pluginID}.{table}"（插件 L1 表集合）。
	// filter 为白名单过滤维度（键值等值匹配）；字段集由调用方（组件声明）
	// 的白名单决定，实现方只返回声明字段。
	// ctx 为发起构建的请求上下文，实现方查库时传播（支持超时取消）。
	ResolveCollection(ctx context.Context, source string, filter map[string]string) (items []map[string]any, err error)
}

// CollectionSchema 集合源元数据（字段白名单 + 过滤/排序维度）。
//
// 这是「内置组件也能声明集合」的通用契约：解析器实现方（content 模块）给出
// 每个集合源允许渲染的字段，内置组件在构建期据此校验字段映射（不变量 4），
// 工作台据它渲染字段下拉 —— 白名单只有一处来源，不在组件里各写一份。
type CollectionSchema struct {
	// Source 集合源标识（"content:article" 等）。
	Source string
	// Label 展示名（工作台下拉用）。
	Label string
	// Fields 允许渲染的字段白名单。
	Fields []string
	// Filters 允许的过滤维度（键 + 枚举）。
	Filters []CollectionFilter
	// OrderBy 允许的排序键白名单。
	OrderBy []string
}

// CollectionSchemaProvider 可选能力：解析器能给出集合源元数据。
//
// 实现方为内容模块。组件侧按「能力探测」使用：实现了就按白名单严格校验，
// 没实现则退回按数据实际字段判断 —— 契约缺失不阻断构建。
type CollectionSchemaProvider interface {
	CollectionSchemas(ctx context.Context) ([]CollectionSchema, error)
}

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

// CollectionFilter 集合过滤维度白名单。
type CollectionFilter struct {
	Key  string   `json:"key"`
	Enum []string `json:"enum,omitempty"` // 空 = 任意值
	// Prefix 是否为**前缀维度**（issue #25）：此时 Key 是维度键前缀，真实维度是
	// "<Key>.<子键>"，子键由集合源自己校验（商品属性值维度 `option.color=red` 就是这种）。
	//
	// 为什么需要它：属性组是数据驱动的（工程里能加任意属性），维度键不可能编译期穷举；
	// 前缀维度把「键的集合」从静态白名单变成**受校验的命名空间**，仍然守住
	// 「Binding 不是 Query DSL」—— 不接受任意过滤表达式，子键必须过服务端校验。
	Prefix bool `json:"prefix,omitempty"`
}

// Package source — 构建期数据源的**共享形状**（issue #35）。
//
// 这里只放纯形状：集合源元数据、过滤维度声明、字段解析契约、实体类型注册表接口。
// 它不 import 任何业务模块，也不 import builder/core。于是：
//
//	· 业务模块（product / content / 将来的插件）import 它来声明自己的数据源能力；
//	· builder/core 也 import 它；
//	· 两边都依赖它、它谁也不依赖 ⇒ 不存在 Go 的 import 环。
//
// 为什么要单独一层：契约包需要 CollectionSchema / EntitySourceRegistry 这些类型，
// 它们原先住在 builder/core 里 —— 那导致 core 不能反过来引用业务契约（会成环），
// 于是数据只能靠「core 定义接口 + 装配期注册」绕进来。剥出本包后，
// 业务契约包不再依赖 core，core 便能直接引用业务侧声明的**受限数据源接口**。
package source

import (
	"context"
	"strings"
)

// CollectionResolver 集合内容解析契约：集合源 → 静态列表数据（构建期填入）。
//
// 只读：没有写能力，也不接受 SQL / 过滤表达式 / endpoint（不变量 4：Binding 不是 Query DSL）。
type CollectionResolver interface {
	// ResolveCollection 按集合源 + 白名单过滤解析为字段值列表（有序）。
	// source 格式："content:{entityType}"（content 实体集合）；
	// 后续扩展 "plugin:{pluginID}.{table}"。
	// filter 为白名单过滤维度（键值等值匹配）；字段集由调用方（组件声明）
	// 的白名单决定，实现方只返回声明字段。
	// ctx 为发起构建的请求上下文，实现方查库时传播（支持超时取消）。
	ResolveCollection(ctx context.Context, source string, filter map[string]string) (items []map[string]any, err error)
}

// CollectionSchema 集合源元数据（字段白名单 + 过滤/排序维度）。
//
// 这是「内置组件也能声明集合」的通用契约：解析器实现方给出每个集合源允许渲染的
// 字段，内置组件在构建期据此校验字段映射（不变量 4），工作台据它渲染字段下拉 ——
// 白名单只有一处来源，不在组件里各写一份。
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
// 组件侧按「能力探测」使用：实现了就按白名单严格校验，没实现则退回按数据实际字段判断 ——
// 契约缺失不阻断构建。
type CollectionSchemaProvider interface {
	CollectionSchemas(ctx context.Context) ([]CollectionSchema, error)
}

// CollectionQuery 集合源的分页取数参数（审计 PERF-019）。
type CollectionQuery struct {
	// Filter 白名单过滤（键值等值匹配），与 ResolveCollection 的 filter 同义。
	Filter map[string]string
	// Offset 起始偏移（0 起）。构建期固定 0（只取第一屏），片段期按页码算。
	Offset int
	// Limit 本次最多取多少条；<= 0 表示由实现方取默认上限。
	Limit int
}

// CollectionPage 一页集合项与该过滤条件下的总量。
type CollectionPage struct {
	Items []map[string]any
	// Total 满足过滤条件的总条数，用于算总页数。
	// 实现方无法便宜得出时返回 -1 —— 调用方据此退化成「还有更多」的游标式提示，
	// 而不是把 -1 当 0 页（那会让列表看起来是空的）。
	Total int
}

// CollectionPager 可选能力：集合源支持**按页取数**（SQL 侧 offset/limit + 总量）。
//
// 为什么是可选能力接口，而不是给 CollectionResolver 加参数：
// 构建期只取第一屏（offset 恒为 0），根本不需要分页；给必选签名加参数会让全部实现
// 与所有测试 fake 都付代价，收益为零。片段期是运行时渲染（每次请求只取当页），
// 用能力探测即可 —— 与本仓既有的「按消费方收窄 / 能力探测」同一手法
// （见 CollectionSchemaProvider 的注释）。
//
// 背景（审计 PERF-019）：组件的分页原先只能在这条能力缺失时退化 —— 集合源一次最多
// 取 100 条，翻页是在这 100 条里切内存，超过上限的数据永远翻不到。
type CollectionPager interface {
	ResolveCollectionPage(ctx context.Context, source string, q CollectionQuery) (CollectionPage, error)
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
	// 「Binding 不是 Query DSL」——不接受任意过滤表达式，子键必须过服务端校验。
	Prefix bool `json:"prefix,omitempty"`
}

// ContentContextFor 拼装实体字段的内容译文语境 "{实体类型}.{字段名}"（docs/06-D §7.5）。
//
// 与 i18n 的内容寻址语境同一口径：业务模块声明可翻译字段时用它，
// 保证「哪一段文本属于哪个字段」只有一处拼法。
func ContentContextFor(entityType, field string) string {
	typ := strings.TrimSpace(entityType)
	f := strings.TrimSpace(field)
	if typ == "" || f == "" {
		return ""
	}
	return typ + "." + f
}

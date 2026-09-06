// Package core — 集合内容解析契约（docs/06-plugin-system.md §9，不变量 4）。
//
// CollectionResolver 与 ContentResolver（单字段）互补：前者解析「集合」
// （列表数据，如 product 列表 / 插件 campaigns 集合），后者解析「单字段」
// （如 product.name）。二者都遵守白名单：集合源的字段/过滤维度由声明方
// （content 模块 / 插件 manifest）封闭定义，Document 只保存白名单绑定，
// 不能保存 SQL、任意过滤表达式或 endpoint（不变量 4）。
package core

// CollectionResolver 集合内容解析契约：集合源 → 静态列表数据（构建期填入）。
type CollectionResolver interface {
	// ResolveCollection 按集合源 + 白名单过滤解析为字段值列表（有序）。
	// source 格式："content:{entityType}"（content 实体集合，MVP）；
	// 后续扩展 "plugin:{pluginID}.{table}"（插件 L1 表集合）。
	// filter 为白名单过滤维度（键值等值匹配）；字段集由调用方（组件声明）
	// 的白名单决定，实现方只返回声明字段。
	ResolveCollection(source string, filter map[string]string) (items []map[string]any, err error)
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
}

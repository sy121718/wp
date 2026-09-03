package core

import "strings"

// RenderContext 单次编译的渲染上下文：HTML 缓冲、CSS 收集器与编译期外部服务。
type RenderContext struct {
	HTML *strings.Builder
	CSS  *CSSBuckets
	// Content CMS 内容解析器（构建期动态绑定注入）。使用字段绑定的组件
	// （core.heading 的 post.title 等）依赖它；未注入时绑定组件渲染返回明确错误。
	Content ContentResolver
	// Block 全局块解析器（构建期内联展开 core.globalref 引用，方案 C）。
	// 未注入时引用节点渲染降级为占位结构（编辑画布仍可选中）。
	Block BlockResolver
}

// ContentResolver CMS 内容解析契约：绑定字段 → 构建期字符串值。
// 规范 docs/02-C1 §2（Dynamic Binding）：发布期数据完全静态填入。
// 实现方由 CMS 模块提供（Phase 0-A2）；core.heading 等绑定组件经此解析。
type ContentResolver interface {
	// ResolveString 按字段路径（如 "post.title"）解析字符串值；不存在返回空串。
	ResolveString(field string) (string, error)
}

// BlockResolver 全局块解析契约：块 ID → 块文档 root 节点（021_blocks.sql 方案 C）。
// core.globalref 组件在构建期经此展开引用块的内容（同一次编译内联，确定性不受影响）。
// 实现方由页面装配层提供（page service 持有 block 契约）；未注入时引用节点渲染降级为占位。
type BlockResolver interface {
	// ResolveBlockRoot 按块 ID 返回块文档 root 节点；块不存在返回错误。
	ResolveBlockRoot(blockID string) ([]*Node, error)
}

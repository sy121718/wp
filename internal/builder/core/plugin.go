package core

// 插件组件的编译内核契约（docs/06-plugin-system.md §5/§7）。
//
// 插件组件 = 数据驱动组件：manifest 声明 props schema + Jet 模板 + styles 规则，
// 不含 Go 代码。内核只消费中性的 PluginComponentSpec；manifest 解析、样式
// 规则编译（internal/builder/style）由装配层（plugin 模块/plugincomp）完成，
// 经 CompileStyles 闭包注入——core 零新增依赖，无循环 import。

// PluginPropControl 插件组件的单个检查器控件声明（manifest props 段投影）。
type PluginPropControl struct {
	// Kind 控件类型白名单：text/textarea/number/boolean/select/color/unit/media。
	Kind string `json:"kind"`
	// Label 检查器显示名（缺省回退键名）。
	Label string `json:"label,omitempty"`
	// Default 默认值（插入组件时的初始 props）。
	Default any `json:"default,omitempty"`
	// Options select 控件的枚举选项（仅 kind=select 必填）。
	Options []string `json:"options,omitempty"`
	// MaxLen 文本类控件的长度上限（0 = 默认 200）。
	MaxLen int `json:"maxlen,omitempty"`
}

// PluginComponentSpec 编译内核消费的插件组件规格。
type PluginComponentSpec struct {
	// Type 组件类型标识："plugin.{pluginID}.{name}"（与内置 "core.*" 命名空间隔离）。
	Type string
	// Label 组件库显示名。
	Label string
	// Template Jet 模板路径（插件命名空间："plugin/{pluginID}/{name}"）。
	Template string
	// Props 检查器控件 schema（键 = props 字段名）。
	Props map[string]PluginPropControl
	// CompileStyles 样式编译闭包：装配层用 style 引擎实现（按 spec 的 styles
	// 规则 + 当前 props 值编译进 CSSBuckets）。nil = 无样式声明。
	CompileStyles func(nodeID string, props map[string]any, b *CSSBuckets) error
	// HasChildren 插件组件是否允许子节点（当前一律 false：叶子组件）。
	HasChildren bool
	// Collection 集合绑定（docs/06 §9）：组件渲染列表数据。nil = 非集合组件。
	// 构建期经 RenderContext.Collection 展开为视图 .V.items。
	Collection *CollectionBinding
}

// CollectionBinding 插件组件集合绑定（docs/06 §9，不变量 4 白名单）。
type CollectionBinding struct {
	// Source 集合源标识（"content:{entityType}" 等）。
	Source string
	// Fields 渲染字段白名单（模板只能渲染声明字段）。
	Fields []string
	// Filter 固定过滤（键值等值）。
	Filter map[string]string
}

// PluginResolver 插件组件解析契约：按节点类型返回组件规格。
// 实现方为装配层（dashboard/page service 经 plugin 模块构建，仅含 enabled 插件）。
// 确定性：同一插件版本集构建的 resolver 查询结果恒定。
type PluginResolver interface {
	LookupPluginComponent(typeName string) (spec *PluginComponentSpec, ok bool)
}

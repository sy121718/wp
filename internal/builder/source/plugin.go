package source

// plugin.go — 插件组件编译规格的**共享形状**（审计 CQ-001）。
//
// 与 collection.go 同一个动机：这些类型原先住在 builder/core，而插件契约包
// （internal/module/plugin/contract）要持有它们。契约包一旦 import core，
// 就违反 AGENTS.md 不变量 7 —— 反向即成环（core → 契约 → core），core
// 从此再也无法持有业务契约（它现在正持有 content / product 的受限数据源接口，
// 见 core/render.go）。
//
// 剥到本包后：契约包只依赖本包（零依赖），core 侧改为**类型别名**继续消费
// 同一份定义 —— 两边指的是同一个类型，不存在两份形状需要同步。
//
// 本文件只放形状与解析契约，不放实现：规格的构建（manifest 解析 + 样式规则编译）
// 仍由装配层 internal/builder/plugincomp 与 style 引擎完成，经 CompileStyles 闭包注入。

// PluginPropControl 插件组件的单个检查器控件声明（manifest props 段投影）。
// 英文入口：docs/plugin-development.en.md。
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

// CollectionBinding 插件组件集合绑定（docs/06 §9，不变量 4 白名单）。
// 英文入口：docs/plugin-development.en.md。
type CollectionBinding struct {
	// Source 集合源标识（"content:{entityType}" 等）。
	Source string
	// Fields 渲染字段白名单（模板只能渲染声明字段）。
	Fields []string
	// Filter 固定过滤（键值等值）。
	Filter map[string]string
}

// StyleSink 样式桶的最小写入能力：把一条已作用域化的规则追加进产物 CSS 的断点桶。
// 英文入口：docs/plugin-development.en.md。
//
// 为什么是接口而不是具体类型：编译期的样式桶实现是 core.CSSBuckets（住在内核里），
// 而本包不能 import core —— 那样共享形状又变回内核的一部分，契约包照样被钉死在 core 上。
// 这里只声明内核实际使用的那一个方法（style 引擎编译规则时只调 Add），
// core.CSSBuckets 天然满足，于是「样式编译闭包」的类型能留在本包，
// 内核侧继续零改动地传 *CSSBuckets。
type StyleSink interface {
	// Add 追加一条规则到指定断点桶（selector 已按 node 作用域化，decls 为声明列表）。
	Add(breakpoint, selector string, decls []string)
}

// PluginComponentSpec 插件组件的编译规格（编译内核消费的中性形状）。
// 英文入口：docs/plugin-development.en.md。
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
	// 规则 + 当前 props 值编译进样式桶）。nil = 无样式声明。
	CompileStyles func(nodeID string, props map[string]any, b StyleSink) error
	// HasChildren 插件组件是否允许子节点（当前一律 false：叶子组件）。
	HasChildren bool
	// Collection 集合绑定（docs/06 §9）：组件渲染列表数据。nil = 非集合组件。
	// 构建期经 RenderContext.Collection 展开为视图 .V.items。
	Collection *CollectionBinding
}

// PluginResolver 插件组件解析契约：按节点类型返回组件规格。
// 英文入口：docs/plugin-development.en.md。
// 实现方为装配层（dashboard/page service 经 plugin 模块构建，仅含 enabled 插件）。
// 确定性：同一插件版本集构建的 resolver 查询结果恒定。
type PluginResolver interface {
	LookupPluginComponent(typeName string) (*PluginComponentSpec, bool)
}

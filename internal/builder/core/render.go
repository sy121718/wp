package core

import "context"

// RenderContext 单次编译的渲染上下文：CSS 收集器与编译期外部服务。
//
// 说明：HTML 渲染已迁移到 Jet 模板路径（builder/jetview.go 的 renderView 直接
// 写入 strings.Builder），故移除了原 HTML 字段；nodeViewOf 只经 CSS/Content/Block
// 编译 CSS 与驱动递归。
type RenderContext struct {
	CSS *CSSBuckets
	// Context 发起构建的请求上下文（构建期查库解析集合/内容时传播）。
	// 未注入（nil）时解析器按后台任务语义处理。
	Context context.Context
	// Content CMS 内容解析器（构建期动态绑定注入）。使用字段绑定的组件
	// （core.heading 的 post.title 等）依赖它；未注入时绑定组件渲染返回明确错误。
	Content ContentResolver
	// Block 全局块解析器（构建期内联展开 core.globalref 引用，方案 C）。
	// 未注入时引用节点渲染降级为占位结构（编辑画布仍可选中）。
	Block BlockResolver
	// Plugin 插件组件解析器（plugin.* 节点经此取规格渲染，docs/06 §7）。
	// 未注入时插件节点返回明确错误（而非静默跳过）。
	Plugin PluginResolver
	// Collection 集合内容解析器（插件组件绑定集合时展开列表数据，docs/06 §9）。
	// 未注入时集合绑定组件返回明确错误。
	Collection CollectionResolver
	// Navigation 公开站点导航解析器（构建期展开 core.nav 的菜单位置绑定）。
	// 未注入时绑定菜单位置的导航节点返回明确错误（不静默渲染空菜单）。
	Navigation NavigationResolver
	// ProjectID 本次编译所属站点工程 ID：导航等「站点级资源」按它取数据。
	// 页面文档本身不携带工程 ID，由装配层（page service）从 pages 表注入。
	ProjectID string
	// CurrentPath 本次编译的页面访问路径（如 /about），用于导航「当前项」高亮。
	// 为空表示未知（预览块/独立编译），此时不标记当前项。
	CurrentPath string
	// ImageDefaults 图片全局默认（主题「图片管理」投影：懒加载策略 + 骨架屏）。
	// core 不依赖 builder，故用轻量投影结构；builder 注入时从 ThemeSettings 转换。
	ImageDefaults ImageDefaults
	// AssetProbe 媒体资源探测（构建期响应式图片用）：传入媒体 URL（如
	// /storage/xxx.jpg），返回同目录变体的可用宽度列表（如 [320, 1280]，按需
	// 降序）与是否可用。未注入时组件不输出 srcset（只出原图）。
	AssetProbe func(url string) []int
	// Lang 本次编译的目标语言（构建期组件文案翻译用，多语言 P4）。
	// 空表示默认语言；由 builder.WithLanguage 显式指定，未指定时取 i18n.GetDefaultLang()。
	// P4 只预留维度：产物路径 /{lang}/ 与多语言路由属后续 P2，本轮不涉及。
	Lang string
	// Translate 构建期取词函数（key, fallback → 文案）。由 builder 注入，
	// 默认 i18n.TranslateFunc(Lang)；nil 时组件直接使用 fallback 原文。
	// 组件包不依赖 pkg/i18n，只经此函数取词（core.I18nAware 契约）。
	Translate func(key, fallback string) string
}

// Text 返回 key 在当前语言下的组件文案。
//
// 兜底链与 pkg/i18n.Translate 一致（取词函数内部已含「当前语言 → 默认语言」回退）：
//  1. Translate 命中 → 译文；
//  2. Translate 为 nil 或返回空串 → fallback（组件包内写的中文原文）；
//  3. fallback 也为空 → key 本身。
//
// 绝不返回空串、绝不报错、绝不 panic（nil 接收者同样安全）。
func (c *RenderContext) Text(key, fallback string) string {
	if c != nil && c.Translate != nil {
		if text := c.Translate(key, fallback); text != "" {
			return text
		}
	}
	if fallback != "" {
		return fallback
	}
	return key
}

// ImageDefaults 图片全局默认值（主题设置投影）。
type ImageDefaults struct {
	// LazyLoad 懒加载默认策略（主题未设置时为 true）。
	LazyLoad bool
	// Skeleton 懒加载时是否显示骨架屏。
	Skeleton bool
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

// NavigationItem 导航菜单项（构建期解析结果，core.nav 消费）。
// 与组件包的 Item 分离：core 不依赖组件包，装配层负责两者转换。
type NavigationItem struct {
	// Label 菜单文字。
	Label string
	// URL 链接地址（来源实体已解析为最终 URL）。
	URL string
	// Target 打开方式：self / blank。
	Target string
	// Children 子菜单项（层级上限由组件校验决定）。
	Children []NavigationItem
}

// NavigationResolver 公开站点导航解析契约：工程 ID + 菜单位置 → 菜单项树。
//
// 「菜单位置」（kind）即导航记录的分类，当前为 header / footer；
// 由 navigation 模块实现（service 持 model 查询，装配层适配为本接口）。
// 未注入时绑定菜单位置的 core.nav 节点在构建期报错（docs/02 §冻结边界：
// 构建期必须显式失败，不允许静默产出空菜单）。
type NavigationResolver interface {
	// ResolveMenu 返回该工程该位置的导航项树（根节点顺序即渲染顺序）。
	ResolveMenu(projectID, kind string) ([]NavigationItem, error)
}

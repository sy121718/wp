package core

import (
	"context"
	"go_wp/internal/builder/source"
)

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
	// BlockStack 当前展开中的全局块 ID 栈（globalref 防环 + 深度限制，
	// 由 builder 渲染层维护；空 = 未在任何块展开内）。
	BlockStack []string
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
	// RevealInherit 滚动显现继承上下文（H5「滚动过去才出内容」的分层开关）：
	// "" 未启用 / "on" 子树默认滚动显现 / "off" 子树豁免（直接显示）。
	// 由装配层按主题/页面设置初始化；container 可在子树内覆盖（栈式进入设置、离开恢复）。
	// 组件级显式设置（interaction.entrance / scrollStory / scrollReveal）永远优先于继承。
	RevealInherit string
	// RevealDefaultEntrance 滚动显现注入的默认入场词（如 "fade-up"；空 = "fade-up"）。
	// 来源：主题 Motion.DefaultEntrance（效果基本库入场词汇表）。
	RevealDefaultEntrance string
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
	// ContentTranslate 构建期内容译文取词函数（source_text, context → 译文），
	// 多语言 P5b：处理**作者在编辑器里填写的文本**（按钮文字/标题/alt/图注/富文本）。
	//
	// 与 Translate（P4 的 sys_i18n 固定文案 key）严格分工、互不覆盖：
	//   - Translate 只填充「未由用户填写」的缺省文案；
	//   - ContentTranslate 只作用于组件 Translatable 白名单字段，且有译文用译文、
	//     无译文回退原文（绝不返回空串）。
	// 由 builder.WithContentTranslator 注入；nil 时渲染层零开销（不复制 props）。
	ContentTranslate func(sourceText, contentContext string) string
	// Locales 站点语言切换器条目（构建期注入，core.languages 消费）。
	// 由装配层按「本页逻辑路径 + 站点启用语言」逐语言算出（与产物 head 的 hreflang
	// 同一份计算）；空或少于两条时切换器整块不渲染（单语言站点字节不变）。
	Locales []LocaleLink
}

// LocaleLink 语言切换器条目（构建期数据，core.languages 消费）。
//
// 只携带「数据」不含「展示」：展示名由组件按语言自称表填充，便于换展示形态。
type LocaleLink struct {
	// Lang 语言码（如 zh-CN）：产物里同时作为 hreflang 与 lang 属性。
	Lang string
	// Href 该语言的对应页面地址（站点内路径或绝对 URL）。
	Href string
	// Current 是否本次构建的目标语言（当前项渲染为不可点的 <span aria-current>）。
	Current bool
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

// ContentResolver — 定义已搬到 internal/builder/source（issue #35），
// 此处保留别名：既有引用（组件、构建管线、工作台）不必跟着改。
type ContentResolver = source.ContentResolver

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

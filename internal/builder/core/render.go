package core

import (
	"context"
	"errors"
	"strings"

	"go_wp/internal/builder/source"

	// issue #35：core 直接持有业务侧声明的受限数据源接口。
	// 这一步能成立，正是因为共享形状已剥到 builder/source ——
	// 业务契约包不再反向依赖 core，环断了。
	contentcontract "go_wp/internal/module/content/contract"
	productcontract "go_wp/internal/module/product/contract"
)

// RenderContext 单次编译的渲染上下文：CSS 收集器与编译期外部服务。
//
// 说明：HTML 渲染已迁移到 Jet 模板路径（builder/jetview.go 的 renderView 直接
// 写入 strings.Builder），故移除了原 HTML 字段；nodeViewOf 只经 CSS/Content/Block
// 编译 CSS 与驱动递归。
type RenderContext struct {
	CSS *CSSBuckets
	// renderPanel 菜单悬浮面板的块渲染闭包（见 SetPanelRenderer / RenderPanel）。
	// 私有：注入方（builder）与消费方（navViewOf）都在同一编译流程内，不对外暴露。
	renderPanel func(blockID string) (string, error)
	// refFailure 引用块未能展开时的策略钩子（见 SetRefFailureHandler / RefFailed）。
	// 私有：注入方（builder.Compile）与消费方（jetview 的展开层）都在同一流程内；
	// nil 表示「一律降级为占位」。
	refFailure func(RefFailure) error
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
	//
	// issue #35 起：**专用组件优先用下面两个具体数据源字段**，本字段保留给
	// 通用组件（cardstack 这类要绑任意集合源的）做按名路由。
	Collection CollectionResolver
	// Product 商品构建期数据源（issue #35）：业务侧声明的**受限接口** ——
	// 只有读集合 / 元数据 / 可筛值，写方法不在它上面。
	//
	// 专用组件（productlist / productcard / productselector）直接用它：
	// 编译期知道调哪个、字段取值有类型、越权在接口形状上就被挡住。
	// 未注入（nil）时组件回退到 Collection 的按名路由（兼容单测与渐进切换）。
	Product productcontract.ProductDataSource
	// ContentSource 内容构建期数据源（issue #35），与 Product 同构。
	ContentSource contentcontract.ContentDataSource
	// Navigation 公开站点导航解析器（构建期展开 core.nav 的菜单位置绑定）。
	// 未注入时绑定菜单位置的导航节点返回明确错误（不静默渲染空菜单）。
	Navigation NavigationResolver
	// ProjectID 本次编译所属站点工程 ID：导航等「站点级资源」按它取数据。
	// 页面文档本身不携带工程 ID，由装配层（page service）从 pages 表注入。
	ProjectID string
	// SitePages 系统页面槽位 → **当前语言**的线上路径（BIZ-1）。
	//
	// 来源：page_site_slots 绑定 + page_publications 激活状态，装配层解析后注入，
	// 构建与片段层共用同一份解析（各解一次迟早分叉）。
	//
	// **只含已绑且已发布的槽位**：没有条目就是「这个站还没指定结算页」，
	// 组件据此不输出链接，而不是猜一个默认路径 —— 猜错的链接比没有链接难查得多。
	//
	// 路径已是最终访问路径（含语言前缀），组件不要自己再拼语言前缀：
	// 那是 pipeline.LangURLRule 的唯一职责，各处手拼是既有明文禁令。
	// 私有 + 只经 SitePage(slot) 读取：直接读 map 会漏记「本页用了这个槽位」，
	// 而漏记的后果是槽位换绑后该页不被标记待重建 —— 产物里的链接仍指向旧路径，
	// 页面上看不出任何异常（详见 VIS-006）。
	sitePages map[string]string
	// usage 渲染期依赖线索记录器（可选）：取值时记录消费过的槽位。
	usage UsageRecorder
	// archiveEntityType / archiveEntityID 当前归档实例的实体（审计 EDT-004）。
	//
	// 归档型实例（分类页 / 标签页 / 品牌页）渲染列表时，筛选值应当来自**实例本身**：
	// 否则「每个分类一个列表页」只能靠复制页面并手改筛选条件，新增分类必然漏配，
	// 而漏配的表现是「页面打得开、但列的是全站商品」——很难被当成故障报上来。
	// 为空 = 不在归档上下文（手工页面 / 详情页），组件回退到 Props 里的静态筛选。
	archiveEntityType string
	archiveEntityID   string
	// siteLinkResolver 站内链接本地化器（审计 I18N-015，可空）：作者手填的站内链接
	// 需要按当前语言加前缀，否则非默认语言站点上的按钮 / 图片链接会跳回默认语言。
	siteLinkResolver func(logicalPath string) string
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
	// Features 本次编译的运行时特征登记表（审计 PERF-014）：组件在渲染期登记自己
	// **真实输出**的属性 / class（hx-* 属性、data-* 控件属性、控件外观类），
	// 产物组装层据此决定注入哪些脚本，不再对整页 HTML 跑一遍 tokenizer。
	//
	// nil = 不收集（片段渲染 RenderNodeHTML、单测直连组件）：此时 UseAttr / UseClass
	// 是空操作，组件不必自己判断 —— 见 core.FeatureReporter 的说明。
	Features *FeatureSet
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

// 引用降级归因码（稳定枚举，进产物占位属性与 Manifest 诊断）。
//
// 为什么是「码」而不是错误原文：错误文本随实现与存储状态变动（驱动措辞、DB 报错），
// 写进产物字节会破坏「同一输入 → 同一字节」的确定性不变量，而诊断要的本来就是**分类**
// （哪一类不可用），原文只进日志。
const (
	// RefReasonResolverMissing 未注入块解析器：这次编译根本没有解析能力。
	RefReasonResolverMissing = "ref_resolver_missing"
	// RefReasonUnavailable 解析器报错：块不存在 / 跨工程 / 文档非法等。
	RefReasonUnavailable = "ref_unavailable"
	// RefReasonEmpty 解析成功但没有任何 root 节点（块存在但是空的）。
	//
	// 与上面两类分开：它**不是**配置错误，而是作者的正常中间态（块建好了还没写内容），
	// 发布期不因为它失败，只记一条诊断。
	RefReasonEmpty = "ref_empty"
	// RefReasonTemplateUnavailable 结构槽位绑定的结构模板拿不到（不存在 / 跨工程 / 非法）。
	RefReasonTemplateUnavailable = "ref_template_unavailable"
	// RefReasonTemplateEmpty 结构模板存在但没有任何 root 节点（空模板，回退块绑定）。
	RefReasonTemplateEmpty = "ref_template_empty"
)

// RefDegradeError 带稳定归因码的引用解析失败。
//
// 解析器只返回 error，而占位与诊断需要的是**稳定的原因码**。解析器用本类型声明
// 「这是哪一类不可用」，渲染层据此归类；未包装的错误统一归为 RefReasonUnavailable。
type RefDegradeError struct {
	// Reason 归因码（RefReason* 常量）。
	Reason string
	// Err 原始错误（只进日志，不进产物）。
	Err error
}

// Error 实现 error；Reason 与 Err 都为空时返回空串（不 panic）。
func (e *RefDegradeError) Error() string {
	switch {
	case e == nil:
		return ""
	case e.Err != nil:
		return e.Err.Error()
	default:
		return e.Reason
	}
}

// Unwrap 保留错误链：归因包装不应吞掉底层原因（errors.Is/As 仍可下探）。
func (e *RefDegradeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// RefDegradeReason 从错误链里取稳定归因码；没有包装时返回 fallback。
func RefDegradeReason(err error, fallback string) string {
	var de *RefDegradeError
	if errors.As(err, &de) && de.Reason != "" {
		return de.Reason
	}
	return fallback
}

// RefFailure 一次「引用块未能展开」的上下文：节点、来源与归因码。
//
// 它同时服务两件事：把归因挂到占位视图上（预览可定位），
// 以及按调用方策略决定「这次降级是否必须让编译失败」（发布期显式绑定不可用）。
type RefFailure struct {
	// NodeID / NodeType 引用节点本身（core.globalref / core.layoutSlot）。
	NodeID, NodeType string
	// BlockID 被引用的来源 ID：全局块 ID，或结构模板槽位的构建期虚拟引用 ID。
	BlockID string
	// Slot 结构槽位名（仅结构槽位节点非空）：诊断里据此区分「槽位绑定」与「文档内引用」。
	Slot string
	// Reason 稳定归因码（RefReason* 常量）。
	Reason string
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
	// PanelBlockID 悬浮面板引用的全局块（超级菜单；空 = 无面板）。
	PanelBlockID string
	// PanelWidth 面板展示宽度 auto / full。
	PanelWidth string
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
	// ResolveNavigation 按**具体菜单项 id** 返回该菜单项及其子树（作为菜单根渲染）。
	//
	// 与 ResolveMenu 并列：按位置取的是"这个位置的全部菜单"，按项取的是"这一支"。
	// 后者让页眉只放一支菜单（例如只有「产品」），而不必为它单独建一个位置。
	ResolveNavigation(projectID, navigationID string) ([]NavigationItem, error)
}

// 系统页面槽位键（BIZ-1）。
//
// UsageRecorder 记录渲染期**真实消费**的依赖线索（审计 VIS-006）。
//
// 由 pipeline 侧的收集器实现；core 只声明接口，不认识具体实现（避免反向 import）。
type UsageRecorder interface {
	UseSiteSlot(slot string)
	// UseMenu 记录一次导航菜单消费（菜单位置 header / footer）。
	//
	// 与 UseSiteSlot 同一理由：页面文档里「绑定了导航位置」这件事要经渲染才算数，
	// 记录下来的才是事实（静态扫节点类型要维护一张「组件 → 依赖」映射表，迟早漂移）。
	UseMenu(kind string)
	// UseNavigation 记录一次「按具体菜单项」的导航消费（规则同 UseMenu）。
	//
	// 两种模式各记各的：按位置记 menu:{project}:{kind}，按项记 navigation:{itemID}。
	// 只记一种会让另一种引用的页面在导航变化后不被标记（产物停在旧菜单）。
	UseNavigation(navigationID string)
	// UseBlock 记录一次「构建期展开的全局块」消费。
	//
	// 记录普通 globalref、嵌套块以及菜单悬浮面板的实际展开，
	// 两类发布来源共用；仅扫描外层文档会漏掉嵌套引用和文档之外的菜单绑定。
	UseBlock(blockID string)
}

// PanelRenderer 渲染「文档外的块内容」（菜单项的悬浮面板，超级菜单）。
//
// 面板内容存在全局块里，而块 id 挂在 navigations 行上 —— **不在页面文档里**，
// 静态扫描看不到它。由 builder 在编译期注入本函数（它持有组件模板集与块解析器），
// navViewOf 展开菜单项时调用：CSS 收集与依赖记录都在同一次 ctx 内完成，
// 面板因此与页面其余部分共用同一套样式与产物。未注入（编辑器画布 / 单测直连）时不展开。
//
// 返回的是**已渲染的受信 HTML**：由本仓的组件模板产出，调用方原样输出（不再二次转义）。
func (c *RenderContext) SetPanelRenderer(fn func(blockID string) (string, error)) {
	if c == nil {
		return
	}
	c.renderPanel = fn
}

// RenderPanel 渲染菜单悬浮面板引用的块；未注入渲染器时返回空串（不展开）。
func (c *RenderContext) RenderPanel(blockID string) (string, error) {
	if c == nil || c.renderPanel == nil {
		return "", nil
	}
	return c.renderPanel(blockID)
}

// SetRefFailureHandler 注入「引用块未能展开」的策略钩子。
//
// 语义：fn 返回非 nil error 表示这次降级必须让编译失败（发布期「显式绑定但拿不到」），
// 返回 nil 表示降级为占位。未注入（nil）等价于「一律降级」—— 预览、片段渲染、
// 单测直连组件不必显式关掉它，行为与改造前一致。
func (c *RenderContext) SetRefFailureHandler(fn func(RefFailure) error) {
	if c == nil {
		return
	}
	c.refFailure = fn
}

// RefFailed 上报一次引用失败；未注入钩子时返回 nil（按降级处理）。
func (c *RenderContext) RefFailed(f RefFailure) error {
	if c == nil || c.refFailure == nil {
		return nil
	}
	return c.refFailure(f)
}

// SetArchiveEntity 注入当前归档实例的实体（构建期由装配层传入）。
func (c *RenderContext) SetArchiveEntity(entityType, entityID string) {
	if c == nil {
		return
	}
	c.archiveEntityType = strings.TrimSpace(entityType)
	c.archiveEntityID = strings.TrimSpace(entityID)
}

// ArchiveEntity 取当前归档实体（空表示不在归档上下文）。
func (c *RenderContext) ArchiveEntity() (entityType, entityID string) {
	if c == nil {
		return "", ""
	}
	return c.archiveEntityType, c.archiveEntityID
}

// SetSiteLinkResolver 注入站内链接本地化器（审计 I18N-015）。
//
// 为什么是注入而不是直接调 pipeline 的 LangURLRule：pipeline 依赖 core，
// 反向依赖即成环；而且组件层只需要「给我站内逻辑路径、还我当前语言的访问路径」
// 这一条语义，不该认识站点语言规则的实现细节。
//
// 调用约定：只对**作者填的静态链接**调用（它们一定是站内逻辑路径）。
// CMS 绑定值不经过这里 —— 内容里的 URL 可能已经是完整访问路径，再前缀一次会指到不存在的地址；
// 那个语义由内容作者掌握，组件层不该替他决定。
func (c *RenderContext) SetSiteLinkResolver(fn func(logicalPath string) string) {
	if c == nil {
		return
	}
	c.siteLinkResolver = fn
}

// ResolveSiteLink 把作者填的站内链接本地化为当前语言的访问路径（审计 I18N-015）。
//
// 只处理**站内相对路径**；以下一律原样返回：
//   - 外链（含 :// 或 mailto: 之类的 scheme）；
//   - 协议相对地址（//cdn.example.com/x）—— 加前缀会变成站内路径；
//   - 锚点（#section）与空值；
//   - 未注入解析器时（装配缺失）也原样返回：链接缺少语言前缀是可见降级，
//     而抛错会让整页构建失败（与非默认语言站点整站不可用相比，前者明显更可接受）。
func (c *RenderContext) ResolveSiteLink(path string) string {
	if c == nil || c.siteLinkResolver == nil {
		return path
	}
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
		return path
	}
	// 带 scheme 的一律按外链处理（http: / https: / mailto: / tel: …）。
	if i := strings.Index(trimmed, ":"); i > 0 && !strings.Contains(trimmed[:i], "/") {
		return path
	}
	if !strings.HasPrefix(trimmed, "/") {
		return path // 相对路径（a/b）不做前缀处理：它的基准是当前页，语义不同
	}
	return c.siteLinkResolver(trimmed)
}

// SiteLinkOrSame 站内链接本地化的空安全包装（审计 I18N-015）。
//
// 组件既可能拿到装配期注入的本地化器，也可能在单测里拿到 nil ——
// 每个组件各写一遍 nil 判断迟早会漏一个，漏掉的那个在测试里 panic。
func SiteLinkOrSame(siteLink func(string) string, href string) string {
	if siteLink == nil {
		return href
	}
	return siteLink(href)
}

// SetSitePages 注入槽位 → 当前语言线上路径映射（由 builder 装配期调用）。
func (c *RenderContext) SetSitePages(pages map[string]string) {
	if c == nil {
		return
	}
	c.sitePages = pages
}

// SetUsageRecorder 注入依赖线索记录器（未注入时渲染路径只取值、不记录）。
func (c *RenderContext) SetUsageRecorder(r UsageRecorder) {
	if c == nil {
		return
	}
	c.usage = r
}

// SitePage 取槽位当前语言的线上路径，**并记录本次渲染消费了该槽位**。
//
// 记录放在取值这一步而不是调用方：漏记的表现是「改了槽位绑定，引用它的页面不被标记
// 待重建」，站点上旧链接继续生效且无人报错。取值即记录之后，新增消费点自动被覆盖。
func (c *RenderContext) SitePage(slot string) string {
	if c == nil {
		return ""
	}
	if c.usage != nil && slot != "" {
		c.usage.UseSiteSlot(slot)
	}
	return c.sitePages[slot]
}

// UseMenu 记录一次导航菜单消费（菜单位置 header / footer）。
//
// 记录时机与 SitePage 同源（审计 VIS-006 的口径）：**取值即记录**。
// 漏记的表现是「改了导航，引用它的页面不被标记待重建」—— 导航在页眉/页脚，
// 全站可见，产物里却一直是旧链接，且没有任何报错。
// 未注入收集器时（预览 / 单测直连）是空操作，调用方不必自己判断。
func (c *RenderContext) UseMenu(kind string) {
	if c == nil || c.usage == nil {
		return
	}
	if k := strings.TrimSpace(kind); k != "" {
		c.usage.UseMenu(k)
	}
}

// UseNavigation 记录一次「按具体菜单项」的导航消费（规则同 UseMenu：取值即记录）。
func (c *RenderContext) UseNavigation(navigationID string) {
	if c == nil || c.usage == nil {
		return
	}
	if id := strings.TrimSpace(navigationID); id != "" {
		c.usage.UseNavigation(id)
	}
}

// UseBlock 记录一次构建期展开的全局块（规则同 UseMenu：取值即记录）。
func (c *RenderContext) UseBlock(blockID string) {
	if c == nil || c.usage == nil {
		return
	}
	if id := strings.TrimSpace(blockID); id != "" {
		c.usage.UseBlock(id)
	}
}

// 权威定义在 page 模块的 enums（SiteSlotDefs，带展示名与用途），但 builder **不依赖任何
// module**（依赖方向是 module → builder），拿不到那一份。所以这里存一份键名，
// 并由测试钉住两边一致（builder 侧键集合必须与 page 侧白名单完全相同）。
//
// 为什么值得为几个字符串常量专门写测试：键名写错**不会报错**，只会静默不生效
// （引擎按已知键查表，查不到就当没配），表现为「明明绑定了，链接就是不出现」。
const (
	SiteSlotShop     = "shop"
	SiteSlotBlog     = "blog"
	SiteSlotCart     = "cart"
	SiteSlotCheckout = "checkout"
	SiteSlotAccount  = "account"
	SiteSlotLogin    = "login"
	SiteSlotRegister = "register"
	SiteSlotForgot   = "forgot"
	SiteSlotReset    = "reset"
	SiteSlotOrders   = "orders"
)

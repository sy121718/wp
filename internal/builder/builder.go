// Package builder 实现 go_wp 页面文档（Page Document）到静态 HTML/CSS 的编译。
//
// 对应规范 docs/02-A《页面设置与容器规范》：
//   - PageSettings 为页面级全局环境配置，编译期直接作用于 <head> 与 <body>；
//   - 组件树节点按 type 分发到已注册组件（core/Registry），一个组件一个目录
//     （components/container、后续的 components/heading 等）；
//   - 所有响应式断点、布局、外观及动画参数均编译为纯净 CSS，浏览器端零 JavaScript 布局计算；
//   - 编译是确定性的：同一 Page Document 产生完全相同的输出字节。
package builder

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"

	"github.com/CloudyKit/jet/v6"

	// 内置组件在各自包的 init() 中 core.Register；import 触发见 jetview.go（唯一 hub）。
	"go_wp/internal/builder/core"

	contentcontract "go_wp/internal/module/content/contract"
	productcontract "go_wp/internal/module/product/contract"
)

// Page 页面文档：页面级设置 + 顶级容器（Section）列表。
type Page struct {
	Settings PageSettings `json:"settings"`
	Root     []*core.Node `json:"root"`
}

// UnmarshalJSON 反序列化页面文档。
func (p *Page) UnmarshalJSON(data []byte) (err error) {
	type pageAlias Page // 避免递归
	var a pageAlias
	if err = json.Unmarshal(data, &a); err != nil {
		return err
	}
	*p = Page(a)
	return nil
}

// ParsePage 从 JSON 字节解析页面文档。
func ParsePage(data []byte) (p *Page, err error) {
	p = &Page{}
	if err = json.Unmarshal(data, p); err != nil {
		return nil, fmt.Errorf("页面文档解析失败: %w", err)
	}
	return p, nil
}

// CompiledPage 页面文档的静态编译输出。
type CompiledPage struct {
	// Lang 本次编译的目标语言（注入 <html lang>，多语言 P3）。
	// 空表示未知，RenderDocument 回退站点默认语言（保持单语言产物字节不变）。
	Lang string
	// Title 页面标题，注入 <title>。
	Title string
	// MetaDescription 页面描述，注入 <meta name="description">。
	MetaDescription string
	// SEOHead 构建期 SEO 头片段（canonical / OG / Twitter / JSON-LD）。
	SEOHead string
	// BodyClasses 注入 <body> 的 class 列表。
	BodyClasses []string
	// HTML 组件树 HTML（单层语义标签，无内联样式、无脚本）。
	HTML string
	// CSS 全部样式（含响应式媒体查询与入场动效关键帧）。
	CSS string
	// ThemeVarsCSS 主题变量块（:root --sky-*，注入 <style> 顶部；空=无主题）。
	ThemeVarsCSS string
	// UISources 原始控件基座源码（来自 WithUISources，按 data-ui-* 特征挑控件注入）。
	UISources map[string]string
	// UIStyle 原始控件基座的样式（来自 WithUIStyle，随控件脚本一起按需注入）。
	// 控件脚本进了产物却没样式，访客看到的就是没有外观的空壳。
	UIStyle string
	// EnhanceSource 客户端增强脚本源码（未裁剪的整份，来自 WithEnhanceSource）。
	//
	// 这里存的是**源码**而不是裁剪结果：裁剪要按产物 HTML 里的 data-* 特征来挑块，
	// 而 HTML 是 Compile 的产物 —— 放在渲染阶段算，正好拿到最终 HTML。
	// 空值表示调用方未注入：RenderDocument 会输出空增强（页面照常渲染，仅失去交互）。
	EnhanceSource string
	// TrackSource 流量来源采集脚本源码（来自 WithTrackSource，整份、不裁剪）。
	//
	// 与 EnhanceSource 的差别是**注入策略**而不是内容：增强按产物特征挑块，
	// 采集每页都要有（漏掉的那页就是归因断点，而断点往往落在转化页上）。
	// 空值表示调用方未注入：产物不含采集脚本，订单归因为空，其余一切照常。
	TrackSource string
	// TrackConfig 打点脚本的运行时配置（window.__skyTrack，BIZ-8 访问计数）。
	// 内容是构建期的工程 ID 与语言（json.Marshal 后的字面量）；工程 ID 为空时零字节。
	TrackConfig string
	// GA4Head 站点统计代码片段（来自 WithGA4MeasurementID；空 = 零字节注入）。
	GA4Head string
	// SearchConsoleHead Google Search Console 站点验证 meta（审计 SEO-009）。
	// 来自 WithSearchConsoleVerification；空 = 零字节注入。
	SearchConsoleHead string
	// Features 渲染期登记的运行时特征（审计 PERF-014）：组件在 BuildView 阶段登记的
	// 「本次真实输出了哪些 hx-* / data-* 属性、哪些控件外观 class」。RenderDocument
	// 据此决定注入哪些脚本，不再对整页 HTML 跑 tokenizer。
	//
	// Compile 路径总会给它一个（可能为空的）集合 —— 空集合意味着「这页确实什么都没用」，
	// 于是零字节注入，而不是「不知道，只能扫一遍」。nil 只出现在手工构造 CompiledPage
	// 的调用方（单测、历史调用点），RenderDocument 对 nil 回退到 tokenize（见 ui_script.go）。
	Features *core.FeatureSet
}

// CompileOption 编译选项。
type CompileOption func(*compileConfig)

// compileConfig 编译配置。
type compileConfig struct {
	content    core.ContentResolver
	block      core.BlockResolver
	set        *jet.Set
	plugin     core.PluginResolver
	collection core.CollectionResolver
	// issue #35：构建期数据源（业务侧声明的受限接口）。专用组件优先用它，
	// 通用组件仍走上面的 collection 按名路由。
	product       productcontract.ProductDataSource
	contentSource contentcontract.ContentDataSource
	navigation    core.NavigationResolver
	projectID     string
	currentPath   string
	// sitePages 系统页面槽位 → 当前语言线上路径（BIZ-1，装配层解析后传入）。
	sitePages map[string]string
	// usage 依赖线索记录器（可选，审计 VIS-006）：编译期记录消费过的系统页面槽位。
	usage core.UsageRecorder
	// archiveEntityType / archiveEntityID 当前归档实例的实体（审计 EDT-004）：
	// 归档页里的集合组件据此取筛选值，而不是把筛选条件写死在 Props 里。
	archiveEntityType string
	archiveEntityID   string
	// siteLinkResolver 站内链接本地化器（审计 I18N-015，可空）。
	siteLinkResolver func(string) string
	assetProbe       func(string) []int
	theme            *ThemeSettings
	ctx              context.Context
	// alternates 同页其他语言版本（hreflang 互指，多语言 P3）。
	alternates []Alternate
	// locales 站点语言切换器条目（多语言 P3）：与 alternates 同源（装配层一次算出）。
	locales []core.LocaleLink
	// lang 本次编译目标语言（空=取 i18n.GetDefaultLang()，多语言 P4）。
	lang string
	// translate 构建期取词函数（空=默认 i18n.TranslateFunc(lang)）。
	translate func(key, fallback string) string
	// contentTranslator 内容译文取词器（多语言 P5b，空=不接入内容翻译，产物字节不变）。
	// 由装配层「每页每语言构造一次」（CollectContentCandidates → 一次 SQL）。
	contentTranslator *i18n.ContentTranslator
	// extraCSS 插件静态样式（插件包 assets/*.css，构建期注入主 CSS 之后；
	// docs/06 §5.1 资产规范——复杂动画/特殊结构不在引擎内表达时由插件自带）。
	extraCSS string
	// uiSources 原始控件基座源码（文件名 → 源码，构建期按 data-ui-* 特征挑控件注入）。
	// 与 enhanceSource 分开：组件增强与原始控件是两层关注点（见 ui_script.go）。
	uiSources map[string]string
	uiStyle   string
	// structureSlots 结构槽位绑定（审计 VIS-001）：编译期展开为 root 首尾的 core.layoutSlot 节点。
	// ga4MeasurementID 站点 GA4 测量 ID（SiteSettings 快照，空 = 不注入统计代码）。
	// 站点级设置在这里进构建上下文：它属于本次构建的输入，不是进程级全局状态。
	ga4MeasurementID string
	// searchConsoleVerification 站点 GSC 验证 token（SiteSettings 快照，空 = 不注入验证 meta）。
	// 与 ga4MeasurementID 同源、同一条链路（审计 SEO-009）。
	searchConsoleVerification string
	structureSlots            []StructureSlot
	// enhanceSource 客户端增强脚本源码（构建期按产物特征裁剪后内联进产物）。
	//
	// 由调用方注入而不是 builder 自己 embed：前端资产统一放在 internal/templates/static/，
	// 一份源文件两个出口（运行时经 /static 给后台页面、构建期经此处内联进静态产物）；
	// 而 builder 不依赖 internal/templates（后者含 gin 依赖），所以只能走注入
	//（与 WithComponentSet 同一条路子）。为空时产物不含增强，交互降级但不影响渲染。
	enhanceSource string
	// trackSource 流量来源采集脚本源码（internal/templates/static/js/track.js）。
	//
	// 内容是常量，因此不改产物字节的确定性（同 Document + BuildContext → 同字节）；
	// 但它会让**每一页**的字节都变一次 —— 这是组件更新的正常代价，
	// 装配层会在启动时把既有产物标记为待重建（见 builder.RegistryVersion 说明）。
	trackSource string
}

// WithContentResolver 注入 CMS 内容解析器（构建期动态绑定静态填入，规范 docs/02-C1）。
func WithContentResolver(r core.ContentResolver) CompileOption {
	return func(c *compileConfig) { c.content = r }
}

// WithBlockResolver 注入全局块解析器（构建期内联展开 core.globalref 引用，方案 C）。
// WithSiteLinkResolver 注入站内链接本地化器（审计 I18N-015）。
//
// 实现由装配层提供（pipeline 的 LangURLRule），必须幂等 —— 见 core.SetSiteLinkResolver。
// 不注入时链接原样输出（缺前缀是可见降级，抛错会让整页构建失败）。
func WithSiteLinkResolver(fn func(logicalPath string) string) CompileOption {
	return func(c *compileConfig) {
		c.siteLinkResolver = fn
	}
}

// WithArchiveEntity 注入当前归档实例的实体（审计 EDT-004）。
//
// 值为空表示不在归档上下文：手工页面与详情页照旧用组件 Props 里的静态筛选。
func WithArchiveEntity(entityType, entityID string) CompileOption {
	return func(c *compileConfig) {
		c.archiveEntityType = entityType
		c.archiveEntityID = entityID
	}
}

// WithUsageRecorder 注入编译期依赖线索记录器（审计 VIS-006）。
//
// 未注入时渲染路径只取值、不记录：预览与片段这类「产物不进 Manifest」的场景
// 不需要记录，行为与改造前完全一致。
func WithUsageRecorder(r core.UsageRecorder) CompileOption {
	return func(c *compileConfig) { c.usage = r }
}

func WithBlockResolver(r core.BlockResolver) CompileOption {
	return func(c *compileConfig) { c.block = r }
}

// WithComponentSet 注入组件模板 Set（构建期组件渲染专用）。
//
// builder 包不直接依赖 internal/templates（后者含 gin 依赖），改为由调用方
// （page service / dashboard / pipeline / 测试）用 templates.NewComponentSet(...)
// 创建后注入。未注入时 Compile 返回明确错误，避免静默走旧路径。
func WithComponentSet(set *jet.Set) CompileOption {
	return func(c *compileConfig) { c.set = set }
}

// WithExtraCSS 注入插件静态样式（插件包 assets/*.css，docs/06 §5.1）。
// 构建期追加到主 CSS 之后（插件扩展覆盖内置语义）；接收时清洗 </style
// 防止逃逸 <style> 块（插件样式为管理员级信任，仍做防御性清洗）。
func WithExtraCSS(css string) CompileOption {
	cleaned := strings.ReplaceAll(css, "</style", "")
	return func(c *compileConfig) { c.extraCSS = cleaned }
}

// WithPluginResolver 注入插件组件解析器（plugin.* 节点渲染，docs/06 §7）。
// 调用方（page service / dashboard）按 enabled 插件集构建；未注入时
// 页面含插件节点将返回明确错误。
func WithPluginResolver(r core.PluginResolver) CompileOption {
	return func(c *compileConfig) { c.plugin = r }
}

// WithCollectionResolver 注入集合内容解析器（插件组件集合绑定渲染，docs/06 §9）。
// 调用方（page service / dashboard）注入 content 模块的 CollectionResolver。
// WithProductDataSource 注入商品构建期数据源（issue #35）。
//
// 传的是**受限接口**：只有读集合 / 元数据 / 可筛值，写方法不在它上面 ——
// 组件拿不到改商品的能力。
func WithProductDataSource(ds productcontract.ProductDataSource) CompileOption {
	return func(c *compileConfig) { c.product = ds }
}

// WithContentDataSource 注入内容构建期数据源（issue #35）。
func WithContentDataSource(ds contentcontract.ContentDataSource) CompileOption {
	return func(c *compileConfig) { c.contentSource = ds }
}

func WithCollectionResolver(r core.CollectionResolver) CompileOption {
	return func(c *compileConfig) { c.collection = r }
}

// WithNavigationResolver 注入公开站点导航解析器（core.nav 绑定菜单位置时构建期展开）。
// 调用方（page service）注入 navigation 契约的适配器；未注入时绑定菜单位置的
// 导航节点返回明确错误（不静默渲染空菜单）。
func WithNavigationResolver(r core.NavigationResolver) CompileOption {
	return func(c *compileConfig) { c.navigation = r }
}

// WithProjectID 注入本次编译所属站点工程 ID（导航等站点级资源的取数上下文）。
// 页面文档不携带工程 ID，由装配层从 pages 表注入；为空时绑定菜单位置的
// 导航节点同样返回明确错误。
func WithProjectID(projectID string) CompileOption {
	return func(c *compileConfig) { c.projectID = projectID }
}

// WithAssetProbe 注入媒体资源探测函数（构建期响应式图片）：
// 传入媒体 URL 返回可用变体宽度（降序），如 [1280, 320]；返回空则只输出原图。
// 由装配层（page service）按本地存储根目录实现——构建期查文件系统，访客零查询。
func WithAssetProbe(fn func(string) []int) CompileOption {
	return func(c *compileConfig) { c.assetProbe = fn }
}

// WithCurrentPath 注入本次编译的页面访问路径（导航「当前项」高亮依据）。
// 为空表示未知（如块预览），导航不标记当前项。
func WithCurrentPath(path string) CompileOption {
	return func(c *compileConfig) { c.currentPath = path }
}

// WithSitePages 注入系统页面槽位 → 当前语言线上路径（BIZ-1）。
//
// 由装配层经 page.ResolveSitePages 解析后传入（只含已绑且已发布的槽位）；
// 未注入或为空时，依赖槽位的组件不输出链接 —— 这是**正常状态**（新站还没配），
// 不是错误，所以不报构建失败。
func WithSitePages(pages map[string]string) CompileOption {
	return func(c *compileConfig) { c.sitePages = pages }
}

// WithThemeSettings 注入主题设置（主题色编译为 :root CSS 变量进产物 head，
// 组件经 var(--sky-c-*) 引用——主题系统真正生效到产物）。
func WithThemeSettings(t *ThemeSettings) CompileOption {
	return func(c *compileConfig) { c.theme = t }
}

// WithEnhanceSource 注入客户端增强脚本源码（internal/templates 的 StaticJS("enhance.js")）。
//
// 不注入时产物不含增强：页面照样渲染，只是轮播/灯箱/卡片环等失去交互。
// 调用方（page service）在装配编译选项时注入；缺失会由增强装配处告警，不静默。
func WithEnhanceSource(js string) CompileOption {
	return func(c *compileConfig) { c.enhanceSource = js }
}

// WithTrackSource 注入流量来源采集脚本源码（internal/templates/static/js/track.js）。
//
// 不注入时产物不含采集脚本：页面照常渲染，只是订单归因为空 —— 采集是增强能力，
// 既不阻断内容发布，也不阻断下单（订单的 attribution 列落在 "{}"）。
func WithTrackSource(js string) CompileOption {
	return func(c *compileConfig) { c.trackSource = js }
}

// WithUISources 注入原始控件基座源码（文件名 → 源码，如 select.js / _util.js / index.js）。
//
// 按产物里出现的 data-ui-* 特征只注入命中的控件，基座与入口随之为其服务；
// 一个都没命中时产物不含任何控件脚本（纯内容页不为增强付流量）。
// nil 表示无脚本模式；非 nil 时命中的资源必须完整，否则 RenderDocument 返回错误。
func WithUISources(sources map[string]string) CompileOption {
	return func(c *compileConfig) { c.uiSources = sources }
}

// WithUIStyle 注入原始控件基座的样式（internal/templates 的 UICSS()）。
//
// 与控件脚本同进同出：产物内联了脚本却没样式，控件就是个没外观的空壳。
// 只在产物真的用到控件（命中 data-ui-* 特征）时才注入这一段。
func WithUIStyle(css string) CompileOption {
	return func(c *compileConfig) { c.uiStyle = css }
}

// WithContext 注入请求上下文：构建期集合/内容解析器查库时传播（超时取消）。
func WithContext(ctx context.Context) CompileOption {
	return func(c *compileConfig) { c.ctx = ctx }
}

// WithLanguage 指定本次编译的目标语言（构建期组件文案翻译，多语言 P4）。
//
// 预留维度：本轮不改变产物路径与路由（站点级 /{lang}/ 输出属后续 P2），
// 只影响 core.RenderContext.Lang 与默认取词函数。未指定（空串）时取
// i18n.GetDefaultLang()，因此现有调用方无需改动即保持中文产物不变。
func WithLanguage(lang string) CompileOption {
	return func(c *compileConfig) { c.lang = strings.TrimSpace(lang) }
}

// WithAlternates 注入同页其他语言版本（多语言 P3）：编译期输出 hreflang 互指。
// 未注入（单语言站点）时产物字节与 P3 之前完全一致。
func WithAlternates(alternates []Alternate) CompileOption {
	return func(c *compileConfig) { c.alternates = alternates }
}

// WithLocaleLinks 注入站点语言切换器条目（多语言 P3）：core.languages 组件据此输出
// 各语言的静态链接。未注入（单语言站点 / 页面未放切换器）时产物字节与 P3 之前一致。
func WithLocaleLinks(links []core.LocaleLink) CompileOption {
	return func(c *compileConfig) { c.locales = links }
}

// WithTranslator 注入自定义取词函数（key, fallback → 文案）。
//
// 缺省为 i18n.TranslateFunc(lang)（读 i18n 内存缓存，兜底链见 pkg/i18n.Translate）。
// 注入点供测试与装配层使用：传入的函数同样应遵守「缺词条回退 fallback、
// 不返回空串」的约定（RenderContext.Text 会再兜一层，绝不输出空串）。
func WithTranslator(fn func(key, fallback string) string) CompileOption {
	return func(c *compileConfig) { c.translate = fn }
}

// WithContentTranslator 注入内容译文取词器（多语言 P5b，docs/06-D §7.7）。
//
// 取词器由装配层按「本页候选原文的 hash 集合 + 目标语言」构造一次（一次批量 SQL），
// 组件渲染期只在内存索引上取词，**不逐组件查库**。未注入（nil）时渲染层零开销，
// 产物字节与接入前逐字节一致（无译文回退原文的等价形态）。
//
// 传入 nil 等价于不接入（可用于显式关闭）。
func WithContentTranslator(t *i18n.ContentTranslator) CompileOption {
	return func(c *compileConfig) { c.contentTranslator = t }
}

// contentTranslateFunc 把取词器转为 RenderContext 的取词函数（nil 取词器返回 nil）。
func contentTranslateFunc(t *i18n.ContentTranslator) func(sourceText, contentContext string) string {
	if t == nil {
		return nil
	}
	return t.TranslateContent
}

// resolveCompileI18n 解析本次编译的语言与取词函数：
//   - 语言为空 → i18n.GetDefaultLang()（i18n 未初始化时内部回退 zh-CN）；
//   - 取词函数未注入 → i18n.TranslateFunc(lang)。
//
// 两条兜底都保证返回值可用：i18n 未初始化时取词函数返回 fallback（原中文），
// 编译不会失败，产物也不会出现空属性或裸 key。
func resolveCompileI18n(cfg *compileConfig) (string, func(key, fallback string) string) {
	var lang string
	var fn func(key, fallback string) string
	if cfg != nil {
		lang = strings.TrimSpace(cfg.lang)
		fn = cfg.translate
	}
	if lang == "" {
		lang = i18n.GetDefaultLang()
	}
	if fn == nil {
		fn = i18n.TranslateFunc(lang)
	}
	return lang, fn
}

// MaxNodeDepth 组件树深度上限（顶级节点为第 1 层）。
// 正常页面 3~4 层（页面主体 > 区块 > 布局 > 叶子），复杂卡片 5~6 层；
// 超过 8 层即结构失控（选择器特异度竞争、响应式覆盖链失控、大纲树不可用，
// 参照 Elementor 硬 3 层 / Webflow 社区最佳实践 ≤6 层）。上限取 10：
// 给合法复杂度留余量，同时拦住无限嵌套（含未来插件预设塞深层结构）。
const MaxNodeDepth = 10

// ValidatePage 只校验页面文档结构，不执行 HTML/CSS 渲染与外部解析。
// 草稿保存入口使用它拒绝非法 Layout、重复 Node ID、未知组件与非法 Props；
// 媒体/CMS Binding 在正式 Build 阶段由注入的 Resolver 解析。
func ValidatePage(p *Page) (err error) {
	if p == nil {
		return errors.New("页面文档为空")
	}
	if err = validateSettings(&p.Settings); err != nil {
		return fmt.Errorf("页面设置: %w", err)
	}
	// 深度防线：超限拒绝（草稿保存即拦截，编译期同样经过此处）。
	for i, n := range p.Root {
		if d := nodeDepth(n); d > MaxNodeDepth {
			return fmt.Errorf("顶级节点 %d: 组件树深度 %d 超过上限 %d（嵌套失控，请简化结构）", i, d, MaxNodeDepth)
		}
	}
	ids := map[string]bool{}
	for i, n := range p.Root {
		if err = core.ValidateNode(n, ids); err != nil {
			return fmt.Errorf("顶级节点 %d: %w", i, err)
		}
	}
	return nil
}

// ValidatePageTolerant 容错校验：致命问题（设置非法 / 组件树深度超限）仍返回错误，
// 单节点配置不完整（如「轮播未拖入 slide」）只记入 skipped 并跳过，不阻断整页。
//
// 用于编译/预览与草稿保存：编辑过程中的「某个组件还没配好」是正常中间态，
// 不应导致整页编译失败（用户补齐后重新预览即可）。
// 需要严格拒绝非法文档的入口仍用 ValidatePage。
func ValidatePageTolerant(p *Page) (skippedIDs map[string]bool, err error) {
	if p == nil {
		return nil, errors.New("页面文档为空")
	}
	if err = validateSettings(&p.Settings); err != nil {
		return nil, fmt.Errorf("页面设置: %w", err)
	}
	for i, n := range p.Root {
		if d := nodeDepth(n); d > MaxNodeDepth {
			return nil, fmt.Errorf("顶级节点 %d: 组件树深度 %d 超过上限 %d（嵌套失控，请简化结构）", i, d, MaxNodeDepth)
		}
	}
	ids := map[string]bool{}
	skippedIDs = map[string]bool{}
	var reasons []string
	for i, n := range p.Root {
		verr := core.ValidateNode(n, ids)
		if verr == nil {
			continue
		}
		// 仅跳过「配置不完整」类（编辑中间态，如轮播未拖入 slide）；
		// 非法 props / 非法属性 key / 未知组件等配置错误仍必须拒绝。
		if errors.Is(verr, core.ErrIncompleteNode) {
			skippedIDs[n.ID] = true
			reasons = append(reasons, fmt.Sprintf("顶级节点 %d: %v", i, verr))
			continue
		}
		return nil, fmt.Errorf("顶级节点 %d: %w", i, verr)
	}
	if len(reasons) > 0 {
		logger.Scene("build").With("skipped", reasons).Warn("部分节点配置不完整，已跳过渲染（其余节点照常编译）")
	}
	return skippedIDs, nil
}

// nodeDepth 节点子树深度（自身为 1）。
func nodeDepth(n *core.Node) int {
	if n == nil || len(n.Children) == 0 {
		return 1
	}
	max := 0
	for _, c := range n.Children {
		if d := nodeDepth(c); d > max {
			max = d
		}
	}
	return max + 1
}

// ComponentSchemas 生成全部已注册组件的 Inspector 面板 schema（docs/02-C3）。
// 键为组件类型（core.container 等），值为 core.Control 描述符数组 JSON。
// 未实现 SpecProvider 的组件跳过（兼容手写校验阶段）。
// 输出确定性：Types 字典序，桶内字段声明序。
//
// 结果进程级缓存：PropsSpec 由组件编译期静态声明（core.Lookup + SchemaJSON
// 均为纯函数），进程内恒定；此前每个工作台请求都重新遍历生成，属冗余开销。
func ComponentSchemas() (map[string]json.RawMessage, error) {
	componentSchemasOnce.Do(func() {
		componentSchemas, componentSchemasErr = buildComponentSchemas()
	})
	return componentSchemas, componentSchemasErr
}

// componentSchemas* 进程级单例（见 ComponentSchemas 注释）。
var (
	componentSchemasOnce sync.Once
	componentSchemas     map[string]json.RawMessage
	componentSchemasErr  error
)

// buildComponentSchemas 实际生成逻辑（仅首调执行一次）。
func buildComponentSchemas() (map[string]json.RawMessage, error) {
	out := make(map[string]json.RawMessage, 8)
	for _, typeName := range core.Types() {
		comp, err := core.Lookup(typeName)
		if err != nil {
			continue
		}
		sp, ok := comp.(core.SpecProvider)
		if !ok {
			continue
		}
		schema, err := core.SchemaJSON(sp.PropsSpec())
		if err != nil {
			return nil, fmt.Errorf("组件 %s schema 生成失败: %w", typeName, err)
		}
		out[typeName] = schema
	}
	return out, nil
}

// Compile 编译页面文档。确定性保证：同一输入产生完全相同的输出。
func Compile(p *Page, opts ...CompileOption) (res *CompiledPage, err error) {
	// 容错校验：配置不完整的节点（如轮播未拖入 slide）被跳过渲染，不阻断整页；
	// 配置非法（非法 props / 未知组件等）仍返回错误（详见 ValidatePageTolerant）。
	skippedIDs, err := ValidatePageTolerant(p)
	if err != nil {
		return nil, err
	}
	cfg := &compileConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.set == nil {
		return nil, errors.New("编译缺少组件模板 Set：请通过 WithComponentSet 注入（templates.NewComponentSet）")
	}
	// 防御性兜底：WithThemeSettings 注入的主题设置在消费（ThemeVarsCSS）前校验，
	// 校验失败返回 error（不 panic），避免非法主题令牌进入产物 CSS 变量。
	if err = ValidateThemeSettings(cfg.theme); err != nil {
		return nil, fmt.Errorf("主题设置: %w", err)
	}

	var b core.CSSBuckets
	compileSettingsCSS(&p.Settings, &b)

	// 运行时特征登记表（审计 PERF-014）：组件在渲染期把「本次真实输出了什么」写进来，
	// 产物组装层读它决定注入哪些脚本（此前是渲染完再扫一遍整页 HTML）。
	features := core.NewFeatureSet()

	// 构建期语言与取词函数（多语言 P4）：未指定语言时取默认语言，
	// 取词函数缺省读 i18n 内存缓存并带完整兜底链（见 resolveCompileI18n）。
	lang, translate := resolveCompileI18n(cfg)

	// 构建上下文补充：目标语言与站点工程 ID 一并进 context。
	//
	// 组件经 RenderContext 读得到这两个值，集合解析器（CollectionResolver）
	// 只拿得到 context.Context —— 商品集合按工程取数（不跨站点串数据）、
	// 可翻译字段按语言取译文都依赖它们。Presenter 路径此前已在装配层包好
	// 构建语言，这里统一兜住，两种构建路径行为一致。
	buildCtx := core.WithBuildProjectID(core.WithBuildLang(cfg.ctx, lang), cfg.projectID)

	var htmlBuf strings.Builder
	ctx := &core.RenderContext{
		CSS: &b, Context: buildCtx, Content: cfg.content, Block: cfg.block,
		Plugin: cfg.plugin, Collection: cfg.collection,
		Product: cfg.product, ContentSource: cfg.contentSource,
		Navigation: cfg.navigation, ProjectID: cfg.projectID, CurrentPath: cfg.currentPath,
		Lang: lang, Translate: translate, Locales: cfg.locales,
		ContentTranslate: contentTranslateFunc(cfg.contentTranslator),
		ImageDefaults: core.ImageDefaults{
			LazyLoad: cfg.theme.LazyLoadEnabled(),
			Skeleton: cfg.theme.SkeletonEnabled(),
		},
		RevealInherit:         cfg.theme.RevealInheritOf(),
		RevealDefaultEntrance: cfg.theme.RevealDefaultEntranceOf(),
		AssetProbe:            cfg.assetProbe,
		Features:              features,
	}
	// 槽位映射与依赖线索记录器走 setter：sitePages 是私有的（取值即记录，见 core.SitePage）。
	ctx.SetSitePages(cfg.sitePages)
	ctx.SetUsageRecorder(cfg.usage)
	ctx.SetArchiveEntity(cfg.archiveEntityType, cfg.archiveEntityID)
	ctx.SetSiteLinkResolver(cfg.siteLinkResolver)
	// 顶层节点先建 view 树（含 CSS 编译），再统一渲染：main 地标要先知道每个顶层节点的
	// 语义标签，才能决定包裹区间（渲染顺序与逐节点渲染完全一致）。
	type rootView struct {
		view *nodeView
		tag  string
		// isSlot 是否为结构槽位节点：main 地标判定跳过它们（页眉页脚本来就在 main 之外，
		// 被包进 main 会让 banner / contentinfo 地标退化成普通元素）。
		isSlot bool
	}
	// 结构槽位展开（审计 VIS-001）：绑定变成 root 首尾的 core.layoutSlot 节点，
	// 页眉页脚因此进入 AST，与页面里的节点走同一条渲染与 CSS 收集路径。
	rootNodes := expandStructureSlots(p.Root, cfg.structureSlots)
	roots := make([]rootView, 0, len(rootNodes))
	for _, n := range rootNodes {
		if skippedIDs[n.ID] {
			continue // 配置不完整，已在校验阶段跳过（日志已记录原因）
		}
		// Jet 路径：nodeViewOf 把 Node 转 view 树（含 CSS 编译与递归），renderView 渲染根 view。
		v, verr := nodeViewOf(n, true, ctx)
		if verr != nil {
			return nil, verr
		}
		roots = append(roots, rootView{
			view: v, tag: strings.ToLower(strings.TrimSpace(v.Tag)),
			isSlot: isLayoutSlotNode(n),
		})
	}

	// main 地标（页面设置开关，默认关）：正文包进唯一的 <main>，屏幕阅读器可直接跳到内容。
	// 首尾连续的 header / footer 顶层节点留在 main 之外 —— 它们只有在 body 直接子级下才构成
	// banner / contentinfo 地标，一旦被包进 main 就退化成普通元素。
	mainStart, mainEnd := -1, -1
	if p.Settings.Layout.MainLandmark && len(roots) > 0 {
		mainStart, mainEnd = 0, len(roots)
		for mainStart < mainEnd && (roots[mainStart].isSlot || roots[mainStart].tag == "header" || roots[mainStart].tag == "footer") {
			mainStart++
		}
		for mainEnd > mainStart && (roots[mainEnd-1].isSlot || roots[mainEnd-1].tag == "footer" || roots[mainEnd-1].tag == "header") {
			mainEnd--
		}
	}
	for i, rv := range roots {
		if i == mainStart {
			htmlBuf.WriteString("<main id=\"main-content\">")
		}
		if i == mainEnd {
			htmlBuf.WriteString("</main>")
		}
		if verr := renderView(cfg.set, rv.view, &htmlBuf); verr != nil {
			return nil, verr
		}
	}
	if mainStart >= 0 && mainStart < mainEnd && mainEnd == len(roots) {
		htmlBuf.WriteString("</main>")
	}

	classes := []string{bodyClassPage}
	if p.Settings.Layout.Mode == LayoutBoxed {
		classes = append(classes, bodyClassBoxed)
	} else {
		classes = append(classes, bodyClassFull)
	}
	classes = append(classes, p.Settings.BodyClasses...)

	// 构建期 SEO 头：canonical / OG / Twitter / JSON-LD（三级回落由 BuildSEOHead 处理）。
	breadcrumbHome := "Home"
	if cfg.translate != nil {
		breadcrumbHome = cfg.translate("site.breadcrumb.home", "首页")
	}
	// SEO 的文本字段按语言取译文（审计 I18N-014）：它们烘在产物头部（<title> / og:title /
	// meta description）—— 英文站点如果标题还是中文，从搜索结果点进来的人会以为走错了站。
	//
	// 与候选收集侧共用语境常量（写读两侧各写一份字面量，迟早分叉而看不出）。
	// contentTranslateFunc 对 nil 取词器返回 nil 函数（见它的注释），这里必须先兜底：
	// 未接入内容翻译的站点（默认语言、单语言、或装配没注入取词器）走的就是这条路，
	// 直接调用会 nil panic 掉整个构建 —— 而「没配译文」恰恰是最常见的情况。
	seoTrans := contentTranslateFunc(cfg.contentTranslator)
	if seoTrans == nil {
		seoTrans = func(sourceText, _ string) string { return sourceText }
	}
	seoTitle := seoTrans(p.Settings.SEO.Title, SEOTitleContext)
	seoDescription := seoTrans(p.Settings.SEO.Description, SEODescriptionContext)
	seoHead := BuildSEOHead(p.Settings.SEO, p.Settings.SEO.Canonical, seoTitle, seoDescription, breadcrumbHome, cfg.alternates)

	// 产物 CSS = 内核编译样式 + 插件静态样式，用 @layer 显式分层：
	//   sky-base（内核基础）< sky-plugin（插件）< sky-auto（容器宽度自动适配）<
	//   sky-theme（主题档位）< sky-local（容器/作者显式声明）< 未分层（用户自定义，最高）。
	// 分层后优先级由层序决定（稳定显式），不再依赖源顺序（脆弱）；
	// 未分层样式天然高于所有层，符合「用户覆盖一切」的预期。
	var cssParts []string
	cssParts = append(cssParts, "@layer sky-base, sky-plugin, sky-auto, sky-theme, sky-local;")
	cssParts = append(cssParts, "@layer sky-base {\n"+b.String()+"\n}")
	if cfg.extraCSS != "" {
		cssParts = append(cssParts, "@layer sky-plugin {\n"+cfg.extraCSS+"\n}")
	}
	// 容器查询块：自动适配（sky-auto）< 主题档位（sky-theme）< 局部显式（sky-local），
	// 层序即优先级——不再依赖规则输出顺序。
	if cq := b.ContainerQueryCSS(); cq != "" {
		cssParts = append(cssParts, cq)
	}
	// 未分层顶层规则（@property 注册等）：注册是全局的，放层外最稳。
	if tl := b.TopLevelCSS(); tl != "" {
		cssParts = append(cssParts, tl)
	}
	css := strings.Join(cssParts, "\n\n")
	// 减弱动态效果无障碍块（主题开关 + 系统偏好双重门控；未开启零输出）。
	if rm := cfg.theme.ReducedMotionCSS(); rm != "" {
		css = css + "\n\n" + rm
	}
	// 页面转场规则（@view-transition，主题开关；未开启零输出）。
	if vt := cfg.theme.ViewTransitionsCSS(); vt != "" {
		css = css + "\n\n" + vt
	}
	// 产物自洽校验：引用的动画必须有定义。放在这里是因为此刻 css 已经拼完整
	// （基础层 / 插件层 / 容器查询 / 顶层 / 无障碍 / 页面转场都在内），
	// 是唯一能一次看到全部动画名来源的位置（见 css_verify.go）。
	if err := verifyAnimationRefs(css); err != nil {
		return nil, err
	}

	return &CompiledPage{
		Lang:              lang,
		Title:             seoTitle,
		MetaDescription:   seoDescription,
		SEOHead:           seoHead,
		GA4Head:           buildGA4Head(cfg.ga4MeasurementID),
		SearchConsoleHead: buildSearchConsoleHead(cfg.searchConsoleVerification),
		TrackConfig:       buildTrackConfig(cfg.projectID, lang),
		BodyClasses:       classes,
		HTML:              htmlBuf.String(),
		CSS:               css,
		ThemeVarsCSS:      ThemeVarsCSS(cfg.theme),
		EnhanceSource:     cfg.enhanceSource,
		TrackSource:       cfg.trackSource,
		UISources:         cfg.uiSources,
		UIStyle:           cfg.uiStyle,
		Features:          features,
	}, nil
}

// RenderNodeHTML 把单个组件节点渲染为 HTML 片段（issue #27）。
//
// 为什么需要它：访问面的 Runtime Fragment 要返回**与静态产物同一份渲染**的 HTML ——
// 若片段自己拼一遍列表，就会出现「点筛选得到的」与「直接打开页面看到的」两份实现，
// 任何一处改动都会让两边悄悄分叉（docs/04 三路径的硬要求）。
//
// 用同一个 node id 渲染是关键：组件类名由 core.NodeClass(id) 派生，id 相同则类名相同，
// 静态产物里已有的样式对片段同样生效，片段不必重复注入 CSS。
//
// set 为组件模板集（装配期构建一次复用），ctx 需带齐该组件依赖的解析器（列表：Collection）。
func RenderNodeHTML(set *jet.Set, node *core.Node, ctx *core.RenderContext) (string, error) {
	if node == nil {
		return "", fmt.Errorf("渲染节点为空")
	}
	if ctx == nil {
		return "", fmt.Errorf("渲染上下文为空")
	}
	// 片段只要 HTML：样式已在静态产物内联，跳过 CSS 编译（EDT-016）。
	fragCtx := *ctx
	fragCtx.CSS = core.DiscardCSS()
	// 片段也不收集运行时特征（审计 PERF-014）：片段不组装脚本，注入决策只发生在
	// 静态产物那条链上。共享调用方的登记表会让多次片段渲染悄悄累积特征。
	fragCtx.Features = nil
	view, err := nodeViewOf(node, true, &fragCtx)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	if err := renderView(set, view, &sb); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// RenderDocument 将编译输出组装为完整 HTML 文档（用于预览与静态发布产物）。
//
// 文档骨架经 go:embed 的 document.jet 模板渲染（声明式可见，IDE 可配平校验），
// 转义策略：Title/MetaDescription 走 Jet 默认 HTML 转义（等价 html.EscapeString）；
// CSS/HTML/ThemeVarsCSS/增强脚本是编译产物，用 unsafe 原样输出，避免二次转义；
// BodyClass 保持现状未转义（父代理单独处理转义问题），同样 unsafe 原样输出。
// 能力集合来自**渲染期登记**（c.featureScan，审计 PERF-014）：组件在 BuildView 阶段
// 登记自己真实输出的 hx-* / data-* 属性与控件外观 class，这里直接读结果，
// 不再对整页 HTML 跑 tokenizer。原先的扫描实现保留在 ui_script.go（collectHTMLScan），
// 供交叉验证测试与手工构造 CompiledPage 的调用方使用。
func RenderDocument(c *CompiledPage) (string, error) {
	scan := c.featureScan()
	uiCSS, uiScript, err := uiAssetsForScan(scan, c.UIStyle, c.UISources)
	if err != nil {
		return "", fmt.Errorf("组装文档控件资源失败: %w", err)
	}
	// <html lang>：目标语言缺省回退站点默认语言（i18n 未初始化时内部回退 zh-CN），
	// 绝不输出空 lang 属性（空 lang 会让浏览器与屏幕阅读器失去语言线索）。
	lang := strings.TrimSpace(c.Lang)
	if lang == "" {
		lang = i18n.GetDefaultLang()
	}
	v := documentView{
		Lang:              lang,
		Title:             c.Title,
		MetaDescription:   c.MetaDescription,
		SEOHead:           c.SEOHead,
		GA4Head:           c.GA4Head,
		SearchConsoleHead: c.SearchConsoleHead,
		BodyClass:         strings.Join(c.BodyClasses, " "),
		HTML:              c.HTML,
		CSS:               c.CSS + uiCSS,
		ThemeVarsCSS:      c.ThemeVarsCSS,
		// 采集脚本无条件排在最前：一是每页都要有（不像增强按特征挑块），
		// 二是它要尽早写 cookie —— 排在交互脚本后面的话，前一个脚本抛错会连坐，
		// 而归因丢数据是静默的，没人会发现少了什么。
		// 打点配置（工程 ID / 语言）排在采集脚本之前：脚本运行时就要读它。
		EnhanceScript: c.TrackConfig + c.TrackSource + enhanceScriptFor(scan.attrs, c.EnhanceSource) + uiScript,
	}
	var sb strings.Builder
	if err := documentTemplate().Execute(&sb, nil, v); err != nil {
		// 渲染失败返回 error 而非 panic：构建路径应可被上层捕获并返回 500，
		// 遵循「组件只返回 error」约定（documentTemplate 首次加载 panic 属
		// 准启动 fail-fast，保留）。
		return "", fmt.Errorf("渲染 document.jet 失败: %w", err)
	}
	return sb.String(), nil
}

// documentView 文档骨架渲染数据（CompiledPage 拍平 + 增强脚本进模板）。
type documentView struct {
	Lang              string // <html lang>（目标语言，空回退默认语言）
	Title             string
	MetaDescription   string
	SEOHead           string // canonical / OG / Twitter / JSON-LD（已转义，模板 unsafe 输出）
	GA4Head           string // 站点统计代码（服务端拼装、ID 过白名单；模板 unsafe 输出）
	SearchConsoleHead string // GSC 站点验证 meta（服务端拼装、token 过白名单；模板 unsafe 输出）
	BodyClass         string // strings.Join(c.BodyClasses, " ")，模板 unsafe 原样输出
	HTML              string
	CSS               string
	ThemeVarsCSS      string
	EnhanceScript     string
}

// documentTpl* document.jet 的进程级单例（embed 静态模板编译一次全局复用）。
var (
	documentTplOnce sync.Once
	documentTpl     *jet.Template
)

// documentTemplate 返回 document.jet 的编译后模板（首次调用加载编译）。
func documentTemplate() *jet.Template {
	documentTplOnce.Do(func() {
		loader := jet.NewInMemLoader()
		loader.Set("document", documentJetSrc)
		t, err := jet.NewSet(loader).GetTemplate("document")
		if err != nil {
			panic(fmt.Sprintf("加载 document.jet 失败: %v", err))
		}
		documentTpl = t
	})
	return documentTpl
}

//go:embed document.jet
var documentJetSrc string

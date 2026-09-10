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

	// 内置组件注册（core.container 为组件树唯一结构载体，包 init 自注册）。
	_ "go_wp/internal/builder/components/container"
	// core.heading：标题组件（CMS 绑定发布期静态填入）。
	_ "go_wp/internal/builder/components/heading"
	// core.text：正文组件（纯文本/富文本双模式，富文本白名单清洗）。
	_ "go_wp/internal/builder/components/text"
	// core.spacer：间隔组件（首个泛型基座 core.Atom 组件）。
	_ "go_wp/internal/builder/components/spacer"
	// core.gallery：图集与画廊组件（网格纯静态直出 / 轮播语义骨架 + 增强属性）。
	_ "go_wp/internal/builder/components/gallery"
	// core.button：按钮与 CTA 组件（统一链接协议 + 双态外观范式）。
	_ "go_wp/internal/builder/components/button"
	// core.divider：分割线组件（纯线 hr 直出 / Flex 嵌入文本或图标）。
	_ "go_wp/internal/builder/components/divider"
	// core.loader：加载器（spinner/dots/bars/pulse 纯 CSS 动画，零 JS）。
	_ "go_wp/internal/builder/components/loader"
	// core.shapedivider：形状分隔线（区块过渡 SVG 装饰，多层景深，WD wd_shapedivider）。
	_ "go_wp/internal/builder/components/shapedivider"
	// core.image：媒体引用组件（构建期 URL 直出，零解析）。
	_ "go_wp/internal/builder/components/image"
	// core.globalref：全局块引用组件（构建期经 BlockResolver 内联展开，方案 C）。
	_ "go_wp/internal/builder/components/globalref"
	// core.slider：容器型轮播（children 即各 slide，可嵌套任意组件，WD wd_slider）。
	_ "go_wp/internal/builder/components/slider"
	// core.list：列表（图标/序号/圆点，WD wd_list）。
	_ "go_wp/internal/builder/components/list"
	// core.infobox：信息框（图标/图+标题+文本+链接，WD wd_infobox）。
	_ "go_wp/internal/builder/components/infobox"
	// core.social_buttons：社交图标组（内联 SVG 品牌图标，WD wd_social_buttons）。
	_ "go_wp/internal/builder/components/socialbuttons"
	// core.video：视频（外链嵌入/本地 MP4，WD wd_video）。
	_ "go_wp/internal/builder/components/video"
	// core.tabs：页签（结构型，radio hack 零 JS 切换，WD wd_tabs）。
	_ "go_wp/internal/builder/components/nav"
	// core.languages：站点语言切换器（构建期注入各语言链接，纯链接零 JS，docs/06-D）。
	_ "go_wp/internal/builder/components/languages"
	_ "go_wp/internal/builder/components/tabs"
	// core.accordion：手风琴（结构型，details/summary 原生，WD wd_accordion）。
	_ "go_wp/internal/builder/components/accordion"
	// core.marquee：跑马灯（容器型，双份内容无缝滚动，WD wd_marquee）。
	_ "go_wp/internal/builder/components/marquee"
	// core.counter：数字计数器（滚动动画增强，WD wd_counter）。
	_ "go_wp/internal/builder/components/counter"
	// 组件库补齐（对标 GrapesJS 组件生态）：
	// core.table：表格（表头/数据行/斑马纹/边框）。
	_ "go_wp/internal/builder/components/table"
	// core.card：卡片（标题/正文/图片/按钮）。
	_ "go_wp/internal/builder/components/card"
	// core.cardstack：卡片堆叠（悬停扇形/直排展开 + 滚动堆叠 + 点击放大，零 JS）。
	_ "go_wp/internal/builder/components/cardstack"
	// core.faq：常见问题（details/summary 原生折叠）。
	_ "go_wp/internal/builder/components/faq"
	// core.quote：引用块（blockquote/cite）。
	_ "go_wp/internal/builder/components/quote"
	// core.countdown：倒计时（客户端增强，构建期零 time.Now）。
	_ "go_wp/internal/builder/components/countdown"
	// core.icon：通用 SVG 图标（白名单）。
	_ "go_wp/internal/builder/components/icon"
	// core.badge：徽章（solid/outline/soft 三态）。
	_ "go_wp/internal/builder/components/badge"
	// core.progress：进度条（role=progressbar）。
	_ "go_wp/internal/builder/components/progress"
	// core.rating：评分（星形填充，支持半星）。
	_ "go_wp/internal/builder/components/rating"
	// core.form：表单（字段白名单/提交）。
	_ "go_wp/internal/builder/components/form"
	"go_wp/internal/builder/core"
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
}

// CompileOption 编译选项。
type CompileOption func(*compileConfig)

// compileConfig 编译配置。
type compileConfig struct {
	content     core.ContentResolver
	block       core.BlockResolver
	set         *jet.Set
	plugin      core.PluginResolver
	collection  core.CollectionResolver
	navigation  core.NavigationResolver
	projectID   string
	currentPath string
	assetProbe  func(string) []int
	theme       *ThemeSettings
	ctx         context.Context
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
}

// WithContentResolver 注入 CMS 内容解析器（构建期动态绑定静态填入，规范 docs/02-C1）。
func WithContentResolver(r core.ContentResolver) CompileOption {
	return func(c *compileConfig) { c.content = r }
}

// WithBlockResolver 注入全局块解析器（构建期内联展开 core.globalref 引用，方案 C）。
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

// WithThemeSettings 注入主题设置（主题色编译为 :root CSS 变量进产物 head，
// 组件经 var(--sky-c-*) 引用——主题系统真正生效到产物）。
func WithThemeSettings(t *ThemeSettings) CompileOption {
	return func(c *compileConfig) { c.theme = t }
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

	// 构建期语言与取词函数（多语言 P4）：未指定语言时取默认语言，
	// 取词函数缺省读 i18n 内存缓存并带完整兜底链（见 resolveCompileI18n）。
	lang, translate := resolveCompileI18n(cfg)

	var htmlBuf strings.Builder
	ctx := &core.RenderContext{
		CSS: &b, Context: cfg.ctx, Content: cfg.content, Block: cfg.block,
		Plugin: cfg.plugin, Collection: cfg.collection,
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
	}
	for _, n := range p.Root {
		if skippedIDs[n.ID] {
			continue // 配置不完整，已在校验阶段跳过（日志已记录原因）
		}
		// Jet 路径：nodeViewOf 把 Node 转 view 树（含 CSS 编译与递归），renderView 渲染根 view。
		v, verr := nodeViewOf(n, true, ctx)
		if verr != nil {
			return nil, verr
		}
		if verr = renderView(cfg.set, v, &htmlBuf); verr != nil {
			return nil, verr
		}
	}

	classes := []string{bodyClassPage}
	if p.Settings.Layout.Mode == LayoutBoxed {
		classes = append(classes, bodyClassBoxed)
	} else {
		classes = append(classes, bodyClassFull)
	}
	classes = append(classes, p.Settings.BodyClasses...)

	// 构建期 SEO 头：canonical / OG / Twitter / JSON-LD（三级回落由 BuildSEOHead 处理）。
	seoHead := BuildSEOHead(p.Settings.SEO, p.Settings.SEO.Canonical, p.Settings.SEO.Title, p.Settings.SEO.Description, cfg.alternates)

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

	return &CompiledPage{
		Lang:            lang,
		Title:           p.Settings.SEO.Title,
		MetaDescription: p.Settings.SEO.Description,
		SEOHead:         seoHead,
		BodyClasses:     classes,
		HTML:            htmlBuf.String(),
		CSS:             css,
		ThemeVarsCSS:    ThemeVarsCSS(cfg.theme),
	}, nil
}

// RenderDocument 将编译输出组装为完整 HTML 文档（用于预览与静态发布产物）。
//
// 文档骨架经 go:embed 的 document.jet 模板渲染（声明式可见，IDE 可配平校验），
// 转义策略：Title/MetaDescription 走 Jet 默认 HTML 转义（等价 html.EscapeString）；
// CSS/HTML/ThemeVarsCSS/增强脚本是编译产物，用 unsafe 原样输出，避免二次转义；
// BodyClass 保持现状未转义（父代理单独处理转义问题），同样 unsafe 原样输出。
func RenderDocument(c *CompiledPage) (string, error) {
	// <html lang>：目标语言缺省回退站点默认语言（i18n 未初始化时内部回退 zh-CN），
	// 绝不输出空 lang 属性（空 lang 会让浏览器与屏幕阅读器失去语言线索）。
	lang := strings.TrimSpace(c.Lang)
	if lang == "" {
		lang = i18n.GetDefaultLang()
	}
	v := documentView{
		Lang:            lang,
		Title:           c.Title,
		MetaDescription: c.MetaDescription,
		SEOHead:         c.SEOHead,
		BodyClass:       strings.Join(c.BodyClasses, " "),
		HTML:            c.HTML,
		CSS:             c.CSS,
		ThemeVarsCSS:    c.ThemeVarsCSS,
		EnhanceScript:   enhanceScript,
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
	Lang            string // <html lang>（目标语言，空回退默认语言）
	Title           string
	MetaDescription string
	SEOHead         string // canonical / OG / Twitter / JSON-LD（已转义，模板 unsafe 输出）
	BodyClass       string // strings.Join(c.BodyClasses, " ")，模板 unsafe 原样输出
	HTML            string
	CSS             string
	ThemeVarsCSS    string
	EnhanceScript   string
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

//go:embed enhance.js
var enhanceScript string

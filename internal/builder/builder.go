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
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"strings"
	"sync"

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
	// Title 页面标题，注入 <title>。
	Title string
	// MetaDescription 页面描述，注入 <meta name="description">。
	MetaDescription string
	// BodyClasses 注入 <body> 的 class 列表。
	BodyClasses []string
	// HTML 组件树 HTML（单层语义标签，无内联样式、无脚本）。
	HTML string
	// CSS 全部样式（含响应式媒体查询与入场动效关键帧）。
	CSS string
	// ThemeVarsCSS 主题变量块（:root --wp-*，注入 <style> 顶部；空=无主题）。
	ThemeVarsCSS string
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
	theme      *ThemeSettings
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

// WithThemeSettings 注入主题设置（主题色编译为 :root CSS 变量进产物 head，
// 组件经 var(--wp-c-*) 引用——主题系统真正生效到产物）。
func WithThemeSettings(t *ThemeSettings) CompileOption {
	return func(c *compileConfig) { c.theme = t }
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
	if err = ValidatePage(p); err != nil {
		return nil, err
	}
	cfg := &compileConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.set == nil {
		return nil, errors.New("编译缺少组件模板 Set：请通过 WithComponentSet 注入（templates.NewComponentSet）")
	}

	var b core.CSSBuckets
	compileSettingsCSS(&p.Settings, &b)

	var htmlBuf strings.Builder
	ctx := &core.RenderContext{CSS: &b, Content: cfg.content, Block: cfg.block, Plugin: cfg.plugin, Collection: cfg.collection}
	for _, n := range p.Root {
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

	return &CompiledPage{
		Title:           p.Settings.SEO.Title,
		MetaDescription: p.Settings.SEO.Description,
		BodyClasses:     classes,
		HTML:            htmlBuf.String(),
		CSS:             b.String(),
		ThemeVarsCSS:    ThemeVarsCSS(cfg.theme),
	}, nil
}

// RenderDocument 将编译输出组装为完整 HTML 文档（用于预览与静态发布产物）。
func RenderDocument(c *CompiledPage) string {
	var sb strings.Builder
	sb.WriteString("<!DOCTYPE html>\n<html lang=\"zh-CN\">\n<head>\n")
	sb.WriteString("<meta charset=\"utf-8\">\n")
	sb.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	if c.Title != "" {
		sb.WriteString("<title>")
		sb.WriteString(html.EscapeString(c.Title))
		sb.WriteString("</title>\n")
	}
	if c.MetaDescription != "" {
		sb.WriteString("<meta name=\"description\" content=\"")
		sb.WriteString(html.EscapeString(c.MetaDescription))
		sb.WriteString("\">\n")
	}
	sb.WriteString("<style>\n")
	// 主题变量块置顶（组件 CSS 可引用 var(--wp-c-*) 消费主题令牌）。
	if c.ThemeVarsCSS != "" {
		sb.WriteString(c.ThemeVarsCSS)
		sb.WriteString("\n")
	}
	sb.WriteString(c.CSS)
	sb.WriteString("\n</style>\n</head>\n<body")
	if len(c.BodyClasses) > 0 {
		sb.WriteString(" class=\"")
		sb.WriteString(strings.Join(c.BodyClasses, " "))
		sb.WriteString("\"")
	}
	sb.WriteString(">\n")
	sb.WriteString(c.HTML)
	sb.WriteString("\n<script>")
	sb.WriteString(enhanceScript)
	sb.WriteString("</script>\n</body>\n</html>\n")
	return sb.String()
}

//go:embed enhance.js
var enhanceScript string

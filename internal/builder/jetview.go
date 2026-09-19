// jetview.go — 组件渲染从「Go 字符串拼接」迁移到 Jet 模板的转换层（Phase 0 + Phase 1）。
//
// 本文件同时是内置组件包的**唯一 import hub**：下方 import 触发各组件 init() 的
// core.Register，勿在 builder.go 再维护一份空导入清单（见 REG-001）。
//
// 职责划分：
//   - nodeViewOf 把 core.Node 树转换为 nodeView 视图树（props 解码、CSS 生成、校验与递归驱动）；
//   - Jet 模板（button.jet / container.jet / <组件>.jet）只根据 nodeView 的字段拼装 HTML；
//   - CSS 复用组件包导出的 CompileCSS（与旧 render 内部逻辑完全一致），保证字节等价。
//
// 这是与 builder.Compile 并行的新路径：旧 render 输出保持不变，字节等价由
// jetview_test.go 的对比测试证明。
package builder

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/CloudyKit/jet/v6"

	accordionPkg "go_wp/internal/builder/components/accordion"
	addtocartPkg "go_wp/internal/builder/components/addtocart"
	badgePkg "go_wp/internal/builder/components/badge"
	breadcrumbPkg "go_wp/internal/builder/components/breadcrumb"
	buttonPkg "go_wp/internal/builder/components/button"
	cardPkg "go_wp/internal/builder/components/card"
	cardstackPkg "go_wp/internal/builder/components/cardstack"
	carticonPkg "go_wp/internal/builder/components/carticon"
	containerPkg "go_wp/internal/builder/components/container"
	countdownPkg "go_wp/internal/builder/components/countdown"
	counterPkg "go_wp/internal/builder/components/counter"
	dividerPkg "go_wp/internal/builder/components/divider"
	faqPkg "go_wp/internal/builder/components/faq"
	formPkg "go_wp/internal/builder/components/form"
	galleryPkg "go_wp/internal/builder/components/gallery"
	globalrefPkg "go_wp/internal/builder/components/globalref"
	headingPkg "go_wp/internal/builder/components/heading"
	iconPkg "go_wp/internal/builder/components/icon"
	imagePkg "go_wp/internal/builder/components/image"
	infoboxPkg "go_wp/internal/builder/components/infobox"
	languagesPkg "go_wp/internal/builder/components/languages"
	layoutslotPkg "go_wp/internal/builder/components/layoutslot"
	listPkg "go_wp/internal/builder/components/list"
	loaderPkg "go_wp/internal/builder/components/loader"
	marqueePkg "go_wp/internal/builder/components/marquee"
	navPkg "go_wp/internal/builder/components/nav"
	orderlistPkg "go_wp/internal/builder/components/orderlist"
	productPkg "go_wp/internal/builder/components/product"
	productcardPkg "go_wp/internal/builder/components/productcard"
	productlistPkg "go_wp/internal/builder/components/productlist"
	productselectorPkg "go_wp/internal/builder/components/productselector"
	progressPkg "go_wp/internal/builder/components/progress"
	quotePkg "go_wp/internal/builder/components/quote"
	ratingPkg "go_wp/internal/builder/components/rating"
	searchresultsPkg "go_wp/internal/builder/components/searchresults"
	shapedividerPkg "go_wp/internal/builder/components/shapedivider"
	sliderPkg "go_wp/internal/builder/components/slider"
	socialbuttonsPkg "go_wp/internal/builder/components/socialbuttons"
	spacerPkg "go_wp/internal/builder/components/spacer"
	tablePkg "go_wp/internal/builder/components/table"
	tabsPkg "go_wp/internal/builder/components/tabs"
	textPkg "go_wp/internal/builder/components/text"
	userformsPkg "go_wp/internal/builder/components/userforms"
	videoPkg "go_wp/internal/builder/components/video"
	"go_wp/internal/builder/core"
)

// nodeView 组件渲染视图：Node 树 → view 树的中间表示。
//
// 通用字段（Type/Template/Classes/Props/Children）承载结构与上下文；
// V 是各组件 BuildView 预计算的渲染数据（标量字段/切片），模板经 .V.XXX 访问；
// button/container 保留拍平字段（Tag/Attrs/Text/Icon*/Shape*）向后兼容既有模板。
type nodeView struct {
	Type     string      // 组件类型标识（core.button / core.container / ...）
	Template string      // 模板名（button / container / heading / ...）
	NodeID   string      // 节点 ID
	Classes  string      // 已合并 class（sky-c-<id> [+ sky-section] [+ 自定义类]）
	CustomID string      // 自定义 Element ID（空则无）
	TopLevel bool        // 是否页面第一层顶级 Section
	Props    any         // 解码后的组件 props
	Children []*nodeView // 子节点视图（container 专用）

	// V 组件 BuildView 预计算的渲染视图数据（模板经 .V.XXX 访问）。
	V any

	// --- button / container 拍平字段（Phase 0 样板，向后兼容） ---
	Tag         string   // 语义标签（a/button/div/section/...）
	Attrs       string   // 前导空格 + 属性串（已转义）
	Text        string   // button 文本
	IconPrefix  string   // button 前缀图标内容片段（path/已转义 img URL；<svg> 骨架由 button.jet 渲染）
	IconSuffix  string   // button 后缀图标内容片段（同上）
	ShapeTop    string   // container 顶部形状分隔线内容片段（<svg> 骨架由 container.jet 渲染）
	ShapeBottom string   // container 底部形状分隔线内容片段（同上）
	BgSlides    []string // container 背景轮播图（渲染为 .sky-bg-slides 背景层）
}

// nodeViewOf 把单个 Node 转换为 nodeView（含递归 children，CSS 加入顺序对齐旧路径）。
func nodeViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	// 内容翻译（多语言 P5b，docs/06-D §7.7）：按组件 Translatable 白名单把作者填写的
	// 文本替换为译文（无译文回退原文）。只做 props 副本替换，AST 与 Page Document 不动；
	// 未接入（ctx.ContentTranslate == nil）时返回原节点，零开销、产物字节不变。
	if ctx != nil {
		node = applyContentTranslation(node, ctx.ContentTranslate)
	}
	// 滚动显现注入在 advancedClasses / containerViewOf 内完成（结构体层，见 reveal.go）。
	switch node.Type {
	case buttonPkg.Type:
		return buttonViewOf(node, topLevel, ctx)
	case containerPkg.Type:
		return containerViewOf(node, topLevel, ctx)
	case headingPkg.Type:
		return headingViewOf(node, topLevel, ctx)
	case textPkg.Type:
		return textViewOf(node, topLevel, ctx)
	case imagePkg.Type:
		return imageViewOf(node, topLevel, ctx)
	case dividerPkg.Type:
		return dividerViewOf(node, topLevel, ctx)
	case spacerPkg.Type:
		return spacerViewOf(node, topLevel, ctx)
	case shapedividerPkg.Type:
		return shapedividerViewOf(node, topLevel, ctx)
	case loaderPkg.Type:
		return loaderViewOf(node, topLevel, ctx)
	case listPkg.Type:
		return listViewOf(node, topLevel, ctx)
	case infoboxPkg.Type:
		return infoboxViewOf(node, topLevel, ctx)
	case socialbuttonsPkg.Type:
		return socialbuttonsViewOf(node, topLevel, ctx)
	case videoPkg.Type:
		return videoViewOf(node, topLevel, ctx)
	case counterPkg.Type:
		return counterViewOf(node, topLevel, ctx)
	case galleryPkg.Type:
		return galleryViewOf(node, topLevel, ctx)
	case sliderPkg.Type:
		return sliderViewOf(node, topLevel, ctx)
	case navPkg.Type:
		return navViewOf(node, topLevel, ctx)
	case languagesPkg.Type:
		return languagesViewOf(node, topLevel, ctx)
	case tabsPkg.Type:
		return tabsViewOf(node, topLevel, ctx)
	case accordionPkg.Type:
		return accordionViewOf(node, topLevel, ctx)
	case marqueePkg.Type:
		return marqueeViewOf(node, topLevel, ctx)
	case layoutslotPkg.Type:
		return layoutSlotViewOf(node, topLevel, ctx)
	case globalrefPkg.Type:
		return globalrefViewOf(node, topLevel, ctx)
	case tablePkg.Type:
		return tableViewOf(node, topLevel, ctx)
	case cardPkg.Type:
		return cardViewOf(node, topLevel, ctx)
	case cardstackPkg.Type:
		return cardstackViewOf(node, topLevel, ctx)
	case faqPkg.Type:
		return faqViewOf(node, topLevel, ctx)
	case quotePkg.Type:
		return quoteViewOf(node, topLevel, ctx)
	case countdownPkg.Type:
		return countdownViewOf(node, topLevel, ctx)
	case iconPkg.Type:
		return iconViewOf(node, topLevel, ctx)
	case badgePkg.Type:
		return badgeViewOf(node, topLevel, ctx)
	case breadcrumbPkg.Type:
		return breadcrumbViewOf(node, topLevel, ctx)
	case progressPkg.Type:
		return progressViewOf(node, topLevel, ctx)
	case productPkg.Type:
		return productViewOf(node, topLevel, ctx)
	case productcardPkg.Type:
		return productCardViewOf(node, topLevel, ctx)
	case productlistPkg.Type:
		return productListViewOf(node, topLevel, ctx)
	case productselectorPkg.Type:
		return productSelectorViewOf(node, topLevel, ctx)
	case addtocartPkg.Type:
		return addToCartViewOf(node, topLevel, ctx)
	case carticonPkg.Type:
		return cartIconViewOf(node, topLevel, ctx)
	case orderlistPkg.Type:
		return orderListViewOf(node, topLevel, ctx)
	case searchresultsPkg.Type:
		return searchResultsViewOf(node, topLevel, ctx)
	case userformsPkg.Type:
		return userFormsViewOf(node, topLevel, ctx)
	case ratingPkg.Type:
		return ratingViewOf(node, topLevel, ctx)
	case formPkg.Type:
		return formViewOf(node, topLevel, ctx)
	default:
		// 插件组件（plugin.{id}.{name}）：经 RenderContext.Plugin 取规格渲染。
		if strings.HasPrefix(node.Type, pluginTypePrefix) {
			return pluginViewOf(node, topLevel, ctx)
		}
		return nil, fmt.Errorf("nodeView: 不支持的组件类型 %q", node.Type)
	}
}

// declareViewFeatures 让组件视图声明本次渲染**真实输出**的运行时特征（审计 PERF-014）。
//
// 由各渲染分支在 BuildView 之后调用（视图是模板渲染的输入，判定与模板同源，
// 不存在「判定条件与模板分叉」的第二份实现）。产物组装层（RenderDocument → ui_script）
// 读登记结果决定注入哪些脚本，取代了此前「渲染完成后对整页 HTML 跑一遍 tokenizer」。
//
// 未实现 core.ViewFeatureDeclarer 的组件（纯内容型，产物里没有任何 hx-* / data-* /
// 控件外观类）自动跳过 —— 「没用到就零字节注入」由此成立。
func declareViewFeatures(view any, ctx *core.RenderContext) {
	if ctx == nil || ctx.Features == nil {
		return
	}
	declarer, ok := view.(core.ViewFeatureDeclarer)
	if !ok {
		return
	}
	attrs, classes := declarer.DeclareFeatures()
	ctx.UseAttr(attrs...)
	ctx.UseClass(classes...)
}

// ---- 公共收敛骨架 ----
//
// 28 个 xxxViewOf 的公共序列收敛为以下 helper（字节等价：仅抽取完全相同的公共子序列）：
//   - decodeProps：props JSON 解码（返回 Props 值副本，与旧路径 var p P 一致）
//   - advancedClasses：Advanced 通用层编译 + class 合并（customID 仅 adv != nil 时非空）
//   - atomViewOf：纯叶子 Atom（BuildView(p) 无 error，有 Advanced 层，V 字段）
//   - contentAtomViewOf：叶子 Atom（BuildView(p, content) 返回 (View, error)，有 Advanced 层）
//   - leafViewOf：无 Advanced 层的叶子组件（BuildView(p) 无 error，无 CustomID）
//
// 特殊组件（button/container/image/gallery/slider/tabs/accordion/marquee/globalref）
// 因拍平字段、递归 children、可见性分支或 BuildView 签名差异，保留独立实现。

// decodeProps 解码节点 props 为组件 Props 值（空 props 返回零值，与旧路径一致）。
func decodeProps[P any](node *core.Node) (P, error) {
	var p P
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return p, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	return p, nil
}

// advancedClasses 执行 Advanced 通用层编译并合并 class：返回 [NodeClass, extraClasses...]
// 与 customID（adv == nil 时 extraClasses 为空、customID 为空串）。
func advancedClasses[P any](node *core.Node, p *P, ctx *core.RenderContext) (classes []string, customID string) {
	classes = []string{core.NodeClass(node.ID)}
	if adv := core.AdvancedOf(p); adv != nil {
		// 滚动显现分层注入（H5「滚动过去才出内容」）：上下文启用且未显式配置时注入。
		if revealActive(ctx) {
			applyScrollReveal(&adv.Interaction, ctx.RevealDefaultEntrance)
		}
		extra, id := core.CompileAdvanced(node.ID, adv, ctx.CSS)
		classes = append(classes, extra...)
		customID = id
	}
	return classes, customID
}

// applyI18n 组件视图实现 core.I18nAware 时按当前语言回填文案字段（多语言 P4）。
//
// 与 ApplyImageLoading 同形：BuildView 之后由渲染层统一回填，组件包不感知语言来源。
// 取词函数为 ctx.Text（内部：注入函数 → fallback 原中文 → key），ctx 为 nil 亦安全。
func applyI18n(view any, ctx *core.RenderContext) {
	if aware, ok := view.(core.I18nAware); ok {
		aware.ApplyI18n(ctx.Text)
	}
}

// atomViewOf 收敛纯叶子 Atom 组件模板：props 解码 → Advanced → CompileCSS → BuildView(p) → nodeView。
// typeName 为组件类型常量，template 为模板名，compileCSS/buildView 为组件包导出函数。
func atomViewOf[P any, V any](
	node *core.Node,
	topLevel bool,
	ctx *core.RenderContext,
	typeName, template string,
	compileCSS func(id string, p *P, b *core.CSSBuckets),
	buildView func(p *P) V,
) (*nodeView, error) {
	p, err := decodeProps[P](node)
	if err != nil {
		return nil, err
	}
	classes, customID := advancedClasses(node, &p, ctx)
	compileCSS(node.ID, &p, ctx.CSS)
	view := buildView(&p)
	// 输出 <img> 的组件（card/infobox 等）按主题「图片管理」默认解析懒加载三态：
	// 视图自持 Loading 原值，经接口回填 IsEager/Skeleton/Class。
	if aware, ok := any(&view).(core.ImageLoadingAware); ok && aware.ApplyImageLoading(ctx.ImageDefaults) {
		core.AddImageSkeletonCSS(ctx.CSS)
	}
	// 构建期文案回填（countdown 单元标签 / form 提交按钮 / rating 无障碍描述等）。
	applyI18n(&view, ctx)
	declareViewFeatures(&view, ctx)
	return &nodeView{
		Type:     typeName,
		Template: template,
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

// contentAtomViewOf 收敛依赖 Content 的叶子 Atom 组件模板：
// props 解码 → Advanced → CompileCSS → BuildView(p, content) → nodeView。
func contentAtomViewOf[P any, V any](
	node *core.Node,
	topLevel bool,
	ctx *core.RenderContext,
	typeName, template string,
	compileCSS func(id string, p *P, b *core.CSSBuckets),
	buildView func(p *P, content core.ContentResolver) (V, error),
) (*nodeView, error) {
	p, err := decodeProps[P](node)
	if err != nil {
		return nil, err
	}
	classes, customID := advancedClasses(node, &p, ctx)
	compileCSS(node.ID, &p, ctx.CSS)
	view, err := buildView(&p, ctx.Content)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	applyI18n(&view, ctx)
	declareViewFeatures(&view, ctx)
	return &nodeView{
		Type:     typeName,
		Template: template,
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

// leafViewOf 收敛无 Advanced 层的叶子组件模板：
// props 解码 → CompileCSS → BuildView(p) → nodeView（Classes 仅 NodeClass，无 CustomID）。
func leafViewOf[P any, V any](
	node *core.Node,
	topLevel bool,
	ctx *core.RenderContext,
	typeName, template string,
	compileCSS func(id string, p *P, b *core.CSSBuckets),
	buildView func(p *P) V,
) (*nodeView, error) {
	p, err := decodeProps[P](node)
	if err != nil {
		return nil, err
	}
	// Advanced 通用层：与 atomViewOf 同源。本类组件此前完全跳过 Advanced，
	// 结果是编辑器里能配、能存，构建产物里却被静默丢弃。
	classes, customID := advancedClasses(node, &p, ctx)
	compileCSS(node.ID, &p, ctx.CSS)
	view := buildView(&p)
	// 输出 <img> 的组件（infobox 等无 Advanced 层的叶子）同样按主题默认解析加载三态。
	if aware, ok := any(&view).(core.ImageLoadingAware); ok && aware.ApplyImageLoading(ctx.ImageDefaults) {
		core.AddImageSkeletonCSS(ctx.CSS)
	}
	// 构建期文案回填（video 的 iframe title 等）。
	applyI18n(&view, ctx)
	declareViewFeatures(&view, ctx)
	return &nodeView{
		Type:     typeName,
		Template: template,
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

// buttonViewOf 转换 button 节点（对应 core.Atom 基座的 Render 流程）。
func buttonViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p buttonPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	// Advanced 通用层：CSS 编译 + 自定义 class/customID（与 Atom.Render 一致）。
	var extraClasses []string
	var customID string
	if adv := core.AdvancedOf(&p); adv != nil {
		extraClasses, customID = core.CompileAdvanced(node.ID, adv, ctx.CSS)
	}
	classes := []string{core.NodeClass(node.ID)}
	classes = append(classes, extraClasses...)

	// 组件样式（与 render 内部 compileCSS 一致）。
	buttonPkg.CompileCSS(node.ID, &p, ctx.CSS)

	// 渲染视图数据（标签 + 属性 + 图标）。
	// 站内链接本地化（审计 I18N-015）：作者填的 /shop 要按当前语言加前缀，
	// 否则英文站点上的按钮点击后跳回默认语言版本。
	view, err := buttonPkg.BuildView(&p, ctx.Content, ctx.ResolveSiteLink)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	// 媒体库图标 <img> 也走统一图片加载三态（按钮通常首屏可见，主题可改默认）。
	if aware, ok := any(&view).(core.ImageLoadingAware); ok && aware.ApplyImageLoading(ctx.ImageDefaults) {
		core.AddImageSkeletonCSS(ctx.CSS)
	}
	declareViewFeatures(&view, ctx)

	return &nodeView{
		Type:       buttonPkg.Type,
		Template:   "button",
		NodeID:     node.ID,
		Classes:    strings.Join(classes, " "),
		CustomID:   customID,
		TopLevel:   topLevel,
		Props:      p,
		V:          view,
		Tag:        view.Tag,
		Attrs:      view.Attrs,
		Text:       view.Text,
		IconPrefix: view.IconPrefix,
		IconSuffix: view.IconSuffix,
	}, nil
}

// containerViewOf 转换 container 节点（对应 Container.Render 流程）。
func containerViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p containerPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	// 滚动显现注入（container 的 Interaction 为顶层字段，不走 advancedClasses）。
	if revealActive(ctx) {
		applyScrollReveal(&p.Interaction, ctx.RevealDefaultEntrance)
	}
	// 容器滚动显现覆盖（栈式：进入子树前设置，函数返回自动恢复——
	// 触发路径：nodeViewOf 入口按 ctx.RevealInherit 注入，见 reveal.go）。
	switch p.StyleEx.Reveal {
	case "on":
		old := ctx.RevealInherit
		ctx.RevealInherit = "on"
		defer func() { ctx.RevealInherit = old }()
	case "off":
		old := ctx.RevealInherit
		ctx.RevealInherit = "off"
		defer func() { ctx.RevealInherit = old }()
	}

	// Advanced 通用层：与 atomViewOf 同源。container/slider 此前整段跳过 Advanced，
	// 编辑器里能配、能存，构建产物里却被静默丢弃。
	classes, customID := advancedClasses(node, &p, ctx)
	cls := strings.Join(classes, " ")
	if topLevel {
		cls += " " + core.SectionClass
	}

	// 先递归 children：CSS 加入顺序为「子节点先、容器自身后」，与 Container.Render 一致。
	children := make([]*nodeView, 0, len(node.Children))
	for _, child := range node.Children {
		cv, err := nodeViewOf(child, false, ctx)
		if err != nil {
			return nil, err
		}
		children = append(children, cv)
	}

	// 容器自身样式（与 Render 内部 compileCSS 一致）。
	containerPkg.CompileCSS(node.ID, &p, ctx.CSS)

	view := containerPkg.BuildView(node, &p)
	declareViewFeatures(&view, ctx)

	return &nodeView{
		Type:        containerPkg.Type,
		Template:    "container",
		NodeID:      node.ID,
		Classes:     cls,
		CustomID:    customID,
		TopLevel:    topLevel,
		Props:       p,
		Children:    children,
		Tag:         view.Tag,
		Attrs:       view.Attrs,
		ShapeTop:    view.ShapeTop,
		ShapeBottom: view.ShapeBottom,
		BgSlides:    view.BgSlides,
	}, nil
}

// productViewOf 转换商品详情节点（字段经商品解析器静态填入，越界字段编译期报错）。
func productViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return contentAtomViewOf(node, topLevel, ctx, productPkg.Type, "product", productPkg.CompileCSS,
		// 闭包适配：实时价格核对 / 可用量两个片段位都要把站点工程 id 烘进 URL，
		// 而 contentAtomViewOf 只接受 func(*Props, ContentResolver) (View, error)
		// （与 cardViewOf 同路）。工程取自构建上下文，组件包不感知它从哪来。
		func(p *productPkg.Props, content core.ContentResolver) (productPkg.View, error) {
			return productPkg.BuildView(p, content, ctx.ProjectID)
		})
}

// productCardViewOf 转换商品卡节点（issue #22）。
//
// 与商品详情同一条链路（contentAtomViewOf）：字段经解析器静态填入 —— 在集合里解析器是
// 集合项作用域（item.* 取当前商品），集合外是页面级解析器（product.* 取当前实体）。
// 越界字段在编译期报错，不静默渲染成空卡。
func productCardViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return contentAtomViewOf(node, topLevel, ctx, productcardPkg.Type, "product_card", productcardPkg.CompileCSS, productcardPkg.BuildView)
}

// headingViewOf 转换 heading 节点（对应 core.Atom 基座的 Render 流程）。
func headingViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return contentAtomViewOf(node, topLevel, ctx, headingPkg.Type, "heading", headingPkg.CompileCSS, headingPkg.BuildView)
}

// textViewOf 转换 text 节点（对应 core.Atom 基座的 Render 流程）。
func textViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return contentAtomViewOf(node, topLevel, ctx, textPkg.Type, "text", textPkg.CompileCSS, textPkg.BuildView)
}

// imageViewOf 转换 image 节点（对应 core.Atom 基座的 Render 流程）。
// 说明：image 的 customID 位置随点击动作分支变化（img 前 / a 上 / 忽略），
// 故 class 与 customID 传入 BuildView 预计算完整 HTML，模板仅原样输出。
func imageViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p imagePkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	var extraClasses []string
	var customID string
	if adv := core.AdvancedOf(&p); adv != nil {
		extraClasses, customID = core.CompileAdvanced(node.ID, adv, ctx.CSS)
	}
	classes := []string{core.NodeClass(node.ID)}
	classes = append(classes, extraClasses...)
	classStr := strings.Join(classes, " ")

	imagePkg.CompileCSS(node.ID, &p, ctx.CSS)

	// 站内链接本地化（审计 I18N-015）：图片上作者填的链接。
	view, err := imagePkg.BuildView(node, &p, classStr, customID, ctx.Content, ctx.ImageDefaults, ctx.AssetProbe, ctx.ResolveSiteLink)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	// 懒加载骨架屏（主题「图片管理 → 骨架屏」开启且本图懒加载）：
	// 给 <img> 加 is-skeleton 类，用纯 CSS 渐变占位——图片加载完成后内容自然覆盖背景，
	// 无需任何 JS（产物零脚本约束）。
	if view.Skeleton {
		view.Class = core.ImageSkeletonClass(classStr, true)
		core.AddImageSkeletonCSS(ctx.CSS)
	}

	return &nodeView{
		Type:     imagePkg.Type,
		Template: "image",
		NodeID:   node.ID,
		Classes:  classStr,
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

// dividerViewOf 转换 divider 节点（对应 core.Atom 基座的 Render 流程）。
func dividerViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return atomViewOf(node, topLevel, ctx, dividerPkg.Type, "divider", dividerPkg.CompileCSS, dividerPkg.BuildView)
}

// spacerViewOf 转换 spacer 节点（对应 core.Atom 基座的 Render 流程）。
func spacerViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return atomViewOf(node, topLevel, ctx, spacerPkg.Type, "spacer", spacerPkg.CompileCSS, spacerPkg.BuildView)
}

// shapedividerViewOf 转换 shapedivider 节点（对应 core.Atom 基座的 Render 流程）。
func shapedividerViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return atomViewOf(node, topLevel, ctx, shapedividerPkg.Type, "shapedivider", shapedividerPkg.CompileCSS, shapedividerPkg.BuildView)
}

// loaderViewOf 转换 loader 节点（对应 core.Atom 基座的 Render 流程）。
func loaderViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return atomViewOf(node, topLevel, ctx, loaderPkg.Type, "loader", loaderPkg.CompileCSS, loaderPkg.BuildView)
}

// 以下 10 个 ViewOf 为新组件库补齐（对标 GrapesJS 组件生态），
// 均为叶子原子组件（core.Atom 基座），模式与 spacer/divider 一致：
// props 解码 → Advanced 编译 → 组件 CSS → BuildView → nodeView。

func tableViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return atomViewOf(node, topLevel, ctx, tablePkg.Type, "table", tablePkg.CompileCSS, tablePkg.BuildView)
}

func cardViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	// 闭包适配：站内链接本地化要传 ctx，而 atomViewOf 只接受 func(*Props) View（审计 I18N-015）。
	return atomViewOf(node, topLevel, ctx, cardPkg.Type, "card", cardPkg.CompileCSS, func(p *cardPkg.Props) cardPkg.View {
		return cardPkg.BuildView(p, ctx.ResolveSiteLink)
	})
}

// cardstackViewOf 转换卡片堆叠节点：结构型组件 —— 子节点即卡片内容（没有则退回数字卡），
// props 只描述几何与交互，故不走 atomViewOf，与 tabs/accordion 同路。
// cardstackView 卡片堆叠的模板视图：几何/字段数据来自组件包，子节点分组由本层组装 ——
// nodeView 是本包私有类型，组件包看不到，所以「集合项 → 一组子节点」只能在这里拼。
type cardstackView struct {
	cardstackPkg.View
	// CardNodes 集合项模板模式：每个集合项一组已渲染子树（第 i 组 = 第 i 张卡的内容）。
	CardNodes [][]*nodeView
	// HasCardNodes 是否走子节点模板（模板里据此分支，空切片也能表达）。
	HasCardNodes bool
}

func cardstackViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p cardstackPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	// 先解析视图（内容集合模式要在这里展开成 N 张卡），再按实际卡片数编译 CSS ——
	// 集合条数运行期才知道，逐卡 :nth-child 规则必须与之对齐。
	base, err := cardstackPkg.BuildView(node, &p, ctx)
	if err != nil {
		return nil, err
	}
	view := cardstackView{View: base}

	children := make([]*nodeView, 0, len(node.Children))
	isCollection := cardstackPkg.IsCollection(&p)
	for _, child := range node.Children {
		// 集合模式：子节点不再各自成卡，而是「每张卡的模板」——延后到按项展开时渲染。
		if isCollection {
			continue
		}
		cv, err := nodeViewOf(child, false, ctx)
		if err != nil {
			return nil, err
		}
		children = append(children, cv)
	}

	// 集合 + 子节点 = 子节点模板模式：按集合项展开子树，每项渲染时把 ContentResolver
	// 换成作用域化的 ItemScope —— 子节点组件照旧调 ResolveString（写 item.title 即取当前项），
	// 不需要知道自己在集合里。
	if isCollection && len(node.Children) > 0 {
		items, ierr := cardstackPkg.CollectionItems(node, &p, ctx)
		if ierr != nil {
			return nil, ierr
		}
		view.CardNodes = make([][]*nodeView, 0, len(items))
		for _, item := range items {
			itemCtx := *ctx
			itemCtx.Content = core.ItemScope{Inner: ctx.Content, Item: item}
			group := make([]*nodeView, 0, len(node.Children))
			for _, child := range node.Children {
				cv, cerr := nodeViewOf(child, false, &itemCtx)
				if cerr != nil {
					return nil, cerr
				}
				group = append(group, cv)
			}
			view.CardNodes = append(view.CardNodes, group)
		}
		view.HasCardNodes = len(view.CardNodes) > 0
	}

	classes, customID := advancedClasses(node, &p, ctx)
	cardstackPkg.CompileCSS(node, &p, len(base.Cards), ctx.CSS)
	declareViewFeatures(&view, ctx)

	return &nodeView{
		Type:     cardstackPkg.Type,
		Template: "cardstack",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		Children: children,
		V:        view,
	}, nil
}

// productListViewOf 转换商品列表节点（issue #23）：集合型组件 ——
// 取数 / 排序 / 截断 / 卡片映射都在组件包（BuildView），本层只做 Advanced 类名、
// CSS 编译与 nodeView 组装（与 cardstack 的「结构型组件不走 atomViewOf」同路）。
// productSelectorViewOf 转换规格选择器节点（issue #26）：与商品详情同一条链路
// （contentAtomViewOf），只是渲染的是「可独立拖拽的选择器」而不是整块详情。
func productSelectorViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return contentAtomViewOf(node, topLevel, ctx, productselectorPkg.Type, "product_selector", productselectorPkg.CompileCSS,
		// 与商品详情同一条闭包适配：规格选择器也要把工程 id 烘进片段 URL。
		func(p *productselectorPkg.Props, content core.ContentResolver) (productselectorPkg.View, error) {
			return productselectorPkg.BuildView(p, content, ctx.ProjectID)
		})
}

// cartIconViewOf 转换购物车图标节点（BIZ-1 访问面）。
//
// 手写而不是走 contentAtomViewOf：它除了构建上下文还需要**槽位路径**
// （无 JS 时图标的兜底链接目标），而那个通用助手只把内容解析器传给 BuildView。
func cartIconViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p carticonPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	classes, customID := advancedClasses(node, &p, ctx)
	carticonPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := carticonPkg.BuildView(&p, ctx.ProjectID, ctx.Lang, ctx.SitePage(core.SiteSlotCart))
	applyI18n(&view, ctx)
	declareViewFeatures(&view, ctx)
	return &nodeView{
		Type:     carticonPkg.Type,
		Template: "cart_icon",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

// searchResultsViewOf 转换站内搜索节点（BIZ-2）。
func searchResultsViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p searchresultsPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	classes, customID := advancedClasses(node, &p, ctx)
	searchresultsPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := searchresultsPkg.BuildView(&p, ctx.ProjectID, ctx.Lang)
	declareViewFeatures(&view, ctx)
	return &nodeView{
		Type:     searchresultsPkg.Type,
		Template: "search_widget",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

// orderListViewOf 转换访客订单列表节点（BIZ-1 访问面）。
//
// 手写而不是走 contentAtomViewOf：它需要两条**槽位路径**（未登录引导的登录页、
// 无 JS 时的订单页），而那个通用助手只把内容解析器传给 BuildView。
func orderListViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p orderlistPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	classes, customID := advancedClasses(node, &p, ctx)
	orderlistPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := orderlistPkg.BuildView(&p, ctx.ProjectID, ctx.Lang,
		ctx.SitePage(core.SiteSlotLogin), ctx.SitePage(core.SiteSlotOrders))
	declareViewFeatures(&view, ctx)
	return &nodeView{
		Type:     orderlistPkg.Type,
		Template: "orders_widget",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

// userFormsViewOf 转换访客账号表单节点（issue #36）。
//
// 手写而不是走 contentAtomViewOf：它要的是构建上下文里的**工程 id 与语言**
// （片段地址带它们），而那个通用助手只把内容解析器传给 BuildView。
func userFormsViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p userformsPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	classes, customID := advancedClasses(node, &p, ctx)
	userformsPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := userformsPkg.BuildView(&p, ctx.ProjectID, ctx.Lang)
	declareViewFeatures(&view, ctx)
	return &nodeView{
		Type:     userformsPkg.Type,
		Template: "user_forms_widget",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

// addToCartViewOf 转换加购节点（BIZ-1 访问面）。
//
// 手写而不是走 contentAtomViewOf：加购表单除了商品字段还要**站点工程 id**
// （片段端据此定位工程），而那个通用助手只把内容解析器传给 BuildView。
// 工程 id 来自构建上下文（与导航取站点级资源同源），不猜、不从字段里凑。
func addToCartViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p addtocartPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	classes, customID := advancedClasses(node, &p, ctx)
	addtocartPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view, err := addtocartPkg.BuildView(&p, ctx.Content, ctx.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	applyI18n(&view, ctx)
	declareViewFeatures(&view, ctx)
	return &nodeView{
		Type:     addtocartPkg.Type,
		Template: "add_to_cart",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

func productListViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p productlistPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	view, err := productlistPkg.BuildView(node, &p, ctx)
	if err != nil {
		return nil, err
	}
	classes, customID := advancedClasses(node, &p, ctx)
	productlistPkg.CompileCSS(node.ID, &p, ctx.CSS)
	declareViewFeatures(&view, ctx)
	return &nodeView{
		Type:     productlistPkg.Type,
		Template: "product_list",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

func faqViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return atomViewOf(node, topLevel, ctx, faqPkg.Type, "faq", faqPkg.CompileCSS, faqPkg.BuildView)
}

func quoteViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return atomViewOf(node, topLevel, ctx, quotePkg.Type, "quote", quotePkg.CompileCSS, quotePkg.BuildView)
}

func countdownViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return atomViewOf(node, topLevel, ctx, countdownPkg.Type, "countdown", countdownPkg.CompileCSS, countdownPkg.BuildView)
}

func iconViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return atomViewOf(node, topLevel, ctx, iconPkg.Type, "icon", iconPkg.CompileCSS, iconPkg.BuildView)
}

func badgeViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return atomViewOf(node, topLevel, ctx, badgePkg.Type, "badge", badgePkg.CompileCSS, badgePkg.BuildView)
}

// breadcrumbViewOf 转换面包屑节点（内容型，无 children）。
//
// 走 leafViewOf 而不是 atomViewOf：BuildView 除了 props 还要构建上下文
// （未手填 items 时层级按 RenderContext.CurrentPath 派生），故用闭包适配
// （同 list / card 的写法）。
func breadcrumbViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return leafViewOf(node, topLevel, ctx, breadcrumbPkg.Type, "breadcrumb", breadcrumbPkg.CompileCSS, func(p *breadcrumbPkg.Props) breadcrumbPkg.View {
		return breadcrumbPkg.BuildView(p, ctx)
	})
}

func progressViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return atomViewOf(node, topLevel, ctx, progressPkg.Type, "progress", progressPkg.CompileCSS, progressPkg.BuildView)
}

func ratingViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return atomViewOf(node, topLevel, ctx, ratingPkg.Type, "rating", ratingPkg.CompileCSS, ratingPkg.BuildView)
}

func formViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return atomViewOf(node, topLevel, ctx, formPkg.Type, "form", formPkg.CompileCSS, formPkg.BuildView)
}

// listViewOf 转换 list 节点（对应 Component.Render 流程，无 Advanced 层）。
func listViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	// 闭包适配：同 card（审计 I18N-015）。
	return leafViewOf(node, topLevel, ctx, listPkg.Type, "list", listPkg.CompileCSS, func(p *listPkg.Props) listPkg.View {
		return listPkg.BuildView(p, ctx.ResolveSiteLink)
	})
}

// infoboxViewOf 转换 infobox 节点（对应 Component.Render 流程，无 Advanced 层）。
func infoboxViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return leafViewOf(node, topLevel, ctx, infoboxPkg.Type, "infobox", infoboxPkg.CompileCSS, infoboxPkg.BuildView)
}

// socialbuttonsViewOf 转换 socialbuttons 节点（对应 Component.Render 流程，无 Advanced 层）。
func socialbuttonsViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return leafViewOf(node, topLevel, ctx, socialbuttonsPkg.Type, "socialbuttons", socialbuttonsPkg.CompileCSS, socialbuttonsPkg.BuildView)
}

// videoViewOf 转换 video 节点（对应 Component.Render 流程，无 Advanced 层）。
func videoViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return leafViewOf(node, topLevel, ctx, videoPkg.Type, "video", videoPkg.CompileCSS, videoPkg.BuildView)
}

// counterViewOf 转换 counter 节点（对应 Component.Render 流程，无 Advanced 层）。
func counterViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return leafViewOf(node, topLevel, ctx, counterPkg.Type, "counter", counterPkg.CompileCSS, counterPkg.BuildView)
}

// galleryViewOf 转换 gallery 节点（对应 core.Atom 基座的 Render 流程）。
func galleryViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p galleryPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	var extraClasses []string
	var customID string
	if adv := core.AdvancedOf(&p); adv != nil {
		extraClasses, customID = core.CompileAdvanced(node.ID, adv, ctx.CSS)
	}
	classes := []string{core.NodeClass(node.ID)}
	classes = append(classes, extraClasses...)

	view, err := galleryPkg.BuildView(node.ID, &p, ctx.Content)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}

	// 构建期文案回填（轮播箭头 aria-label）。
	applyI18n(&view, ctx)
	declareViewFeatures(&view, ctx)

	// 隐藏（空图集且无占位）时旧路径不编译组件样式；可见才编译。
	if view.Visible {
		galleryPkg.CompileCSS(node.ID, &p, ctx.CSS)
		// 图集内所有图片共用组件级三态（主题默认解析后统一输出 loading + 骨架类）。
		if aware, ok := any(&view).(core.ImageLoadingAware); ok && aware.ApplyImageLoading(ctx.ImageDefaults) {
			core.AddImageSkeletonCSS(ctx.CSS)
		}
	}

	return &nodeView{
		Type:     galleryPkg.Type,
		Template: "gallery",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

// sliderViewOf 转换 slider 节点（对应 Component.Render 流程，children 为各 slide）。
func sliderViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p sliderPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	// Advanced 通用层：与 atomViewOf 同源。container/slider 此前整段跳过 Advanced，
	// 编辑器里能配、能存，构建产物里却被静默丢弃。
	classes, customID := advancedClasses(node, &p, ctx)
	cls := strings.Join(classes, " ")
	if topLevel {
		cls += " " + core.SectionClass
	}

	// 先递归 children：CSS 加入顺序为「子节点先、组件自身后」，与 Render 一致。
	children := make([]*nodeView, 0, len(node.Children))
	for _, child := range node.Children {
		cv, err := nodeViewOf(child, false, ctx)
		if err != nil {
			return nil, err
		}
		children = append(children, cv)
	}

	sliderPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := sliderPkg.BuildView(node, &p)
	applyI18n(&view, ctx)
	declareViewFeatures(&view, ctx)

	return &nodeView{
		Type:     sliderPkg.Type,
		Template: "slider",
		NodeID:   node.ID,
		Classes:  cls,
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		Children: children,
		V:        view,
	}, nil
}

// tabsViewOf 转换 tabs 节点（对应 Component.Render 流程，children 为各面板）。
// languagesViewOf 转换语言切换器节点（原子，无 children）。
//
// 与 navViewOf 同形而非 atomViewOf：BuildView 需要构建期注入的 ctx.Locales
// （各语言链接与当前语言标记），不是只依赖 props 的纯函数。
func languagesViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p languagesPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	classes, customID := advancedClasses(node, &p, ctx)
	languagesPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := languagesPkg.BuildView(node, &p, ctx)
	applyI18n(&view, ctx)
	return &nodeView{
		Type:     languagesPkg.Type,
		Template: "languages",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

// navViewOf 转换导航菜单组件（内容型，无 children；toggle id 依赖节点 ID）。
func navViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p navPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	if err := resolveNavMenu(node, &p, ctx); err != nil {
		return nil, err
	}
	// 当前项高亮：按本次编译的页面路径标记（块预览等无路径时不标记）。
	navPkg.MarkCurrent(p.Items, ctx.CurrentPath)
	// Advanced 通用层：与 atomViewOf 同源。本类组件此前完全跳过 Advanced，
	// 结果是编辑器里能配、能存，构建产物里却被静默丢弃。
	classes, customID := advancedClasses(node, &p, ctx)
	navPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := navPkg.BuildView(node, &p)
	applyI18n(&view, ctx)
	return &nodeView{
		Type:     navPkg.Type,
		Template: "nav",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

// resolveNavMenu 导航节点绑定了菜单位置（header/footer）时，用构建期解析结果
// 覆盖手写菜单项：产物仍是静态 HTML，导航数据在构建期一次性读库。
// 未注入解析器/工程 ID 时显式报错（构建期失败优先于静默产出空菜单）。
func resolveNavMenu(node *core.Node, p *navPkg.Props, ctx *core.RenderContext) error {
	kind := strings.TrimSpace(p.Menu)
	if kind == "" {
		return nil
	}
	if ctx.Navigation == nil || strings.TrimSpace(ctx.ProjectID) == "" {
		return fmt.Errorf("节点 %s: 已绑定导航位置 %q，但构建期缺少导航解析器或工程 ID（装配未注入）", node.ID, kind)
	}
	items, err := ctx.Navigation.ResolveMenu(ctx.ProjectID, kind)
	if err != nil {
		return fmt.Errorf("节点 %s: 导航位置 %q 解析失败: %w", node.ID, kind, err)
	}
	// 取值即记录（审计 VIS-006 同一口径）：本次编译确实把该位置的导航烘进了产物，
	// 产物依赖里就必须留下 menu:{projectID}:{kind}，否则改导航后该产物不会被标 stale。
	// 解析失败会让整次编译失败（无产物、也就无依赖可失效），故只记成功路径。
	ctx.UseMenu(kind)
	p.Items = navPkg.ItemsOf(items)
	return navPkg.ValidateItems(p.Items, node.ID)
}

func tabsViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p tabsPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	children := make([]*nodeView, 0, len(node.Children))
	for _, child := range node.Children {
		cv, err := nodeViewOf(child, false, ctx)
		if err != nil {
			return nil, err
		}
		children = append(children, cv)
	}

	// Advanced 通用层：与 atomViewOf 同源。本类组件此前完全跳过 Advanced，
	// 结果是编辑器里能配、能存，构建产物里却被静默丢弃。
	classes, customID := advancedClasses(node, &p, ctx)
	tabsPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := tabsPkg.BuildView(node, &p)

	return &nodeView{
		Type:     tabsPkg.Type,
		Template: "tabs",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		Children: children,
		V:        view,
	}, nil
}

// accordionViewOf 转换 accordion 节点（对应 Component.Render 流程，children 为各折叠内容）。
func accordionViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p accordionPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	children := make([]*nodeView, 0, len(node.Children))
	for _, child := range node.Children {
		cv, err := nodeViewOf(child, false, ctx)
		if err != nil {
			return nil, err
		}
		children = append(children, cv)
	}

	// Advanced 通用层：与 atomViewOf 同源。本类组件此前完全跳过 Advanced，
	// 结果是编辑器里能配、能存，构建产物里却被静默丢弃。
	classes, customID := advancedClasses(node, &p, ctx)
	accordionPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := accordionPkg.BuildView(&p)

	return &nodeView{
		Type:     accordionPkg.Type,
		Template: "accordion",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		Children: children,
		V:        view,
	}, nil
}

// marqueeViewOf 转换 marquee 节点（对应 Component.Render 流程，children 为滚动内容）。
func marqueeViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p marqueePkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	children := make([]*nodeView, 0, len(node.Children))
	for _, child := range node.Children {
		cv, err := nodeViewOf(child, false, ctx)
		if err != nil {
			return nil, err
		}
		children = append(children, cv)
	}

	// Advanced 通用层：与 atomViewOf 同源。本类组件此前完全跳过 Advanced，
	// 结果是编辑器里能配、能存，构建产物里却被静默丢弃。
	classes, customID := advancedClasses(node, &p, ctx)
	marqueePkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := marqueePkg.BuildView(&p)

	return &nodeView{
		Type:     marqueePkg.Type,
		Template: "marquee",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		Children: children,
		V:        view,
	}, nil
}

// maxBlockExpandDepth 全局块展开深度上限（合法嵌套 3~6 层，32 为充裕上限）。
// 超限或循环引用立即报错——否则无限展开会指数级耗尽内存（OOM，而非栈溢出）。
const maxBlockExpandDepth = 32

// globalrefViewOf 转换 core.globalref 节点（占位或展开）。
func globalrefViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	blockID, err := globalrefPkg.BlockIDOf(node)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	return blockRefViewOf(node, topLevel, ctx, globalrefPkg.Type, "globalref", blockID)
}

// layoutSlotViewOf 转换 core.layoutSlot 节点：结构槽位的展开规则与 globalref 完全一致，
// 只有类型与模板名不同 —— 防环、深度限制、ID 前缀重写都走同一份实现，
// 免得这类安全约束只在一个入口生效。
func layoutSlotViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	blockID, err := layoutslotPkg.BlockIDOf(node)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	return blockRefViewOf(node, topLevel, ctx, layoutslotPkg.Type, "layoutslot", blockID)
}

// blockRefViewOf 转换「引用全局块的节点」：占位渲染或展开块内容。
//
// 含循环引用与深度防护：同一块 ID 不允许嵌套展开（a→b→a），栈深超限报错。
func blockRefViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext, refType, template, blockID string) (*nodeView, error) {
	for _, id := range ctx.BlockStack {
		if id == blockID {
			return nil, fmt.Errorf("节点 %s: 全局块循环引用（%s → %s）", node.ID, strings.Join(ctx.BlockStack, " → "), blockID)
		}
	}
	if len(ctx.BlockStack) >= maxBlockExpandDepth {
		return nil, fmt.Errorf("节点 %s: 全局块嵌套超过 %d 层上限", node.ID, maxBlockExpandDepth)
	}

	view, roots, err := globalrefPkg.BuildView(node, ctx.Block)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}

	if view.IsPlaceholder {
		return &nodeView{
			Type: refType, Template: template, NodeID: node.ID,
			Classes: core.NodeClass(node.ID), TopLevel: topLevel, V: view,
		}, nil
	}

	// 展开：递归块 root children（ID 前缀已由 BuildView 重写）。
	// 块 ID 入栈/出栈维护展开上下文（防环检查见函数头）。
	ctx.BlockStack = append(ctx.BlockStack, blockID)
	children := make([]*nodeView, 0, len(roots))
	for _, r := range roots {
		cv, cerr := nodeViewOf(r, false, ctx)
		if cerr != nil {
			ctx.BlockStack = ctx.BlockStack[:len(ctx.BlockStack)-1]
			return nil, cerr
		}
		children = append(children, cv)
	}
	ctx.BlockStack = ctx.BlockStack[:len(ctx.BlockStack)-1]

	return &nodeView{
		Type: refType, Template: template, NodeID: node.ID,
		Classes: core.NodeClass(node.ID), TopLevel: topLevel,
		Children: children, V: view,
	}, nil
}

func renderView(set *jet.Set, root *nodeView, w io.Writer) error {
	tpl, err := set.GetTemplate(root.Template)
	if err != nil {
		return fmt.Errorf("获取组件模板 %q 失败: %w", root.Template, err)
	}
	return tpl.Execute(w, nil, root)
}

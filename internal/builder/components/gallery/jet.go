// Package gallery — Jet 渲染路径辅助导出（Phase 2）。
//
// 与 render 函数并行的新路径：props 解码 / CSS 生成 / 数据源解析保留在 Go，
// HTML 拼装交给 gallery.jet 模板（grid/carousel 两模式 if/else + 单图项结构）。
// render 函数保持不变（旧输出），本文件只做最小导出与等价的数据准备。
package gallery

import (
	"fmt"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

// CompileCSS 导出图集样式编译（复用 render 内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// View gallery 渲染视图数据（供 gallery.jet 模板使用）。
type View struct {
	// Visible 是否输出组件（空图集且无占位图时 false，组件整体隐藏，不编译样式）。
	Visible bool
	// IsCarousel 是否轮播模式（否则为 grid 网格模式）。
	IsCarousel bool
	// FirstEager 轮播首图优先加载（carousel 且未关闭 carousel.firstEagerOff 时为真）。
	FirstEager bool
	// Items 每个单图项的渲染视图数据（URL/alt/图注/点击动作分支）。
	Items []ItemView
	// CarouselAttr 轮播增强属性 data-carousel='...'（carousel 模式，原样输出）。
	CarouselAttr string
	// Arrows 显示左右箭头（carousel 模式）。
	Arrows bool
	// Dots 显示圆点指示器（carousel 模式）。
	Dots bool
	// SlideLabel 圆点 aria-label 模板（含单个 %s；由 ApplyI18n 填充）。
	SlideLabel string
	// DotItems 圆点导航锚点（构建期生成：<a href="#...">点击由浏览器原生滚动 +
	// scroll-snap 对齐，**零 JS 可用**；aria-label 构建期按语言 + 序号填好）。
	DotItems []DotItem

	// Loading 组件级加载策略三态原值（空=继承主题，由 ApplyImageLoading 解析）。
	Loading string
	// IsEager 立即加载（ApplyImageLoading 后有效，作用于图集内全部 <img>）。
	IsEager bool
	// Skeleton 懒加载骨架屏（ApplyImageLoading 后有效）。
	Skeleton bool
	// Class 附加 class（仅骨架类；模板拼在 "gi" 之后，空则不追加）。
	Class string

	// PrevLabel / NextLabel 轮播箭头 aria-label（构建期按当前语言填充，多语言 P4）。
	PrevLabel string
	NextLabel string
}

// 访客面组件文案 key：site.component.{type}.{prop}（docs/06-D §10.3）。
const (
	// TextKeyPrev 轮播「上一张」箭头的词条 key。
	TextKeyPrev = "site.component.gallery.prev"
	// TextKeyNext 轮播「下一张」箭头的词条 key。
	TextKeyNext = "site.component.gallery.next"
	// TextKeySlideLabel 圆点 aria-label 模板的词条 key（含单个 %s）。
	TextKeySlideLabel = "site.component.gallery.slide_label"
)

// DotItem 圆点导航项（锚点 id 指向对应 slide 元素）。
type DotItem struct {
	// Anchor slide 元素 id。
	Anchor string
	// Label 无障碍标签（构建期翻译 + 序号替换）。
	Label string
}

// textFallbackPrev / textFallbackNext 缺词条时的原中文兜底（绝不输出空串）。
const (
	textFallbackPrev       = "上一张"
	textFallbackNext       = "下一张"
	textFallbackSlideLabel = "第 %s 张"
)

// ItemView 单个图集项的渲染视图数据（供 gallery.jet 模板使用）。
type ItemView struct {
	// AnchorID slide 元素 id（carousel 模式构建期生成：圆点锚点跳转目标）。
	AnchorID string
	// URL 图片地址（img src 与灯箱原图，模板输出时由 Jet 默认转义）。
	URL string
	// Alt 替代文本（模板输出时由 Jet 默认转义）。
	Alt string
	// Caption 图注文本（模板输出时由 Jet 默认转义）。
	Caption string
	// HasFigure 是否 figure 包裹（图注模式 below/hover 且图注非空）。
	HasFigure bool
	// IsLightbox 灯箱分支（点击原图，data-lightbox 相册标记）。
	IsLightbox bool
	// IsLink 链接分支（打开 href）。
	IsLink bool
	// Href 链接地址（link 分支，模板输出时由 Jet 默认转义）。
	Href string

	// Loading 单图 loading 三态原值（空=继承组件级 loading）。
	Loading string
	// FetchPriority 单图 fetchpriority 原值（空=不输出属性）。
	FetchPriority string
	// IsEager 立即加载（ApplyImageLoading 后有效）。
	IsEager bool
	// Skeleton 懒加载骨架屏（ApplyImageLoading 后有效）。
	Skeleton bool
	// Class 附加 class（仅骨架类；模板拼在 "gi" 之后）。
	Class string
	// FetchHigh / FetchLow 资源提示（ApplyImageLoading 后有效）。
	FetchHigh bool
	FetchLow  bool
}

// BuildView 生成图集渲染视图：数据源解析（绑定优先/静态兜底）+ 单图项视图准备
// + 轮播属性拼装（与 render 输出结构一致）。
func BuildView(nodeID string, p *Props, content core.ContentResolver) (View, error) {
	// 与旧 render 一致：空模式回落 grid，并写回 p.Mode（compileCSS 依赖它选择分支）。
	if p.Mode == "" {
		p.Mode = LayoutGrid
	}
	mode := p.Mode

	items, err := resolveSourceContent(p, content)
	if err != nil {
		return View{}, err
	}
	if items == nil {
		return View{}, nil // 空图集 + 无占位：组件隐藏（Visible 默认 false）
	}

	views := make([]ItemView, 0, len(items))
	for i, r := range items {
		iv := buildItemView(p, r)
		if mode == LayoutCarousel {
			iv.AnchorID = "sky-gslide-" + nodeID + "-" + strconv.Itoa(i)
		}
		views = append(views, iv)
	}

	v := View{Visible: true, IsCarousel: mode == LayoutCarousel, Items: views, Loading: p.Loading,
		FirstEager: mode == LayoutCarousel && !p.Carousel.FirstEagerOff}
	// 圆点锚点：构建期按图片数生成（label 由 ApplyI18n 填）。
	if mode == LayoutCarousel && p.Carousel.Dots {
		v.DotItems = make([]DotItem, 0, len(views))
		for i := range views {
			v.DotItems = append(v.DotItems, DotItem{
				Anchor: views[i].AnchorID,
			})
		}
	}
	if mode == LayoutCarousel {
		c := p.Carousel
		interval := c.Interval
		if interval == 0 {
			interval = 4000
		}
		v.CarouselAttr = fmt.Sprintf(`data-carousel='{"autoplay":%t,"interval":%d,"infinite":%t,"pauseOnHover":%t,"slidesPerView":{"desktop":%s,"tablet":%s,"mobile":%s}}'`,
			c.Autoplay, interval, c.Infinite, c.PauseOnHover,
			slideNum(c.SlidesPerView.Desktop, 1), slideNum(c.SlidesPerView.Tablet, 1), slideNum(c.SlidesPerView.Mobile, 1))
		v.Arrows = c.Arrows
		v.Dots = c.Dots
	}
	return v, nil
}

// buildItemView 单图项渲染视图：img 字段 + 点击动作分支 + 图注包裹判定
// （与旧 renderItem 输出结构一致）。
func buildItemView(p *Props, r Item) ItemView {
	href := r.Link
	if href == "" {
		href = p.DefaultLink
	}

	iv := ItemView{URL: r.URL, Alt: r.Alt, Caption: r.Caption, Loading: r.Loading, FetchPriority: r.FetchPriority}
	switch {
	case p.ClickAction == ClickLightbox && href == "":
		// 默认相册灯箱：点击打开原图（客户端增强脚本接管为滑动相册；无脚本时浏览器直开图片）。
		iv.IsLightbox = true
	case (p.ClickAction == ClickLink || r.Link != "") && href != "":
		iv.IsLink = true
		iv.Href = href
	}

	if (p.CaptionMode == CaptionBelow || p.CaptionMode == CaptionHover) && r.Caption != "" {
		iv.HasFigure = true
	}
	return iv
}

// resolveSourceContent 图集数据源解析（绑定优先/静态兜底），与 render 内部的
// resolveSource 逻辑等价，参数改为 ContentResolver 接口（避免依赖 AtomRender 结构）。
func resolveSourceContent(p *Props, content core.ContentResolver) (items []Item, err error) {
	if p.Binding == nil || p.Binding.Field == "" {
		return p.Items, nil
	}
	if content == nil {
		return nil, fmt.Errorf("编译上下文缺少内容解析器")
	}
	v, err := content.ResolveString(p.Binding.Field)
	if err != nil {
		return nil, fmt.Errorf("解析绑定 %q 失败: %w", p.Binding.Field, err)
	}
	if v != "" {
		items, err = parseValues(v)
		if err != nil {
			return nil, err
		}
		return filterUnsafeItems(items), nil
	}
	if p.Binding.Placeholder != "" {
		return filterUnsafeItems([]Item{{URL: p.Binding.Placeholder}}), nil
	}
	return nil, nil // 隐藏组件
}

// filterUnsafeItems 过滤 URL 未过协议校验的绑定项（对齐 button 组件「降级不阻断编译」：
// 绑定值是 CMS 运行时数据，校验失败静默丢弃而非让整页编译失败）。
// URL 失败丢弃整项；Link 失败降级为空（图仍显示，仅失去点击链接）。
func filterUnsafeItems(items []Item) []Item {
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if it.URL == "" || !core.IsSafeURL(it.URL) {
			continue
		}
		if it.Link != "" && !core.IsSafeURL(it.Link) {
			it.Link = ""
		}
		out = append(out, it)
	}
	return out
}

// ApplyImageLoading 按主题「图片管理」默认解析加载三态（实现 core.ImageLoadingAware）。
// 返回 true 表示需要骨架屏 CSS，由渲染层统一输出（CSSBuckets 去重，多图不重复）。
func (v *View) ApplyImageLoading(d core.ImageDefaults) bool {
	// 优先级：单图设置 → 组件级设置 → 主题默认 → 内置默认（开启）。
	needSkeleton := false
	allEager := true
	for i := range v.Items {
		it := &v.Items[i]
		loading := it.Loading
		if loading == "" && v.FirstEager && i == 0 {
			// 轮播首图优先加载（默认开启）：首屏 LCP 图立即取，不等懒加载判定。
			// 显式的单图 loading 优先于该默认。
			loading = "off"
		}
		if loading == "" {
			loading = v.Loading
		}
		attrs := core.ResolveImageLoading(loading, d)
		it.IsEager, it.Skeleton = attrs.IsEager, attrs.Skeleton
		it.Class = core.ImageSkeletonClass("", attrs.Skeleton)
		fp := core.ResolveFetchPriority(it.FetchPriority)
		if it.FetchPriority == "" && v.IsCarousel {
			// 轮播首屏自动分级：第 1 张是 LCP 候选（high），其余延后（low）。
			// 显式设置（high/low）优先，不受此默认影响。
			if i == 0 {
				fp = core.ImageFetchPriority{High: true}
			} else {
				fp = core.ImageFetchPriority{Low: true}
			}
		}
		it.FetchHigh, it.FetchLow = fp.High, fp.Low
		if attrs.Skeleton {
			needSkeleton = true
		}
		if !attrs.IsEager {
			allEager = false
		}
	}
	// 组件级字段保留（全部图都立即加载才算 eager），供模板/测试的组件级判断使用。
	v.IsEager = allEager
	v.Skeleton = needSkeleton
	v.Class = core.ImageSkeletonClass("", needSkeleton)
	return needSkeleton
}

// ApplyI18n 按当前语言填充轮播箭头无障碍标签（实现 core.I18nAware）。
// text 为 nil 或未命中词条时使用包内中文兜底，保证属性永不为空。
func (v *View) ApplyI18n(text func(key, fallback string) string) {
	if v == nil {
		return
	}
	if text == nil {
		v.PrevLabel, v.NextLabel = textFallbackPrev, textFallbackNext
		v.SlideLabel = textFallbackSlideLabel
		v.fillDotLabels()
		return
	}
	v.PrevLabel = text(TextKeyPrev, textFallbackPrev)
	v.NextLabel = text(TextKeyNext, textFallbackNext)
	v.SlideLabel = text(TextKeySlideLabel, textFallbackSlideLabel)
	v.fillDotLabels()
}

// SlideLabel 圆点 aria-label 模板（含单个 %s；由 ApplyI18n 填充）。
// fillDotLabels 用已翻译模板 + 序号填充圆点标签。
func (v *View) fillDotLabels() {
	for i := range v.DotItems {
		v.DotItems[i].Label = strings.Replace(v.SlideLabel, "%s", strconv.Itoa(i+1), 1)
	}
}

// DeclareFeatures 实现 core.ViewFeatureDeclarer（审计 PERF-014）：图集要么走轮播增强
// （data-carousel，随 CarouselAttr 输出），要么每张图各自可点开灯箱（data-lightbox）。
// Visible=false（空图集且无占位）时模板整块不输出，这里也必须什么都不登记。
func (v View) DeclareFeatures() (attrs, classes []string) {
	if !v.Visible {
		return nil, nil
	}
	if v.CarouselAttr != "" {
		attrs = append(attrs, "data-carousel")
	}
	for _, item := range v.Items {
		if item.IsLightbox {
			attrs = append(attrs, "data-lightbox")
			break
		}
	}
	return attrs, nil
}

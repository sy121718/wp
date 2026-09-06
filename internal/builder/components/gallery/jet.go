// Package gallery — Jet 渲染路径辅助导出（Phase 2）。
//
// 与 render 函数并行的新路径：props 解码 / CSS 生成 / 数据源解析保留在 Go，
// HTML 拼装交给 gallery.jet 模板（grid/carousel 两模式 if/else + 单图项结构）。
// render 函数保持不变（旧输出），本文件只做最小导出与等价的数据准备。
package gallery

import (
	"fmt"

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
	// Items 每个单图项的渲染视图数据（URL/alt/图注/点击动作分支）。
	Items []ItemView
	// CarouselAttr 轮播增强属性 data-carousel='...'（carousel 模式，原样输出）。
	CarouselAttr string
	// Arrows 显示左右箭头（carousel 模式）。
	Arrows bool
	// Dots 显示圆点指示器（carousel 模式）。
	Dots bool
}

// ItemView 单个图集项的渲染视图数据（供 gallery.jet 模板使用）。
type ItemView struct {
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
}

// BuildView 生成图集渲染视图：数据源解析（绑定优先/静态兜底）+ 单图项视图准备
// + 轮播属性拼装（与 render 输出结构一致）。
func BuildView(p *Props, content core.ContentResolver) (View, error) {
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
	for _, r := range items {
		views = append(views, buildItemView(p, r))
	}

	v := View{Visible: true, IsCarousel: mode == LayoutCarousel, Items: views}
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

	iv := ItemView{URL: r.URL, Alt: r.Alt, Caption: r.Caption}
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
		return parseValues(v)
	}
	if p.Binding.Placeholder != "" {
		return []Item{{URL: p.Binding.Placeholder}}, nil
	}
	return nil, nil // 隐藏组件
}

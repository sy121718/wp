// Package gallery — Jet 渲染路径辅助导出（Phase 2）。
//
// 与 render 函数并行的新路径：props 解码 / CSS 生成 / 数据源解析与单图项 HTML
// 预计算保留在 Go，HTML 拼装交给 gallery.jet 模板（grid/carousel 两模式 if/else）。
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
	// Items 每个单图项的完整 HTML（renderItem 预计算，含 img/a/figure 包裹），原样输出。
	Items []string
	// CarouselAttr 轮播增强属性 data-carousel='...'（carousel 模式，原样输出）。
	CarouselAttr string
	// Arrows 显示左右箭头（carousel 模式）。
	Arrows bool
	// Dots 显示圆点指示器（carousel 模式）。
	Dots bool
}

// BuildView 生成图集渲染视图：数据源解析（绑定优先/静态兜底）+ 单图项 HTML 预计算
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

	htmls := make([]string, 0, len(items))
	for _, r := range items {
		htmls = append(htmls, renderItem(p, r))
	}

	v := View{Visible: true, IsCarousel: mode == LayoutCarousel, Items: htmls}
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

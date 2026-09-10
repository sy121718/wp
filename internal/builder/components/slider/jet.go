// Package slider — Jet 渲染路径辅助导出（Phase 2）。
//
// 与 Render 方法并行的新路径：props 解码 / CSS 生成 / 属性与 slide 轨道预计算
// 保留在 Go，HTML 拼装交给 slider.jet 模板（children 递归 include）。
package slider

import (
	"html"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

// CompileCSS 导出轮播样式编译（复用 Render 内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// View slider 渲染视图数据（供 slider.jet 模板使用）。
type View struct {
	// DataSlider data-slider 属性值（节点 ID，已转义）。
	DataSlider string
	// HasAutoplay 是否输出 data-autoplay 属性（Autoplay > 0）。
	HasAutoplay bool
	// Autoplay 自动播放间隔（秒，序列化字符串）。
	Autoplay string
	// Loop 循环开关（data-loop="1"）。
	Loop bool
	// ShowArrows 显示左右箭头。
	ShowArrows bool
	// ShowDots 显示圆点指示器。
	ShowDots bool
	// PrevLabel / NextLabel 箭头 aria-label（构建期按当前语言填充，多语言 P4）。
	PrevLabel string
	NextLabel string
	// SlideLabel 圆点 aria-label 模板（含单个 %s；兜底保留）。
	SlideLabel string
	// Dots 圆点导航（构建期生成 <a> 锚点：点击由浏览器原生滚动 + scroll-snap 对齐，
	// **零 JS 可用**；aria-label 已按当前语言 + 序号填好）。
	Dots []DotItem
}

// DotItem 圆点导航项。
type DotItem struct {
	// Anchor slide 元素 id（模板输出 href="#<Anchor>"）。
	Anchor string
	// Label 无障碍标签（构建期翻译 + 序号替换）。
	Label string
}

// 访客面组件文案 key：site.component.{type}.{prop}（docs/06-D §10.3）。
const (
	// TextKeyPrev 箭头「上一张」的词条 key。
	TextKeyPrev = "site.component.slider.prev"
	// TextKeyNext 箭头「下一张」的词条 key。
	TextKeyNext = "site.component.slider.next"
	// TextKeySlideLabel 圆点 aria-label 模板的词条 key（含单个 %s）。
	TextKeySlideLabel = "site.component.slider.slide_label"
)

// textFallbackPrev / textFallbackNext / textFallbackSlideLabel 缺词条时的原中文兜底（绝不输出空串）。
const (
	textFallbackPrev       = "上一张"
	textFallbackNext       = "下一张"
	textFallbackSlideLabel = "第 %s 张"
)

// ApplyI18n 按当前语言填充无障碍标签（实现 core.I18nAware），并同步填充圆点标签。
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

// fillDotLabels 用已翻译的模板 + 序号填充圆点 aria-label。
func (v *View) fillDotLabels() {
	for i := range v.Dots {
		v.Dots[i].Label = strings.Replace(v.SlideLabel, "%s", strconv.Itoa(i+1), 1)
	}
}

// BuildView 生成轮播渲染视图：data-slider/data-autoplay/data-loop 属性 + 圆点锚点预计算。
// children 的递归渲染由 nodeView 层驱动。
func BuildView(node *core.Node, p *Props) View {
	v := View{
		DataSlider: html.EscapeString(node.ID),
		Loop:       p.Loop,
		ShowArrows: p.ShowArrows,
		ShowDots:   p.ShowDots,
	}
	if p.Autoplay > 0 {
		v.HasAutoplay = true
		v.Autoplay = strconv.FormatFloat(p.Autoplay, 'f', -1, 64)
	}
	// 圆点锚点：构建期按 children 数生成（label 由 ApplyI18n 填充）。
	if p.ShowDots {
		v.Dots = make([]DotItem, 0, len(node.Children))
		for i := range node.Children {
			v.Dots = append(v.Dots, DotItem{
				Anchor: "sky-slide-" + node.ID + "-" + strconv.Itoa(i),
			})
		}
	}
	return v
}

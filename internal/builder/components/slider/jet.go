// Package slider — Jet 渲染路径辅助导出（Phase 2）。
//
// 与 Render 方法并行的新路径：props 解码 / CSS 生成 / 属性与 slide 轨道预计算
// 保留在 Go，HTML 拼装交给 slider.jet 模板（children 递归 include）。
// Render 方法保持不变（旧输出），本文件只做最小导出与等价的数据准备。
package slider

import (
	"html"
	"strconv"

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
	// SlideLabel 圆点按钮 aria-label 模板（含单个 %s，客户端增强按序号替换）。
	// 圆点由 wp-enhance.js 在访客浏览器里创建，故构建期只下发「已翻译的模板」，
	// 客户端不再硬编码中文（文案唯一真源仍是 sys_i18n）。
	SlideLabel string
}

// 访客面组件文案 key：site.component.{type}.{prop}（docs/06-D §10.3）。
const (
	// TextKeyPrev 箭头「上一张」的词条 key。
	TextKeyPrev = "site.component.slider.prev"
	// TextKeyNext 箭头「下一张」的词条 key。
	TextKeyNext = "site.component.slider.next"
	// TextKeySlideLabel 圆点按钮 aria-label 模板的词条 key（含单个 %s）。
	TextKeySlideLabel = "site.component.slider.slide_label"
)

// textFallbackPrev / textFallbackNext / textFallbackSlideLabel 缺词条时的原中文兜底（绝不输出空串）。
const (
	textFallbackPrev       = "上一张"
	textFallbackNext       = "下一张"
	textFallbackSlideLabel = "第 %s 张"
)

// ApplyI18n 按当前语言填充箭头无障碍标签（实现 core.I18nAware）。
// text 为 nil 或未命中词条时使用包内中文兜底，保证属性永不为空。
func (v *View) ApplyI18n(text func(key, fallback string) string) {
	if v == nil {
		return
	}
	if text == nil {
		v.PrevLabel, v.NextLabel = textFallbackPrev, textFallbackNext
		v.SlideLabel = textFallbackSlideLabel
		return
	}
	v.PrevLabel = text(TextKeyPrev, textFallbackPrev)
	v.NextLabel = text(TextKeyNext, textFallbackNext)
	v.SlideLabel = text(TextKeySlideLabel, textFallbackSlideLabel)
}

// BuildView 生成轮播渲染视图：data-slider/data-autoplay/data-loop 属性预计算
// （与 Render 输出结构一致）。children 的递归渲染由 nodeView 层驱动。
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
	return v
}

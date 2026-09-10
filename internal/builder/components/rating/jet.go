// Package rating — Jet 渲染路径辅助导出（Phase 1）。
//
// 与 render 函数并行的新路径：props 解码 / CSS 生成 / 星形填充状态列表预计算
// 保留在 Go，HTML 拼装交给 rating.jet 模板。render 函数保持不变（旧输出），
// 本文件只做最小导出与等价的数据准备。
package rating

import (
	"fmt"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

// CompileCSS 导出评分样式编译（复用 render 内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// 星形填充形态（供 rating.jet 模板选择 <svg> 骨架）。
const (
	starFormFull  = "full"  // 实心
	starFormHalf  = "half"  // 半星（左半填充）
	starFormEmpty = "empty" // 空星
)

// StarView 单颗星渲染视图（供 rating.jet 模板使用）。
type StarView struct {
	// Form 星形形态（full/half/empty），模板据此选择 <svg> 骨架。
	Form string
	// Points 星形 polygon 顶点（starPoints，模板填充 points 属性）。
	Points string
}

// View rating 渲染视图数据（供 rating.jet 模板使用）。
type View struct {
	// Stars 星形列表（长度 = effectiveMax）。
	Stars []StarView
	// Label 无障碍描述文本（role=img 的 aria-label；构建期按当前语言填充，多语言 P4）。
	Label string

	// value / max 原始数值：ApplyI18n 按语言重新格式化 Label 时使用（不参与模板输出）。
	value float64
	max   int
}

// TextKeyLabel 评分无障碍描述的词条 key（site.component.{type}.{prop}）。
// 词条值含两个 %s（评分值 / 满分），与 pkg/i18n.HasStringPlaceholdersOnly 约定一致。
const TextKeyLabel = "site.component.rating.label"

// textFallbackLabel 缺词条时的原中文兜底模板（绝不输出空串）。
const textFallbackLabel = "评分 %s / %s"

// BuildView 生成评分渲染视图：按 Value/Max 计算每颗星填充状态（实心/半星/空星）。
func BuildView(p *Props) View {
	max := effectiveMax(p)
	full := fullCount(p)
	half := hasHalf(p)

	stars := make([]StarView, 0, max)
	for i := range max {
		var form string
		switch {
		case i < full:
			form = starFormFull
		case i == full && half:
			form = starFormHalf
		default:
			form = starFormEmpty
		}
		stars = append(stars, StarView{Form: form, Points: starPoints})
	}
	return View{Stars: stars, Label: ratingLabel(p), value: p.Value, max: effectiveMax(p)}
}

// ApplyI18n 按当前语言重新格式化无障碍描述（实现 core.I18nAware）。
//
// 兜底：text 为 nil / 未命中词条 → 包内中文模板；词条缺 %s 占位符 → 原样输出；
// 占位符数量不足时只注入能对应的参数，绝不产生 %!s(MISSING) 之类的破损文本。
func (v *View) ApplyI18n(text func(key, fallback string) string) {
	if v == nil {
		return
	}
	tpl := textFallbackLabel
	if text != nil {
		tpl = text(TextKeyLabel, textFallbackLabel)
	}
	value := strconv.FormatFloat(v.value, 'f', -1, 64)
	max := strconv.Itoa(v.max)
	switch n := strings.Count(tpl, "%s"); {
	case n >= 2:
		v.Label = fmt.Sprintf(tpl, value, max)
	case n == 1:
		v.Label = fmt.Sprintf(tpl, value)
	default:
		v.Label = tpl
	}
}

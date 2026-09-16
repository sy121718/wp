// Package counter — Jet 渲染路径辅助导出（Phase 1）。
//
// 与 Render 方法并行的新路径：props 解码 / CSS 生成 / 数字格式化与增强属性
// 保留在 Go，HTML 拼装交给 counter.jet 模板。Render 方法保持不变（旧输出），
// 本文件只做最小导出与等价的数据准备。
package counter

import (
	"strconv"

	"go_wp/internal/builder/core"
)

// CompileCSS 导出计数器样式编译（复用 Render 内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// View counter 渲染视图数据（供 counter.jet 模板使用）。
type View struct {
	// StartData / EndData / DecimalsData / DurationData 增强 data 属性值（数字串）。
	StartData    string
	EndData      string
	DecimalsData string
	DurationData string
	// CSSMode 零 JS 计数模式（小数位为 0；数值由 @property + counter() 生成，
	// 模板不输出 data-* 增强属性，不激活内嵌脚本）。
	CSSMode bool
	// Value 无 JS 时的结束值（最终态；CSSMode 时为空，数值由 ::after 生成）。
	Value string
	// Prefix / Suffix / Label 前缀/后缀/标签（模板输出时由 Jet 默认转义）。
	Prefix string
	Suffix string
	Label  string
}

// BuildView 生成计数器渲染视图：数字格式化 + 增强 data 属性。
func BuildView(p *Props) View {
	duration := p.Duration
	if duration <= 0 {
		duration = 2
	}
	cssMode := p.Decimals == 0
	value := formatNum(p.End, p.Decimals)
	if cssMode {
		value = "" // 数值由 .sky-counter-value::after 生成（零 JS）
	}
	return View{
		CSSMode:      cssMode,
		StartData:    strconv.FormatFloat(p.Start, 'f', -1, 64),
		EndData:      strconv.FormatFloat(p.End, 'f', -1, 64),
		DecimalsData: strconv.Itoa(p.Decimals),
		DurationData: strconv.FormatFloat(duration, 'f', -1, 64),
		Value:        value,
		Prefix:       p.Prefix,
		Suffix:       p.Suffix,
		Label:        p.Label,
	}
}

// DeclareFeatures 实现 core.ViewFeatureDeclarer（审计 PERF-014）：计数器只在**非 CSS 模式**
// 输出增强属性（counter.jet 的 {{ if not .V.CSSMode }} 分支）—— CSS 模式用 @property + counter()
// 生成数值，没有脚本参与，登记了反而会白白注入一份增强块。
func (v View) DeclareFeatures() (attrs, classes []string) {
	if v.CSSMode {
		return nil, nil
	}
	return []string{"data-counter", "data-start", "data-end", "data-decimals", "data-duration"}, nil
}

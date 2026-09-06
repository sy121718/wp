// Package rating 实现 core.rating 评分组件（对标 GrapesJS rating 组件生态）。
//
// 轻量数据展示原子组件：星形评分，前 floor(Value) 颗实心、小数部分半星（左半填充）、
// 其余空星，零客户端 JS。validateExtra 强约束 value ∈ [0, max]、max ≤ 10（缺省 5）。
// BuildView 预计算每颗星的 SVG 形态（full/half/empty），模板循环原样输出。
package rating

import (
	"fmt"
	"math"
	"strconv"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.rating"

// defaultMax 缺省星数上限。
const defaultMax = 5

// maxLimit 星数上限（超过则视为结构失控）。
const maxLimit = 10

// starPoints 星形 polygon 顶点（24 viewBox，fill 版本，与 core.IconSVG 的 star 对齐）。
const starPoints = "12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"

// Props rating 属性。
type Props struct {
	// Value 评分值（1~Max，可小数）。
	Value float64 `json:"value,omitempty"`
	// Max 星数上限（默认 5，最大 10）。
	Max int `json:"max,omitempty"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced"`
}

// Widget 基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		TypeName:      Type,
		ValidateExtra: validateExtra,
	},
}

// validateExtra 关系性校验：值非负且不超过上限、上限不超过 10。
func validateExtra(p *Props, nodeID string) (err error) {
	if p.Max < 0 {
		return fmt.Errorf("评分星数不能为负: %d", p.Max)
	}
	if p.Value < 0 {
		return fmt.Errorf("评分值不能为负: %v", p.Value)
	}
	max := effectiveMax(p)
	if max > maxLimit {
		return fmt.Errorf("评分星数不能超过 %d: %d", maxLimit, max)
	}
	if p.Value > float64(max) {
		return fmt.Errorf("评分值 %v 超过上限 %d", p.Value, max)
	}
	return nil
}

// effectiveMax 有效星数（0 视为缺省 5）。
func effectiveMax(p *Props) int {
	if p.Max <= 0 {
		return defaultMax
	}
	return p.Max
}

// fullCount 实心星数（floor）。
func fullCount(p *Props) int {
	n := int(math.Floor(p.Value))
	if n < 0 {
		n = 0
	}
	if m := effectiveMax(p); n > m {
		n = m
	}
	return n
}

// hasHalf 是否存在半星（存在小数部分且未填满上限）。
func hasHalf(p *Props) bool {
	frac := p.Value - math.Floor(p.Value)
	return frac > 0 && fullCount(p) < effectiveMax(p)
}

// compileCSS 评分容器/星形尺寸/配色样式。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	b.Add(core.BreakpointDesktop, sel, []string{
		"display: inline-flex",
		"align-items: center",
		"gap: 2px",
		"color: #f59e0b",
		"line-height: 0",
	})
	b.Add(core.BreakpointDesktop, sel+" .wp-star", []string{
		"display: inline-flex",
		"width: 1.25em",
		"height: 1.25em",
	})
	b.Add(core.BreakpointDesktop, sel+" .wp-star svg", []string{
		"width: 100%",
		"height: 100%",
	})
}

// init 注册评分组件。
func init() {
	core.Register(Widget)
}

// starFull / starEmpty / starHalf 三种星形 SVG 形态（fill 实心 / stroke 空星 / 左半填充）。
var (
	starFull  = `<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><polygon points="` + starPoints + `"/></svg>`
	starEmpty = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" aria-hidden="true"><polygon points="` + starPoints + `"/></svg>`
	// starHalf 空星轮廓 + clipPath 裁剪的左半实心星。
	starHalf = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" aria-hidden="true"><defs><clipPath id="wp-star-half"><rect x="0" y="0" width="12" height="24"/></clipPath></defs><polygon points="` + starPoints + `" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"/><polygon points="` + starPoints + `" fill="currentColor" clip-path="url(#wp-star-half)"/></svg>`
)

// ratingLabel 无障碍描述文本（如「评分 4.5 / 5」）。
func ratingLabel(p *Props) string {
	return "评分 " + strconv.FormatFloat(p.Value, 'f', -1, 64) + " / " + strconv.Itoa(effectiveMax(p))
}

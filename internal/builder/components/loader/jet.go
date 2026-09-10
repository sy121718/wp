// Package loader — Jet 渲染路径辅助导出（与其他 Atom 基座组件同构）：
// props 解码 / CSS 生成 / 形态视图预计算保留在 Go，HTML 拼装交给 loader.jet 模板。
package loader

import (
	"go_wp/internal/builder/core"
)

// CompileCSS 导出加载器样式编译（atomViewOf 接线用）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// View 加载器渲染视图数据（供 loader.jet 模板使用）。
// 结构由 shapes 形态表派生，不是「每形态一个布尔」：新增形态只改表一处，
// 模板与 View 字段都不再增长，也不可能出现「模板分支漏写某形态」的分叉。
type View struct {
	// Self 根下元素类名后缀（模板输出 class="wp-loader-{{ .V.Self }}"）。
	// 单元素形态（ring/pulse/plane/orbit）= 该元素本身；
	// 多点形态（dot/bar/bounce）= 每个平铺子元素的类名；
	// 容器形态（grid/wave）= 包裹子元素的容器类名。
	Self string
	// Wrap 子元素是否包一层容器：grid/wave=true；dot/bar/bounce=false
	// （直接平铺在根下，让根容器的 flex gap 继续作用于点与点之间）。
	Wrap bool
	// Items 子元素计数（模板 range 输出）；nil = 无子元素（单元素形态）。
	Items []int
	// Label 可见文本（空则仅 aria-label，不产出空标签）。
	Label string
	// AriaText 无障碍文本。
	AriaText string
}

// shapeSpec 单一形态的 DOM 结构描述。
type shapeSpec struct {
	// variant 形态取值（Props.Variant 的合法值）。
	variant string
	// self 根下元素类名后缀，必须与 compileCSS 的选择器后缀一致。
	self string
	// wrap 子元素是否包一层容器。
	wrap bool
	// items 子元素个数（0 = 单元素形态）。
	items int
}

// shapes 形态结构表（顺序 = 后台下拉顺序 = 测试遍历顺序）。
// 单一真源：validateExtra 白名单、BuildView 结构、compileCSS 选择器后缀全部对齐到这里，
// 三者不再各写一份形态清单（分叉会产出「模板输出没有样式的类名 / 样式指向不存在的元素」）。
var shapes = []shapeSpec{
	{variant: VariantSpinner, self: "ring"},
	{variant: VariantDots, self: "dot", items: 3},
	{variant: VariantBars, self: "bar", items: 4},
	{variant: VariantPulse, self: "pulse"},
	{variant: VariantPlane, self: "plane"},
	{variant: VariantGrid, self: "grid", wrap: true, items: 9},
	{variant: VariantOrbit, self: "orbit"},
	{variant: VariantWave, self: "wave", wrap: true, items: 5},
	{variant: VariantBounce, self: "bounce", items: 3},
}

// shapeByVariant 形态索引（构建期派生；重名直接 panic，对齐 core 关键帧索引的防御风格）。
var shapeByVariant = buildShapeIndex(shapes)

// buildShapeIndex 建立形态索引。
func buildShapeIndex(list []shapeSpec) map[string]shapeSpec {
	m := make(map[string]shapeSpec, len(list))
	for _, s := range list {
		if _, dup := m[s.variant]; dup {
			panic("loader: 重复的形态: " + s.variant)
		}
		m[s.variant] = s
	}
	return m
}

// BuildView 生成加载器渲染视图（形态 → DOM 结构，全部来自 shapes 表）。
func BuildView(p *Props) View {
	v := View{AriaText: "加载中", Label: p.Label}
	spec := shapeByVariant[effectiveVariant(p)]
	v.Self, v.Wrap = spec.self, spec.wrap
	if spec.items > 0 {
		v.Items = make([]int, spec.items)
	}
	return v
}

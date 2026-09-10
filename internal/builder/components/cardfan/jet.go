// Package cardfan — Jet 渲染路径辅助导出（与其他 Atom 基座组件同构）：
// props 解码 / CSS 生成保留在 Go，HTML 拼装交给 cardfan.jet 模板。
package cardfan

import (
	"strconv"

	"go_wp/internal/builder/core"
)

// CompileCSS 导出扇形卡片墙样式编译（atomViewOf 接线用）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// CardView 单张卡片的渲染视图。
type CardView struct {
	// Label 卡片数字（1~N）。
	Label string
}

// View 扇形卡片墙渲染视图（供 cardfan.jet 模板使用）。
type View struct {
	// Cards 卡片列表（长度 = 有效数量）。
	Cards []CardView
}

// BuildView 生成扇形卡片墙渲染视图（1~N 数字卡片）。
func BuildView(p *Props) View {
	count := effectiveCount(p)
	cards := make([]CardView, count)
	for i := 0; i < count; i++ {
		cards[i] = CardView{Label: strconv.Itoa(i + 1)}
	}
	return View{Cards: cards}
}

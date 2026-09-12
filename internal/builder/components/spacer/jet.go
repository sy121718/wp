// Package spacer — Jet 渲染路径辅助导出（Phase 1）。
//
// 与 render 函数并行的新路径：props 解码 / CSS 生成保留在 Go，
// HTML 拼装交给 spacer.jet 模板（仅单层 div，无额外预计算）。
// render 函数保持不变（旧输出），本文件只做最小导出与等价的数据准备。
package spacer

import (
	_ "embed" // spacer.css 经 //go:embed 打进二进制
	"fmt"

	"go_wp/internal/builder/core"
)

// spacerCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组
// （有补全 / lint / 格式化），而作用域替换、桶划分、确定性输出仍由构建期负责。
//
//go:embed spacer.css
var spacerCSS string

// CompileCSS 导出间隔组件样式编译（三端高度；某端为空则该端不产出声明）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	vars := map[string]string{
		"h_desktop": p.Height.Desktop,
		"h_tablet":  p.Height.Tablet,
		"h_mobile":  p.Height.Mobile,
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, spacerCSS, vars); err != nil {
		// 样式源解析失败属于构建期缺陷，必须在测试/构建时暴露；静默跳过的后果是产物悄悄少了样式。
		panic(fmt.Sprintf("spacer 组件样式解析失败: %v", err))
	}
}

// View spacer 渲染视图数据（空——模板仅依赖 nodeView 通用字段 Classes/CustomID）。
type View struct{}

// BuildView 生成 spacer 渲染视图（无内容预计算）。
func BuildView(p *Props) View {
	return View{}
}

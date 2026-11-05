package jet

import (
	"io"
	"strings"

	"github.com/CloudyKit/jet/v6"

	"go_wp/pkg/view/ui/icons"
)

// IconPrinter 是模板里的受控图标入口（Jet 全局 icon）。
//
// 与 Jet 内置 safeHtml / raw 同一形态：Jet 先把调用参数打印成字节交给它，
// 因此模板写成 {{ icon("mail") }}，参数只可能是名字，不会是 SVG 绕过检查。
//
// 只接受公共图标目录里的名称并输出目录中登记的内联 SVG：
// 路径分隔符、点号穿越、未知名称一律输出空，fallback 由公共模板显式选择
// 目录中的固定图标。业务数据不需要也不允许自带 SVG。
func IconPrinter(w io.Writer, b []byte) {
	name := strings.TrimSpace(string(b))
	if name == "" || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return
	}
	svg, ok := icons.IconSVG(name)
	if !ok {
		return
	}
	_, _ = io.WriteString(w, svg)
}

// iconGlobal 是注入 Jet 的 SafeWriter 形态。
func iconGlobal() jet.SafeWriter {
	return jet.SafeWriter(IconPrinter)
}

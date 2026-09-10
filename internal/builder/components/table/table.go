// Package table 实现 core.table 表格组件（对标 GrapesJS Table 组件生态）。
// 基座 core.Atom 吸收公共样板；本文件为业务本体：
// 表头（Headers）+ 二维数据行（Rows）+ 可选标题（Caption），
// 斑马纹（Striped）与边框（Bordered）由编译期 CSS 生成，零客户端 JS。
package table

import (
	"fmt"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.table"

// 常量上限。
const (
	maxCols    = 20  // 最大列数
	maxRows    = 200 // 最大行数
	maxCellLen = 500 // 单元格文本长度上限
)

// Props core.table 表格属性。
type Props struct {
	// Caption 表格标题（非空时输出 <caption>）。
	Caption string `json:"caption,omitempty" ct:"text,maxlen=100,sec=content,label=表格标题"`
	// Headers 表头列（非空时输出 <thead>）。
	Headers []string `json:"headers,omitempty"`
	// Rows 数据行（二维切片；行内列数与 Headers 一致）。
	Rows [][]string `json:"rows,omitempty"`
	// Striped 斑马纹（tbody 偶数行浅色背景）。
	Striped bool `json:"striped,omitempty" ct:"bool,sec=content,label=斑马纹"`
	// RowHover 行悬停高亮（触屏治理：仅真悬浮设备生效，core.AddHover）。
	RowHover bool `json:"rowHover,omitempty" ct:"bool,sec=content,label=行悬停高亮"`
	// Bordered 边框（th/td 加 1px 边框）。
	Bordered bool `json:"bordered,omitempty" ct:"bool,sec=content,label=显示边框"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Widget 基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		TypeName:      Type,
		ValidateExtra: validateExtra,
		// Translatable 可翻译字段白名单（多语言 P5b，docs/06-D §7.5 决策 F6）：
		// 只有这里列出的字段参与内容翻译，未声明字段永不翻译。
		Translatable: []string{"caption", "headers", "rows"},
	},
}

// validateExtra 关系性校验：列/行数量上限、行列一致、单元格文本长度。
// Headers / Rows 为数组字段（无 ct tag），边界约束在此统一校验。
func validateExtra(p *Props, nodeID string) (err error) {
	if len(p.Headers) > maxCols {
		return fmt.Errorf("表头列数超上限（%d）: %d", maxCols, len(p.Headers))
	}
	if len(p.Rows) > maxRows {
		return fmt.Errorf("数据行数超上限（%d）: %d", maxRows, len(p.Rows))
	}
	for i, h := range p.Headers {
		if h == "" {
			return fmt.Errorf("第 %d 个表头为空", i+1)
		}
		if len(h) > maxCellLen {
			return fmt.Errorf("第 %d 个表头超长（上限 %d）", i+1, maxCellLen)
		}
	}
	for ri, row := range p.Rows {
		if len(row) > maxCols {
			return fmt.Errorf("第 %d 行列数超上限（%d）: %d", ri+1, maxCols, len(row))
		}
		if len(p.Headers) > 0 && len(row) != len(p.Headers) {
			return fmt.Errorf("第 %d 行列数（%d）与表头列数（%d）不一致", ri+1, len(row), len(p.Headers))
		}
		for ci, cell := range row {
			if len(cell) > maxCellLen {
				return fmt.Errorf("第 %d 行第 %d 列文本超长（上限 %d）", ri+1, ci+1, maxCellLen)
			}
		}
	}
	return nil
}

// compileCSS 表格样式：基础布局 + 斑马纹 + 边框。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	// 表格基础布局。
	b.Add(core.BreakpointDesktop, sel, []string{
		"width: 100%",
		"border-collapse: collapse",
	})
	// 标题样式。
	b.Add(core.BreakpointDesktop, sel+" caption", []string{
		"caption-side: top",
		"text-align: left",
		"padding: 8px 0",
		"font-weight: 600",
	})
	// 单元格基础内边距与对齐。
	b.Add(core.BreakpointDesktop, sel+" th, "+sel+" td", []string{
		"padding: 10px 12px",
		"text-align: left",
	})
	// 表头底纹与分隔线。
	b.Add(core.BreakpointDesktop, sel+" thead th", []string{
		"font-weight: 600",
		"background: var(--sky-c-surface, #f5f6f8)",
		"border-bottom: 2px solid rgba(0,0,0,0.12)",
	})
	// 斑马纹：tbody 偶数行浅色背景。
	if p.Striped {
		b.Add(core.BreakpointDesktop, sel+" tbody tr:nth-child(even)", []string{
			"background: rgba(0,0,0,0.04)",
		})
	}
	// 行悬停高亮（H5 触屏治理：AddHover 包 @media hover:hover，触屏不粘滞）。
	if p.RowHover {
		b.Add(core.BreakpointDesktop, sel+" tbody tr", []string{
			"transition: background 0.15s ease",
		})
		b.AddHover(sel+" tbody tr:hover", []string{
			"background: rgba(0,0,0,0.05)",
		})
	}
	// 边框：th/td 加 1px 边框。
	if p.Bordered {
		b.Add(core.BreakpointDesktop, sel+" th, "+sel+" td", []string{
			"border: 1px solid rgba(0,0,0,0.12)",
		})
	}
}

// init 注册表格组件。
func init() {
	core.Register(Widget)
}

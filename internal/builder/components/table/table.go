// Package table 实现 core.table 表格组件（对标 GrapesJS Table 组件生态）。
// 基座 core.Atom 吸收公共样板；本文件为业务本体：
// 表头（Headers）+ 二维数据行（Rows）+ 可选标题（Caption），
// 斑马纹（Striped）与边框（Bordered）由编译期 CSS 生成，零客户端 JS。
package table

import (
	_ "embed" // table.css 经 //go:embed 打进二进制
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
		DisplayName:     "表格",
		Hint:            "数据表格",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"caption": "数据表格",
			"headers": []any{
				"列一",
				"列二",
			},
			"rows": []any{
				[]any{
					"A",
					"B",
				},
				[]any{
					"C",
					"D",
				},
			},
			"striped":  true,
			"bordered": true,
		},
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

// tableCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组。
//
//go:embed table.css
var tableCSS string

// compileCSS 表格样式：基础布局 + 斑马纹 + 边框。
//
// 三个开关是「整条规则存在与否」，所以走样式源的规则级 @if —— Go 侧只翻译布尔值。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	vars := map[string]string{
		"striped":  core.BoolVar(p.Striped),
		"rowHover": core.BoolVar(p.RowHover),
		"bordered": core.BoolVar(p.Bordered),
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, tableCSS, vars); err != nil {
		panic(fmt.Sprintf("table 组件样式解析失败: %v", err))
	}
}

// init 注册表格组件。
func init() {
	core.Register(Widget)
	core.RegisterTemplate("table", tableTemplate)
}

// tableTemplate 组件模板。与 .go / .css 同目录：改结构不必去 internal/templates/components/ 找
// （注册后由 loader 优先采用，见 core.RegisterTemplate）。
//
//go:embed table.jet
var tableTemplate string

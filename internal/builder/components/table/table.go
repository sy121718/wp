package table

import (
	_ "embed"
	"fmt"

	"go_wp/internal/builder/core"
)

const Type = "core.table"

const (
	maxCols    = 20
	maxRows    = 200
	maxCellLen = 500
)

type Props struct {
	Caption string `json:"caption,omitempty" ct:"text,maxlen=100,sec=content,label=表格标题"`

	Headers []string `json:"headers,omitempty"`

	Rows [][]string `json:"rows,omitempty"`

	Striped bool `json:"striped,omitempty" ct:"bool,sec=content,label=斑马纹"`

	RowHover bool `json:"rowHover,omitempty" ct:"bool,sec=content,label=行悬停高亮"`

	Bordered bool `json:"bordered,omitempty" ct:"bool,sec=content,label=显示边框"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

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

		Translatable: []string{"caption", "headers", "rows"},
	},
}

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

//go:embed table.css
var tableCSS string

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

func init() {
	core.Register(Widget)
	core.RegisterTemplate("table", tableTemplate)
}

//go:embed table.jet
var tableTemplate string

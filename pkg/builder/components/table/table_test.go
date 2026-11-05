package table

import (
	"fmt"
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
)

func repeatHeaders(n int) []string {
	hs := make([]string, n)
	for i := range hs {
		hs[i] = fmt.Sprintf("列%d", i+1)
	}
	return hs
}

func repeatRows(n, cols int) [][]string {
	rows := make([][]string, n)
	for i := range rows {
		row := make([]string, cols)
		for j := range row {
			row[j] = fmt.Sprintf("r%dc%d", i+1, j+1)
		}
		rows[i] = row
	}
	return rows
}

func TestValidateExtra(t *testing.T) {
	tests := []struct {
		name    string
		props   *Props
		wantErr bool
	}{
		{"空 props 合法", &Props{}, false},
		{"表头合法", &Props{Headers: repeatHeaders(2)}, false},
		{"数据行合法", &Props{Headers: repeatHeaders(2), Rows: repeatRows(2, 2)}, false},
		{"表头列数超上限", &Props{Headers: repeatHeaders(maxCols + 1)}, true},
		{"数据行数超上限", &Props{Rows: repeatRows(maxRows+1, 1)}, true},
		{"行内列数超上限", &Props{Rows: repeatRows(1, maxCols+1)}, true},
		{"行数列数与表头不一致", &Props{Headers: repeatHeaders(2), Rows: repeatRows(1, 1)}, true},
		{"空表头", &Props{Headers: []string{"a", ""}}, true},
		{"单元格超长", &Props{Headers: repeatHeaders(1), Rows: [][]string{{strings.Repeat("x", maxCellLen+1)}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExtra(tt.props, "n1")
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateExtra(%+v) err=%v, wantErr=%v", tt.props, err, tt.wantErr)
			}
		})
	}
}

func TestCompileCSS(t *testing.T) {
	tests := []struct {
		name  string
		props *Props
		wants []string
		not   []string
	}{
		{
			name:  "基础布局",
			props: &Props{},
			wants: []string{"width: 100%", "border-collapse: collapse", " caption", "thead th"},
			not:   []string{"nth-child(even)", "border: 1px solid"},
		},
		{
			name:  "斑马纹",
			props: &Props{Striped: true},
			wants: []string{"tbody tr:nth-child(even)", "background: rgba(0,0,0,0.04)"},
		},
		{
			name:  "边框",
			props: &Props{Bordered: true},
			wants: []string{"border: 1px solid var(--sky-c-border-strong, #d1d5db)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &core.CSSBuckets{}
			compileCSS("n1", tt.props, b)
			css := b.String()
			for _, want := range tt.wants {
				if !strings.Contains(css, want) {
					t.Errorf("CSS 缺少 %q\n%s", want, css)
				}
			}
			for _, n := range tt.not {
				if strings.Contains(css, n) {
					t.Errorf("CSS 不应包含 %q\n%s", n, css)
				}
			}
		})
	}
}

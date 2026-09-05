package divider

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestValidateExtra 分割线校验：CSS 安全值、嵌入配置。
func TestValidateExtra(t *testing.T) {
	inset := func(kind string) Inset { return Inset{Kind: kind} }
	tests := []struct {
		name    string
		props   *Props
		wantErr bool
	}{
		{"空 props 合法", &Props{}, false},
		{"线粗合法", &Props{Weight: "2px"}, false},
		{"线粗非注入宽松放行", &Props{Weight: "abc"}, false},
		{"线粗注入非法", &Props{Weight: "1px;background:red"}, true},
		{"颜色合法", &Props{Color: "#333"}, false},
		{"颜色非法", &Props{Color: "red;"}, true},
		{"宽度合法", &Props{Width: Width{Desktop: "50%"}}, false},
		{"宽度非注入宽松放行", &Props{Width: Width{Desktop: "110%"}}, false},
		{"宽度注入非法", &Props{Width: Width{Desktop: "50%;"}}, true},
		{"text 嵌入缺文案", &Props{Inset: inset(InsetText)}, true},
		{"text 嵌入有文案", &Props{Inset: Inset{Kind: InsetText, Text: "或"}}, false},
		{"icon 嵌入合法", &Props{Inset: Inset{Kind: InsetIcon, IconName: "star"}}, false},
		{"icon 嵌入缺图标", &Props{Inset: inset(InsetIcon)}, true},
		{"icon 嵌入非法图标", &Props{Inset: Inset{Kind: InsetIcon, IconName: "x"}}, true},
		{"嵌入间距非法", &Props{Inset: Inset{Kind: InsetText, Text: "或", Spacing: "10px;"}}, true},
		{"嵌入字号合法", &Props{Inset: Inset{Kind: InsetText, Text: "或", FontSize: "14px"}}, false},
		{"嵌入字号注入非法", &Props{Inset: Inset{Kind: InsetText, Text: "或", FontSize: "1em;"}}, true},
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

// TestCompileCSS 样式编译：线型缺省、双线最小线宽、嵌入 flex 比例、宽度对齐。
func TestCompileCSS(t *testing.T) {
	tests := []struct {
		name  string
		props *Props
		wants []string
		not   []string
	}{
		{
			name:  "纯线缺省 solid 1px currentColor",
			props: &Props{},
			wants: []string{"border-top: 1px solid currentColor", "margin: 0"},
		},
		{
			name:  "dashed 线型",
			props: &Props{Style: LineDashed},
			wants: []string{"border-top: 1px dashed currentColor"},
		},
		{
			name:  "double 缺省权重提升到 3px",
			props: &Props{Style: LineDouble},
			wants: []string{"border-top: 3px double currentColor"},
		},
		{
			name:  "自定义颜色与权重",
			props: &Props{Weight: "2px", Color: "#f00"},
			wants: []string{"border-top: 2px solid #f00"},
		},
		{
			name:  "text 嵌入居中 flex 等分",
			props: &Props{Inset: Inset{Kind: InsetText, Text: "或"}},
			wants: []string{"display: flex", "flex: 1", ".dt-inset"},
		},
		{
			name:  "text 嵌入靠左 flex 0.5/1.5",
			props: &Props{Inset: Inset{Kind: InsetText, Text: "或", Position: PosLeft}},
			wants: []string{"flex: 0.5", "flex: 1.5"},
		},
		{
			name:  "宽度 50% 左对齐",
			props: &Props{Width: Width{Desktop: "50%"}, Align: "left"},
			wants: []string{"width: 50%", "margin-left: 0", "margin-right: auto"},
		},
		{
			name:  "宽度右对齐",
			props: &Props{Width: Width{Desktop: "50%"}, Align: "right"},
			wants: []string{"margin-left: auto", "margin-right: 0"},
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

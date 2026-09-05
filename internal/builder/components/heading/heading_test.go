package heading

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestResolveWeight 字重解析：token / 数值 / 非法输入。
func TestResolveWeight(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"空", "", "", false},
		{"regular token", "regular", "400", false},
		{"medium token", "medium", "500", false},
		{"semibold token", "semibold", "600", false},
		{"bold token", "bold", "700", false},
		{"token 大小写不敏感", "BOLD", "700", false},
		{"数值 100", "100", "100", false},
		{"数值 400", "400", "400", false},
		{"数值 900", "900", "900", false},
		{"数值 50 过小", "50", "", true},
		{"数值 950 过大", "950", "", true},
		{"数值 123 非百步长", "123", "", true},
		{"非数字", "abc", "", true},
		{"小数", "700.0", "", true},
		{"负数", "-100", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveWeight(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("resolveWeight(%q) err=%v, wantErr=%v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("resolveWeight(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestValidateExtra 关系性校验：文本/绑定二选一、字段路径、装饰、字重。
func TestValidateExtra(t *testing.T) {
	binding := func(field string) *Binding {
		return &Binding{Field: field}
	}
	tests := []struct {
		name    string
		props   *Props
		wantErr bool
	}{
		{"空 props 报错", &Props{}, true},
		{"仅静态文本", &Props{Text: "你好"}, false},
		{"仅绑定", &Props{Binding: binding("post.title")}, false},
		{"文本+绑定并存", &Props{Text: "你好", Binding: binding("post.title")}, false},
		{"绑定首字母大写非法", &Props{Binding: binding("Post.title")}, true},
		{"绑定数字段非法", &Props{Binding: binding("post.123")}, true},
		{"绑定多段非法", &Props{Binding: binding("post.title.sub")}, true},
		{"绑定空字段名非法", &Props{Binding: binding("post.")}, true},
		{"绑定下划线合法", &Props{Binding: binding("post_1.title_2")}, false},
		{"装饰 underline 合法", &Props{Text: "x", Decor: DecorProps{Decoration: "underline"}}, false},
		{"装饰 line-through 合法", &Props{Text: "x", Decor: DecorProps{Decoration: "line-through"}}, false},
		{"装饰非法", &Props{Text: "x", Decor: DecorProps{Decoration: "strike"}}, true},
		{"字重 token 合法", &Props{Text: "x", Weight: "bold"}, false},
		{"字重非法", &Props{Text: "x", Weight: "50"}, true},
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

// cssBlock 提取 css 中指定选择器（selector {）的声明块内容，用于块级断言。
func cssBlock(css, selector string) string {
	i := strings.Index(css, selector+" {")
	if i < 0 {
		return ""
	}
	j := strings.Index(css[i:], "}")
	if j < 0 {
		return css[i:]
	}
	return css[i : i+j]
}

// TestCompileCSS 样式编译：字重/装饰/对齐/截断/阴影。
func TestCompileCSS(t *testing.T) {
	b := &core.CSSBuckets{}
	compileCSS("n1", &Props{
		Text:         "标题",
		Weight:       "bold",
		Color:        "#ff0000",
		Align:        Align{Desktop: "center"},
		TextShadow:   "subtle",
		LineClamp:    2,
		Subtitle:     "副标题",
		SubtitleColor: "#888",
	}, b)
	css := b.String()

	for _, want := range []string{
		"font-weight: 700",
		"color: #ff0000",
		"text-align: center",
		"text-shadow: 0 1px 2px rgba(0,0,0,0.45)",
		"-webkit-line-clamp: 2",
		".wp-heading-sub",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("CSS 缺少 %q\n%s", want, css)
		}
	}
	// Transform 为空时主块不得输出 text-transform（副标题固定 uppercase 属特性）。
	if strings.Contains(cssBlock(css, ".wp-c-n1"), "text-transform") {
		t.Errorf("Transform 为空不应输出 text-transform\n%s", css)
	}
}

// TestCompileCSSInvalidWeight 非法字重不输出声明（编译期容错）。
func TestCompileCSSInvalidWeight(t *testing.T) {
	b := &core.CSSBuckets{}
	compileCSS("n1", &Props{Text: "x", Weight: "oops"}, b)
	css := b.String()
	// 副标题样式固定含 font-weight: 500，只检查主选择器块。
	if strings.Contains(cssBlock(css, ".wp-c-n1"), "font-weight") {
		t.Errorf("非法字重不应输出 font-weight 声明\n%s", css)
	}
}

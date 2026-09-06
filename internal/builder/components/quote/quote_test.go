package quote

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestValidateExtra 引用校验：内容非空、出处链接协议。
func TestValidateExtra(t *testing.T) {
	tests := []struct {
		name    string
		props   *Props
		wantErr bool
	}{
		{"空 props 非法", &Props{}, true},
		{"有内容合法", &Props{Text: "引用"}, false},
		{"作者合法", &Props{Text: "引用", Author: "张三"}, false},
		{"出处 http 合法", &Props{Text: "引用", Author: "张三", Source: "https://example.com"}, false},
		{"出处相对路径合法", &Props{Text: "引用", Source: "/about"}, false},
		{"出处 javascript 非法", &Props{Text: "引用", Source: "javascript:alert(1)"}, true},
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

// TestCompileCSS 引用样式编译：左边框 + 对齐 + cite。
func TestCompileCSS(t *testing.T) {
	tests := []struct {
		name  string
		props *Props
		wants []string
		not   []string
	}{
		{
			name:  "默认左对齐",
			props: &Props{Text: "引用"},
			wants: []string{"border-left: 4px solid", "font-style: italic", " p", " cite"},
			not:   []string{"text-align: center"},
		},
		{
			name:  "居中",
			props: &Props{Text: "引用", Align: AlignCenter},
			wants: []string{"text-align: center"},
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

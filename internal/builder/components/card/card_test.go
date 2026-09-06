package card

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestValidateExtra 卡片校验：按钮链接协议白名单。
func TestValidateExtra(t *testing.T) {
	tests := []struct {
		name    string
		props   *Props
		wantErr bool
	}{
		{"空 props 合法", &Props{}, false},
		{"http 链接合法", &Props{ButtonLink: "https://example.com/x"}, false},
		{"相对路径合法", &Props{ButtonLink: "/about"}, false},
		{"锚点合法", &Props{ButtonLink: "#sec"}, false},
		{"javascript 协议非法", &Props{ButtonLink: "javascript:alert(1)"}, true},
		{"data 协议非法", &Props{ButtonLink: "data:text/html,x"}, true},
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

// TestCompileCSS 卡片样式编译：布局与子元素选择器。
func TestCompileCSS(t *testing.T) {
	b := &core.CSSBuckets{}
	compileCSS("n1", &Props{}, b)
	css := b.String()
	for _, want := range []string{
		"display: flex", "flex-direction: column", "border-radius: 12px",
		" img", " h3", " p", "a.wp-card-btn",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("CSS 缺少 %q\n%s", want, css)
		}
	}
}

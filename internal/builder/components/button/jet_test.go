package button

import (
	"testing"
)

// mockResolver 测试用内容解析器：字段路径 → 字符串值。
type mockResolver struct {
	vals map[string]string
}

func (m mockResolver) ResolveString(field string) (string, error) {
	return m.vals[field], nil
}

// TestBuildAttrsActionLinkProtocol 验证 ActionLink 绑定值渲染 href 前过协议白名单：
// 合法 http/https/相对路径/# 锚点/mailto 保留；javascript:/data: 等危险协议安全降级为无 href。
func TestBuildAttrsActionLinkProtocol(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  string // 期望 attrs；空串表示降级为无 href
	}{
		{"https 外链保留", "https://example.com/post", ` href="https://example.com/post"`},
		{"站内相对路径保留", "/products/xxx", ` href="/products/xxx"`},
		{"锚点保留", "#section", ` href="#section"`},
		{"mailto 保留", "mailto:hi@example.com", ` href="mailto:hi@example.com"`},
		{"javascript 注入降级", "javascript:alert(1)", ""},
		{"data 协议降级", "data:text/html;base64,xxx", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := &Props{Action: ActionLink, Binding: &Binding{Field: "post.permalink"}}
			res := mockResolver{vals: map[string]string{"post.permalink": c.value}}
			_, attrs, err := buildAttrs(p, res)
			if err != nil {
				t.Fatalf("buildAttrs 意外报错: %v", err)
			}
			if attrs != c.want {
				t.Errorf("attrs = %q, 期望 %q", attrs, c.want)
			}
		})
	}
}

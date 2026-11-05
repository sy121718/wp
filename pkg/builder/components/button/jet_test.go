package button

import (
	"strings"
	"testing"
)

type mockResolver struct {
	vals map[string]string
}

func (m mockResolver) ResolveString(field string) (string, error) {
	return m.vals[field], nil
}

func TestBuildAttrsActionLinkProtocol(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  string
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
			_, attrs, err := buildAttrs(p, res, nil)
			if err != nil {
				t.Fatalf("buildAttrs 意外报错: %v", err)
			}
			if attrs != c.want {
				t.Errorf("attrs = %q, 期望 %q", attrs, c.want)
			}
		})
	}
}

func TestBuildAttrsLocalizesSiteLink(t *testing.T) {

	link := func(p string) string {
		if strings.HasPrefix(p, "/") {
			return "/en" + p
		}
		return p
	}
	for _, tc := range []struct {
		name  string
		value string
		want  string
	}{
		{"站内路径加前缀", "/shop", ` href="/en/shop"`},
		{"外链不动", "https://example.com/x", ` href="https://example.com/x"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, attrs, err := buildAttrs(&Props{Action: ActionInternal, Value: tc.value}, nil, link)
			if err != nil {
				t.Fatalf("buildAttrs 意外报错: %v", err)
			}
			if attrs != tc.want {
				t.Errorf("attrs = %q, 期望 %q", attrs, tc.want)
			}
		})
	}
}

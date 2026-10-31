package unit

import (
	"strings"
	"testing"

	"go_wp/internal/builder"
)

func TestImageCompilePipeline(t *testing.T) {
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"pic","type":"core.image","props":{"src":"/storage/hero.jpg","alt":"局部覆盖","title":"标题"}}]}`
	c, err := compile(t, mustParse(t, doc))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	for _, want := range []string{
		`<img src="` + testMediaBaseURL + `/hero.jpg"`,
		`loading="lazy"`,
		`decoding="async"`,
		`alt="局部覆盖"`,
		`title="标题"`,
	} {
		if !strings.Contains(c.HTML, want) {
			t.Errorf("HTML 缺少 %q\n实际: %s", want, c.HTML)
		}
	}
}

func TestImageValidateProps(t *testing.T) {
	cases := []struct{ name, json, want string }{
		{"非法地址", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"i1","type":"core.image","props":{"src":"javascript:alert(1)"}}]}`, "必须提供完整 HTTP(S) 地址"},
		{"图片带子节点", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"i1","type":"core.image","props":{"src":"/storage/x.jpg"},"children":[{"id":"c1","type":"core.image","props":{"src":"/storage/y.jpg"}}]}]}`, "叶子节点"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compile(t, mustParse(t, tc.json))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("期望错误含 %q，实际: %v", tc.want, err)
			}
		})
	}
}

func mustParse(t *testing.T, jsonStr string) *builder.Page {
	t.Helper()
	p, err := builder.ParsePage([]byte(jsonStr))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	return p
}

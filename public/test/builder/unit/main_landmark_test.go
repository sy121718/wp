package unit

import (
	"strings"
	"testing"
)

func TestMainLandmarkWrapsContent(t *testing.T) {
	doc := `{"settings":{"layout":{"mode":"full","mainLandmark":true}},"root":[` +
		`{"id":"hd","type":"core.container","props":{"tag":"header","layout":{"engine":"flex","flex":{"direction":"column","justify":"center","align":"center","wrap":false,"gap":"12px"}}}},` +
		`{"id":"bd","type":"core.container","props":{"tag":"section","layout":{"engine":"flex","flex":{"direction":"column","justify":"center","align":"center","wrap":false,"gap":"12px"}}}},` +
		`{"id":"ft","type":"core.container","props":{"tag":"footer","layout":{"engine":"flex","flex":{"direction":"column","justify":"center","align":"center","wrap":false,"gap":"12px"}}}}]}`
	c, err := compile(t, parse(t, doc))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	html := c.HTML
	for _, want := range []string{"<main id=\"main-content\">", "</main>", "<header", "<footer"} {
		if !strings.Contains(html, want) {
			t.Errorf("产物缺少 %q：%s", want, html)
		}
	}
	hi := strings.Index(html, "<header")
	mi := strings.Index(html, "<main")
	mc := strings.Index(html, "</main>")
	fi := strings.Index(html, "<footer")
	if !(hi >= 0 && hi < mi && mi < mc && mc < fi) {
		t.Errorf("地标区间不对（期望 header < main < /main < footer）：%s", html)
	}
}

func TestMainLandmarkOffByDefault(t *testing.T) {
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[` +
		`{"id":"bd","type":"core.container","props":{"tag":"section","layout":{"engine":"flex","flex":{"direction":"column","justify":"center","align":"center","wrap":false,"gap":"12px"}}}}]}`
	c, err := compile(t, parse(t, doc))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if strings.Contains(c.HTML, "<main") {
		t.Errorf("默认不该输出 main 地标：%s", c.HTML)
	}
}

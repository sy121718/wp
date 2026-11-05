package productcard

import (
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
)

func TestProductCardCSSHoverBuckets(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{}, &b)
	out := b.String()
	for _, want := range []string{
		"@media (hover: hover) {\n  .sky-c-t {\n  transform: translateY(-2px);\n  box-shadow: 0 8px 24px rgba(0,0,0,.10);\n\n}",
		"@media (hover: none) {\n  .sky-c-t {\n  box-shadow: 0 2px 8px rgba(0,0,0,.06);\n\n}",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}

	active := ".sky-c-t {\n  transform: translateY(0);\n  box-shadow: 0 1px 4px rgba(0,0,0,.08);\n\n}"
	if !strings.Contains(out, active) {
		t.Errorf("按压反馈规则缺失或落进了别的桶:\n%s", out)
	}
}

func TestProductCardCSSRules(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{}, &b)
	out := b.String()
	for _, want := range []string{
		".sky-c-t {\n  display: flex;",
		"transition: transform .18s ease, box-shadow .18s ease;",
		".sky-c-t .sky-product-card-media img {",
		"width: min(100%, 100%);",
		".sky-c-t .sky-product-card-body {",
		"padding: var(--sky-density-pad, 14px);",
		".sky-c-t .sky-product-card-price {",
		".sky-c-t .sky-product-card-tag {",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}

	if b.ContainerQueryCSS() != "" || b.TopLevelCSS() != "" {
		t.Errorf("商品卡不该产出容器查询或全局规则:\n%s\n%s", b.ContainerQueryCSS(), b.TopLevelCSS())
	}
	if strings.Contains(out, "&") || strings.Contains(out, "{{") {
		t.Errorf("产物里有未替换的作用域前缀或未展开的占位:\n%s", out)
	}
}

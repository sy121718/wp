package builder

import (
	"strings"
	"testing"
)

const fragLangDoc = `{
  "settings": {"layout": {"mode": "full"}, "seo": {"title": "片段", "description": "片段"}},
  "root": [
    {"id": "p1", "type": "core.product", "props": {"titleField": "product.name", "optionsField": "product.options", "variantsField": "product.variants"}},
    {"id": "a1", "type": "core.addToCart", "props": {}}
  ]
}`

const (
	fragLangOptions  = `[{"key":"color","name":"颜色","values":[{"key":"red","label":"红"},{"key":"blue","label":"蓝"}]}]`
	fragLangVariants = `[{"id":"var-1","sku":"a-1","price":"99","enabled":true,"options":{"color":"red"}},` +
		`{"id":"var-2","sku":"a-2","price":"129","enabled":true,"options":{"color":"blue"}}]`
)

type fragLangResolver struct{}

func (fragLangResolver) ResolveString(field string) (string, error) {
	switch field {
	case "product.name":
		return "衬衫", nil
	case "product.options":
		return fragLangOptions, nil
	case "product.variants":
		return fragLangVariants, nil
	}
	return "", nil
}

func compileFragLangDoc(t *testing.T, opts ...CompileOption) string {
	t.Helper()
	p, err := ParsePage([]byte(fragLangDoc))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	opts = append([]CompileOption{
		WithComponentSet(i18nTestComponentSet(t)),
		WithContentResolver(fragLangResolver{}),
		WithArchiveEntity("product", "current-product"),
	}, opts...)
	res, err := Compile(p, opts...)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return res.HTML
}

func TestCompileFragmentURLsCarryBuildLang(t *testing.T) {
	for _, lang := range []string{"en-US", "ja"} {
		t.Run(lang, func(t *testing.T) {
			html := compileFragLangDoc(t, WithLanguage(lang), WithProjectID("proj-1"))

			if !strings.Contains(html, "lang="+lang) {
				t.Fatalf("片段请求 URL 应带构建语言 %s\nHTML=%s", lang, html)
			}

			if !strings.Contains(html, `name="lang" value="`+lang+`"`) {
				t.Fatalf("加购表单应带语言 %s 的 hidden 域\nHTML=%s", lang, html)
			}
		})
	}
}

func TestCompileFragmentURLsUseDefaultLangWhenUnset(t *testing.T) {
	html := compileFragLangDoc(t, WithProjectID("proj-1"))
	if !strings.Contains(html, "lang=zh-CN") {
		t.Fatalf("未指定语言时应取站点默认语言 zh-CN\nHTML=%s", html)
	}
}

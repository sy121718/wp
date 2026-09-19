package builder

// fragment_lang_test.go — 片段请求 URL 的语言**接线**回归（I18N-011）。
//
// 组件单测证明「拿到语言时会拼进去」；真正容易静默失效的是装配那一段 ——
// jetview 把 ctx.Lang 传进组件 BuildView。少传一个参数，编译照过、单测照绿，
// 线上只是「英文站点刷新一次商品位，文案换回中文」。
//
// 所以这里走完整 Compile：产物里的片段请求（GET 查询串 + POST 表单域）必须带上
// **本次构建的目标语言**。
//
// 注意目标语言**永远非空**：WithLanguage 未指定时取 i18n.GetDefaultLang()
// （builder.resolveCompileI18n），所以「不带 lang 参数」只可能出现在独立渲染
// （testProjectID 那类单测）里，不是静态产物的形态。

import (
	"strings"
	"testing"
)

// fragLangDoc 两个「构建期烘片段地址」的组件各一个节点。
const fragLangDoc = `{
  "settings": {"layout": {"mode": "full"}, "seo": {"title": "片段", "description": "片段"}},
  "root": [
    {"id": "p1", "type": "core.product", "props": {"titleField": "product.name", "optionsField": "product.options", "variantsField": "product.variants"}},
    {"id": "a1", "type": "core.addToCart", "props": {"optionsField": "product.options", "variantsField": "product.variants"}}
  ]
}`

// fragLangOptions / fragLangVariants 商品规格与变体。
// 两条启用变体是刻意的：规格选择位只在「组合数 > 1」时才输出（一行的选择器没有意义），
// 而那两个片段位恰恰挂在组合行上。
const (
	fragLangOptions  = `[{"key":"color","name":"颜色","values":[{"key":"red","label":"红"},{"key":"blue","label":"蓝"}]}]`
	fragLangVariants = `[{"id":"var-1","sku":"a-1","price":"99","enabled":true,"options":{"color":"red"}},` +
		`{"id":"var-2","sku":"a-2","price":"129","enabled":true,"options":{"color":"blue"}}]`
)

// fragLangResolver 固定字段值的构建期内容解析器。
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

// compileFragLangDoc 编译片段语言用例文档，返回产物 HTML。
func compileFragLangDoc(t *testing.T, opts ...CompileOption) string {
	t.Helper()
	p, err := ParsePage([]byte(fragLangDoc))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	opts = append([]CompileOption{
		WithComponentSet(i18nTestComponentSet(t)),
		WithContentResolver(fragLangResolver{}),
	}, opts...)
	res, err := Compile(p, opts...)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return res.HTML
}

// TestCompileFragmentURLsCarryBuildLang 目标语言进片段请求（GET 查询串与 POST 表单域）。
func TestCompileFragmentURLsCarryBuildLang(t *testing.T) {
	for _, lang := range []string{"en-US", "ja"} {
		t.Run(lang, func(t *testing.T) {
			html := compileFragLangDoc(t, WithLanguage(lang), WithProjectID("proj-1"))

			// 商品侧两个片段位（实时价格核对 / 实时可用量）是 hx-get，语言进查询串。
			if !strings.Contains(html, "lang="+lang) {
				t.Fatalf("片段请求 URL 应带构建语言 %s\nHTML=%s", lang, html)
			}
			// 加购是 POST：片段端点只读 PostForm，语言只能走表单 hidden 域。
			if !strings.Contains(html, `name="lang" value="`+lang+`"`) {
				t.Fatalf("加购表单应带语言 %s 的 hidden 域\nHTML=%s", lang, html)
			}
		})
	}
}

// TestCompileFragmentURLsUseDefaultLangWhenUnset 未指定语言时取站点默认语言（而非不带参数）。
func TestCompileFragmentURLsUseDefaultLangWhenUnset(t *testing.T) {
	html := compileFragLangDoc(t, WithProjectID("proj-1"))
	if !strings.Contains(html, "lang=zh-CN") {
		t.Fatalf("未指定语言时应取站点默认语言 zh-CN\nHTML=%s", html)
	}
}

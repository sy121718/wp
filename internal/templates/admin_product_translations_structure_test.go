package templates

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func translationPageData() map[string]any {
	return groupDData(map[string]any{
		"Scope": map[string]any{"ProjectID": "pr1", "Projects": groupDProjects()},
		"Translations": map[string]any{"ProjectID": "pr1", "ProductID": "p1", "Lang": "en-US", "SourceLang": "zh-CN", "Langs": []string{"en-US", "ja-JP"}, "Done": 1, "Total": 2,
			"Groups": []map[string]any{{"EntityType": "product", "EntityID": "p1", "EntityName": "商品", "Rows": []map[string]any{
				{"Field": "name", "Context": "product.name", "Source": "夏季衬衫", "Target": "Summer Shirt", "SourceHash": "h1", "Engine": "manual"},
				{"Field": "subtitle", "Context": "product.subtitle", "Source": "纯棉", "Target": "", "SourceHash": "h2", "Engine": ""},
			}}},
		},
	})
}

func TestProductTranslationsSaveUsesJSONBase(t *testing.T) {
	out, err := render(t, groupDSet(t), "admin/product/product_translations", translationPageData(), groupDRenderScope())
	if err != nil {
		t.Fatalf("译文工作台渲染失败: %v", err)
	}
	root, err := html.Parse(strings.NewReader(out))
	if err != nil {
		t.Fatalf("译文工作台输出无法解析为 HTML: %v", err)
	}

	forms := peAll(root, func(n *html.Node) bool {
		return peEl(n, "form") && peAttr(n, "hx-post") != ""
	})
	if len(forms) != 1 {
		t.Fatalf("译文工作台应恰有 1 张写表单，实际 %d", len(forms))
	}
	form := forms[0]
	if got := peAttr(form, "hx-post"); got != "/api/product/translations/save" {
		t.Fatalf("写表单端点 = %q，want /api/product/translations/save", got)
	}
	if !peHasAttr(form, "data-write-reload") {
		t.Fatal("写表单缺 data-write-reload —— 成功后结论不会跨刷新显示")
	}

	for _, name := range []string{"rowTarget", "rowContext", "rowHash"} {
		if n := len(peAll(form, func(m *html.Node) bool {
			return peHasAttr(m, "name") && peAttr(m, "name") == name
		})); n == 0 {
			t.Errorf("表单里没有 name=%s 的控件 —— 三组并行数组少一组，服务端长度对齐会判失败", name)
		}
	}

	for _, name := range []string{"projectId", "productId", "lang"} {
		if n := len(peAll(form, func(m *html.Node) bool {
			return peEl(m, "input") && peAttr(m, "name") == name && peAttr(m, "type") == "hidden"
		})); n != 1 {
			t.Fatalf("保存表单应有 1 个 name=%s 的隐藏域，实际 %d", name, n)
		}
	}
	if n := len(peAll(form, func(m *html.Node) bool {
		return peEl(m, "input") && peAttr(m, "name") == "csrf_token"
	})); n != 0 {
		t.Fatalf("保存表单仍有 csrf_token 隐藏域（基座发 X-CSRF-Token 头），实际 %d", n)
	}

	if n := len(peAll(form, func(m *html.Node) bool {
		return peEl(m, "textarea") && peAttr(m, "name") == "rowTarget"
	})); n != 2 {
		t.Fatalf("rowTarget 控件数应与行数一致（2），实际 %d", n)
	}
	if n := len(peAll(form, func(m *html.Node) bool {
		return peEl(m, "input") && peAttr(m, "name") == "rowContext"
	})); n != 2 {
		t.Fatalf("rowContext 控件数应与行数一致（2），实际 %d", n)
	}

	btn := peAll(form, func(m *html.Node) bool { return peEl(m, "button") && peAttr(m, "type") == "submit" })
	if len(btn) != 1 {
		t.Fatalf("保存表单应有 1 个提交按钮，实际 %d", len(btn))
	}
	if !peHasAttr(btn[0], "data-requires-js") || !peHasAttr(btn[0], "hidden") || !peHasAttr(btn[0], "disabled") {
		t.Fatal("保存按钮缺少 data-requires-js/hidden/disabled 三者之一")
	}
	if n := len(peAll(form, func(m *html.Node) bool {
		return peEl(m, "p") && peHasAttr(m, "data-write-status")
	})); n != 1 {
		t.Fatalf("保存表单应有 1 个 data-write-status 结论行，实际 %d", n)
	}

	if strings.Contains(out, `action="/admin/products/translations/save"`) {
		t.Fatal("页面仍在提交已下线的页面 POST /admin/products/translations/save")
	}
}

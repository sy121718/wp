package templates

import (
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
)

func detailTemplatePageData(published bool) map[string]any {
	p := groupDProductRow()

	return groupDData(map[string]any{
		"Scope":     map[string]any{"ProjectID": "pr1", "Projects": groupDProjects()},
		"Available": true, "Product": p, "DetailPath": "/products/p-one",
		"Templates": []map[string]any{
			{"ID": "tpl1", "Name": "活动版式详情页", "DraftVersion": 1, "UpdatedAt": time.Time{}},
			{"ID": "tpl2", "Name": "极简详情页", "DraftVersion": 3, "UpdatedAt": time.Time{}},
		},
		"Bound":             map[string]any{"Exists": published, "URLPath": "/products/p-one", "Status": "active", "TemplateID": "tpl1"},
		"DefaultTemplateID": "tpl1",
	})
}

func renderDetailTemplatePage(t *testing.T, published bool) (*html.Node, string) {
	t.Helper()
	out, err := render(t, groupDSet(t), "admin/product/product_detail_template", detailTemplatePageData(published))
	if err != nil {
		t.Fatalf("详情模板页渲染失败: %v", err)
	}
	root, err := html.Parse(strings.NewReader(out))
	if err != nil {
		t.Fatalf("详情模板页输出无法解析为 HTML: %v", err)
	}
	return root, out
}

func detailTemplateWriteForms(root *html.Node) map[string]*html.Node {
	forms := map[string]*html.Node{}
	for _, n := range peAll(root, func(n *html.Node) bool {
		return peEl(n, "form") && peAttr(n, "data-write-url") != ""
	}) {
		forms[peAttr(n, "data-write-url")] = n
	}
	return forms
}

func TestDetailTemplateWritesUseJSONBase(t *testing.T) {
	root, out := renderDetailTemplatePage(t, true)
	forms := detailTemplateWriteForms(root)

	want := []string{
		"/api/product/detail-template/apply",
		"/api/product/detail-template/update-url",
		"/api/product/detail-template/create",
	}
	for _, api := range want {
		f, ok := forms[api]
		if !ok {
			t.Fatalf("详情模板页缺少挂 %s 的写表单，实际端点数 %d", api, len(forms))
		}

		if got := peAttr(f, "data-write"); got == "" {
			t.Fatalf("%s 表单缺少 data-write（写基座的必填属性），实际属性为空", api)
		}

		if !peHasAttr(f, "data-write-reload") {
			t.Fatalf("%s 表单缺少 data-write-reload —— 写成功后结论不会显示", api)
		}

		if n := len(peAll(f, func(m *html.Node) bool {
			return peEl(m, "input") && peAttr(m, "name") == "csrf_token"
		})); n != 0 {
			t.Fatalf("%s 表单里仍有 csrf_token 隐藏域 %d 个（基座发 X-CSRF-Token 头）", api, n)
		}

		btn := peAll(f, func(m *html.Node) bool { return peEl(m, "button") && peAttr(m, "type") == "submit" })
		if len(btn) != 1 {
			t.Fatalf("%s 表单应有 1 个提交按钮，实际 %d", api, len(btn))
		}
		if !peHasAttr(btn[0], "data-requires-js") || !peHasAttr(btn[0], "hidden") || !peHasAttr(btn[0], "disabled") {
			t.Fatalf("%s 的提交按钮缺少 data-requires-js/hidden/disabled 三者之一", api)
		}
		if n := len(peAll(f, func(m *html.Node) bool { return peEl(m, "p") && peHasAttr(m, "data-write-status") })); n != 1 {
			t.Fatalf("%s 表单应有 1 个 data-write-status 结论行，实际 %d", api, n)
		}
	}

	if n := len(peAll(forms["/api/product/detail-template/update-url"], func(m *html.Node) bool {
		return peEl(m, "input") && peAttr(m, "type") == "checkbox" && peAttr(m, "name") == "withRedirect"
	})); n != 1 {
		t.Fatalf("改 URL 表单里应有 1 个 name=withRedirect 的复选框，实际 %d 个", n)
	}

	for _, gone := range []string{
		"/admin/products/template/apply",
		"/admin/products/template/url",
		"/admin/products/template/publish",
		"/admin/products/template/create",
	} {
		if strings.Contains(out, `action="`+gone+`"`) {
			t.Fatalf("页面仍在提交已下线的端点 %s", gone)
		}
	}

}

func TestDetailTemplateUnpublishedShowsPublishOnly(t *testing.T) {
	root, out := renderDetailTemplatePage(t, false)
	forms := detailTemplateWriteForms(root)

	if _, ok := forms["/api/product/detail-template/publish"]; !ok {
		t.Fatalf("未发布态缺少首次发布写表单，实际端点 %v", forms)
	}
	for _, absent := range []string{
		"/api/product/detail-template/apply",
		"/api/product/detail-template/update-url",
	} {
		if _, ok := forms[absent]; ok {
			t.Fatalf("未发布态不该渲染 %s 表单（此时还没有实例）", absent)
		}
	}
	if !strings.Contains(out, "发布详情页") {
		t.Fatal("未发布态应给出首次发布入口")
	}
}

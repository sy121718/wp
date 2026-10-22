package templates

// admin_product_detail_template_structure_test.go — 详情页模板页的写动作用 JSON 写基座（批次 B）。
//
// 迁移后这一页只剩两个页面级入口：整页 GET 与「预览」（原生 POST 开新标签页，只读）。
// 新建 / 首次发布 / 切换模板 / 改 URL 四个写动作都走 /api/product/detail-template/*，
// 由 internal/templates/static/js/ui/json_write.js 提交。结构判据钉住四件事：
//
//  1. 每个写表单都带 data-write-api 且指向自己的端点（一条路由一个权限点）；
//  2. 表单里不再有 csrf_token 隐藏域（基座发 X-CSRF-Token 头，留它会被当未知字段拒掉）；
//  3. 提交按钮带 data-requires-js hidden disabled（无 JS 时不可点，而不是点了没反应）；
//  4. 页面有 data-write-flash-target —— 结论行是「刷新后显示一次」的唯一落点。
import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// detailTemplatePageData 详情模板页的渲染数据（与 handler 的装配同形）。
//
// published 控制「有没有发布实例」：页面据此二选一渲染「切换模板 + 改 URL」或「首次发布」，
// 两支都要覆盖 —— 只测一支时另一支的端点写错没人发现。
func detailTemplatePageData(published bool) map[string]any {
	p := groupDProductRow()
	p["URLPath"] = "p-one"
	return groupDData(map[string]any{
		"SelectedProject": "pr1", "Ready": true, "Err": "",
		"Projects": groupDProjects(),
		"Product":  p, "ProductID": "p1",
		"Templates": []map[string]any{
			{"ID": "tpl1", "Name": "活动版式详情页", "DraftVersion": 1, "IsDefault": true, "IsBound": true},
			{"ID": "tpl2", "Name": "极简详情页", "DraftVersion": 3, "IsDefault": false, "IsBound": false},
		},
		"TemplateCount":  2,
		"InstanceExists": published, "InstanceURL": "/products/p-one",
		"InstanceStatus": "已发布", "BoundTemplateID": "tpl1", "DefaultTemplateID": "tpl1",
	})
}

// renderDetailTemplatePage 渲染并把输出解析成 DOM（模板中断会让渲染直接报错）。
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

// detailTemplateWriteForms 页面上的写表单（hx-post 非空）按端点索引。
func detailTemplateWriteForms(root *html.Node) map[string]*html.Node {
	forms := map[string]*html.Node{}
	for _, n := range peAll(root, func(n *html.Node) bool {
		return peEl(n, "form") && peAttr(n, "hx-post") != ""
	}) {
		forms[peAttr(n, "hx-post")] = n
	}
	return forms
}

// TestDetailTemplateWritesUseJSONBase 已发布态：切换模板 + 改 URL 两支写表单挂基座，
// 且都不再是页面 POST。
func TestDetailTemplateWritesUseJSONBase(t *testing.T) {
	root, out := renderDetailTemplatePage(t, true)
	forms := detailTemplateWriteForms(root)

	want := []string{
		"/api/product/detail-template/apply",
		"/api/product/detail-template/update-url",
		"/api/product/detail-template/create", // 抽屉里的新建入口始终在
	}
	for _, api := range want {
		f, ok := forms[api]
		if !ok {
			t.Fatalf("详情模板页缺少挂 %s 的写表单，实际端点数 %d", api, len(forms))
		}
		if got := peAttr(f, "hx-swap"); got != "none" {
			t.Fatalf("%s 表单的 hx-swap = %q，want none（写动作没有要换进 DOM 的内容）", api, got)
		}
		if got := peAttr(f, "data-write-flash"); got != "product-detail-template" {
			t.Fatalf("%s 表单的 data-write-flash = %q，want product-detail-template", api, got)
		}
		// CSRF 走请求头：表单里留着隐藏域会被端点当未知字段整条拒掉。
		if n := len(peAll(f, func(m *html.Node) bool {
			return peEl(m, "input") && peAttr(m, "name") == "csrf_token"
		})); n != 0 {
			t.Fatalf("%s 表单里仍有 csrf_token 隐藏域 %d 个（基座发 X-CSRF-Token 头）", api, n)
		}
		// 无 JS 时按钮不可点：hidden + disabled 由基座摘除。
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

	// 改 URL 的「旧链接 301」复选框要把勾选状态交给基座（未勾选时客户端不发这个键）。
	if got := peAttr(forms["/api/product/detail-template/update-url"], "data-write-bool"); got != "withRedirect" {
		t.Fatalf("改 URL 表单的 data-write-bool = %q，want withRedirect", got)
	}

	// 反向：页面上不该再有这四条页面 POST（迁移后它们连路由都不存在）。
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

	// 结论行落点：写动作整页刷新后在这里显示一次。
	if n := len(peAll(root, func(m *html.Node) bool {
		return peAttr(m, "data-write-flash-target") == "product-detail-template"
	})); n != 1 {
		t.Fatalf("详情模板页应有 1 个 data-write-flash-target=product-detail-template，实际 %d", n)
	}
}

// TestDetailTemplateUnpublishedShowsPublishOnly 未发布态：只渲染「首次发布」写表单，
// 切换模板与改 URL 两支不出现（它们的对象是已存在的实例）。
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

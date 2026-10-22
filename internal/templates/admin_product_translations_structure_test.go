package templates

// admin_product_translations_structure_test.go — 商品译文工作台的保存走 JSON 写基座（批次 C）。
//
// 这一页的保存有两个容易写错、且错了不会报错的点，用结构判据钉住：
//
//  1. 三组 row* 字段必须按行对齐（data-write-array 同时点名三个）且**保留空译文行占位**：
//     基座按控件 name 收集数组，漏点一个 name 就变成标量，服务端长度对齐直接判失败；
//     把空行剔掉则让后面所有行错位一格，症状只是「这一行译文对不上」。
//  2. 隐藏域的 name 必须与接口 JSON 字段一致（projectId / productId）：基座按 name 组请求体，
//     写成 project / product 会被严格白名单整条拒掉。
//
// 另两条与 B 批同形：表单里不再有 csrf_token 隐藏域（基座发 X-CSRF-Token 头）、
// 提交按钮带 data-requires-js hidden disabled（无 JS 时不可点）。
import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// translationPageData 工作台的渲染数据：两组、三行（其中一行译文为空 —— 空行占位是本页的契约）。
func translationPageData() map[string]any {
	return groupDData(map[string]any{
		"SelectedProject": "pr1",
		"ProjectID":       "pr1", "ProductID": "p1", "Lang": "en-US", "SourceLang": "zh-CN",
		"Langs": []map[string]any{
			{"Code": "en-US", "Label": "English", "Active": true},
			{"Code": "ja-JP", "Label": "日本語", "Active": false},
		},
		"Done": 1, "Total": 2, "NoTargetLang": false, "Errors": []map[string]any{},
		"ProjectOptions": []map[string]any{{"ID": "pr1", "Name": "主站", "Selected": true}},
		"Groups": []map[string]any{{
			"TitleKey": "x", "TitleFallback": "商品", "Title": "商品", "Subtitle": "product.name",
			"Rows": []map[string]any{
				{"FieldLabel": "商品名", "Context": "product.name", "Source": "夏季衬衫",
					"Target": "Summer Shirt", "SourceHash": "h1", "Rich": false,
					"Translated": true, "Engine": "manual", "EngineTone": "success"},
				{"FieldLabel": "副标题", "Context": "product.subtitle", "Source": "纯棉",
					"Target": "", "SourceHash": "h2", "Rich": false,
					"Translated": false, "Engine": "", "EngineTone": "mute"},
			},
		}},
	})
}

// TestProductTranslationsSaveUsesJSONBase 保存表单挂基座、三组数组点名齐全、无 csrf 域。
func TestProductTranslationsSaveUsesJSONBase(t *testing.T) {
	out, err := render(t, groupDSet(t), "admin/product/product_translations", translationPageData())
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
	if got := peAttr(form, "hx-swap"); got != "none" {
		t.Fatalf("hx-swap = %q，want none", got)
	}
	if got := peAttr(form, "data-write-flash"); got != "product-translations" {
		t.Fatalf("data-write-flash = %q，want product-translations", got)
	}
	// 三组并行数组必须**全部**点名：漏一个就按标量发，服务端长度对齐直接判失败。
	for _, name := range []string{"rowTarget", "rowContext", "rowHash"} {
		if !strings.Contains(peAttr(form, "data-write-array"), name) {
			t.Fatalf("data-write-array 未点名 %s，实际 %q", name, peAttr(form, "data-write-array"))
		}
	}

	// 控件 name 与接口 JSON 字段同名（基座按 name 组请求体）。
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

	// 三行译文输入与三个语境 / 指纹隐藏域一一对应 —— 空译文行不能被剔掉。
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

	// 按钮门槛与结论行。
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
	if n := len(peAll(root, func(m *html.Node) bool {
		return peAttr(m, "data-write-flash-target") == "product-translations"
	})); n != 1 {
		t.Fatalf("页面应有 1 个 data-write-flash-target=product-translations，实际 %d", n)
	}
	// 旧页面 POST 已下线：页面上不该再有这条 action。
	if strings.Contains(out, `action="/admin/products/translations/save"`) {
		t.Fatal("页面仍在提交已下线的页面 POST /admin/products/translations/save")
	}
}

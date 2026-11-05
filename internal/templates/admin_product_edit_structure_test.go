package templates

import (
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
)

var productEditProtocolFields = []string{

	"projectId", "id",
	"name", "subtitle", "slug", "sku", "status", "defaultPrice", "unit", "weight",

	"images", "imageAlts",
	"attributeIds", "categoryIds", "primaryCategoryId", "brandId", "tagIds",
}

func productEditPageData() map[string]any {
	return productCatalogData(map[string]any{
		"SelectedProject": "pr1", "Err": "", "Done": "",
		"Projects":  groupDProjects(),
		"ProductID": "p1", "HasProduct": true, "Product": groupDProductRow(),
		"BackURL":          "/admin/products?project=pr1",
		"WarehouseOptions": []map[string]any{{"ID": "w1", "Name": "苏州仓", "Code": "SZ", "IsDefault": true}},
		"AttributeChecks":  []map[string]any{{"ID": "a1", "Label": "颜色（color）", "IsVariation": true, "Checked": true}},
		"ImagesInput":      "https://cdn.example.com/1.jpg",
		"ImageAltsInput":   "图一",
		"WeightInput":      "0.5", "DefaultPriceInput": "10.00",

		"TplPanel": map[string]any{
			"Available": true, "Bound": map[string]any{"InstanceID": "inst1", "TemplateID": "tpl1", "Status": "published", "RenderMode": "document", "SourceTemplateVersionID": "v1"}, "CurrentTemplateVersionID": "v2",
			"HasAffectedTemplateCount": true, "AffectedTemplateCount": 3,
			"Snapshots": []map[string]any{{"ID": "s1", "CreatedAt": time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)}},
		},
	})
}

func productEditRender(t *testing.T) (*html.Node, string) {
	t.Helper()
	out, err := render(t, groupDSet(t), "admin/product/product_edit", productEditPageData(), groupDRenderScope())
	if err != nil {
		t.Fatalf("商品编辑页渲染失败: %v", err)
	}
	root, err := html.Parse(strings.NewReader(out))
	if err != nil {
		t.Fatalf("商品编辑页输出无法解析为 HTML: %v", err)
	}
	return root, out
}

func productEditStack(t *testing.T, root *html.Node) *html.Node {
	t.Helper()
	stacks := peAll(root, func(n *html.Node) bool {
		if !peClass(n, "stack") {
			return false
		}
		return len(peAll(n, func(m *html.Node) bool {
			return peEl(m, "header") && peClass(m, "page-head")
		})) == 1
	})
	if len(stacks) != 1 {
		t.Fatalf("应能唯一定位到本页的 .stack 根容器，实际 %d 个", len(stacks))
	}
	return stacks[0]
}

func productEditFormByWriteAPI(root *html.Node, api string) []*html.Node {
	return peAll(root, func(n *html.Node) bool {
		return peEl(n, "form") && peAttr(n, "hx-post") == api
	})
}

func TestProductEditListCardTitlesUseCardTitle(t *testing.T) {
	root, _ := productEditRender(t)
	stack := productEditStack(t, root)

	toolbars := peAll(stack, func(n *html.Node) bool {
		return peEl(n, "div") && peClass(n, "list-toolbar")
	})
	if len(toolbars) != 2 {
		t.Fatalf("变体 / 评分应各有一个 .list-toolbar，实际 %d 个", len(toolbars))
	}
	wantTitles := []string{"变体", "评分"}
	for i, tb := range toolbars {
		var titles []*html.Node
		for _, kid := range peKids(tb) {
			if peClass(kid, "card-title") {
				titles = append(titles, kid)
			}
		}
		if len(titles) != 1 {
			t.Fatalf("第 %d 个列表卡标题行应有且只有一个直接子 .card-title，实际 %d 个", i+1, len(titles))
		}
		got := strings.TrimSpace(peText(titles[0]))
		if got != wantTitles[i] {
			t.Fatalf("第 %d 个列表卡标题应为 %q，实际 %q", i+1, wantTitles[i], got)
		}
		if strongs := peAll(tb, func(n *html.Node) bool { return peEl(n, "strong") }); len(strongs) != 0 {
			t.Fatalf("列表卡标题行不该再用裸 <strong>（.card-title 才是列表卡标题的形态），实际 %d 个", len(strongs))
		}

		if btns := peAll(tb, func(n *html.Node) bool { return peEl(n, "button") }); len(btns) == 0 {
			t.Fatalf("第 %d 个列表卡标题行丢了动作按钮", i+1)
		}
	}
}

func TestProductEditLowFrequencyBlocksFolded(t *testing.T) {
	root, raw := productEditRender(t)
	stack := productEditStack(t, root)

	folds := peAll(stack, func(n *html.Node) bool {
		return peEl(n, "details") && peClass(n, "section-fold")
	})
	if len(folds) != 1 {
		t.Fatalf("低频只读块应恰好 1 个折叠区（详情页模板；SEO 检查已改抽屉），实际 %d 个", len(folds))
	}
	for i, f := range folds {
		var summary *html.Node
		var bodies int
		for _, kid := range peKids(f) {
			if peEl(kid, "summary") {
				summary = kid
			}
			if peClass(kid, "fold-body") {
				bodies++
			}
		}
		if summary == nil {
			t.Fatalf("第 %d 个折叠区缺少 <summary>（没有它就不是可展开的折叠区）", i+1)
		}
		titles := peAll(summary, func(n *html.Node) bool { return peClass(n, "fold-title") })
		if len(titles) != 1 || strings.TrimSpace(peText(titles[0])) == "" {
			t.Fatalf("第 %d 个折叠区的 <summary> 里应有且只有一个非空 .fold-title", i+1)
		}
		if bodies != 1 {
			t.Fatalf("第 %d 个折叠区应有且只有一个 .fold-body 承载内容，实际 %d 个", i+1, bodies)
		}
	}

	if !strings.Contains(raw, `data-drawer-open="#tpl-product-seo"`) {
		t.Fatal("商品编辑页缺少SEO本地抽屉入口")
	}
	if strings.Contains(raw, "/admin/products/seo/drawer") || strings.Contains(raw, `action="/admin/products/seo-score"`) {
		t.Fatal("SEO 抽屉结构必须来自本地模板，评分结果另走明确的 Jet HTML 端点")
	}
	seoTemplates := peAll(root, func(n *html.Node) bool { return peEl(n, "template") && peAttr(n, "id") == "tpl-product-seo" })
	if len(seoTemplates) != 1 {
		t.Fatalf("SEO必须有一个本地抽屉模板，实际%d", len(seoTemplates))
	}
	seoForms := peAll(seoTemplates[0], func(n *html.Node) bool {
		return peEl(n, "form") && peAttr(n, "hx-post") == "/api/product/seo-score"
	})
	if len(seoForms) != 1 || peAttr(seoForms[0], "hx-target") != "find [data-write-result]" || peAttr(seoForms[0], "hx-swap") != "innerHTML" || peHasAttr(seoForms[0], "data-write-handler") {
		t.Fatal("SEO 抽屉必须由 HTMX 请求 Jet HTML 并局部交换")
	}
	if peUp(seoForms[0], func(n *html.Node) bool { return n == stack }) != nil {
		t.Fatal("SEO表单应处于未激活的本地模板，不能常驻编辑区")
	}

	reapply := productEditFormByWriteAPI(stack, "/api/product/detail-template/reapply-preset")
	rollback := productEditFormByWriteAPI(stack, "/api/product/detail-template/rollback-document")
	if len(reapply) != 1 || len(rollback) != 1 {
		t.Fatalf("详情页模板应有「重新套用预设」与「回滚文档」两个表单，实际 %d / %d 个", len(reapply), len(rollback))
	}

	for _, fr := range append(append([]*html.Node{}, reapply...), rollback...) {
		if a := peAttr(fr, "action"); a != "" {
			t.Fatalf("详情页模板表单同时声明了 action=%q：原生提交会打到已删除的页面端点", a)
		}
		if !peHasAttr(fr, "data-write-reload") {
			t.Fatalf("两个动作成功后都要整页刷新，结论经 data-write-reload 跨刷新传递（裸属性缺失）")
		}
	}
	tplFold := peUp(reapply[0], func(n *html.Node) bool { return peEl(n, "details") && peClass(n, "section-fold") })
	if tplFold == nil {
		t.Fatal("「重新套用预设」表单应在折叠区内")
	}

	if got := peUp(rollback[0], func(n *html.Node) bool { return peEl(n, "details") && peClass(n, "section-fold") }); got != tplFold {
		t.Fatal("「回滚文档」表单应在详情页模板那一个折叠区内")
	}

	selects := peAll(rollback[0], func(n *html.Node) bool { return peEl(n, "select") && peAttr(n, "name") == "snapshotId" })
	if len(selects) != 1 {
		t.Fatalf("回滚表单应有且只有一个版本下拉 select[name=snapshotId]，实际 %d 个", len(selects))
	}
	if peUp(selects[0], func(n *html.Node) bool { return n == tplFold }) == nil {
		t.Fatal("版本下拉被折叠边界切到了外面：收起时它与按钮不同区，展开后也选不到")
	}
	submitInFold(t, tplFold, "/api/product/detail-template/reapply-preset")
	submitInFold(t, tplFold, "/api/product/detail-template/rollback-document")

	for i, f := range folds {
		for _, n := range peAll(f, func(n *html.Node) bool { return peAttr(n, "form") != "" }) {
			id := peAttr(n, "form")
			inside := peAll(f, func(m *html.Node) bool { return peEl(m, "form") && peAttr(m, "id") == id })
			if len(inside) == 0 {
				t.Fatalf("第 %d 个折叠区内的控件 / 按钮引用了折叠区外的表单 id=%q：折叠边界把表单切开了", i+1, id)
			}
		}
	}
}

func TestProductEditSaveButtonRelatedToOutsideForm(t *testing.T) {
	root, _ := productEditRender(t)
	stack := productEditStack(t, root)

	forms := peAll(stack, func(n *html.Node) bool {
		return peEl(n, "form") && peAttr(n, "id") == "product-edit-main"
	})
	if len(forms) != 1 {
		t.Fatalf("编辑表单应带 id=product-edit-main（页头按钮的关联目标），实际命中 %d 个", len(forms))
	}
	form := forms[0]

	if got := peAttr(form, "hx-post"); got != "/api/product/update" {
		t.Fatalf("编辑表单 hx-post 应为 /api/product/update，实际 %q", got)
	}
	if !peHasAttr(form, "data-write") || peAttr(form, "data-write-builder") != "product-edit" || peAttr(form, "hx-target") != "#product-edit-result" || peAttr(form, "hx-swap") != "innerHTML" {
		t.Fatal("编辑表单应由 HTMX 提交真实对象，成功只更新声明的结果区")
	}

	for _, ctl := range peAll(form, peIsFormControl) {
		if peAttr(ctl, "name") == "csrf_token" {
			t.Fatal("编辑表单不该再有 csrf_token 隐藏域（基座把 token 放请求头）")
		}
	}

	have := map[string]bool{}
	for _, ctl := range peAll(form, peIsFormControl) {
		if name := peAttr(ctl, "name"); name != "" {
			have[name] = true
		}
	}
	for _, want := range productEditProtocolFields {
		if !have[want] {
			t.Fatalf("编辑表单缺少协议字段 name=%q（页头保存按钮提交的就是这份表单）", want)
		}
	}

	btns := peAll(stack, func(n *html.Node) bool {
		return peEl(n, "button") && peAttr(n, "form") == "product-edit-main"
	})
	if len(btns) != 1 {
		t.Fatalf("保存按钮应恰好一个（页头），实际 %d 个", len(btns))
	}
	btn := btns[0]
	if got := strings.ToLower(peAttr(btn, "type")); got != "submit" {
		t.Fatalf("页头保存按钮 type 应为 submit，实际 %q", got)
	}
	if peUp(btn, func(n *html.Node) bool { return n == form }) != nil {
		t.Fatal("保存按钮仍在表单内 —— form= 关联是表单外按钮的机制，写在表单里等于没上提")
	}
	if peUp(btn, func(n *html.Node) bool { return peEl(n, "header") && peClass(n, "page-head") }) == nil {
		t.Fatal("保存按钮应在页头 .page-head 内（页级动作与页头同行）")
	}
	actions := peUp(btn, func(n *html.Node) bool { return peClass(n, "page-actions") })
	if actions == nil {
		t.Fatal("保存按钮应在页头的 .page-actions 里（而不是散在页头其它位置）")
	}

	if got := len(peAll(actions, func(n *html.Node) bool {
		return peEl(n, "button") && strings.EqualFold(peAttr(n, "type"), "submit")
	})); got != 1 {
		t.Fatalf("页头动作行里应只有保存这一个 submit 按钮，实际 %d 个", got)
	}
	if got := len(peAll(form, func(n *html.Node) bool {
		return peEl(n, "button") && strings.EqualFold(peAttr(n, "type"), "submit")
	})); got != 0 {
		t.Fatalf("上提之后表单内不该再有 submit 按钮（同一个动作两个按钮会各自下发一份字段），实际 %d 个", got)
	}
}

func TestProductEditEveryContentCardHasTitle(t *testing.T) {
	root, _ := productEditRender(t)
	stack := productEditStack(t, root)

	var cards []*html.Node
	var collectCards func(n *html.Node)
	collectCards = func(n *html.Node) {
		for _, kid := range peKids(n) {
			if peClass(kid, "card") {
				cards = append(cards, kid)
				continue
			}
			collectCards(kid)
		}
	}
	collectCards(stack)

	const wantCards = 5
	if len(cards) != wantCards {
		t.Fatalf("商品编辑页应有 %d 张内容卡，实际 %d 张", wantCards, len(cards))
	}
	cardTitles, foldTitles := 0, 0
	for i, card := range cards {
		titles := peAll(card, func(n *html.Node) bool { return peClass(n, "card-title") })
		folds := peAll(card, func(n *html.Node) bool {
			if !peClass(n, "fold-title") {
				return false
			}
			return peUp(n, func(a *html.Node) bool { return peEl(a, "summary") }) != nil
		})
		if len(titles)+len(folds) != 1 {
			t.Fatalf("第 %d 张卡应有且只有一个标题（.card-title，或折叠区 summary 里的 .fold-title），实际 .card-title=%d / .fold-title=%d",
				i+1, len(titles), len(folds))
		}
		if len(titles) == 1 && strings.TrimSpace(peText(titles[0])) == "" {
			t.Fatalf("第 %d 张卡的标题是空的", i+1)
		}
		cardTitles += len(titles)
		foldTitles += len(folds)
	}
	if cardTitles+foldTitles != wantCards {
		t.Fatalf("卡片标题元素数（.card-title %d + .fold-title %d）应与卡片数 %d 一致", cardTitles, foldTitles, wantCards)
	}
	if cardTitles < 4 {
		t.Fatalf("本页 .card-title 至少 4 个（基本信息 / 变体 / 评分 / 捆绑构成），实际 %d 个", cardTitles)
	}
}

func submitInFold(t *testing.T, fold *html.Node, api string) {
	t.Helper()
	forms := productEditFormByWriteAPI(fold, api)
	if len(forms) != 1 {
		t.Fatalf("折叠区内应恰好一个 hx-post=%s 的表单，实际 %d 个", api, len(forms))
	}
	btns := peAll(forms[0], func(n *html.Node) bool {
		return peEl(n, "button") && strings.EqualFold(peAttr(n, "type"), "submit")
	})
	if len(btns) != 1 {
		t.Fatalf("折叠区内的 hx-post=%s 表单应恰好一个提交按钮，实际 %d 个", api, len(btns))
	}

	if !peHasAttr(btns[0], "data-requires-js") || !peHasAttr(btns[0], "disabled") || !peHasAttr(btns[0], "hidden") {
		t.Fatalf("data-write-url=%s 的提交按钮缺少 data-requires-js/hidden/disabled", api)
	}
	if peUp(btns[0], func(n *html.Node) bool { return n == fold }) == nil {
		t.Fatalf("data-write-url=%s 的提交按钮不在折叠区内：收起时它就点不到了", api)
	}
}

func peIsFormControl(n *html.Node) bool {
	return peEl(n, "input") || peEl(n, "select") || peEl(n, "textarea")
}

func peEl(n *html.Node, tag string) bool {
	return n.Type == html.ElementNode && n.Data == tag
}

func peHasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

func peAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func peClass(n *html.Node, cls string) bool {
	for _, c := range strings.Fields(peAttr(n, "class")) {
		if c == cls {
			return true
		}
	}
	return false
}

func peKids(n *html.Node) []*html.Node {
	var out []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			out = append(out, c)
		}
	}
	return out
}

func peAll(root *html.Node, pred func(*html.Node) bool) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if pred(n) {
			out = append(out, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return out
}

func peUp(n *html.Node, pred func(*html.Node) bool) *html.Node {
	for p := n.Parent; p != nil; p = p.Parent {
		if pred(p) {
			return p
		}
	}
	return nil
}

func peText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.TextNode {
			b.WriteString(x.Data)
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

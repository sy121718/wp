package templates

// admin_product_edit_structure_test.go — 商品编辑页的三条结构契约（审计 02-L P1-9）。
//
// 为什么这三条要用结构判据而不是「看了一眼没问题」：
//  ① 列表卡标题写成裸 <strong>：能跑、看着也不丑，但 theme.css §13 专门为
//     `.list-card .list-toolbar .card-title` 定的字号 / 字重落不到它身上 —— 与
//     「.fold-title 被列表卡借用时是零样式」是同一类问题（写的时候顺手，样式悄悄丢掉）。
//  ② 低频只读块不折叠：SEO 检查与详情页模板把「改字段」推到很后面（清单记的 2.88 屏）。
//  ③ 保存按钮与表单脱钩：按钮在页头、表单在下面，靠 `form="product-edit-main"` 关联 ——
//     id 打错一个字符，按钮照旧渲染、点下去什么都不发生（页面上完全看不出来）。
//     所以「提交后落库字段与改造前一致」不能靠肉眼看表单，要把协议字段逐个钉死。
//
// 折叠不丢字段是**判据**不是印象：SEO 卡里没有可见输入字段（只有 hidden + 按钮 + 结果容器），
// 详情页模板卡里唯一的真实字段是回滚的版本下拉 —— 它和它的按钮在同一个折叠区内，
// 且折叠区不切开任何表单（区内的 `form=` 关联只能指向区内的表单）。

import (
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
)

// productEditProtocolFields 编辑表单的协议字段（与 /api/product/update 的 UpdateReq
// json 标签逐字对齐，见 product_edit_page.go 的文件头）：改字段名等于改协议，
// 也因此是「页头保存按钮提交的东西与改造前一致」的可复算判据。
var productEditProtocolFields = []string{
	// csrf_token 不在清单里：它从表单隐藏域改成请求头（基座发 X-CSRF-Token），
	// 表单里若又出现它，端点会把它当未知字段整条拒掉。
	"projectId", "id",
	"name", "subtitle", "slug", "sku", "status", "defaultPrice", "unit", "weight",
	// seoTitle / seoDescription 已合并进 name 与 subtitle（2026-09-30），表单不再提交它们 ——
	// 清单挪掉这两项是跟着**行为**走，不是放宽判据：其余字段一个都没少。
	"images", "imageAlts",
	"attributeIds", "categoryIds", "primaryCategoryId", "brandId", "tagIds",
}

// productEditPageData 编辑页渲染数据。与 handler 的装配同形：
// 商品存在 + 捆绑商品 + 详情页模板面板（document 模式、有历史版本）——
// 这样 6 张内容卡与两个折叠区的最全形态都被渲染到，判据才有覆盖面。
func productEditPageData() map[string]any {
	return groupDData(map[string]any{
		"SelectedProject": "pr1", "Err": "", "Done": "",
		"Projects":  groupDProjects(),
		"ProductID": "p1", "HasProduct": true, "Product": groupDProductRow(),
		"BackURL":          "/admin/products?project=pr1",
		"WarehouseOptions": []map[string]any{{"ID": "w1", "Label": "苏州仓"}},
		"AttributeChecks":  []map[string]any{{"ID": "a1", "Label": "颜色（color）", "IsVariation": true, "Checked": true}},
		"ImagesInput":       "https://cdn.example.com/1.jpg",
		"ImageAltsInput":    "图一",
		"WeightInput":       "0.5", "DefaultPriceInput": "10.00",
		// 捆绑商品：多渲染一张 .card（捆绑构成的编辑入口），卡片计数因此是 6。
		"IsBundle": true,
		"TplPanel": map[string]any{
			"Available": true, "InstanceID": "inst1", "TemplateID": "tpl1",
			"Status": "published", "RenderMode": "document",
			"SourceTemplateVersionID": "v1", "CurrentTemplateVersionID": "v2",
			"HasAffectedTemplateCount": true, "AffectedTemplateCount": 3,
			"Snapshots": []map[string]any{{"ID": "s1", "CreatedAt": time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)}},
		},
	})
}

// productEditRender 渲染编辑页并解析成 DOM 树（模板中断会让渲染直接报错，
// 这正是本页最容易出的整页级事故）。
func productEditRender(t *testing.T) (*html.Node, string) {
	t.Helper()
	out, err := render(t, groupDSet(t), "admin/product/product_edit", productEditPageData())
	if err != nil {
		t.Fatalf("商品编辑页渲染失败: %v", err)
	}
	root, err := html.Parse(strings.NewReader(out))
	if err != nil {
		t.Fatalf("商品编辑页输出无法解析为 HTML: %v", err)
	}
	return root, out
}

// productEditStack 定位模板根容器（.stack 且含 page-head 的那个）——
// 判据都在这棵子树里做，避免命中 layout 外壳里的同名元素。
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

// productEditFormByAction 按 action 找页面上的表单（用来定位两块折叠区）。
//
// 写动作迁到 JSON 写基座之后 action 就不再存在了，新页面一律用
// productEditFormByWriteAPI；这个还留着是因为「原生提交」仍是合法形态
// （例如整页 GET 的筛选表单），按 action 找它们没有替代品。
func productEditFormByAction(root *html.Node, action string) []*html.Node {
	return peAll(root, func(n *html.Node) bool {
		return peEl(n, "form") && peAttr(n, "action") == action
	})
}

// productEditFormByWriteAPI 按 data-write-api 找页面上的表单（写动作的定位依据）。
func productEditFormByWriteAPI(root *html.Node, api string) []*html.Node {
	return peAll(root, func(n *html.Node) bool {
		return peEl(n, "form") && peAttr(n, "data-write-url") == api
	})
}

// TestProductEditListCardTitlesUseCardTitle ① 变体 / 评分两张列表卡的标题用 .card-title。
//
// 判据不只是「有 .card-title」，还要求它就在 .list-toolbar 的**直接子**里（theme.css 的选择器是
// `.list-card .list-toolbar .card-title`，套一层 div 就落不到样式），并反向钉住 toolbar 里
// 不再有裸 <strong>（写法漂移的形态本身），以及动作按钮仍在同一个 toolbar 内。
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
		// 动作按钮（新建变体 / 生成组合 / 添加评分）仍在同一个标题行里 —— 改标题不该把动作挤出去。
		if btns := peAll(tb, func(n *html.Node) bool { return peEl(n, "button") }); len(btns) == 0 {
			t.Fatalf("第 %d 个列表卡标题行丢了动作按钮", i+1)
		}
	}
}

// TestProductEditLowFrequencyBlocksFolded ② 低频只读块收进折叠区，且折叠不丢字段。
//
// 「低频只读块」现在只剩**详情页模板**一个：SEO 检查已移入抽屉（页头按钮是入口）——
// 抽屉同样满足这条判据的本意（不常驻、不占版面），只是换了容器。
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

	// SEO 检查已移入抽屉（页头按钮是入口）：这里只校验**入口**在、且评测表单不在页面上。
	// 评测表单与结果容器本身在 admin/product/entity_seo_drawer.html，由模块内的用例守着
	// （那边断言 hx-post / hx-target / 容器 id 三者一致）。
	//
	// 这条替代了原来的「SEO 表单应在折叠区内」：本判据的本意是「低频只读内容不得摊在页面上」，
	// 抽屉满足它，只是换了容器 —— 所以断言入口与「不再常驻」两件事。
	if !strings.Contains(raw, `data-drawer-url="/admin/products/seo/drawer?productId=`) {
		t.Fatal("商品编辑页缺少 SEO 抽屉入口（评测面板的唯一落点）")
	}
	if n := len(productEditFormByAction(stack, "/admin/products/seo-score")); n != 0 {
		t.Fatalf("SEO 评分表单不应再出现在编辑页上（实际 %d 个）：它属于抽屉片段", n)
	}

	// 详情页模板：两个 form-inline（重新套用预设 / 回滚文档）都在同一个折叠区内。
	// 判据从 action 换成 data-write-api：两个动作已迁到 JSON 写基座，表单不再有页面端点。
	reapply := productEditFormByWriteAPI(stack, "/api/product/detail-template/reapply-preset")
	rollback := productEditFormByWriteAPI(stack, "/api/product/detail-template/rollback-document")
	if len(reapply) != 1 || len(rollback) != 1 {
		t.Fatalf("详情页模板应有「重新套用预设」与「回滚文档」两个表单，实际 %d / %d 个", len(reapply), len(rollback))
	}
	// 两份声明并存时浏览器会按原生表单提交，而那条页面端点在服务端已经删了 ——
	// 用户看到的是 404，不是结果。
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
	// （原「两块低频内容不该塞进同一个 details」的判据随 SEO 检查移入抽屉而消失：
	//  折叠区现在只剩这一个，不存在「两块挤在一起」的可能。）
	if got := peUp(rollback[0], func(n *html.Node) bool { return peEl(n, "details") && peClass(n, "section-fold") }); got != tplFold {
		t.Fatal("「回滚文档」表单应在详情页模板那一个折叠区内")
	}
	// 唯一的真实字段：回滚的版本下拉。它必须与它的按钮同处折叠区内（展开后可选、可提交）。
	selects := peAll(rollback[0], func(n *html.Node) bool { return peEl(n, "select") && peAttr(n, "name") == "snapshotId" })
	if len(selects) != 1 {
		t.Fatalf("回滚表单应有且只有一个版本下拉 select[name=snapshotId]，实际 %d 个", len(selects))
	}
	if peUp(selects[0], func(n *html.Node) bool { return n == tplFold }) == nil {
		t.Fatal("版本下拉被折叠边界切到了外面：收起时它与按钮不同区，展开后也选不到")
	}
	submitInFold(t, tplFold, "/api/product/detail-template/reapply-preset")
	submitInFold(t, tplFold, "/api/product/detail-template/rollback-document")

	// 折叠边界不得切开任何表单：折叠区内的 `form=` 关联只能指向**同一个折叠区内**的表单
	// （指向区外的表单意味着字段与按钮分处折叠两侧：看得见按钮时填不了字段，反之亦然）。
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

// TestProductEditSaveButtonRelatedToOutsideForm ③ 主行动（保存）在页头，用 form= 关联表单外的那份表单。
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
	// 提交走 JSON 写基座：端点与字段声明写在 data-write-* 上，模板不再有 action / method。
	if got := peAttr(form, "data-write-url"); got != "/api/product/update" {
		t.Fatalf("编辑表单 hx-post 应为 /api/product/update，实际 %q", got)
	}
	if got := peAttr(form, "data-write"); got == "" {
		t.Fatalf("编辑表单缺少 data-write（写基座必填属性；hx-swap=none 已由基座内置）")
	}
	// csrf 走请求头：表单里再留隐藏域会变成一条未知字段（严格白名单整条拒掉）。
	for _, ctl := range peAll(form, peIsFormControl) {
		if peAttr(ctl, "name") == "csrf_token" {
			t.Fatal("编辑表单不该再有 csrf_token 隐藏域（基座把 token 放请求头）")
		}
	}

	// 字段留在表单里：协议字段逐个钉住（少一个 = 保存不再落那个字段）。
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

	// 页头按钮：恰好一个、type=submit、form 指向该表单，且它**不在**表单内。
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
	// 主行动唯一：同一个动作不该出现两个按钮。
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

// TestProductEditEveryContentCardHasTitle 卡片标题全覆盖（清单记的「.card-title 数」判据）。
//
// 口径说明：本页 6 张内容卡的标题元素是 **4 个 .card-title + 2 个 .fold-title**。
// 折叠区的标题按既有设计用 .fold-title —— theme.css 只在 `.section-fold > summary` 里
// 给它样式，站内已有 5 个页面这么做；字面凑够 6 个 .card-title 只能把标题塞进不该塞的地方。
// 所以这里钉的判据是「每张内容卡都有且只有一个标题元素」，它比数字更直接地表达意图。
func TestProductEditEveryContentCardHasTitle(t *testing.T) {
	root, _ := productEditRender(t)
	stack := productEditStack(t, root)

	// 递归收集「顶层 card」（不再嵌在另一张 card 里）：两栏化之后表单卡与预览卡分别落在
	// .product-edit-panes 与 .product-edit-aside 里，按 stack 的直接子级数会漏掉它们 ——
	// 而漏掉的正是这次改动新增/搬走的东西，断言会静默失去意义。
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
	// 基本信息表单 / 变体 / 评分 / 详情页模板 / 捆绑构成 / 详情页预览（右栏）。
	// SEO 检查改抽屉后不再自占一卡。
	const wantCards = 6
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

// submitInFold 断言某个 action 的表单，与它的提交按钮同处一个折叠区里。
func submitInFold(t *testing.T, fold *html.Node, api string) {
	t.Helper()
	forms := productEditFormByWriteAPI(fold, api)
	if len(forms) != 1 {
		t.Fatalf("折叠区内应恰好一个 data-write-api=%s 的表单，实际 %d 个", api, len(forms))
	}
	btns := peAll(forms[0], func(n *html.Node) bool {
		return peEl(n, "button") && strings.EqualFold(peAttr(n, "type"), "submit")
	})
	if len(btns) != 1 {
		t.Fatalf("折叠区内的 data-write-api=%s 表单应恰好一个提交按钮，实际 %d 个", api, len(btns))
	}
	// 无 JS 时按钮必须是 hidden + disabled：表单没有原生端点，按得动就等于「点了没反应」。
	// 三个都是布尔属性（值为空串），存在性用 peHasAttr 判，不能拿 peAttr 的返回值当判据。
	if !peHasAttr(btns[0], "data-requires-js") || !peHasAttr(btns[0], "disabled") || !peHasAttr(btns[0], "hidden") {
		t.Fatalf("data-write-api=%s 的提交按钮缺少 data-requires-js/hidden/disabled", api)
	}
	if peUp(btns[0], func(n *html.Node) bool { return n == fold }) == nil {
		t.Fatalf("data-write-api=%s 的提交按钮不在折叠区内：收起时它就点不到了", api)
	}
}

// peIsFormControl 表单控件（input / select / textarea）。
func peIsFormControl(n *html.Node) bool {
	return peEl(n, "input") || peEl(n, "select") || peEl(n, "textarea")
}

// ── 下面几个是本文件的 DOM 小工具（pe = product edit，避免与包内既有辅助重名）。 ──

// peEl 判断元素节点且标签名匹配（小写标签名）。
func peEl(n *html.Node, tag string) bool {
	return n.Type == html.ElementNode && n.Data == tag
}

// peHasAttr 属性是否存在。布尔属性（data-requires-js / hidden / disabled）的值是空串，
// 用 peAttr 的返回值判存在会把「有属性」读成「没有」。
func peHasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

// peAttr 取属性值，缺失给空串。
func peAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// peClass 判断元素是否带某个 class（按空白切分的 token 比较，不做子串匹配）。
func peClass(n *html.Node, cls string) bool {
	for _, c := range strings.Fields(peAttr(n, "class")) {
		if c == cls {
			return true
		}
	}
	return false
}

// peKids 直接子元素（跳过文本 / 注释节点）。
func peKids(n *html.Node) []*html.Node {
	var out []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			out = append(out, c)
		}
	}
	return out
}

// peAll 前序收集子树里所有满足 pred 的节点（含 root 自身）。
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

// peUp 最近的满足 pred 的祖先，没有给 nil。
func peUp(n *html.Node, pred func(*html.Node) bool) *html.Node {
	for p := n.Parent; p != nil; p = p.Parent {
		if pred(p) {
			return p
		}
	}
	return nil
}

// peText 递归拼接元素的文本（判标题文案用）。
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

package templates

// admin_control_selection_test.go — 控件选型的模板契约（审计 02-L §3 P2-14、02-O §3 任务 1 与任务 5）。
//
// 这批改的是「控件的选择」本身而不是文案：日期时间用日期时间控件、多维度下拉要分组、
// 字段标签用 label 而不是说明文字类。这里钉的四条都是**结构事实**（type / optgroup / for 指向 /
// 空值占位符形态），源码与渲染任意一侧退化都会被抓到：
//
//	① 时间窗控件是 datetime-local，且每个都被自己的 label 指到；
//	② 实体类型下拉把「结构模板」与「内容实体」两组分开；
//	③ 字段标签是 label（不是被当标签用的 .hint）；
//	④ 空值占位符在同一页里只有一种写法。
//
// 「后端收不收控件提交的格式」不在本文件 —— 那一半在 order 模块：
// internal/module/order/inbound/http/coupon_page_query_test.go（归口转换）
// 与 public/test/order/feature/coupon_window_timezone_test.go（真实 service 落库）。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// adminTemplatePath 短名 → 在 admin/ 下的实际路径（模板已按后端模块分进子目录）。
func adminTemplatePath(t *testing.T, name string) string {
	t.Helper()
	for _, p := range adminTemplateFiles(t) {
		if filepath.Base(p) == name {
			return p
		}
	}
	t.Fatalf("找不到模板 %s（admin/ 及其子目录）", name)
	return ""
}

// adminTemplateSource 读取一份后台模板源码（短名 → 在 admin/ 下递归查找实际路径）。
//
// 模板按后端模块分进了子目录，测试里不该再硬编码那条路径 —— 硬编码的下场是每搬一次文件
// 就要改一片调用点，而改漏的**表现是「读文件失败」而不是「断言失败」**，最容易被当成
// 环境问题糊过去。所以按 basename 递归查找（同包的 adminTemplateFiles）。
func adminTemplateSource(t *testing.T, name string) string {
	t.Helper()
	src, err := os.ReadFile(adminTemplatePath(t, name))
	if err != nil {
		t.Fatalf("读取模板 %s 失败: %v", name, err)
	}
	return string(src)
}

// templateBlock 截取 start 到 end 之间的一段（都按字面量找，找不到直接失败）。
func templateBlock(t *testing.T, src, start, end, what string) string {
	t.Helper()
	i := strings.Index(src, start)
	if i < 0 {
		t.Fatalf("%s：找不到起点 %q", what, start)
	}
	rest := src[i:]
	j := strings.Index(rest, end)
	if j < 0 {
		t.Fatalf("%s：起点之后找不到终点 %q", what, end)
	}
	return rest[:j+len(end)]
}

// controlIDs 取一段 HTML 里所有匹配到的控件 id。
func controlIDs(html string, controlRe *regexp.Regexp) []string {
	var ids []string
	for _, m := range controlRe.FindAllStringSubmatch(html, -1) {
		ids = append(ids, m[1])
	}
	return ids
}

// couponDateTimeLocalRe 匹配一个 datetime-local 输入及其 id。
var couponDateTimeLocalRe = regexp.MustCompile(`<input[^>]*type="datetime-local"[^>]*id="([^"]+)"`)

// couponFormSources coupons.html 与两个抽屉表单片段的拼接源码。
//
// 2026-09「写失败不丢输入」批把新建/编辑抽屉的表单本体迁进了独立片段
// （coupon_create_form.html / coupon_edit_form.html，片段同时是失败重渲染载体），
// 时间窗控件跟着表单走 —— 控件选型与 label 关联的判据对象跟着扩成三个文件，
// 数量判据（4 个时间窗控件）不变，只是分布从单文件变成「页面 0 + 片段 2+2」。
func couponFormSources(t *testing.T) string {
	t.Helper()
	return adminTemplateSource(t, "coupons.html") + "\n" +
		adminTemplateSource(t, "coupon_create_form.html") + "\n" +
		adminTemplateSource(t, "coupon_edit_form.html")
}

// TestCouponWindowInputsUseDateTimeLocal 时间窗必须用原生日期时间控件。
//
// 缺陷形态（审计 02-O §3 任务 1）：四个时间窗输入是 `<input type="text">`，占位符写
// 「…如 2026-01-01 或 2026-01-01 09:00」—— 用户必须记住格式，打错被服务端拒绝且只回页顶一句
// 错误；而同域的 customers.html / analytics.html 早已用原生日期控件，同一域内两种做法并存。
func TestCouponWindowInputsUseDateTimeLocal(t *testing.T) {
	src := couponFormSources(t)

	const want = 4 // 编辑抽屉 2（开始 / 结束）+ 新建抽屉 2
	if got := strings.Count(src, `type="datetime-local"`); got != want {
		t.Fatalf("时间窗应有 %d 个 datetime-local 控件，实际 %d", want, got)
	}

	// 每个控件都要带 step="1"：回填值可能含秒（库里已有的券），默认 step=60 会把带秒的值
	// 判成 stepMismatch —— 浏览器拦在提交前，**服务端一行日志都没有**（Chrome 153 实测）。
	if got := strings.Count(src, `type="datetime-local" step="1"`); got != want {
		t.Fatalf("%d 个时间窗控件都必须带 step=\"1\"（否则带秒的回填值会让表单提交不了），实际 %d",
			want, got)
	}

	// 逐行核对：凡 name=startsAt / endsAt 的**可见**控件都必须是日期时间控件
	//（隐藏域是另一回事：行内启停表单要原样回送时间窗，它不受控件选型约束）。
	for _, line := range strings.Split(src, "\n") {
		if !strings.Contains(line, `name="startsAt"`) && !strings.Contains(line, `name="endsAt"`) {
			continue
		}
		if strings.Contains(line, `type="hidden"`) {
			continue
		}
		if !strings.Contains(line, `type="datetime-local"`) {
			t.Errorf("时间窗字段仍是文本输入（用户要自己记格式）: %s", strings.TrimSpace(line))
		}
	}
}

// TestCouponWindowInputsAreLabelled 每个 datetime-local 都要有 label 的 for 指到它。
//
// 为什么这条和时间窗绑在一起：datetime-local **不渲染 placeholder**（浏览器忽略它），
// 而新建抽屉的字段说明一贯靠 placeholder —— 不给可见 label，那一行就只剩两个看不出
// 「开始 / 结束」的框；同时 for 必须真的指到控件（裸 label 与控件是兄弟节点，隐式关联不成立）。
func TestCouponWindowInputsAreLabelled(t *testing.T) {
	src := couponFormSources(t)
	ids := controlIDs(src, couponDateTimeLocalRe)
	if len(ids) != 4 {
		t.Fatalf("应能解析出 4 个 datetime-local 的 id，实际 %d", len(ids))
	}
	for _, id := range ids {
		if !strings.Contains(src, `for="`+id+`"`) {
			t.Errorf("控件 id=%s 没有 label 的 for 指到它（读屏与脚本都拿不到字段名）", id)
		}
	}
}

// TestCouponEditDrawerEveryFieldIsLabelled 编辑抽屉里每个可见控件都被 label 关联。
//
// 缺陷形态（审计 02-O §3 任务 5）：maxUses / perUserLimit 只有 placeholder（「总次数上限
// （0 = 不限）」/「每人限次（0 = 不限）」），而同抽屉其它字段都有 label —— 两个都是数字、单位
// 都是次，填完值回看时 placeholder 已经消失，分不清哪个是哪个。remark 同病。
// 本用例同时挡住「补了 label 但没接 for」这种半成品：抽屉里所有可见控件一律要有 id 且被指到。
// 判据对象原是 coupons.html 的 template 块，表单本体迁进片段后改为片段全文（结构与契约不变）。
func TestCouponEditDrawerEveryFieldIsLabelled(t *testing.T) {
	drawer := adminTemplateSource(t, "coupon_edit_form.html")

	// 可见控件（hidden 不在此列：它们由其它表单提交，不需要字段名提示）。
	controlRe := regexp.MustCompile(`<(?:input|select)[^>]*\bname="([a-zA-Z]+)"[^>]*>`)
	var missing []string
	for _, m := range controlRe.FindAllStringSubmatch(drawer, -1) {
		tag := m[0]
		if strings.Contains(tag, `type="hidden"`) {
			continue
		}
		idRe := regexp.MustCompile(`\bid="([^"]+)"`)
		idMatch := idRe.FindStringSubmatch(tag)
		if idMatch == nil {
			missing = append(missing, "name="+m[1]+"（没有 id）")
			continue
		}
		if !strings.Contains(drawer, `for="`+idMatch[1]+"\"") {
			missing = append(missing, "name="+m[1]+"（id="+idMatch[1]+" 没有 label 指到它）")
		}
	}
	if len(missing) > 0 {
		t.Fatalf("编辑抽屉里这些字段没有可关联的 label：%s", strings.Join(missing, "、"))
	}

	// 只读的券码字段也在这条契约里（它同样需要一个字段名）。
	// id 里的 {{ceId}} 是片段的局部变量（首屏=行 id、失败档=提交的 id，两条路径同一个 id 池）。
	if !strings.Contains(drawer, `for="coupon-{{ceId}}-code"`) {
		t.Error("只读的券码字段缺少 label 关联")
	}
}

// TestCouponCreateDrawerWindowFieldsAreLabelled 新建抽屉的时间窗字段有可见 label。
// 判据对象原是 coupons.html 的 template 块，表单本体迁进片段后改为片段全文。
func TestCouponCreateDrawerWindowFieldsAreLabelled(t *testing.T) {
	drawer := adminTemplateSource(t, "coupon_create_form.html")

	for _, id := range []string{"coupon-create-starts-at", "coupon-create-ends-at"} {
		if !strings.Contains(drawer, `id="`+id+`"`) {
			t.Fatalf("新建抽屉缺少控件 id=%s", id)
		}
		if !strings.Contains(drawer, `for="`+id+`"`) {
			t.Errorf("新建抽屉的 %s 没有 label 指到它（datetime-local 不渲染 placeholder，没有 label 就没有字段名）", id)
		}
	}
}

// TestContentTemplateEntityTypeSelectGroupsDimensions 实体类型下拉把两个维度分开。
//
// 缺陷形态（审计 02-L §3 P2-14）：5 个 option 平铺 ——「页眉（结构模板）」「页脚（结构模板）」
// 与「product」「article」在同一个平面列表里。前者没有样例实体、不接受字段绑定，可编辑性与
// 删除后果都不一样，平铺时用户看不出它们为什么会在一张下拉里。
func TestContentTemplateEntityTypeSelectGroupsDimensions(t *testing.T) {
	src := adminTemplateSource(t, "content_templates.html")
	sel := templateBlock(t, src, `<select class="form-select" id="ct-entity-type"`, "</select>", "实体类型下拉")

	if got := strings.Count(sel, "<optgroup"); got != 2 {
		t.Fatalf("实体类型下拉应有 2 个 optgroup（结构模板 / 内容实体），实际 %d", got)
	}
	if got := strings.Count(sel, "</optgroup>"); got != 2 {
		t.Fatalf("optgroup 开闭不配平：%d 个闭合标签", got)
	}

	structure := templateBlock(t, sel, `<optgroup label="{{ .["t"]("admin.content.templates.entityGroupStructure"`,
		"</optgroup>", "结构模板分组")
	for _, v := range []string{`value="header"`, `value="footer"`} {
		if !strings.Contains(structure, v) {
			t.Errorf("结构模板分组里缺少 %s", v)
		}
	}
	content := templateBlock(t, sel, `<optgroup label="{{ .["t"]("admin.content.templates.entityGroupContent"`,
		"</optgroup>", "内容实体分组")
	for _, v := range []string{`value="product"`, `value="article"`} {
		if !strings.Contains(content, v) {
			t.Errorf("内容实体分组里缺少 %s", v)
		}
	}

	// 「全部」不属于任何一组：它必须留在第一个 optgroup 之前。
	allIdx := strings.Index(sel, `value=""`)
	firstGroup := strings.Index(sel, "<optgroup")
	if allIdx < 0 || allIdx > firstGroup {
		t.Error("「全部」选项必须在 optgroup 之外（它是维度选择本身，不属于任何一个分组）")
	}
}

// TestProductPricingFieldLabelsAreNotHints 字段标签用 label，不是被当标签用的 .hint。
//
// 缺陷形态（审计 02-L §3 P1-21）：`.hint` 是「说明文字」类（margin:0、muted、14px 级，
// 见 ui.css），拿它当字段标签时读屏只能靠包裹关系猜，脚本也定位不到（没有 for）。
func TestProductPricingFieldLabelsAreNotHints(t *testing.T) {
	src := adminTemplateSource(t, "product_pricing.html")
	for _, key := range []string{
		"admin.product_pricing.apply.labelScope",
		"admin.product_pricing.apply.labelFilter",
	} {
		line := templateLineWith(t, src, key)
		if strings.Contains(line, `class="hint"`) {
			t.Errorf("%s 仍是 .hint 当字段标签: %s", key, strings.TrimSpace(line))
		}
		if !strings.Contains(line, "<label") || !strings.Contains(line, `class="form-label"`) {
			t.Errorf("%s 的标签应为 <label class=\"form-label\">: %s", key, strings.TrimSpace(line))
		}
		if !strings.Contains(line, `for="`) {
			t.Errorf("%s 的 label 没有 for（裸 label 与控件是兄弟节点，隐式关联不成立）: %s",
				key, strings.TrimSpace(line))
		}
	}
}

// templateLineWith 取含某字面量的那一行（找不到直接失败）。
func templateLineWith(t *testing.T, src, needle string) string {
	t.Helper()
	for _, line := range strings.Split(src, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	t.Fatalf("模板里找不到 %q", needle)
	return ""
}

// TestCatalogEmptyCellPlaceholderIsConsistent 空值占位符在同一页里只有一种写法。
//
// 缺陷形态（审计 02-L §3 P2-11）：product_categories.html 的 SEO 标题列写「未填」，
// 同一张表的 Slug 列写「—」；product_brands.html 同病。两页都渲染一次，断言：
//   - SEO 标题为空的行渲染出与 Slug 列**逐字相同**的占位（同一个 span + 同一个字符）；
//   - 渲染结果里不再出现「未填」。
func TestCatalogEmptyCellPlaceholderIsConsistent(t *testing.T) {
	const placeholder = `<span class="text-mute">—</span>`

	brands := assertGroupDPage(t, "product_brands", groupDData(map[string]any{
		"Err": "", "SelectedProject": "pr1", "Projects": groupDProjects(),
		"Brands": []map[string]any{{
			// Slug 与 SEOTitle 都为空：两列的空值形态必须一样。
			"ID": "b1", "Name": "山野", "Slug": "", "Sort": 0, "Logo": "",
			"SEOTitle": "", "SEODescription": "", "Description": "",
		}},
	}), "商品品牌")
	assertEmptyCellPlaceholder(t, "product_brands", brands, placeholder)

	categories := assertGroupDPage(t, "product_categories", groupDData(map[string]any{
		"Err": "", "SelectedProject": "pr1", "Projects": groupDProjects(),
		"Options": []map[string]any{{"ID": "c1", "Label": "男装"}},
		"Categories": []map[string]any{{
			"ID": "c1", "Label": "男装", "Slug": "", "Sort": 0, "SEOTitle": "",
			"ParentID": "", "Name": "男装", "Image": "", "SEODescription": "", "Description": "",
		}},
	}), "分类树")
	assertEmptyCellPlaceholder(t, "product_categories", categories, placeholder)
}

// assertEmptyCellPlaceholder 一页里「Slug 空 + SEO 标题空」两处占位形态一致。
func assertEmptyCellPlaceholder(t *testing.T, name, out, placeholder string) {
	t.Helper()
	if strings.Contains(out, "未填") {
		t.Errorf("%s 仍渲染出「未填」—— 同一张表里两种空值写法会让人以为它们代表不同的状态", name)
	}
	// 两列（slug / seo title）各一处，形态逐字相同。
	if got := strings.Count(out, placeholder); got < 2 {
		t.Errorf("%s 的空值占位应有两处（slug 列与 SEO 标题列），实际匹配 %d 处", name, got)
	}
}

// —— 渲染级：源码扫描看不出 Jet 表达式**求值之后**的结果 ——
//
// `id="coupon-{{r.Form.ID}}-starts-at"` 与 `for="coupon-{{r.Form.ID}}-starts-at"` 是两个
// 互不相干的表达式，写岔了（少一个连字符 / 换一个词）源码扫描一样是绿的，而浏览器里
// 点 label 不会聚焦控件、读屏读不到字段名。所以控件与 label 的配平必须在**渲染产物**上核对。

// adminRenderSet 渲染一个后台页面（整目录文件系统加载器，与其它模板测试同一档）。
func renderAdminPage(t *testing.T, name string, data map[string]any) string {
	t.Helper()
	out, err := render(t, groupDSet(t), "admin/"+name, data)
	if err != nil {
		t.Fatalf("渲染 %s 失败: %v", name, err)
	}
	if strings.Contains(out, "{{") {
		t.Fatalf("%s 输出残留模板语法字面量（Jet 在中间某行中断，后续整块消失）", name)
	}
	return out
}

// couponPageData 优惠码列表页（含 1 行数据 → 编辑抽屉模板会渲染出来）。
//
// 键清单照 coupon_page_view.go 的 row / edit 视图给全：这是「直接渲染模板」的必付成本 ——
// 缺键会让 Jet 在那一行中断，而症状是「后半截页面凭空消失」（HTTP 仍是 200）。
func couponPageData() map[string]any {
	form := map[string]any{
		"ID": "7", "Name": "双十一全场券", "DiscountType": "percent", "DiscountValue": int64(20),
		"MinSubtotal": int64(9900), "MaxUses": 100, "PerUserLimit": 1,
		"StartsAt": "2026-01-01T09:00", "EndsAt": "2026-01-31T23:59",
		"Remark": "国庆活动", "StatusValue": "1",
	}
	return groupDData(map[string]any{
		"SelectedProject": "pr1",
		"Projects":        groupDProjects(),
		"PermSet":         map[string]any{"order:coupon_create": true, "order:coupon_update": true},
		"Err":             "", "Ok": "", "Done": "",
		"Total": 1, "CreateBack": "project=pr1",
		"FilterOptions": []map[string]any{{"Value": "1", "Label": "生效中"}},
		"TypeOptions":   []map[string]any{{"Value": "percent", "Label": "按比例"}, {"Value": "fixed", "Label": "固定金额"}},
		"StatusOptions": []map[string]any{{"Value": "1", "Label": "启用"}, {"Value": "0", "Label": "停用"}},
		"HasDetail":     false, "Detail": map[string]any{}, "Redemptions": []map[string]any{},
		"RedemptionTotal": 0, "RedemptionLimit": 20,
		"Rows": []map[string]any{{
			"Code": "SAVE20", "Name": "双十一全场券", "DiscountLabel": "减 20%",
			"MinSubtotalLabel": "99.00", "UsageLabel": "3 / 100", "PerUserLabel": "1",
			"WindowLabel": "2026-01-01 09:00 ~ 2026-01-31 23:59", "StatusLabel": "生效中",
			"Badge": "badge-success", "Remark": "国庆活动", "EditURL": "/admin/coupons?couponId=7",
			"CollapseURL": "/admin/coupons", "Expanded": false, "ToggleStatus": "0", "ToggleLabel": "停用",
			"Form": form, "Back": "project=pr1",
		}},
	})
}

// TestCouponPageRendersDateTimeControlsWithMatchingLabels 按需编辑后，列表仅下发新建抽屉；
// 编辑抽屉由 /admin/coupons/edit 返回独立片段，不能把旧 4 控件断言降格为 2 控件。
func couponEditFragmentData() map[string]any {
	echo := map[string]any{"id": "7", "projectId": "pr1", "returnQuery": "project=pr1", "name": "双十一全场券",
		"discountType": "percent", "discountValue": "20", "minSubtotal": "99", "maxUses": "100",
		"perUserLimit": "1", "status": "1", "startsAt": "2026-01-01T09:00",
		"endsAt": "2026-01-31T23:59", "remark": "国庆活动"}
	return groupDData(map[string]any{"FormEcho": echo, "EditCode": "SAVE20",
		"TypeOptions": []any{}, "StatusOptions": []any{}, "SubmitErr": ""})
}

func TestCouponPageRendersDateTimeControlsWithMatchingLabels(t *testing.T) {
	page := renderAdminPage(t, "coupons.html", couponPageData())
	createIDs := controlIDs(page, couponDateTimeLocalRe)
	if len(createIDs) != 2 {
		t.Fatalf("列表页应只下发新建抽屉的两个时间控件，实际 %d", len(createIDs))
	}
	if !strings.Contains(page, `data-drawer-url=`) {
		t.Fatal("编辑入口没有按需加载地址")
	}
	for _, id := range []string{"coupon-create-starts-at", "coupon-create-ends-at"} {
		if !strings.Contains(page, `id="`+id+`"`) || !strings.Contains(page, `for="`+id+`"`) {
			t.Errorf("新建抽屉时间控件 %s 的 label 未关联", id)
		}
	}

	edit := renderAdminPage(t, "coupon_edit_form.html", couponEditFragmentData())
	for _, id := range []string{"coupon-7-starts-at", "coupon-7-ends-at"} {
		if !strings.Contains(edit, `id="`+id+`"`) || !strings.Contains(edit, `for="`+id+`"`) {
			t.Errorf("编辑片段时间控件 %s 的 label 未关联", id)
		}
	}
}

// TestCouponPageRendersLabelsForNumberAndRemarkFields 编辑字段随按需片段交付，逐项核对 label。
func TestCouponPageRendersLabelsForNumberAndRemarkFields(t *testing.T) {
	out := renderAdminPage(t, "coupon_edit_form.html", couponEditFragmentData())
	for _, name := range []string{"maxUses", "perUserLimit", "remark"} {
		if !strings.Contains(out, `for="coupon-7-`+dashName(name)+`"`) {
			t.Errorf("字段 %s 的 label 没有 for 关联（按需片段渲染）", name)
		}
	}
	for _, want := range []string{"总次数上限（0 = 不限）", "每人限次（0 = 不限）"} {
		if !strings.Contains(out, want) {
			t.Errorf("编辑片段缺少字段标签文案 %q", want)
		}
	}
}

// TestContentTemplatesPageRendersGroupedEntitySelect 渲染后 optgroup 的 label 真的出来了。
func TestContentTemplatesPageRendersGroupedEntitySelect(t *testing.T) {
	data := groupDData(map[string]any{
		"Ready": true, "SelectedProject": "pr1", "Projects": groupDProjects(),
		"EntityType": "", "Err": "", "ImpactNote": "", "ImpactAvailable": true,
		"LoadFailed": false, "TemplateCount": 1,
		"Templates": []map[string]any{{
			"ID": "t1", "Name": "商品详情", "TypeLabel": "商品", "EntityType": "product",
			"IsStructure": false, "RoleLabel": "默认", "DraftVersion": 1, "IsDefault": true,
			"RefCount": 0, "RefPageCount": 0, "RefInstanceCount": 0,
			"RefPages": []map[string]any{}, "RefInstances": []map[string]any{},
			"SampleErr": "", "EditURL": "", "UpdatedAt": "2026-01-01",
		}},
	})
	out := renderAdminPage(t, "content_templates.html", data)

	if got := strings.Count(out, "<optgroup"); got != 2 {
		t.Fatalf("渲染后应有 2 个 optgroup，实际 %d", got)
	}
	// label 走 t() —— 未初始化 i18n 时取模板内原文，所以这里断言的是「标签真的渲染出来了」。
	for _, want := range []string{
		`<optgroup label="结构模板">`,
		`<optgroup label="内容实体">`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("渲染结果缺少分组标签 %s", want)
		}
	}
}

// dashName 字段名 → 模板里 id 后缀的连字符形式（maxUses → max-uses）。
func dashName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r >= 'A' && r <= 'Z' {
			b.WriteByte('-')
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

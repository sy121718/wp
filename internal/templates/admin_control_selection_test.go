package templates

import (
	"io/fs"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	productdto "go_wp/internal/module/product/dto"
	"golang.org/x/net/html"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// adminTemplatePath 按文件名在应用与 pkg/view/ui 两层源里找模板逻辑键。
func adminTemplatePath(t *testing.T, name string) string {
	t.Helper()
	for _, layer := range templateLayers(t) {
		var found string
		err := fs.WalkDir(layer.fsys, "admin", func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil || d.IsDir() {
				return nil
			}
			if filepath.Base(p) == name && found == "" {
				found = p
			}
			return nil
		})
		if err != nil {
			t.Fatalf("遍历 %s 失败: %v", layer.name, err)
		}
		if found != "" {
			return found
		}
	}
	t.Fatalf("找不到模板 %s（admin/ 及其子目录，含 pkg/view/ui）", name)
	return ""
}

func adminTemplateSource(t *testing.T, name string) string {
	t.Helper()
	return layeredTemplateSource(t, adminTemplatePath(t, name))
}

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

func controlIDs(html string, controlRe *regexp.Regexp) []string {
	var ids []string
	for _, m := range controlRe.FindAllStringSubmatch(html, -1) {
		ids = append(ids, m[1])
	}
	return ids
}

var couponDateTimeLocalRe = regexp.MustCompile(`<input[^>]*type="datetime-local"[^>]*id="([^"]+)"`)

func couponFormSources(t *testing.T) string {
	t.Helper()
	return adminTemplateSource(t, "coupons.html") + "\n" +
		adminTemplateSource(t, "coupon_create_form.html") + "\n" +
		adminTemplateSource(t, "coupon_edit_form.html")
}

func TestCouponWindowInputsUseDateTimeLocal(t *testing.T) {
	src := couponFormSources(t)

	const want = 4
	if got := strings.Count(src, `type="datetime-local"`); got != want {
		t.Fatalf("时间窗应有 %d 个 datetime-local 控件，实际 %d", want, got)
	}

	if got := strings.Count(src, `type="datetime-local" step="1"`); got != want {
		t.Fatalf("%d 个时间窗控件都必须带 step=\"1\"（否则带秒的回填值会让表单提交不了），实际 %d",
			want, got)
	}

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

func TestCouponEditDrawerEveryFieldIsLabelled(t *testing.T) {
	drawer := adminTemplateSource(t, "coupon_edit_form.html")

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

	if !strings.Contains(drawer, `for="coupon-{{id}}-code"`) {
		t.Error("只读的券码字段缺少 label 关联")
	}
}

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

	allIdx := strings.Index(sel, `value=""`)
	firstGroup := strings.Index(sel, "<optgroup")
	if allIdx < 0 || allIdx > firstGroup {
		t.Error("「全部」选项必须在 optgroup 之外（它是维度选择本身，不属于任何一个分组）")
	}
}

func TestProductPricingFieldLabelsAreNotHints(t *testing.T) {
	src := adminTemplateSource(t, "product_pricing.html")
	for _, key := range []string{
		"prApplyScope",
		"prApplyFlt",
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

func TestCatalogEmptyCellPlaceholderIsConsistent(t *testing.T) {
	const placeholder = `<span class="text-mute">—</span>`
	brands := assertGroupDPage(t, "product_brands", taxonomyBrands(&productdto.BrandResp{ID: "b1", Name: "山野"}), "商品品牌")
	assertEmptyCellPlaceholder(t, "product_brands", brands, placeholder)
	categories := assertGroupDPage(t, "product_categories", taxonomyCategories(&productdto.CategoryResp{ID: "c1", ProjectID: "pr1", Name: "男装"}), "分类树")
	assertEmptyCellPlaceholder(t, "product_categories", categories, placeholder)
}

func assertEmptyCellPlaceholder(t *testing.T, name, out, placeholder string) {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	for _, cell := range peAll(doc, func(n *html.Node) bool { return peEl(n, "td") }) {

		if peUp(cell, func(n *html.Node) bool { return peEl(n, "template") }) != nil {
			continue
		}
		if strings.TrimSpace(peText(cell)) == "未填" {
			t.Errorf("%s 列表空值仍使用未填", name)
		}
	}

	if got := strings.Count(out, placeholder); got < 1 {
		t.Errorf("%s 的空值占位应有一处（slug 列），实际匹配 %d 处", name, got)
	}
}

func renderAdminPage(t *testing.T, name string, data map[string]any) string {
	t.Helper()
	out, err := render(t, groupDSet(t), "admin/"+name, data, groupDRenderScope())
	if err != nil {
		t.Fatalf("渲染 %s 失败: %v", name, err)
	}
	if strings.Contains(out, "{{") {
		t.Fatalf("%s 输出残留模板语法字面量（Jet 在中间某行中断，后续整块消失）", name)
	}
	return out
}

func couponPageData() map[string]any {

	starts := time.Date(2026, 1, 1, 9, 0, 0, 0, time.Local)
	ends := time.Date(2026, 1, 31, 23, 59, 0, 0, time.Local)
	return groupDData(map[string]any{
		"menu": "coupons", "Projects": groupDProjects(), "SelectedProject": "pr1",

		"Coupons": []map[string]any{{
			"ID": uint64(7), "ProjectID": "pr1", "Code": "SAVE20", "Name": "双十一全场券",
			"DiscountType": "percent", "DiscountValue": int64(20), "MinSubtotal": int64(9900),
			"MaxUses": 100, "UsedCount": 3, "PerUserLimit": 1,
			"StartsAt": &starts, "EndsAt": &ends, "Status": 1, "State": "enabled",
			"Remark": "国庆活动",
		}},
		"Total": int64(1),

		"StatusValues": []string{"enabled", "disabled", "expired", "exhausted"},
		"StateLabels": map[string]map[string]any{
			"enabled":     {"Key": "admin.coupons.status.enabled", "Fallback": "生效中"},
			"disabled":    {"Key": "admin.coupons.status.disabled", "Fallback": "已停用"},
			"expired":     {"Key": "admin.coupons.status.expired", "Fallback": "已过期"},
			"not_started": {"Key": "admin.coupons.status.not_started", "Fallback": "未开始"},
			"exhausted":   {"Key": "admin.coupons.status.exhausted", "Fallback": "已用完"},
		},
		"FilterStatus": "", "FilterStatusValid": false,
		"LoadErr": "",

		"ListQuery": "project=pr1", "Echo": url.Values{}, "SubmitErr": "",

		"Expanded": map[string]any{
			"ID": uint64(7), "Code": "SAVE20", "Name": "双十一全场券", "State": "enabled",
			"DiscountType": "percent", "DiscountValue": int64(20), "MinSubtotal": int64(9900),
			"MaxUses": 100, "UsedCount": 3, "PerUserLimit": 1, "Status": 1, "Remark": "国庆活动",
			"StartsAt": &starts, "EndsAt": &ends,
		},
		"Redemptions": []map[string]any{{
			"ID": uint64(9), "OrderNo": "SO20260101001", "UserID": uint64(42),
			"DiscountAmount": int64(2000), "CreateTime": starts,
		}},
		"RedemptionTotal": int64(1), "RedemptionLimit": 50,

		"Buttons": map[string]any{"order.coupon_create": true, "order.coupon_update": true},
	})
}

func couponEditFragmentData() map[string]any {
	starts := time.Date(2026, 1, 1, 9, 0, 0, 0, time.Local)
	ends := time.Date(2026, 1, 31, 23, 59, 0, 0, time.Local)
	return groupDData(map[string]any{

		"Coupon": map[string]any{
			"ID": uint64(7), "ProjectID": "pr1", "Code": "SAVE20", "Name": "双十一全场券",
			"DiscountType": "percent", "DiscountValue": int64(20), "MinSubtotal": int64(9900),
			"MaxUses": 100, "PerUserLimit": 1, "Status": 1, "Remark": "国庆活动",
			"StartsAt": &starts, "EndsAt": &ends,
		},
		"Echo": url.Values{}, "SubmitErr": "", "ListQuery": "project=pr1",
	})
}

func TestCouponPageRendersDateTimeControlsWithMatchingLabels(t *testing.T) {
	page := renderAdminPage(t, "coupons.html", couponPageData())
	createIDs := controlIDs(page, couponDateTimeLocalRe)
	if len(createIDs) != 2 {
		t.Fatalf("列表页应只下发新建抽屉的两个时间控件，实际 %d", len(createIDs))
	}
	if !strings.Contains(page, `data-drawer-open hx-target="#admin-drawer-body" hx-swap="innerHTML" hx-get=`) {
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

func TestContentTemplatesPageRendersGroupedEntitySelect(t *testing.T) {
	data := groupDData(map[string]any{
		"Ready": true, "SelectedProject": "pr1", "Projects": groupDProjects(),
		"EntityType": "", "Err": "", "ImpactNote": "", "ImpactAvailable": true,
		"LoadFailed": false, "TemplateCount": 1,

		"Templates": []contenttemplatedto.TemplateListRowResp{{
			ID: "t1", Name: "商品详情", EntityType: "product",
			Type:        contenttemplateenums.EntityTypeLabel("product"),
			Role:        contenttemplateenums.TemplateRoleLabel(""),
			IsStructure: false, DraftVersion: 1, IsDefault: true,
			RefPages:     []contenttemplatedto.TemplatePageRefResp{},
			RefInstances: []contenttemplatedto.TemplateInstanceRefResp{},
		}},
	})
	out := renderAdminPage(t, "content_templates.html", data)

	if got := strings.Count(out, "<optgroup"); got != 2 {
		t.Fatalf("渲染后应有 2 个 optgroup，实际 %d", got)
	}

	for _, want := range []string{
		`<optgroup label="结构模板">`,
		`<optgroup label="内容实体">`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("渲染结果缺少分组标签 %s", want)
		}
	}
}

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

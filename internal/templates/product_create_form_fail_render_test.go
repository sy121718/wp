package templates

// product_create_form_fail_render_test.go — 商品建表单「提交失败原地回填」的渲染契约。
//
// 片段（admin/product/product_create_form.html）在两条入口渲染：新建整页与列表页抽屉，
// 且**提交失败时由 handler 单独渲染它自己**。三件事必须真的渲出来，否则表现为
// 「保存失败后回填没了 / 表单被整页替换掉」，而模板层没有任何报错：
//   · host 容器 + htmx 三分属性（hx-post / hx-target=closest / hx-swap=outerHTML）——
//     缺任何一个，htmx 就不会替换「那一个单元」，失败页面变成半旧半新；
//   · 错误槽（role=alert）在表单**外**、host 内 —— 放进表单里会被下一次提交一起换掉；
//   · 回填值真的落到 value / selected / checked 上，且**多选只勾命中项**
//     （用「该字段提交过」判会一次勾满整组）。
//
// 首屏（无 FormEcho / SubmitErr）必须仍是空表单 + 无错误槽：这是 cfFill 开关的契约。

import (
	"strings"
	"testing"
)

// productCreateFailData 模拟「提交失败后由 handler 重渲染片段」的那份 data。
func productCreateFailData() map[string]any {
	return groupDData(map[string]any{
		"SelectedProject": "pr1", "Err": "",
		"Projects": groupDProjects(),
		"WarehouseOptions": []map[string]any{
			{"ID": "w1", "Label": "苏州仓", "Code": "SZ", "IsDefault": true},
			{"ID": "w2", "Label": "杭州仓", "Code": "HZ", "IsDefault": false},
		},
		"AttributeOptions": []map[string]any{
			{"ID": "a1", "Label": "颜色", "IsVariation": true},
			{"ID": "a2", "Label": "尺码", "IsVariation": false},
		},
		"WarehouseSKUOptions": []map[string]any{{
			"WarehouseID": "w1", "WarehouseCode": "SZ", "Label": "苏州仓", "IsDefault": true,
			"Items": []map[string]any{{"SKUCode": "SZ_TEE_001", "Label": "T恤", "ExternalSKU": "E1"}},
		}},
		"TemplatesAvail": false,
		// products_new.html 用点号取这两个键（`{{defaultTplID := .DefaultTemplateID}}`）——
		// 直接渲染模板时缺键会让 Jet 在那一行中断，症状是「页面后半截凭空消失」。
		"Templates": []map[string]any{}, "DefaultTemplateID": "",
		"SubmitErr": "捆绑商品必须填写套餐价",
		"FormEcho": map[string]any{
			"projectId": "pr1", "name": "半填的商品", "type": "bundle", "slug": "half",
			"skuSource": "custom", "sku": "HALF_B", "warehouseSku": "", "externalSku": "EXT-1",
			"defaultPrice": "19.90", "quantity": "3", "trackQuantity": "1",
			"attributeIds": "a1", "warehouseIds": "w1",
		},
		"FormEchoChecked": map[string]any{"trackQuantity": true, "attributeIds": true, "warehouseIds": true},
		// 这次提交里只勾了 a1 / w1：另一个选项必须**不**被勾上。
		"FormEchoMulti": map[string]any{
			"attributeIds": []string{"a1"},
			"warehouseIds": []string{"w1"},
		},
	})
}

// 首屏数据：handler 不注入 FormEcho* 三键，片段以 cfFill 开关走空表单分支。
func productCreateFirstPaintData() map[string]any {
	d := productCreateFailData()
	delete(d, "SubmitErr")
	delete(d, "FormEcho")
	delete(d, "FormEchoChecked")
	delete(d, "FormEchoMulti")
	return d
}

func TestProductCreateFormRendersFailFragment(t *testing.T) {
	out := renderAdminPage(t, "product/products_new", productCreateFailData())

	// ① htmx 分档三分属性 + host 容器（host 在 hx-target 里也出现一次，故 ≥2）。
	for _, want := range []string{
		`hx-post="/admin/products/create"`,
		`hx-target="closest [data-product-create-host]"`,
		`hx-swap="outerHTML"`,
		`<div data-product-create-host>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("失败片段应渲染 %s，实际未见", want)
		}
	}
	if got := strings.Count(out, "data-product-create-host"); got < 2 {
		t.Errorf("data-product-create-host 出现 %d 次，want ≥2（容器 + hx-target 选择器）", got)
	}
	// 原生回退仍在：没有 JS 时表单照旧 POST 到 action。
	if !strings.Contains(out, `action="/admin/products/create"`) {
		t.Error("表单丢了原生 action —— 无 JS 环境（htmx 未加载）下会提交不动")
	}

	// ② 错误槽：在表单外（host 内），且带 role=alert。
	if !strings.Contains(out, `role="alert"`) || !strings.Contains(out, "捆绑商品必须填写套餐价") {
		t.Error("失败片段应带 role=alert 的错误槽与文案")
	}
	// 判据锚在**本片段自己的**两个标记上，不能用页面里第一个 `<form>`
	//（layout 自己也可能有表单，用它当基准会把「位置对不对」判成随机结果）。
	hostAt := strings.Index(out, "<div data-product-create-host>")
	errAt := strings.Index(out, "data-create-err")
	formAt := strings.Index(out, "data-product-create-form")
	if hostAt < 0 || errAt < 0 || formAt < 0 {
		t.Fatalf("片段标记缺失：host=%d err=%d form=%d", hostAt, errAt, formAt)
	}
	if !(hostAt < errAt && errAt < formAt) {
		t.Error("错误槽必须在 host 内、建表单之前 —— 放进表单里会被下一次提交整块换掉")
	}

	// ③ 单值回填。
	for _, want := range []string{
		`value="半填的商品"`,                    // name
		`value="HALF_B"`,                   // sku
		`value="EXT-1"`,                    // externalSku
		`value="19.90"`,                    // defaultPrice
		`value="half"`,                     // slug
		`<option value="bundle" selected>`, // 类型下拉还原成用户选的那个
	} {
		if !strings.Contains(out, want) {
			t.Errorf("回填应渲染 %s，实际未见", want)
		}
	}
	if strings.Contains(out, `<option value="variant" selected>`) {
		t.Error("type 回填成了 variant，用户提交的是 bundle（下拉没还原）")
	}

	// ④ 多选只勾命中项（a1 勾上、a2 不勾）—— 这是「按值命中」与「按字段是否提交过」的区别。
	if !strings.Contains(out, `value="a1" checked`) {
		t.Error("attributeIds 中 a1 应被勾上")
	}
	if strings.Contains(out, `value="a2" checked`) {
		t.Error("attributeIds 中 a2 不该被勾上（用户没选它）—— 回填按「字段提交过」判会一次勾满整组")
	}
	if !strings.Contains(out, `data-default="1" checked`) {
		t.Error("warehouseIds 中 w1 应被勾上")
	}
	if !strings.Contains(out, `data-track-quantity checked`) {
		t.Error("trackQuantity 勾选态应回填")
	}
	// trackQuantity 勾了 → 数量框必须是可填的（否则「填了数量却被禁用」）。
	if at := strings.Index(out, `name="quantity"`); at >= 0 {
		if seg := out[at:min(at+260, len(out))]; strings.Contains(seg, "disabled") {
			t.Error("trackQuantity 已勾选，数量框不该 disabled")
		}
	} else {
		t.Error("未渲染数量框")
	}
}

func TestProductCreateFormFirstPaintHasNoEcho(t *testing.T) {
	out := renderAdminPage(t, "product/products_new", productCreateFirstPaintData())

	if strings.Contains(out, "data-create-err") || strings.Contains(out, `role="alert"`) {
		t.Error("首屏不该出现提交失败的错误槽（SubmitErr 只在失败重渲染时注入）")
	}
	if strings.Contains(out, `value="半填的商品"`) {
		t.Error("首屏不该带任何回填值（FormEcho 是失败重渲染才注入的键）")
	}
	// 首屏数量框按「不跟踪」起步：禁用且留空（0 是「明确没货」这个具体事实，绝不预填）。
	if at := strings.Index(out, `name="quantity"`); at >= 0 {
		if seg := out[at:min(at+260, len(out))]; !strings.Contains(seg, "disabled") {
			t.Error("首屏数量框应 disabled（未勾跟踪数量）")
		}
	} else {
		t.Error("未渲染数量框")
	}
	// htmx 分档与 host 是结构性的，首屏一样要有 —— 否则第一次提交就退化成整页跳转。
	if !strings.Contains(out, `hx-target="closest [data-product-create-host]"`) {
		t.Error("首屏表单也必须有 hx-target（分档能力来自表单属性，不是失败时才补）")
	}
}

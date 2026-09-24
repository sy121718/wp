package feature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	productdto "go_wp/internal/module/product/dto"
)

var bundleFieldName = regexp.MustCompile(`name="(variantId|required|defaultQty|minQty|maxQty|sourceKind|memberWarehouseId|memberWarehouseSku|memberExternalSku)"`)
var bundleMemberRow = regexp.MustCompile(`(?s)<tr data-bundle-member-row[^>]*>.*?</tr>`)

func TestBundlePageSaveFailureEcho(t *testing.T) {
	engine, f := newBundleMemberPageEngine(t)
	if engine == nil {
		return
	}
	price := 88.0
	main := f.mkProduct(t, "失败回填套餐", "echo-bundle", &price)
	item := f.mkProduct(t, "失败回填子项", "echo-addon", nil)
	v := f.firstVariant(t, item.ID)
	form := url.Values{
		"productId": {main.ID}, "maxOptions": {"7"}, "minTotalQty": {"99"}, "maxTotalQty": {"3"},
		"variantId": {v.ID, ""}, "required": {"0", "1"}, "defaultQty": {"2", "1"},
		"minQty": {"0", "1"}, "maxQty": {"3", "0"},
		"sourceKind": {"BundleSourceWarehouse", ""}, "memberWarehouseId": {"wh-echo", ""},
		"memberWarehouseSku": {"WH-ECHO", ""}, "memberExternalSku": {"EXT-ECHO", ""},
	}
	for _, hx := range []bool{false, true} {
		t.Run(map[bool]string{false: "native", true: "htmx"}[hx], func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/admin/products/bundle/save", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if hx {
				req.Header.Set("HX-Request", "true")
			}
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || strings.Contains(rec.Header().Get("Location"), "err=") {
				t.Fatalf("失败需原请求 200 回显，实际 %d location=%q", rec.Code, rec.Header().Get("Location"))
			}
			body := rec.Body.String()
			beforeTemplate, _, _ := strings.Cut(body, "<template data-bundle-member-tpl>")
			rows := bundleMemberRow.FindAllString(beforeTemplate, -1)
			if len(rows) != 2 || !strings.Contains(rows[0], `value="wh-echo"`) ||
				!strings.Contains(rows[0], `value="EXT-ECHO"`) || !strings.Contains(rows[0], `value="2"`) {
				t.Fatal("并行数组的首行来源与数量回填丢失")
			}
			if !strings.Contains(body, `role="alert"`) || !strings.Contains(body, `value="99"`) ||
				!strings.Contains(body, `value="wh-echo"`) || !strings.Contains(body, `value="EXT-ECHO"`) ||
				!strings.Contains(body, `value="`+v.ID+`"`) {
				t.Fatalf("失败丢失用户输入或错误槽：%s", oneLine(body))
			}
			if hx {
				if !strings.Contains(body, `data-bundle-form-host`) || strings.Contains(body, "<html") {
					t.Fatal("HX 失败必须只返回带 host 的表单片段")
				}
			} else if !strings.Contains(body, "</html>") {
				t.Fatal("原生失败需返回完整页面")
			}
		})
	}
}

func TestBundlePageSlimFormContract(t *testing.T) {
	engine, f := newBundleMemberPageEngine(t)
	if engine == nil {
		return
	}
	price := 99.0
	main := f.mkProduct(t, "精简套餐", "slim-main", &price)
	items := []string{"slim-a", "slim-b", "slim-c"}
	variants := make([]string, 0, len(items))
	for _, slug := range items {
		p := f.mkProduct(t, slug, slug, nil)
		variants = append(variants, f.firstVariant(t, p.ID).ID)
	}
	f.setBundleConfig(t, main.ID, productdto.BundleConfig{
		MaxOptions: 20,
		Options: []productdto.BundleOption{
			{VariantID: variants[0], Required: true, DefaultQty: 1, MinQty: 1},
			{VariantID: variants[1], Required: true, DefaultQty: 1, MinQty: 1},
		},
	})
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/products/bundle?project="+f.projectID+"&product="+main.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("页面状态 %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	t.Logf("页面样本：%d bytes，%d 个 option", len(body), strings.Count(body, "<option"))
	if !strings.Contains(body, `name="csrf_token"`) || !strings.Contains(body, `action="/admin/products/bundle/save"`) {
		t.Fatal("原生表单及 CSRF 契约丢失")
	}
	beforeTemplate, _, _ := strings.Cut(body, "<template data-bundle-member-tpl>")
	rows := bundleMemberRow.FindAllString(beforeTemplate, -1)
	if len(rows) != 3 {
		t.Fatalf("两条已存成员加唯一空白候选行，应是 3 行，实际 %d", len(rows))
	}
	want := []string{"variantId", "sourceKind", "memberWarehouseId", "memberWarehouseSku", "memberExternalSku", "required", "defaultQty", "minQty", "maxQty"}
	for i, row := range rows {
		names := bundleFieldName.FindAllStringSubmatch(row, -1)
		if len(names) != len(want) {
			t.Fatalf("第 %d 行字段数 %d，不等于 %d: %v", i, len(names), len(want), names)
		}
		for n, match := range names {
			if match[1] != want[n] {
				t.Fatalf("第 %d 行字段顺序第 %d 项 %s，应为 %s", i, n, match[1], want[n])
			}
		}
		if i < 2 && (strings.Contains(row, `<select class="form-select" name="variantId"`) || !strings.Contains(row, `type="hidden" name="variantId" value="`+variants[i]+`"`)) {
			t.Fatalf("已存成员第 %d 行须显示 SKU 文本与隐藏身份", i)
		}
	}
	if strings.Count(beforeTemplate, `name="variantId"`) != 3 || strings.Count(beforeTemplate, `<option value="`+variants[2]+`"`) != 1 {
		t.Fatal("候选列表应只在唯一空白行出现，模板与已存行不重复候选")
	}
	if !strings.Contains(rows[2], `<select class="form-select" name="variantId"`) {
		t.Fatal("无 JS 空白行须保留可选 SKU 下拉")
	}
	if !strings.Contains(body, `data-bundle-member-tpl`) {
		t.Fatal("来源解析所需模板丢失")
	}
	if got, err := f.products.GetBundleConfig(context.Background(), &productdto.GetBundleConfigReq{ProductID: main.ID}); err != nil || len(got.Options) != 2 {
		t.Fatalf("GET 不应改变配置: %v %+v", err, got)
	}
	// 已存变体停用后不再属于候选池，但该成员的身份与可读 SKU 仍须保留。
	off := false
	if _, err := f.products.UpdateVariant(context.Background(), &productdto.UpdateVariantReq{
		ID: variants[0], ProjectID: f.projectID, Enabled: &off,
	}); err != nil {
		t.Fatalf("停用已存 SKU: %v", err)
	}
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/products/bundle?project="+f.projectID+"&product="+main.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("停用后页面应 200，实际 %d", rec.Code)
	}
	beforeTemplate, _, _ = strings.Cut(rec.Body.String(), "<template data-bundle-member-tpl>")
	rows = bundleMemberRow.FindAllString(beforeTemplate, -1)
	if len(rows) != 3 || !strings.Contains(rows[0], `type="hidden" name="variantId" value="`+variants[0]+`"`) ||
		!strings.Contains(rows[0], "SLIMA") {
		t.Fatal("停用的已存成员必须保留身份和 SKU 文本，不能静默清除")
	}
	if strings.Contains(rows[2], `<option value="`+variants[0]+`"`) {
		t.Fatal("停用 SKU 不应重新进入新增候选池")
	}
}

func TestBundlePageFailurePreservesInterleavedRows(t *testing.T) {
	engine, f := newBundleMemberPageEngine(t)
	if engine == nil {
		return
	}
	price := 72.0
	main := f.mkProduct(t, "交错套餐", "interleave-bundle", &price)
	first := f.mkProduct(t, "甲", "interleave-a", nil)
	second := f.mkProduct(t, "乙", "interleave-b", nil)
	a, b := f.firstVariant(t, first.ID), f.firstVariant(t, second.ID)
	form := url.Values{
		"productId": {main.ID}, "maxOptions": {"12"}, "minTotalQty": {"20"}, "maxTotalQty": {"2"},
		"variantId": {b.ID, "", a.ID}, "required": {"0", "1", "1"},
		"defaultQty": {"2", "1", "3"}, "minQty": {"0", "1", "2"}, "maxQty": {"2", "0", "4"},
		"sourceKind": {"", "", ""}, "memberWarehouseId": {"", "", ""},
		"memberWarehouseSku": {"B-SOURCE", "", "A-SOURCE"},
		"memberExternalSku":  {"B-EXT", "", "A-EXT"},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/products/bundle/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("交错失败回填状态 %d", rec.Code)
	}
	beforeTemplate, _, _ := strings.Cut(rec.Body.String(), "<template data-bundle-member-tpl>")
	rows := bundleMemberRow.FindAllString(beforeTemplate, -1)
	if len(rows) != 3 || !strings.Contains(rows[0], `value="`+b.ID+`"`) ||
		!strings.Contains(rows[0], `value="B-EXT"`) || !strings.Contains(rows[1], `data-bundle-candidate`) ||
		!strings.Contains(rows[2], `value="`+a.ID+`"`) || !strings.Contains(rows[2], `value="A-EXT"`) {
		t.Fatal("交错空白行与逆序成员必须逐行保真，不按库内顺序重排")
	}
}

func TestBundlePageSuccessHXRedirect(t *testing.T) {
	engine, f := newBundleMemberPageEngine(t)
	if engine == nil {
		return
	}
	price := 66.0
	main := f.mkProduct(t, "成功套餐", "success-bundle", &price)
	item := f.mkProduct(t, "成功子项", "success-addon", nil)
	v := f.firstVariant(t, item.ID)
	form := url.Values{
		"productId": {main.ID}, "maxOptions": {"20"}, "minTotalQty": {"1"}, "maxTotalQty": {"0"},
		"variantId": {v.ID, ""}, "required": {"1", "1"}, "defaultQty": {"1", "1"},
		"minQty": {"1", "1"}, "maxQty": {"0", "0"},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/products/bundle/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("HX-Redirect") != "/admin/products/bundle?product="+main.ID || rec.Header().Get("Location") != "" {
		t.Fatalf("HX 成功须 HX-Redirect 而非 302，实际 %d headers=%v", rec.Code, rec.Header())
	}
}

func TestBundlePageTwentyMembersNoExtraCandidate(t *testing.T) {
	engine, f := newBundleMemberPageEngine(t)
	if engine == nil {
		return
	}
	price := 99.0
	main := f.mkProduct(t, "上限套餐", "limit-bundle", &price)
	item := f.mkProduct(t, "上限子项", "limit-addon", nil)
	v := f.firstVariant(t, item.ID)
	// 回填是提交状态，不依赖服务保存 20 条真实变体，测试上限控制的 DOM 行数。
	form := url.Values{"productId": {main.ID}, "maxOptions": {"20"}, "minTotalQty": {"99"}, "maxTotalQty": {"1"}}
	for i := 0; i < 20; i++ {
		form.Add("variantId", v.ID)
		form.Add("required", "1")
		form.Add("defaultQty", "1")
		form.Add("minQty", "1")
		form.Add("maxQty", "1")
		for _, field := range []string{"sourceKind", "memberWarehouseId", "memberWarehouseSku", "memberExternalSku"} {
			form.Add(field, "")
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/products/bundle/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("20 项回填应 200，实际 %d", rec.Code)
	}
	beforeTemplate, _, _ := strings.Cut(rec.Body.String(), "<template data-bundle-candidate-template>")
	if rows := bundleMemberRow.FindAllString(beforeTemplate, -1); len(rows) != 20 {
		t.Fatalf("上限 20 项时不生成第 21 候选行，实际 %d", len(rows))
	}
}

package feature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	productdto "go_wp/internal/module/product/dto"
)

var bundleFieldName = regexp.MustCompile(`name="(variantId|required|defaultQty|minQty|maxQty|sourceKind|memberWarehouseId|memberWarehouseSku|memberExternalSku)"`)
var bundleMemberRow = regexp.MustCompile(`(?s)<tr data-bundle-member-row[^>]*>.*?</tr>`)

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
	// 保存已迁到 JSON 写基座：端点、builder 与行收集声明都在表单上（CSRF 走请求头）。
	for _, want := range []string{
		`hx-post="/api/product/bundle/set"`,
		`data-write-builder="product-bundle-config"`,
		`data-write-rows="[data-bundle-member-row]`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("捆绑表单缺基座声明 %q", want)
		}
	}
	if strings.Contains(body, `action="/admin/products/bundle/save`) || strings.Contains(body, `hx-post="/admin/products/bundle/save`) {
		t.Fatal("保存表单不该再有页面端 action / hx-post（那条端点已删除）")
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

func TestBundlePageTwentyMembersNoExtraCandidate(t *testing.T) {
	engine, f := newBundleMemberPageEngine(t)
	if engine == nil {
		return
	}
	price := 99.0
	main := f.mkProduct(t, "上限套餐", "limit-bundle", &price)
	// 上限 20 是配置约束，不依赖成员是不是同一个变体以外的东西：给 20 个不同变体，
	// 让 service 的「同一 SKU 不重复」校验放行，页面再按配置渲染 20 行。
	options := make([]productdto.BundleOption, 0, 20)
	for i := 0; i < 20; i++ {
		p := f.mkProduct(t, "上限子项"+strconv.Itoa(i), "limit-addon-"+strconv.Itoa(i), nil)
		options = append(options, productdto.BundleOption{
			VariantID: f.firstVariant(t, p.ID).ID, Required: true, DefaultQty: 1, MinQty: 1,
		})
	}
	f.setBundleConfig(t, main.ID, productdto.BundleConfig{MaxOptions: 20, Options: options})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/products/bundle?project="+f.projectID+"&product="+main.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("上限 20 的页面应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	beforeTemplate, _, _ := strings.Cut(rec.Body.String(), "<template data-bundle-candidate-template>")
	if rows := bundleMemberRow.FindAllString(beforeTemplate, -1); len(rows) != 20 {
		t.Fatalf("上限 20 项时不生成第 21 候选行，实际 %d", len(rows))
	}

}

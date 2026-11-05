package feature

import (
	"html"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	projectdto "go_wp/internal/module/project/dto"
	json "go_wp/pkg/jsonx"
)

func createdProductID(t *testing.T, body, projectID string) string {
	t.Helper()
	match := regexp.MustCompile(`<a data-product-created-link href="([^"]+)"`).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("成功响应没有 Jet 创建的编辑链接：%s", body)
	}
	link, err := url.Parse(html.UnescapeString(match[1]))
	if err != nil || link.Path != "/admin/products/edit" || link.Query().Get("project") != projectID || link.Query().Get("product") == "" {
		t.Fatalf("创建链接不对应真实业务实体：%v %s", err, body)
	}
	return link.Query().Get("product")
}

func TestProductCreateHTTPFormPersistedInputs(t *testing.T) {
	engine, f := newCreateFlowEngine(t)
	if engine == nil {
		t.Fatal("本专项必须使用真实 PostgreSQL")
	}
	color := mkVariationAttr(t, f, "颜色", "html-color", []string{"red", "blue"})
	size := mkVariationAttr(t, f, "尺寸", "html-size", []string{"s", "m"})
	for _, price := range []string{"absent", "0", "12.50"} {
		t.Run(price, func(t *testing.T) {
			values := url.Values{"projectId": {f.projectID}, "name": {"HTML 商品 " + price}, "slug": {"html-create-" + strings.ReplaceAll(price, ".", "-")}, "attributeIds": {color.ID, size.ID}}
			if price != "absent" {
				values.Set("defaultPrice", price)
			}
			rec := postProductHTMLForm(engine, "/api/product/create?projectId=forged-query&name=forged-name", values)
			body := assertProductHTMLResponse(t, rec, http.StatusOK)
			id := createdProductID(t, body, f.projectID)
			p, err := f.svc.Get(t.Context(), &productdto.GetReq{ProjectID: f.projectID, ID: id})
			if err != nil {
				t.Fatal(err)
			}
			if p.Name != values.Get("name") || p.ProjectID != f.projectID || !reflect.DeepEqual(p.AttributeIDs, values["attributeIds"]) {
				t.Fatalf("真实持久化输入不符：%+v", p)
			}
			if price == "absent" && p.DefaultPrice != nil || price == "0" && (p.DefaultPrice == nil || *p.DefaultPrice != 0) || price == "12.50" && (p.DefaultPrice == nil || *p.DefaultPrice != 12.5) {
				t.Fatalf("数据库未保留未设置、零和实际价格：%v", p.DefaultPrice)
			}
			if len(p.Variants) != 1 {
				t.Fatalf("创建链必须生成一个默认变体：%+v", p.Variants)
			}
			if price == "12.50" && p.Variants[0].Price != 12.5 {
				t.Fatalf("默认变体未继承实际金额：%+v", p.Variants[0])
			}
		})
	}
}

// TestProductUpdateHTTPPersistsEditorObject 验证真实对象、集合清空与可选数值在 HTML 写链路落库。
func TestProductUpdateHTTPPersistsEditorObject(t *testing.T) {
	engine, f := newCreateFlowEngine(t)
	if engine == nil {
		t.Fatal("本专项必须使用真实 PostgreSQL")
	}
	ctx := t.Context()
	attribute := mkVariationAttr(t, f, "颜色", "html-update-color", []string{"red", "blue"})
	category, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: f.projectID, Name: "编辑分类", Slug: "html-update-category"})
	if err != nil {
		t.Fatal(err)
	}
	brand, err := f.svc.CreateBrand(ctx, &productdto.CreateBrandReq{ProjectID: f.projectID, Name: "编辑品牌", Slug: "html-update-brand"})
	if err != nil {
		t.Fatal(err)
	}
	tag, err := f.svc.CreateTag(ctx, &productdto.CreateTagReq{ProjectID: f.projectID, Name: "编辑标签", Slug: "html-update-tag"})
	if err != nil {
		t.Fatal(err)
	}
	price := 12.5
	p, err := f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "编辑前", Slug: "html-update", DefaultPrice: &price, Weight: &price})
	if err != nil {
		t.Fatal(err)
	}
	get := func() *productdto.ProductResp {
		t.Helper()
		got, err := f.svc.Get(ctx, &productdto.GetReq{ProjectID: f.projectID, ID: p.ID})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	description := map[string]any{"html": "<p>真实富文本 &amp; 内容</p>"}
	images := []string{testMediaBaseURL + "/img/one.png", testMediaBaseURL + "/img/two.png"}
	alts := []string{"", "第二张"}
	body := map[string]any{
		"projectId": f.projectID, "id": p.ID, "name": "编辑后", "subtitle": "副标题",
		"description": description, "images": images, "imageAlts": alts,
		"defaultImage": images[0], "attributeIds": []string{attribute.ID},
		"categoryIds": []string{category.ID}, "primaryCategoryId": category.ID,
		"brandId": brand.ID, "tagIds": []string{tag.ID},
	}
	response := postJSON(t, engine, "/api/product/update?name=query-only&projectId=forged-query", body)
	html := assertProductHTMLResponse(t, response, http.StatusOK)
	if !strings.Contains(html, `<p role="status">操作成功</p>`) {
		t.Fatalf("成功必须返回真实 Jet 成功片段：%s", html)
	}
	got := get()
	var storedDescription map[string]any
	if err := json.Unmarshal(got.Description, &storedDescription); err != nil {
		t.Fatal(err)
	}
	if got.Name != "编辑后" || got.Subtitle != "副标题" || !reflect.DeepEqual(storedDescription, description) ||
		!reflect.DeepEqual(got.Images, images) || !reflect.DeepEqual(got.ImageAlts, alts) || got.DefaultImage != images[0] ||
		!reflect.DeepEqual(got.AttributeIDs, []string{attribute.ID}) || !reflect.DeepEqual(got.CategoryIDs, []string{category.ID}) ||
		got.PrimaryCategoryID != category.ID || got.BrandID != brand.ID || !reflect.DeepEqual(got.TagIDs, []string{tag.ID}) {
		t.Fatalf("真实对象未完整持久化：%+v description=%v", got, storedDescription)
	}
	if got.DefaultPrice == nil || *got.DefaultPrice != price || got.Weight == nil || *got.Weight != price {
		t.Fatalf("数值缺失必须不修改：%+v", got)
	}
	body = map[string]any{
		"projectId": f.projectID, "id": p.ID, "subtitle": "", "description": map[string]any{"html": ""},
		"images": []string{}, "imageAlts": []string{}, "defaultImage": "",
		"attributeIds": []string{}, "categoryIds": []string{}, "tagIds": []string{},
		"primaryCategoryId": "", "brandId": "", "defaultPrice": 0, "weight": 0,
	}
	assertProductHTMLResponse(t, postJSON(t, engine, "/api/product/update", body), http.StatusOK)
	got = get()
	if err := json.Unmarshal(got.Description, &storedDescription); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(storedDescription, map[string]any{"html": ""}) {
		t.Fatalf("明确清空的富文本没有落库：%v", storedDescription)
	}
	if got.Subtitle != "" || got.DefaultImage != "" || got.BrandID != "" || got.PrimaryCategoryID != "" ||
		len(got.Images)+len(got.ImageAlts)+len(got.AttributeIDs)+len(got.CategoryIDs)+len(got.TagIDs) != 0 ||
		got.DefaultPrice == nil || *got.DefaultPrice != 0 || got.Weight == nil || *got.Weight != 0 {
		t.Fatalf("空关联、全取消集合或数值零没有落库：%+v", got)
	}
	assertProductHTMLResponse(t, postJSON(t, engine, "/api/product/update", map[string]any{
		"projectId": f.projectID, "id": p.ID, "clearDefaultPrice": true, "clearWeight": true,
	}), http.StatusOK)
	got = get()
	if got.DefaultPrice != nil || got.Weight != nil || len(got.Variants) != 1 || got.Variants[0].Price != price {
		t.Fatalf("明确清空不得变成零或修改已有变体价格：%+v", got)
	}
}

// TestProductUpdateHTTPRejectsInvalidAndForeign 验证错误输入和跨工程引用不留下部分更新。
func TestProductUpdateHTTPRejectsInvalidAndForeign(t *testing.T) {
	engine, f := newCreateFlowEngine(t)
	if engine == nil {
		t.Fatal("本专项必须使用真实 PostgreSQL")
	}
	ctx := t.Context()
	p, err := f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "保持原名", Slug: "html-update-guard"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := f.projects.Create(ctx, &projectdto.CreateReq{Name: "另一个更新工程"})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: other.ID, Name: "外部分类", Slug: "foreign-update"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		body   map[string]any
		status int
	}{
		{"缺少ID", map[string]any{"name": "不得写入"}, http.StatusBadRequest},
		{"错误类型", map[string]any{"id": p.ID, "weight": "not-number"}, http.StatusBadRequest},
		{"默认价清空冲突", map[string]any{"id": p.ID, "name": "不得写入", "defaultPrice": 0, "clearDefaultPrice": true}, http.StatusBadRequest},
		{"重量清空冲突", map[string]any{"id": p.ID, "name": "不得写入", "weight": 0, "clearWeight": true}, http.StatusBadRequest},
		{"跨工程商品", map[string]any{"projectId": other.ID, "id": p.ID, "name": "不得写入"}, http.StatusNotFound},
		{"跨工程分类", map[string]any{"id": p.ID, "name": "不得写入", "categoryIds": []string{foreign.ID}}, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := tc.body["projectId"]; !ok {
				tc.body["projectId"] = f.projectID
			}
			assertProductHTMLResponse(t, postJSON(t, engine, "/api/product/update", tc.body), tc.status)
			got, err := f.svc.Get(ctx, &productdto.GetReq{ProjectID: f.projectID, ID: p.ID})
			if err != nil || got.Name != p.Name || len(got.CategoryIDs) != 0 || got.Weight != nil || got.DefaultPrice != nil {
				t.Fatalf("拒绝后商品不得被部分修改：%v %+v", err, got)
			}
		})
	}
	assertProductHTMLResponse(t, postProductHTMLForm(engine, "/api/product/update", url.Values{"projectId": {f.projectID}, "id": {p.ID}, "name": {"旧表单不得绑定"}}), http.StatusBadRequest)
}

func TestProductCreateDeleteHTTPFailureAndAtomicity(t *testing.T) {
	engine, f := newCreateFlowEngine(t)
	if engine == nil {
		t.Fatal("本专项必须使用真实 PostgreSQL")
	}
	for _, values := range []url.Values{
		{"projectId": {f.projectID}},
		{"projectId": {f.projectID}, "name": {"非法价格"}, "defaultPrice": {"not-money"}},
	} {
		rec := postProductHTMLForm(engine, "/api/product/create?name=query-only", values)
		assertProductHTMLResponse(t, rec, http.StatusBadRequest)
	}
	result, err := f.svc.QueryProducts(t.Context(), &productdto.ListReq{ProjectID: f.projectID, Page: 1, Size: 20})
	if err != nil || result.Total != 0 {
		t.Fatalf("非法表单不得留下商品或默认变体：%v %+v", err, result)
	}
	values := url.Values{"projectId": {f.projectID}, "name": {"真实表单商品"}, "slug": {"html-write-atomic"}}
	rec := postProductHTMLForm(engine, "/api/product/create", values)
	id := createdProductID(t, assertProductHTMLResponse(t, rec, http.StatusOK), f.projectID)
	// 同一编码冲突继续拒绝，不自动改名或覆盖已有实体。
	rec = postProductHTMLForm(engine, "/api/product/create", values)
	assertProductHTMLResponse(t, rec, http.StatusConflict)
	rec = postProductHTMLForm(engine, "/api/product/delete?id="+id, url.Values{"projectId": {f.projectID}})
	assertProductHTMLResponse(t, rec, http.StatusBadRequest)
	if _, err := f.svc.Get(t.Context(), &productdto.GetReq{ProjectID: f.projectID, ID: id}); err != nil {
		t.Fatalf("query 中的 ID 不得进入删除 body：%v", err)
	}
	rec = postProductHTMLForm(engine, "/api/product/delete", url.Values{"projectId": {f.projectID}, "id": {id}})
	assertProductHTMLResponse(t, rec, http.StatusOK)
	if _, err := f.svc.Get(t.Context(), &productdto.GetReq{ProjectID: f.projectID, ID: id}); err == nil {
		t.Fatal("成功删除后仍可读取商品")
	}
}

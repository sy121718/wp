package feature

import (
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	projectdto "go_wp/internal/module/project/dto"
)

// TestProductSEOHTMLReadOnlyAndScope 验证三个真实评分入口的 HTML、只读语义与工程隔离。
func TestProductSEOHTMLReadOnlyAndScope(t *testing.T) {
	engine, f := newCreateFlowEngine(t)
	if engine == nil {
		t.Fatal("SEO 专项必须使用真实 PostgreSQL")
	}
	ctx := t.Context()
	p, err := f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "SEO 商品", Slug: "seo-product"})
	if err != nil {
		t.Fatal(err)
	}
	category, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: f.projectID, Name: "SEO 分类", Slug: "seo-category"})
	if err != nil {
		t.Fatal(err)
	}
	brand, err := f.svc.CreateBrand(ctx, &productdto.CreateBrandReq{ProjectID: f.projectID, Name: "SEO 品牌", Slug: "seo-brand"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := f.projects.Create(ctx, &projectdto.CreateReq{Name: "SEO 其他工程"})
	if err != nil {
		t.Fatal(err)
	}
	beforeCategory, err := f.svc.GetCategory(ctx, &productdto.GetCategoryReq{ProjectID: f.projectID, ID: category.ID})
	if err != nil {
		t.Fatal(err)
	}
	beforeBrand, err := f.svc.GetBrand(ctx, &productdto.GetBrandReq{ProjectID: f.projectID, ID: brand.ID})
	if err != nil {
		t.Fatal(err)
	}
	beforeProduct, err := f.svc.Get(ctx, &productdto.GetReq{ProjectID: f.projectID, ID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	beforeWrites := map[string]int{}
	for _, table := range []string{"product_outbox_events", "presentation_instances", "presentation_artifacts", "document_snapshots"} {
		beforeWrites[table] = countRows(t, f.db, table, "")
	}
	for _, tc := range []struct{ path, id, title string }{
		{"/api/product/seo-score", p.ID, "SEO 商品"},
		{"/api/product/category/seo-score", category.ID, "SEO 分类"},
		{"/api/product/brand/seo-score", brand.ID, "SEO 品牌"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			values := url.Values{"projectId": {f.projectID}, "id": {tc.id}}
			// query 中的伪造工程与实体不能覆盖 body。
			body := assertProductHTMLResponse(t, postProductHTMLForm(engine, tc.path+"?projectId="+other.ID+"&id=missing", values), http.StatusOK)
			for _, want := range []string{"data-product-seo-panel", "data-seo-sections", "</section>", tc.title} {
				if !strings.Contains(body, want) {
					t.Fatalf("评分 HTML 缺少 %q：%s", want, body)
				}
			}
			assertProductHTMLResponse(t, postProductHTMLForm(engine, tc.path, url.Values{"projectId": {other.ID}, "id": {tc.id}}), http.StatusNotFound)
			assertProductHTMLResponse(t, postProductHTMLForm(engine, tc.path+"?id="+tc.id, url.Values{"projectId": {f.projectID}}), http.StatusBadRequest)
			assertProductHTMLResponse(t, postProductHTMLForm(engine, tc.path+"?projectId="+f.projectID, url.Values{"id": {tc.id}}), http.StatusBadRequest)
			assertProductHTMLResponse(t, postJSON(t, engine, tc.path, map[string]any{"projectId": f.projectID, "id": tc.id}), http.StatusBadRequest)
			assertProductHTMLResponse(t, postProductHTMLForm(engine, tc.path, values), http.StatusOK)
		})
	}
	afterProduct, err := f.svc.Get(ctx, &productdto.GetReq{ProjectID: f.projectID, ID: p.ID})
	if err != nil || !reflect.DeepEqual(beforeProduct, afterProduct) {
		t.Fatalf("SEO 不得修改商品、评分、变体或版本：%v", err)
	}
	afterCategory, err := f.svc.GetCategory(ctx, &productdto.GetCategoryReq{ProjectID: f.projectID, ID: category.ID})
	if err != nil || !reflect.DeepEqual(beforeCategory, afterCategory) {
		t.Fatalf("SEO 不得修改分类：%v", err)
	}
	afterBrand, err := f.svc.GetBrand(ctx, &productdto.GetBrandReq{ProjectID: f.projectID, ID: brand.ID})
	if err != nil || !reflect.DeepEqual(beforeBrand, afterBrand) {
		t.Fatalf("SEO 不得修改品牌：%v", err)
	}
	for table, before := range beforeWrites {
		if after := countRows(t, f.db, table, ""); after != before {
			t.Fatalf("SEO 不得写入 %s：before=%d after=%d", table, before, after)
		}
	}
}

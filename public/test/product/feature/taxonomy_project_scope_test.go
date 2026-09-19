package feature

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	productdto "go_wp/internal/module/product/dto"
	producthttp "go_wp/internal/module/product/inbound/http"
	projectdto "go_wp/internal/module/project/dto"
)

// 两个工程时不能靠 service 的单工程回退；表单读到的 projectId 必须贯穿写入契约。
func TestTaxonomyFormsCarryProjectScope(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := t.Context()
	if _, err := f.projects.Create(ctx, &projectdto.CreateReq{Name: "另一个工程"}); err != nil {
		t.Fatal(err)
	}
	h := producthttp.NewProductPageHandle(f.svc, f.projects)
	router := gin.New()
	router.POST("/brand/update", h.ProductBrandsUpdate)
	router.POST("/brand/delete", h.ProductBrandsDelete)
	router.POST("/category/update", h.ProductCategoriesUpdate)
	router.POST("/category/delete", h.ProductCategoriesDelete)
	router.POST("/tag/update", h.ProductTagsUpdate)
	router.POST("/tag/delete", h.ProductTagsDelete)
	brand, err := f.svc.CreateBrand(ctx, &productdto.CreateBrandReq{ProjectID: f.projectID, Name: "品牌", Slug: "scope-brand"})
	if err != nil {
		t.Fatal(err)
	}
	category, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: f.projectID, Name: "分类", Slug: "scope-category"})
	if err != nil {
		t.Fatal(err)
	}
	tag, err := f.svc.CreateTag(ctx, &productdto.CreateTagReq{ProjectID: f.projectID, Name: "标签", Slug: "scope-tag", Kind: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ kind, id string }{{"brand", brand.ID}, {"category", category.ID}, {"tag", tag.ID}} {
		t.Run(tc.kind, func(t *testing.T) {
			for _, action := range []string{"update", "delete"} {
				form := url.Values{"projectId": {f.projectID}, "id": {tc.id}, "name": {"修改后"}, "slug": {"scope-" + tc.kind}, "kind": {"manual"}, "seoTitle": {"修改后的标题"}}
				rec := postForm(router, "/"+tc.kind+"/"+action, form)
				if rec.Code != http.StatusFound || strings.Contains(rec.Header().Get("Location"), "err=") {
					t.Fatalf("多工程 %s %s 失败：%d %s", tc.kind, action, rec.Code, rec.Header().Get("Location"))
				}
			}
		})
	}
	if _, err := f.svc.GetBrand(ctx, &productdto.GetBrandReq{ProjectID: f.projectID, ID: brand.ID}); err == nil {
		t.Fatal("品牌未删除")
	}
	if _, err := f.svc.GetCategory(ctx, &productdto.GetCategoryReq{ProjectID: f.projectID, ID: category.ID}); err == nil {
		t.Fatal("分类未删除")
	}
	if _, err := f.svc.GetTag(ctx, &productdto.GetTagReq{ProjectID: f.projectID, ID: tag.ID}); err == nil {
		t.Fatal("标签未删除")
	}
}

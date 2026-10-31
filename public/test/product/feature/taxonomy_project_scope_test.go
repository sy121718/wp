package feature

import (
	"testing"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectdto "go_wp/internal/module/project/dto"
)

func TestTaxonomyFormsCarryProjectScope(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := t.Context()
	if _, err := f.projects.Create(ctx, &projectdto.CreateReq{Name: "另一个工程"}); err != nil {
		t.Fatal(err)
	}

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
	name, slug, kind := "修改后", "scope-tag", productenums.TagKindManual
	if _, err := f.svc.UpdateTag(ctx, &productdto.UpdateTagReq{
		ProjectID: f.projectID, ID: tag.ID, Name: &name, Slug: &slug, Kind: &kind,
	}); err != nil {
		t.Fatalf("改标签失败: %v", err)
	}
	if got, err := f.svc.GetTag(ctx, &productdto.GetTagReq{ProjectID: f.projectID, ID: tag.ID}); err != nil || got.Name != name {
		t.Fatalf("标签改名未生效: %v %+v", err, got)
	}
	if err := f.svc.DeleteTag(ctx, &productdto.DeleteTagReq{ProjectID: f.projectID, ID: tag.ID}); err != nil {
		t.Fatalf("删标签失败: %v", err)
	}

	if err := f.svc.DeleteCategory(ctx, &productdto.DeleteCategoryReq{ProjectID: f.projectID, ID: category.ID}); err != nil {
		t.Fatalf("删分类失败: %v", err)
	}
	if err := f.svc.DeleteBrand(ctx, &productdto.DeleteBrandReq{ProjectID: f.projectID, ID: brand.ID}); err != nil {
		t.Fatalf("删品牌失败: %v", err)
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

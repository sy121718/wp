package feature

import (
	"context"
	"testing"

	"github.com/google/uuid"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"gorm.io/gorm"
)

// 只统计业务读取，不把工程作用域设置误算成实体查询。
func productQueryCounts(t *testing.T, db *gorm.DB, run func()) map[string]int {
	t.Helper()
	counts := map[string]int{}
	name := "product-query-" + uuid.NewString()
	if err := db.Callback().Query().After("gorm:after_query").Register(name, func(tx *gorm.DB) {
		counts[tx.Statement.Table]++
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove(name)
	run()
	return counts
}

func TestPublicProductOptionsReadsOnlyRequiredRelations(t *testing.T) {
	f := newTRFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	enabled := true
	attr, err := f.products.CreateAttribute(ctx, &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: "颜色", Key: "color", IsVariation: &enabled,
		Values: []productdto.AttributeValueReq{{Key: "red", Label: "红色", Enabled: &enabled}},
	})
	if err != nil {
		t.Fatal(err)
	}
	category, err := f.products.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: f.projectID, Name: "杯具", Slug: "cups"})
	if err != nil {
		t.Fatal(err)
	}
	brand, err := f.products.CreateBrand(ctx, &productdto.CreateBrandReq{ProjectID: f.projectID, Name: "茶具", Slug: "tea"})
	if err != nil {
		t.Fatal(err)
	}
	tag, err := f.products.CreateTag(ctx, &productdto.CreateTagReq{ProjectID: f.projectID, Name: "推荐", Slug: "featured", Kind: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	product, err := f.products.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "红杯", Slug: "red-cup",
		AttributeIDs: []string{attr.ID}, CategoryIDs: []string{category.ID}, BrandID: brand.ID, TagIDs: []string{tag.ID}})
	if err != nil {
		t.Fatal(err)
	}
	published := productenums.StatusPublished
	if _, err = f.products.Update(ctx, &productdto.UpdateReq{ID: product.ID, Status: &published}); err != nil {
		t.Fatal(err)
	}
	if err = f.db.Exec(
		`UPDATE product_variants SET option_values = '{"color":"red"}'::jsonb WHERE product_id = ?`, product.ID).Error; err != nil {
		t.Fatal(err)
	}
	f.putTranslation(t, "颜色", "product_attribute.name", "en-US", "Color")
	f.putTranslation(t, "红色", "product_attribute.values", "en-US", "Red")
	counts := productQueryCounts(t, f.db, func() {
		out, err := f.products.PublicProductOptions(ctx, &productdto.PublicProductOptionsReq{ProjectID: f.projectID, ProductID: product.ID, Lang: "en-US"})
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Options) != 1 || out.Options[0].Name != "Color" || len(out.Options[0].Values) != 1 || out.Options[0].Values[0].Name != "Red" {
			t.Fatalf("规格翻译或引用丢失: %+v", out)
		}
	})
	if counts["product_attributes"] != 1 {
		t.Errorf("属性应只读一次，实际 %v", counts)
	}
	for _, table := range []string{"product_categories", "product_brands", "product_tags"} {
		if counts[table] != 0 {
			t.Errorf("规格接口额外读取 %s: %v", table, counts)
		}
	}
}

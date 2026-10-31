package feature

import (
	"context"
	"errors"
	"reflect"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"gorm.io/gorm"
)

func TestProductWritesRollBackWhenAutoTagFails(t *testing.T) {
	for _, operation := range []string{"商品编辑", "变体编辑", "商品创建", "变体创建", "变体删除", "变体清单"} {
		t.Run(operation, func(t *testing.T) {
			f := newPricingFixture(t)
			if f == nil {
				return
			}
			ctx := context.Background()
			_, err := f.svc.CreateTag(ctx, &productdto.CreateTagReq{
				ProjectID: f.projectID, Name: "原子标签", Kind: productenums.TagKindRule,
				RuleType: productenums.TagRulePriceRange, RuleParams: []byte(`{"minPrice":100,"maxPrice":200}`),
			})
			if err != nil {
				t.Fatal(err)
			}
			p := pfCreateProduct(t, f, "原子商品", 500, 200)
			var beforeProduct productmodel.ProductEntity
			var beforeVariant productmodel.VariantEntity
			if err := f.db.First(&beforeProduct, "id = ?", p.ID).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.db.First(&beforeVariant, "id = ?", p.Variants[0].ID).Error; err != nil {
				t.Fatal(err)
			}
			injected := errors.New("injected product tag failure")
			const callback = "test:product_write_tag_failure"
			if err := f.db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table == "product_tags" {
					tx.AddError(injected)
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer f.db.Callback().Update().Remove(callback)
			var productsBefore, variantsBefore int64
			if err := f.db.Model(&productmodel.ProductEntity{}).Where("project_id = ?", f.projectID).Count(&productsBefore).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.db.Model(&productmodel.VariantEntity{}).Where("product_id = ?", p.ID).Count(&variantsBefore).Error; err != nil {
				t.Fatal(err)
			}
			switch operation {
			case "商品编辑":
				subtitle := "本次不应提交"
				_, err = f.svc.Update(ctx, &productdto.UpdateReq{ID: p.ID, ProjectID: f.projectID, Subtitle: &subtitle})
			case "商品创建":
				_, err = f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "失败创建", Slug: "failed-create"})
			case "变体创建":
				_, err = f.svc.CreateVariant(ctx, &productdto.CreateVariantReq{ProjectID: f.projectID, ProductID: p.ID, SKUCode: "atomic-new-variant"})
			case "变体删除":
				err = f.svc.DeleteVariant(ctx, &productdto.DeleteVariantReq{ProjectID: f.projectID, ID: p.Variants[0].ID})
			case "变体清单":
				_, err = f.svc.SaveVariantList(ctx, &productdto.SaveVariantListReq{ProjectID: f.projectID, ProductID: p.ID})
			case "变体编辑":
				price := 150.0
				_, err = f.svc.UpdateVariant(ctx, &productdto.UpdateVariantReq{ID: p.Variants[0].ID, ProjectID: f.projectID, Price: &price})
			}
			if !errors.Is(err, injected) {
				t.Fatalf("标签失败应使写操作失败: %v", err)
			}
			var productsAfter, variantsAfter int64
			if err := f.db.Model(&productmodel.ProductEntity{}).Where("project_id = ?", f.projectID).Count(&productsAfter).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.db.Model(&productmodel.VariantEntity{}).Where("product_id = ?", p.ID).Count(&variantsAfter).Error; err != nil {
				t.Fatal(err)
			}
			if productsBefore != productsAfter || variantsBefore != variantsAfter {
				t.Fatal("失败写入残留商品或变体")
			}
			var afterProduct productmodel.ProductEntity
			var afterVariant productmodel.VariantEntity
			if err := f.db.First(&afterProduct, "id = ?", p.ID).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.db.First(&afterVariant, "id = ?", p.Variants[0].ID).Error; err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(beforeProduct, afterProduct) || !reflect.DeepEqual(beforeVariant, afterVariant) {
				t.Fatal("标签失败后商品或变体发生了持久化变化")
			}
		})
	}
}

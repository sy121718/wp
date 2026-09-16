// Package feature product 模块 feature 测试（真实 PostgreSQL + 生产 DDL，issue #5）。
//
// 覆盖本票的四条核心语义：
//  1. 商品恒有变体 —— 创建商品即生成首个变体；
//  2. 商品级字段是「新增变体的默认值模板」，逐字段继承；
//  3. 「空」以 NULL 判定 —— 显式 0 / false 视为已填，不被默认值覆盖；
//  4. 编辑路径不做默认值填充 —— 清空就是清空，不回落。
package feature

import (
	"context"
	"testing"

	"gorm.io/gorm"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"

	"go_wp/public/test/support"
)

// fixture 隔离 PG schema + 按生产迁移建表 + 真实工程行 + 商品 service。
type fixture struct {
	svc       *productservice.Service
	db        *gorm.DB
	projectID string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "商品测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	return &fixture{
		svc:       productservice.NewService(productmodel.NewModel(db), projects),
		db:        db,
		projectID: project.ID,
	}
}

// TestCreateProductGeneratesFirstVariant 建商品即生成首个变体，且继承商品级默认价。
func TestCreateProductGeneratesFirstVariant(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	price := 99.0
	res, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "夏季衬衫", DefaultPrice: &price,
		DefaultImage: "/img/a.jpg", Slug: "summer-shirt",
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}
	if len(res.Variants) != 1 {
		t.Fatalf("商品恒有至少一个变体，实际 %d", len(res.Variants))
	}
	v := res.Variants[0]
	if v.Price != 99 {
		t.Fatalf("首个变体应继承商品级默认价 99，实际 %v", v.Price)
	}
	if v.Image != "/img/a.jpg" {
		t.Fatalf("首个变体应继承商品级默认图，实际 %q", v.Image)
	}
	if v.SKUCode == "" {
		t.Fatalf("未指定 SKU 编码时应自动生成")
	}
	if res.Slug != "summer-shirt" || res.Status != productenums.StatusDraft {
		t.Fatalf("商品主体字段错误: %+v", res)
	}
}

// TestVariantDefaultsOnlyFillNull 默认值只填空字段；显式 0 / false 视为已填。
func TestVariantDefaultsOnlyFillNull(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	defaultPrice := 50.0
	p, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "带默认价的商品", DefaultPrice: &defaultPrice,
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}

	// 不给价格 → 继承 50。
	v1, err := f.svc.CreateVariant(ctx, &productdto.CreateVariantReq{ProductID: p.ID})
	if err != nil {
		t.Fatalf("新增变体失败: %v", err)
	}
	if v1.Price != 50 {
		t.Fatalf("未给价格应继承默认值 50，实际 %v", v1.Price)
	}

	// 显式给 0 → 就是 0，不被默认值覆盖（0 是已填，不是空）。
	zero := 0.0
	v2, err := f.svc.CreateVariant(ctx, &productdto.CreateVariantReq{ProductID: p.ID, Price: &zero})
	if err != nil {
		t.Fatalf("新增变体失败: %v", err)
	}
	if v2.Price != 0 {
		t.Fatalf("显式 0 应保留为 0（0 不是空），实际 %v", v2.Price)
	}

	// 显式 enabled=false → 就是 false，不被默认值 true 覆盖。
	no := false
	v3, err := f.svc.CreateVariant(ctx, &productdto.CreateVariantReq{ProductID: p.ID, Enabled: &no})
	if err != nil {
		t.Fatalf("新增变体失败: %v", err)
	}
	if v3.Enabled {
		t.Fatalf("显式 false 应保留（false 不是空）")
	}
}

// TestUpdateVariantNeverFillsDefaults 编辑路径不填充默认值：清空就是清空。
func TestUpdateVariantNeverFillsDefaults(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	defaultPrice := 88.0
	p, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "编辑路径商品", DefaultPrice: &defaultPrice,
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}
	v, err := f.svc.CreateVariant(ctx, &productdto.CreateVariantReq{ProductID: p.ID})
	if err != nil {
		t.Fatalf("新增变体失败: %v", err)
	}
	if v.Price != 88 {
		t.Fatalf("前置条件：变体初始价应为 88，实际 %v", v.Price)
	}

	// 编辑时显式把价格改成 0：必须就是 0，不回落到商品级默认 88。
	zero := 0.0
	got, err := f.svc.UpdateVariant(ctx, &productdto.UpdateVariantReq{ID: v.ID, Price: &zero})
	if err != nil {
		t.Fatalf("修改变体失败: %v", err)
	}
	if got.Price != 0 {
		t.Fatalf("编辑路径不应回填默认值，期望 0，实际 %v", got.Price)
	}
	// 图片显式清空 → 保持空。
	empty := ""
	got, err = f.svc.UpdateVariant(ctx, &productdto.UpdateVariantReq{ID: v.ID, Image: &empty})
	if err != nil {
		t.Fatalf("修改变体失败: %v", err)
	}
	if got.Image != "" {
		t.Fatalf("编辑路径清空字段不应回落默认值，实际 %q", got.Image)
	}
}

// TestSlugAndSKUUniqueness slug 在工程内唯一、SKU 在商品内唯一。
func TestSlugAndSKUUniqueness(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	if _, err := f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "A", Slug: "same"}); err != nil {
		t.Fatalf("首个商品创建失败: %v", err)
	}
	_, err := f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "B", Slug: "same"})
	if err == nil || err.Error() != productenums.ErrSlugTaken {
		t.Fatalf("slug 重复应返回 ErrSlugTaken，实际 %v", err)
	}

	p, err := f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "C", Slug: "c"})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}
	if _, err = f.svc.CreateVariant(ctx, &productdto.CreateVariantReq{ProductID: p.ID, SKUCode: "SKU-1"}); err != nil {
		t.Fatalf("首个 SKU 创建失败: %v", err)
	}
	_, err = f.svc.CreateVariant(ctx, &productdto.CreateVariantReq{ProductID: p.ID, SKUCode: "SKU-1"})
	if err == nil || err.Error() != productenums.ErrSkuTaken {
		t.Fatalf("SKU 重复应返回 ErrSkuTaken，实际 %v", err)
	}
}

// TestDeleteProductCascadesVariants 删除商品连带删除其变体。
func TestDeleteProductCascadesVariants(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	p, err := f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "待删商品"})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}
	if _, err = f.svc.CreateVariant(ctx, &productdto.CreateVariantReq{ProductID: p.ID, SKUCode: "KEEP-1"}); err != nil {
		t.Fatalf("新增变体失败: %v", err)
	}
	if err = f.svc.Delete(ctx, &productdto.DeleteReq{ID: p.ID}); err != nil {
		t.Fatalf("删除商品失败: %v", err)
	}
	var n int64
	f.db.Model(&productmodel.VariantEntity{}).Where("product_id = ?", p.ID).Count(&n)
	if n != 0 {
		t.Fatalf("商品删除后变体应连带删除，残留 %d", n)
	}
}

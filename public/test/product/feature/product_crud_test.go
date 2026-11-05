package feature

import (
	"context"
	"testing"

	"gorm.io/gorm"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdatamodel "go_wp/internal/module/masterdata/model"
	masterdataservice "go_wp/internal/module/masterdata/service"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"

	"go_wp/public/test/support"
)

type fixture struct {
	svc       *productservice.Service
	db        *gorm.DB
	projectID string

	// changes 是当前服务的审计变更依赖，rewireProductChanges 换完依赖后用同一份重建服务。
	changes masterdatacontract.MasterDataService
}

// rewireProductChanges 用另一份审计端口重建商品服务：审计是构造依赖，
// 想注入故障/捕获就必须在装配处换掉，而不是运行中晚接。
func (f *fixture) rewireProductChanges(t *testing.T, changes masterdatacontract.MasterDataService) {
	t.Helper()
	f.changes = changes
	projects := projectservice.NewService(projectmodel.NewProjectModel(f.db))
	f.svc = productservice.NewService(productmodel.NewModel(f.db), projects, changes)
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
	changes := masterdataservice.NewService(masterdatamodel.NewModel(db), projects)
	return &fixture{
		svc:       productservice.NewService(productmodel.NewModel(db), projects, changes),
		db:        db,
		projectID: project.ID,
		changes:   changes,
	}
}

func TestCreateProductGeneratesFirstVariant(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	price := 99.0
	res, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "夏季衬衫", DefaultPrice: &price,
		DefaultImage: "/storage/img/a.jpg", Slug: "summer-shirt",
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
	if v.Image != testMediaBaseURL+"/img/a.jpg" {
		t.Fatalf("首个变体应继承商品级默认图，实际 %q", v.Image)
	}
	if v.SKUCode == "" {
		t.Fatalf("未指定 SKU 编码时应自动生成")
	}
	if res.Slug != "summer-shirt" || res.Status != productenums.StatusDraft {
		t.Fatalf("商品主体字段错误: %+v", res)
	}
}

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

	v1, err := f.svc.CreateVariant(ctx, &productdto.CreateVariantReq{ProductID: p.ID})
	if err != nil {
		t.Fatalf("新增变体失败: %v", err)
	}
	if v1.Price != 50 {
		t.Fatalf("未给价格应继承默认值 50，实际 %v", v1.Price)
	}

	zero := 0.0
	v2, err := f.svc.CreateVariant(ctx, &productdto.CreateVariantReq{ProductID: p.ID, Price: &zero})
	if err != nil {
		t.Fatalf("新增变体失败: %v", err)
	}
	if v2.Price != 0 {
		t.Fatalf("显式 0 应保留为 0（0 不是空），实际 %v", v2.Price)
	}

	no := false
	v3, err := f.svc.CreateVariant(ctx, &productdto.CreateVariantReq{ProductID: p.ID, Enabled: &no})
	if err != nil {
		t.Fatalf("新增变体失败: %v", err)
	}
	if v3.Enabled {
		t.Fatalf("显式 false 应保留（false 不是空）")
	}
}

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

	zero := 0.0
	got, err := f.svc.UpdateVariant(ctx, &productdto.UpdateVariantReq{ID: v.ID, Price: &zero})
	if err != nil {
		t.Fatalf("修改变体失败: %v", err)
	}
	if got.Price != 0 {
		t.Fatalf("编辑路径不应回填默认值，期望 0，实际 %v", got.Price)
	}

	empty := ""
	got, err = f.svc.UpdateVariant(ctx, &productdto.UpdateVariantReq{ID: v.ID, Image: &empty})
	if err != nil {
		t.Fatalf("修改变体失败: %v", err)
	}
	if got.Image != "" {
		t.Fatalf("编辑路径清空字段不应回落默认值，实际 %q", got.Image)
	}
}

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

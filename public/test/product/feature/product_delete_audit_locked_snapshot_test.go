package feature

import (
	"context"
	"errors"
	"testing"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/rls"

	"gorm.io/gorm"
)

func TestDeleteAuditUsesLockedSnapshot(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	p, err := f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "锁前名字", Slug: "delete-locked-snapshot"})
	if err != nil {
		t.Fatal(err)
	}
	variantID := p.Variants[0].ID
	changes := &capturedProductChanges{}
	f.rewireProductChanges(t, changes)

	const (
		bName  = "锁后名字"
		bSub   = "锁后副标题"
		bPrice = 321.0
		bCost  = 47.5
	)
	fired := afterProductSnapshot(t, f.db, "products", func() error {
		if err := rls.InProjectScope(ctx, f.db, f.projectID, func(tx *gorm.DB) error {
			return tx.Model(&productmodel.ProductEntity{}).Where("id = ?", p.ID).
				Updates(map[string]any{"name": bName, "subtitle": bSub, "default_price": bPrice}).Error
		}); err != nil {
			return err
		}
		return rls.InProjectScope(ctx, f.db, f.projectID, func(tx *gorm.DB) error {
			return tx.Model(&productmodel.VariantEntity{}).Where("id = ?", variantID).
				Updates(map[string]any{"cost_price": bCost}).Error
		})
	})

	if err = f.svc.Delete(ctx, &productdto.DeleteReq{ID: p.ID, ProjectID: f.projectID}); err != nil {
		t.Fatal(err)
	}
	if !*fired {
		t.Fatal("未触发锁前交错：插桩没跑到，本用例没测到快照窗口")
	}

	productChange := findChange(t, changes.inputs, masterdataenums.OriginProduct, p.ID)
	if productChange.Before["name"] != bName {
		t.Fatalf("删除留痕用了锁前旧名称: before=%v（期望 %q）", productChange.Before["name"], bName)
	}
	if productChange.Before["default_price"] != "321.00" {
		t.Fatalf("删除留痕用了锁前旧价格: before=%v", productChange.Before["default_price"])
	}
	variantChange := findChange(t, changes.inputs, masterdataenums.OriginVariant, variantID)
	if variantChange.Before["cost_price"] != "47.50" {
		t.Fatalf("删除留痕用了锁前旧成本价: before=%v", variantChange.Before["cost_price"])
	}

	var left int64
	if err = rls.InProjectScope(ctx, f.db, f.projectID, func(tx *gorm.DB) error {
		return tx.Model(&productmodel.ProductEntity{}).Where("id = ?", p.ID).Count(&left).Error
	}); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("商品应已删除，仍查到 %d 行", left)
	}
}

func TestDeleteAuditRollsBackOnFailure(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	p, err := f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "回滚商品", Slug: "delete-audit-rollback"})
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected delete audit failure")
	f.rewireProductChanges(t, &failingChangesPort{err: injected})
	if err = f.svc.Delete(ctx, &productdto.DeleteReq{ID: p.ID, ProjectID: f.projectID}); !errors.Is(err, injected) {
		t.Fatalf("审计错误未返回: %v", err)
	}

	var products, variants int64
	if err = rls.InProjectScope(ctx, f.db, f.projectID, func(tx *gorm.DB) error {
		return tx.Model(&productmodel.ProductEntity{}).Where("id = ?", p.ID).Count(&products).Error
	}); err != nil {
		t.Fatal(err)
	}
	if err = rls.InProjectScope(ctx, f.db, f.projectID, func(tx *gorm.DB) error {
		return tx.Model(&productmodel.VariantEntity{}).Where("product_id = ?", p.ID).Count(&variants).Error
	}); err != nil {
		t.Fatal(err)
	}
	if products != 1 || variants != int64(len(p.Variants)) {
		t.Fatalf("审计失败后删除未回滚: products=%d variants=%d（期望 1 / %d）", products, variants, len(p.Variants))
	}
}

func TestBulkDeleteAuditUsesLockedSnapshot(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	first, err := f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "批删甲-旧", Slug: "bulk-delete-a"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "批删乙-旧", Slug: "bulk-delete-b"})
	if err != nil {
		t.Fatal(err)
	}
	changes := &capturedProductChanges{}
	f.rewireProductChanges(t, changes)

	fired := afterProductSnapshot(t, f.db, "products", func() error {
		return rls.InProjectScope(ctx, f.db, f.projectID, func(tx *gorm.DB) error {
			return tx.Model(&productmodel.ProductEntity{}).Where("id = ?", second.ID).
				Updates(map[string]any{"name": "批删乙-新", "default_price": 88.0}).Error
		})
	})

	res, err := f.svc.BulkDeleteProducts(ctx, &productdto.BulkDeleteReq{
		ProjectID: f.projectID, IDs: []string{first.ID, second.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !*fired {
		t.Fatal("未触发锁前交错：插桩没跑到，本用例没测到快照窗口")
	}
	if res == nil || res.Done != 2 {
		t.Fatalf("两条都应删除: %+v", res)
	}
	got := findChange(t, changes.inputs, masterdataenums.OriginProduct, second.ID)
	if got.Before["name"] != "批删乙-新" {
		t.Fatalf("批删留痕用了锁前旧名称: before=%v", got.Before["name"])
	}
	if got.Before["default_price"] != "88.00" {
		t.Fatalf("批删留痕用了锁前旧价格: before=%v", got.Before["default_price"])
	}
	firstChange := findChange(t, changes.inputs, masterdataenums.OriginProduct, first.ID)
	if firstChange.Before["name"] != "批删甲-旧" {
		t.Fatalf("没被并发改的那条被误改: before=%v", firstChange.Before["name"])
	}
}

func TestBulkDeleteAuditRollsBackOnFailure(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	first, err := f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "批删回滚甲", Slug: "bulk-rollback-a"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "批删回滚乙", Slug: "bulk-rollback-b"})
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected bulk delete audit failure")
	f.rewireProductChanges(t, &failingChangesPort{err: injected})
	_, err = f.svc.BulkDeleteProducts(ctx, &productdto.BulkDeleteReq{
		ProjectID: f.projectID, IDs: []string{first.ID, second.ID},
	})
	if !errors.Is(err, injected) {
		t.Fatalf("审计错误未返回: %v", err)
	}
	var left int64
	if err = rls.InProjectScope(ctx, f.db, f.projectID, func(tx *gorm.DB) error {
		return tx.Model(&productmodel.ProductEntity{}).Where("id IN ?", []string{first.ID, second.ID}).Count(&left).Error
	}); err != nil {
		t.Fatal(err)
	}
	if left != 2 {
		t.Fatalf("审计失败后批删未回滚，只剩 %d 条", left)
	}
}

func findChange(t *testing.T, inputs []*masterdatacontract.ChangeInput, origin, entityID string) *masterdatacontract.ChangeInput {
	t.Helper()
	for _, in := range inputs {
		if in == nil || in.Origin != origin || in.EntityID != entityID {
			continue
		}
		return in
	}
	t.Fatalf("没找到 %s/%s 的留痕，实际输入：%+v", origin, entityID, inputs)
	return nil
}

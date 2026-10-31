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

// 删除留痕的**锁后快照**：单删与批删各一组。
//
// 这一组钉住的是「审计说删的是 A、实际删掉的是 B」那个旧快照窗口：
// 旧写法在事务外读商品与变体、先构造 before 快照，然后才进事务加锁 ——
// 校验与加锁之间别人改名 / 改价 / 回写成本价，快照记的是改之前的值，
// 而行被删掉时已经是新值。现在读快照排在锁之后。
//
// 交错一律用 afterProductSnapshot 插桩（gorm after_query 回调触发一次即可），
// 不靠 sleep 抢时序：B 的写入在 A 的第一次快照读之后、加锁之前提交，
// 于是「锁后读」必须看到 B，「锁前读」必然看不到 —— 结果确定。

// TestDeleteAuditUsesLockedSnapshot 单删：B 先提交，A 的删除留痕必须反映 B。
//
// 断言落在**变更记录里的实际字段值**（before 快照），不是「记录条数 > 0」：
// 条数在旧实现下同样成立，只有字段值能区分锁前 / 锁后。
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
	f.svc.SetMasterDataChanges(changes)

	// B：把商品名称、副标题、默认价与变体成本价一起改掉，全部在 A 的快照读之后提交。
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
	// 行真的没了，且没有留痕之外的副作用。
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

// TestDeleteAuditRollsBackOnFailure 审计端口故障 → 删除整体回滚。
//
// 顺序改成「锁后读 → 删 → 留痕」之后，留痕失败必须把删除一起回滚：
// 否则「删掉了但没人知道删的是什么」，比锁前快照更糟。
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
	f.svc.SetMasterDataChanges(&failingChangesPort{err: injected})
	if err = f.svc.Delete(ctx, &productdto.DeleteReq{ID: p.ID, ProjectID: f.projectID}); !errors.Is(err, injected) {
		t.Fatalf("审计错误未返回: %v", err)
	}
	// 商品与变体都还在：删除与留痕同一事务。
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

// TestBulkDeleteAuditUsesLockedSnapshot 批删：同一窗口，逐商品都要是锁后值。
//
// 批删的旧写法更隐蔽：LockProductsTx 只 Pluck 出 id（够做「这批还在不在」的集合校验），
// 行本身与变体都是锁前读的。这里两件商品各被并发改名，锁后读必须两条都看到新名。
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
	f.svc.SetMasterDataChanges(changes)

	// 只改**后创建**的那一条：旧实现里两条都是锁前读，若只有一条错就够红。
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

// TestBulkDeleteAuditRollsBackOnFailure 批删留痕失败 → 两条都还在。
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
	f.svc.SetMasterDataChanges(&failingChangesPort{err: injected})
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

// findChange 在留痕输入里按「实体类型 + 实体 id」找那一条。
//
// 不用下标取：批删一条请求产出多条留痕，下标顺序是实现的内部细节。
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

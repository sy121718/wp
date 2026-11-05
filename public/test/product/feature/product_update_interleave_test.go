package feature

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/rls"

	"gorm.io/gorm"
)

type capturedProductChanges struct {
	masterdatacontract.MasterDataService
	inputs []*masterdatacontract.ChangeInput
}

func (p *capturedProductChanges) RecordChangesTx(_ context.Context, _ *gorm.DB, inputs []*masterdatacontract.ChangeInput) error {
	p.inputs = append(p.inputs, inputs...)
	return nil
}

func afterProductSnapshot(t *testing.T, db *gorm.DB, table string, inject func() error) *bool {
	t.Helper()
	fired := false
	name := "test:product_snapshot_interleave"
	if err := db.Callback().Query().After("gorm:after_query").Register(name, func(tx *gorm.DB) {
		if fired || tx.Statement.Table != table || tx.Error != nil {
			return
		}
		fired = true
		if err := inject(); err != nil {
			tx.AddError(fmt.Errorf("交错写入失败: %w", err))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Query().Remove(name) })
	return &fired
}

func TestUpdateVariantKeepsInterleavedFields(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	p, err := f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "变体交错更新", Slug: "variant-interleave"})
	if err != nil {
		t.Fatal(err)
	}
	id := p.Variants[0].ID
	changes := &capturedProductChanges{}
	f.rewireProductChanges(t, changes)
	refs := []productmodel.WarehouseSKURef{{StockID: "00000000-0000-4000-8000-000000000001", WarehouseID: "00000000-0000-4000-8000-000000000002", WarehouseCode: "WH", SKUCode: "CONCURRENT"}}
	fired := afterProductSnapshot(t, f.db, "product_variants", func() error {
		return f.db.Model(&productmodel.VariantEntity{}).Where("id = ?", id).Updates(map[string]any{"cost_price": 23.5, "metadata": `{"concurrent":true}`, "warehouse_skus": productmodel.EncodeWarehouseSKUs(refs)}).Error
	})
	price := 88.0
	if _, err = f.svc.UpdateVariant(ctx, &productdto.UpdateVariantReq{ID: id, ProjectID: f.projectID, Price: &price}); err != nil {
		t.Fatal(err)
	}
	if !*fired {
		t.Fatal("未触发锁前交错")
	}
	var got productmodel.VariantEntity
	if err = f.db.Where("id = ?", id).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.Price != price || got.CostPrice == nil || *got.CostPrice != 23.5 || string(got.Metadata) != `{"concurrent": true}` {
		t.Fatalf("局部编辑覆盖并发字段: price=%v cost=%v metadata=%s", got.Price, got.CostPrice, got.Metadata)
	}
	if gotRefs := productmodel.DecodeWarehouseSKUs(got.WarehouseSKUs); !reflect.DeepEqual(gotRefs, refs) {
		t.Fatalf("局部编辑覆盖并发仓库引用: got=%+v want=%+v", gotRefs, refs)
	}
	if len(changes.inputs) != 1 || changes.inputs[0].Before["cost_price"] != changes.inputs[0].After["cost_price"] || changes.inputs[0].Before["cost_price"] == "" {
		t.Fatalf("变体审计重复归因并发成本: %+v", changes.inputs)
	}
}

func TestUpdateVariantCostAuditsLockedSnapshot(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	p, err := f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "成本交错", Slug: "cost-interleave"})
	if err != nil {
		t.Fatal(err)
	}
	id := p.Variants[0].ID
	changes := &capturedProductChanges{}
	f.rewireProductChanges(t, changes)
	fired := afterProductSnapshot(t, f.db, "product_variants", func() error {
		return f.db.Model(&productmodel.VariantEntity{}).Where("id = ?", id).Updates(map[string]any{"cost_price": 23.5, "price": 88}).Error
	})
	if err = f.svc.UpdateVariantCost(ctx, f.projectID, id, 30, ""); err != nil {
		t.Fatal(err)
	}
	var got productmodel.VariantEntity
	if err = f.db.Where("id = ?", id).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if !*fired || got.Price != 88 || got.CostPrice == nil || *got.CostPrice != 30 {
		t.Fatalf("成本回写破坏独立字段: %+v", got)
	}
	if len(changes.inputs) != 1 {
		t.Fatalf("审计笔数: %+v", changes.inputs)
	}
	change := changes.inputs[0]
	if change.Before["cost_price"] != "23.50" || change.After["cost_price"] != "30.00" || change.Before["price"] != "88.00" || change.After["price"] != "88.00" {
		t.Fatalf("成本审计使用锁前快照: before=%v after=%v", change.Before, change.After)
	}
}

func TestUpdateVariantCostRollsBackWhenAuditFails(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	p, err := f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "成本回滚", Slug: "cost-rollback"})
	if err != nil {
		t.Fatal(err)
	}
	id := p.Variants[0].ID
	var before, after productmodel.VariantEntity
	if err = f.db.Where("id = ?", id).First(&before).Error; err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected cost audit failure")
	f.rewireProductChanges(t, &failingChangesPort{err: injected})
	if err = f.svc.UpdateVariantCost(ctx, f.projectID, id, 30, ""); !errors.Is(err, injected) {
		t.Fatalf("审计错误未返回: %v", err)
	}
	if err = f.db.Where("id = ?", id).First(&after).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("成本与审计未原子回滚: before=%+v after=%+v", before, after)
	}
}

func TestUpdateProductKeepsInterleavedPublicationAndAudit(t *testing.T) {
	for _, explicitStatus := range []bool{false, true} {
		t.Run(fmt.Sprintf("explicit_status_%t", explicitStatus), func(t *testing.T) {
			f := newFixture(t)
			if f == nil {
				return
			}
			ctx := context.Background()
			p, err := f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "旧名称", Slug: "product-interleave"})
			if err != nil {
				t.Fatal(err)
			}
			changes := &capturedProductChanges{}
			f.rewireProductChanges(t, changes)
			publishedAt := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
			fired := afterProductSnapshot(t, f.db, "products", func() error {
				return rls.InProjectScope(ctx, f.db, f.projectID, func(tx *gorm.DB) error {
					return tx.Model(&productmodel.ProductEntity{}).Where("id = ?", p.ID).Updates(map[string]any{"name": "并发新名称", "status": productenums.StatusPublished, "published_at": publishedAt}).Error
				})
			})
			subtitle := "本次副标题"
			req := &productdto.UpdateReq{ID: p.ID, ProjectID: f.projectID, Subtitle: &subtitle}
			status := productenums.StatusPublished
			if explicitStatus {
				req.Status = &status
			}
			if _, err = f.svc.Update(ctx, req); err != nil {
				t.Fatal(err)
			}
			if !*fired {
				t.Fatal("未触发锁前交错")
			}
			var got productmodel.ProductEntity
			if err = rls.InProjectScope(ctx, f.db, f.projectID, func(tx *gorm.DB) error { return tx.Where("id = ?", p.ID).First(&got).Error }); err != nil {
				t.Fatal(err)
			}
			if got.Name != "并发新名称" || got.Subtitle != subtitle || got.Status != status || got.PublishedAt == nil || !got.PublishedAt.Equal(publishedAt) {
				t.Fatalf("局部编辑覆盖并发上架事实: name=%s status=%s published=%v", got.Name, got.Status, got.PublishedAt)
			}
			if len(changes.inputs) != 1 || changes.inputs[0].Before["name"] != "并发新名称" || changes.inputs[0].Before["status"] != status {
				t.Fatalf("审计使用锁前旧快照: %+v", changes.inputs)
			}
		})
	}
}

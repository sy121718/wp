package rlstest

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
	inventoryservice "go_wp/internal/module/inventory/service"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/pkg/rls"
)

type invScope struct {
	db  *gorm.DB
	im  *inventorymodel.Model
	svc *inventoryservice.Service

	pA, pB string

	whA, whB       string
	parentA, compA string
	parentB, compB string
}

func newInvScope(t *testing.T) *invScope {
	t.Helper()
	db, _ := rlsFixture(t)
	pA, pB := uuid.NewString(), uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedProject(t, db, pB, "工程 B")

	im := inventorymodel.NewModel(db)
	svc := inventoryservice.NewService(im, projectservice.NewService(projectmodel.NewProjectModel(db)))

	f := &invScope{db: db, im: im, svc: svc, pA: pA, pB: pB}
	f.whA, f.parentA, f.compA = seedInvProject(t, db, pA, "A")
	f.whB, f.parentB, f.compB = seedInvProject(t, db, pB, "B")
	return f
}

func seedInvProject(t *testing.T, db *gorm.DB, projectID, sfx string) (warehouseID, parentVariantID, componentVariantID string) {
	t.Helper()
	warehouseID = seedWarehouse(t, db, projectID, "WH"+sfx)
	parentProductID, parentVariantID := seedProductWithVariant(t, db, projectID, "父件 "+sfx)
	componentProductID, componentVariantID := seedProductWithVariant(t, db, projectID, "子件 "+sfx)
	seedStockQty(t, db, projectID, parentProductID, parentVariantID, warehouseID, 10)
	seedStockQty(t, db, projectID, componentProductID, componentVariantID, warehouseID, 10)
	return warehouseID, parentVariantID, componentVariantID
}

func seedStockQty(t *testing.T, db *gorm.DB, projectID, productID, variantID, warehouseID string, qty int) {
	t.Helper()
	now := time.Now()

	track := true
	_, err := inventorymodel.NewModel(db).EnsureStock(context.Background(), &inventorymodel.StockEntity{
		ID: uuid.NewString(), ProjectID: projectID, WarehouseID: warehouseID,
		ProductID: &productID, VariantID: &variantID, SKUCode: "SKU-" + variantID[:8],
		Quantity: qty, TrackQuantity: track, Metadata: []byte("{}"), CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("写入库存行失败（RLS 生效时写入必须经 InProjectScope）: %v", err)
	}
}

func seedBuiltinReason(t *testing.T, db *gorm.DB, code, direction string) {
	t.Helper()
	err := inventorymodel.NewModel(db).CreateReason(context.Background(), &inventorymodel.ReasonEntity{
		ProjectID: nil, Code: code, Name: "护栏原因 " + code, Direction: direction,
		IsBuiltin: true, Status: "active", Sort: 0,
		CreateTime: time.Now(), UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("写入内置变动原因失败: %v", err)
	}
}

func seedMovement(t *testing.T, db *gorm.DB, projectID, warehouseID, variantID, direction string) {
	t.Helper()
	im := inventorymodel.NewModel(db)
	now := time.Now().UTC()
	id := uuid.NewString()
	err := im.Transaction(context.Background(), func(tx *gorm.DB) error {
		return im.CreateMovementsTx(context.Background(), tx, []*inventorymodel.MovementEntity{{
			ID: id, ProjectID: projectID, WarehouseID: warehouseID,
			ProductID: inventorymodel.StringPtr(uuid.NewString()), VariantID: inventorymodel.StringPtr(variantID),
			SKUCode:   "SKU-" + variantID[:8],
			Direction: direction, Quantity: 1, Delta: 1, QuantityBefore: 0, QuantityAfter: 1,
			ReasonCode: "guard_in", SourceType: "guard", SourceRef: id,
			BatchID: id, CreatedAt: now,
		}})
	})
	if err != nil {
		t.Fatalf("写入库存流水失败（inventory_stock_movements 是分区表，scope 设在调用方事务上）: %v", err)
	}
}

func TestRLS_InventoryScope_MovementListAndCount(t *testing.T) {
	f := newInvScope(t)
	ctx := context.Background()

	seedMovement(t, f.db, f.pA, f.whA, f.parentA, inventoryenums.DirectionIn)
	seedMovement(t, f.db, f.pA, f.whA, f.parentA, inventoryenums.DirectionOut)
	seedMovement(t, f.db, f.pB, f.whB, f.parentB, inventoryenums.DirectionIn)

	rowsA, err := f.im.ListMovementRows(ctx, inventorymodel.MovementFilter{ProjectID: f.pA}, 0, 0)
	if err != nil {
		t.Fatalf("读工程 A 的流水失败: %v", err)
	}
	if len(rowsA) != 2 {
		t.Fatalf("工程 A 应有 2 条流水，实际 %d 条 —— 缺作用域时这里是静默空集", len(rowsA))
	}
	for _, r := range rowsA {
		if r.ProjectID != f.pA {
			t.Errorf("工程 A 的列表串进了他工程的行: %+v", r)
		}

		if r.WarehouseCode == "" || r.WarehouseName == "" {
			t.Errorf("流水行应带仓库投影（join inventory_warehouses），实际 %+v", r)
		}
	}

	rowsB, err := f.im.ListMovementRows(ctx, inventorymodel.MovementFilter{ProjectID: f.pB}, 0, 0)
	if err != nil {
		t.Fatalf("读工程 B 的流水失败: %v", err)
	}
	if len(rowsB) != 1 || rowsB[0].ProjectID != f.pB {
		t.Fatalf("工程 B 应只见自己的 1 条流水，实际 %+v", rowsB)
	}

	nA, err := f.im.CountMovements(ctx, inventorymodel.MovementFilter{ProjectID: f.pA})
	if err != nil {
		t.Fatalf("数工程 A 的流水失败: %v", err)
	}
	if nA != 2 {
		t.Errorf("工程 A 的流水条数应为 2，实际 %d —— 缺作用域时恒 0", nA)
	}
	nB, err := f.im.CountMovements(ctx, inventorymodel.MovementFilter{ProjectID: f.pB})
	if err != nil {
		t.Fatalf("数工程 B 的流水失败: %v", err)
	}
	if nB != 1 {
		t.Errorf("工程 B 的流水条数应为 1，实际 %d", nB)
	}
}

func TestRLS_InventoryScope_StockRowsList(t *testing.T) {
	f := newInvScope(t)
	ctx := context.Background()

	rowsA, err := f.im.ListStockRows(ctx, inventorymodel.StockFilter{ProjectID: f.pA}, 0, 0)
	if err != nil {
		t.Fatalf("读工程 A 的库存列表失败: %v", err)
	}
	if len(rowsA) != 2 {
		t.Fatalf("工程 A 应有 2 行库存（父件 + 子件），实际 %d 行 —— 缺作用域时列表静默空集", len(rowsA))
	}
	for _, r := range rowsA {
		if r.ProjectID != f.pA {
			t.Errorf("工程 A 的列表串进了他工程的行: %+v", r)
		}
		if r.WarehouseCode == "" {
			t.Errorf("库存行应带仓库投影（join inventory_warehouses），实际 %+v", r)
		}
	}

	rowsB, err := f.im.ListStockRows(ctx, inventorymodel.StockFilter{ProjectID: f.pB}, 0, 0)
	if err != nil {
		t.Fatalf("读工程 B 的库存列表失败: %v", err)
	}
	if len(rowsB) != 2 {
		t.Fatalf("工程 B 应有自己的 2 行库存，实际 %d 行", len(rowsB))
	}
	for _, r := range rowsB {
		if r.ProjectID != f.pB {
			t.Errorf("工程 B 的列表串进了他工程的行: %+v", r)
		}
	}

	onlyA, err := f.im.ListStockRows(ctx, inventorymodel.StockFilter{
		ProjectID: f.pA, VariantID: f.parentA,
	}, 10, 0)
	if err != nil {
		t.Fatalf("按变体过滤读库存失败: %v", err)
	}
	if len(onlyA) != 1 || onlyA[0].VariantID != f.parentA {
		t.Fatalf("按变体过滤应只出 1 行，实际 %+v", onlyA)
	}
}

func TestRLS_InventoryScope_RejectsMissingProjectID(t *testing.T) {
	f := newInvScope(t)
	ctx := context.Background()

	for _, bad := range []string{"", "not-a-uuid"} {
		label := bad
		if label == "" {
			label = "空串"
		}
		cases := []struct {
			name string
			run  func() error
		}{
			{"ListStockRows(inventory_stocks)", func() error {
				_, err := f.im.ListStockRows(ctx, inventorymodel.StockFilter{ProjectID: bad}, 10, 0)
				return err
			}},
			{"ListMovementRows(inventory_stock_movements)", func() error {
				_, err := f.im.ListMovementRows(ctx, inventorymodel.MovementFilter{ProjectID: bad}, 10, 0)
				return err
			}},
			{"CountMovements(inventory_stock_movements)", func() error {
				_, err := f.im.CountMovements(ctx, inventorymodel.MovementFilter{ProjectID: bad})
				return err
			}},
		}
		for _, c := range cases {
			if err := c.run(); err == nil || err.Error() != rls.ErrInvalidProjectID.Error() {
				t.Errorf("工程 id 为%s 时 %s 应返回 rls.ErrInvalidProjectID，实际 %v", label, c.name, err)
			}
		}
	}

	if _, err := f.im.ListStockRows(ctx, inventorymodel.StockFilter{ProjectID: f.pA}, 10, 0); err != nil {
		t.Fatalf("工程 A 读自己的库存列表应成功，实际 %v", err)
	}
}

func TestRLS_InventoryScope_FailClosedWithoutScope(t *testing.T) {
	f := newInvScope(t)
	ctx := context.Background()

	seedMovement(t, f.db, f.pA, f.whA, f.parentA, inventoryenums.DirectionIn)

	bare := []struct {
		table string
		query func() (int64, error)
	}{
		{"inventory_stocks", func() (int64, error) {
			var n int64
			err := f.db.Table("inventory_stocks").Where("project_id = ?", f.pA).Count(&n).Error
			return n, err
		}},
		{"inventory_stock_movements", func() (int64, error) {
			var n int64
			err := f.db.Table("inventory_stock_movements").Where("project_id = ?", f.pA).Count(&n).Error
			return n, err
		}},
	}
	for _, c := range bare {
		n, err := c.query()
		if err != nil {
			t.Fatalf("裸查 %s 失败: %v", c.table, err)
		}
		if n != 0 {
			t.Errorf("未设 app.project_id 时 %s 应 0 行可见（fail closed），实际 %d 行", c.table, n)
		}
	}

	if rows, err := f.im.ListMovementRows(ctx, inventorymodel.MovementFilter{ProjectID: f.pA}, 0, 0); err != nil || len(rows) != 1 {
		t.Fatalf("设了作用域应读得到流水，实际 n=%d err=%v", len(rows), err)
	}

	var raw int64
	if err := f.db.Table("inventory_stocks").Count(&raw).Error; err != nil {
		t.Fatalf("裸查失败: %v", err)
	}
	if raw != 0 {
		t.Fatalf("作用域事务结束后应重新 fail closed（0 行），实际 %d 行 —— 会话变量泄漏", raw)
	}
}

package rlstest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	inventorymodel "go_wp/internal/module/inventory/model"
	productmodel "go_wp/internal/module/product/model"
	purchasemodel "go_wp/internal/module/purchase/model"
	"go_wp/pkg/rls"
)

func seedProductWithVariant(t *testing.T, db *gorm.DB, projectID, name string) (productID, variantID string) {
	t.Helper()
	now := time.Now()
	productID, variantID = uuid.NewString(), uuid.NewString()
	err := productmodel.NewModel(db).CreateWithVariants(context.Background(), &productmodel.ProductEntity{
		ID: productID, ProjectID: projectID,
		Name: name, Slug: "guard-" + productID,
		Status:      "draft",
		Description: []byte("{}"), Images: []byte("[]"), ImageAlts: []byte("[]"),
		AttributeIDs: []byte("[]"), CategoryIDs: []byte("[]"), TagIDs: []byte("[]"),
		RelatedIDs: []byte("[]"), Metadata: []byte("{}"),
		CreatedAt: now, UpdatedAt: now,
	}, []*productmodel.VariantEntity{{
		ID: variantID, ProductID: productID,
		SKUCode: "SKU-" + variantID[:8], Barcode: "", Image: "",
		OptionValues: []byte("[]"), Metadata: []byte("{}"),
		Enabled: true, CreatedAt: now, UpdatedAt: now,
	}})
	if err != nil {
		t.Fatalf("写入商品与变体失败（RLS 生效时写入必须经 InProjectScope）: %v", err)
	}
	return productID, variantID
}

func seedTag(t *testing.T, db *gorm.DB, projectID, name string) string {
	t.Helper()
	now := time.Now()
	id := uuid.NewString()
	err := productmodel.NewModel(db).CreateTag(context.Background(), &productmodel.ProductTagEntity{
		ID: id, ProjectID: projectID, Name: name, Slug: "tag-" + id,
		Kind: "manual", RuleType: "", RuleParams: []byte("{}"),
		Metadata: []byte("{}"), CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("写入标签失败: %v", err)
	}
	return id
}

func seedWarehouse(t *testing.T, db *gorm.DB, projectID, code string) string {
	t.Helper()
	now := time.Now()
	id := uuid.NewString()
	err := inventorymodel.NewModel(db).CreateWarehouse(context.Background(), &inventorymodel.WarehouseEntity{
		ID: id, ProjectID: projectID, Code: code, Name: "护栏仓 " + code,
		Status: "active", IsDefault: false, Sort: 0,

		Type:     "self",
		Config:   []byte("{}"),
		Metadata: []byte("{}"), CreatedAt: now, UpdatedAt: now,
	}, false)
	if err != nil {
		t.Fatalf("写入仓库失败: %v", err)
	}
	return id
}

func seedSource(t *testing.T, db *gorm.DB, projectID, code string) string {
	t.Helper()
	now := time.Now()
	id := uuid.NewString()
	err := purchasemodel.NewModel(db).CreateSource(context.Background(), &purchasemodel.SourceEntity{
		ID: id, ProjectID: projectID, Code: code, Name: "护栏货源 " + code,
		Type: "external", Status: "active", Config: []byte("{}"),
		Metadata: []byte("{}"), CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("写入货源失败: %v", err)
	}
	return id
}

func seedStock(t *testing.T, db *gorm.DB, projectID, productID, variantID, warehouseID string) string {
	t.Helper()
	now := time.Now()
	id := uuid.NewString()

	out, err := inventorymodel.NewModel(db).EnsureStock(context.Background(), &inventorymodel.StockEntity{
		ID: id, ProjectID: projectID, WarehouseID: warehouseID,
		ProductID: &productID, VariantID: &variantID, SKUCode: "SKU-" + variantID[:8],
		Quantity: 7, TrackQuantity: true, Metadata: []byte("{}"), CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("写入库存行失败: %v", err)
	}
	return out.ID
}

func seedPurchaseOrder(t *testing.T, db *gorm.DB, projectID, sourceID, warehouseID, code string) string {
	t.Helper()
	now := time.Now()
	id := uuid.NewString()
	err := purchasemodel.NewModel(db).Transaction(context.Background(), func(tx *gorm.DB) error {
		return purchasemodel.NewModel(db).CreatePurchaseOrderTx(context.Background(), tx, &purchasemodel.PurchaseOrderEntity{
			ID: id, ProjectID: projectID, Code: code,
			SourceID: sourceID, WarehouseID: warehouseID, Status: "ordered",
			OrderedAt: now, Remark: "", OperatorID: "",
			Metadata: []byte("{}"), CreatedAt: now, UpdatedAt: now,
		}, nil)
	})
	if err != nil {
		t.Fatalf("写入采购单失败: %v", err)
	}
	return id
}

func productScopeFixture(t *testing.T, db *gorm.DB) (pA, pB string, aIDs, bIDs map[string]string) {
	t.Helper()
	pA, pB = uuid.NewString(), uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedProject(t, db, pB, "工程 B")

	aIDs, bIDs = map[string]string{}, map[string]string{}
	for _, pair := range []struct {
		project string
		out     map[string]string
		suffix  string
	}{{pA, aIDs, "A"}, {pB, bIDs, "B"}} {
		pid, out, sfx := pair.project, pair.out, pair.suffix
		productID, variantID := seedProductWithVariant(t, db, pid, "护栏商品 "+sfx)
		out["products"] = productID
		out["variant"] = variantID
		out["tag"] = seedTag(t, db, pid, "护栏标签 "+sfx)
		whID := seedWarehouse(t, db, pid, "WH"+sfx)
		out["warehouse"] = whID
		out["stock"] = seedStock(t, db, pid, productID, variantID, whID)
		srcID := seedSource(t, db, pid, "SRC"+sfx)
		out["purchaseOrder"] = seedPurchaseOrder(t, db, pid, srcID, whID, "PO-"+sfx)
	}
	return pA, pB, aIDs, bIDs
}

func TestRLS_ProductScope_ReadsOwnProjectOnly(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	pm := productmodel.NewModel(db)
	im := inventorymodel.NewModel(db)
	pm2 := purchasemodel.NewModel(db)

	pA, pB, aIDs, bIDs := productScopeFixture(t, db)

	if _, err := pm.Get(ctx, aIDs["products"], pA); err != nil {
		t.Fatalf("本工程按 id 读商品应成功，实际 %v", err)
	}
	if _, err := pm.GetTag(ctx, aIDs["tag"], pA); err != nil {
		t.Fatalf("本工程按 id 读标签应成功，实际 %v", err)
	}
	if _, err := im.GetStock(ctx, aIDs["stock"], pA); err != nil {
		t.Fatalf("本工程按 id 读库存行应成功，实际 %v", err)
	}
	if _, err := pm2.GetPurchaseOrder(ctx, aIDs["purchaseOrder"], pA); err != nil {
		t.Fatalf("本工程按 id 读采购单应成功，实际 %v", err)
	}

	cross := []struct {
		table string
		run   func() error
	}{
		{"products", func() error { _, err := pm.Get(ctx, bIDs["products"], pA); return err }},
		{"product_tags", func() error { _, err := pm.GetTag(ctx, bIDs["tag"], pA); return err }},
		{"inventory_stocks", func() error { _, err := im.GetStock(ctx, bIDs["stock"], pA); return err }},
		{"inventory_purchase_orders", func() error { _, err := pm2.GetPurchaseOrder(ctx, bIDs["purchaseOrder"], pA); return err }},
	}
	for _, c := range cross {
		if err := c.run(); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Errorf("拿 A 的作用域按 id 读 B 的 %s，应 ErrRecordNotFound（隔离生效），实际 %v", c.table, err)
		}
	}

	if _, err := pm.Get(ctx, bIDs["products"], pB); err != nil {
		t.Fatalf("工程 B 读自己的商品应成功，实际 %v", err)
	}
}

func TestRLS_ProductScope_FailClosedWithoutScope(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()

	pA, _, aIDs, _ := productScopeFixture(t, db)

	cases := []struct {
		table     string
		query     func() (int64, error)
		projectID string
	}{
		{"products", func() (int64, error) {
			var n int64
			err := db.Table("products").Where("project_id = ?", pA).Count(&n).Error
			return n, err
		}, pA},
		{"product_tags", func() (int64, error) {
			var n int64
			err := db.Table("product_tags").Where("project_id = ?", pA).Count(&n).Error
			return n, err
		}, pA},
		{"inventory_stocks", func() (int64, error) {
			var n int64
			err := db.Table("inventory_stocks").Where("project_id = ?", pA).Count(&n).Error
			return n, err
		}, pA},
		{"inventory_purchase_orders", func() (int64, error) {
			var n int64
			err := db.Table("inventory_purchase_orders").Where("project_id = ?", pA).Count(&n).Error
			return n, err
		}, pA},
	}
	for _, c := range cases {
		n, err := c.query()
		if err != nil {
			t.Fatalf("裸查 %s 失败: %v", c.table, err)
		}
		if n != 0 {
			t.Errorf("未设 app.project_id 时 %s 应 0 行可见（fail closed），实际 %d 行", c.table, n)
		}
	}

	if row, err := inventorymodel.NewModel(db).GetStock(ctx, aIDs["stock"], pA); err != nil || row == nil {
		t.Fatalf("设了作用域应读得到库存行，实际 err=%v", err)
	}

	var raw int64
	if err := db.Table("products").Count(&raw).Error; err != nil {
		t.Fatalf("裸查失败: %v", err)
	}
	if raw != 0 {
		t.Fatalf("作用域事务结束后应重新 fail closed（0 行），实际 %d 行 —— 会话变量泄漏", raw)
	}
}

func TestRLS_ProductScope_RejectsMissingScope(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	pm := productmodel.NewModel(db)
	im := inventorymodel.NewModel(db)

	pA, _, aIDs, _ := productScopeFixture(t, db)

	for _, bad := range []string{"", "not-a-uuid"} {
		label := bad
		if label == "" {
			label = "空串"
		}
		cases := []struct {
			name string
			run  func() error
		}{
			{"Delete(products)", func() error { return pm.Delete(ctx, aIDs["products"], bad) }},
			{"ListRatings(product_ratings)", func() error {
				_, err := pm.ListRatings(ctx, aIDs["products"], bad)
				return err
			}},
			{"DeleteRating(product_ratings)", func() error { return pm.DeleteRating(ctx, aIDs["products"], bad) }},
			{"ListForCollection(products)", func() error {
				_, err := pm.ListForCollection(ctx, productmodel.CollectionFilter{ProjectID: bad}, 10, 0)
				return err
			}},
			{"CountForCollection(products)", func() error {
				_, err := pm.CountForCollection(ctx, productmodel.CollectionFilter{ProjectID: bad})
				return err
			}},
			{"DeleteWarehouse(inventory_warehouses)", func() error { return im.DeleteWarehouse(ctx, aIDs["warehouse"], bad) }},
			{"CountNonZeroStocks(inventory_stocks)", func() error {
				_, err := im.CountNonZeroStocks(ctx, aIDs["warehouse"], bad)
				return err
			}},
			{"ListStockFactsByIDs(inventory_stocks)", func() error {
				_, err := im.ListStockFactsByIDs(ctx, bad, []string{aIDs["stock"]})
				return err
			}},
		}
		for _, c := range cases {
			err := c.run()
			if !errors.Is(err, rls.ErrInvalidProjectID) {
				t.Errorf("工程 id 为%s 时 %s 应返回 rls.ErrInvalidProjectID，实际 %v", label, c.name, err)
			}
		}
	}

	if n, err := im.CountNonZeroStocks(ctx, aIDs["warehouse"], pA); err != nil || n != 1 {
		t.Fatalf("工程 A 数自己仓的非零库存行应得 1，实际 n=%d err=%v", n, err)
	}
}

func TestRLS_ProductScope_DeleteStaysInProject(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	pm := productmodel.NewModel(db)
	im := inventorymodel.NewModel(db)

	pA, pB, aIDs, bIDs := productScopeFixture(t, db)

	if err := pm.Delete(ctx, bIDs["products"], pA); err != nil {
		t.Fatalf("跨工程删除不该报错（策略是「不可见」而非「拒绝」），实际 %v", err)
	}
	if _, err := pm.Get(ctx, bIDs["products"], pB); err != nil {
		t.Fatalf("拿 A 的作用域删 B 的商品：B 的行必须仍在，实际读不到 %v", err)
	}

	if err := im.DeleteWarehouse(ctx, bIDs["warehouse"], pA); err != nil {
		t.Fatalf("跨工程删仓不该报错，实际 %v", err)
	}
	if _, err := im.GetWarehouse(ctx, bIDs["warehouse"], pB); err != nil {
		t.Fatalf("拿 A 的作用域删 B 的仓：B 的仓必须仍在，实际 %v", err)
	}
	if _, err := im.GetStock(ctx, bIDs["stock"], pB); err != nil {
		t.Fatalf("拿 A 的作用域删 B 的仓：B 的库存行必须仍在（否则就是级联丢账），实际 %v", err)
	}

	freeA := seedWarehouse(t, db, pB, "WHFREE-A")
	if err := im.DeleteWarehouse(ctx, freeA, pA); err != nil {
		t.Fatalf("跨工程删空仓不该报错，实际 %v", err)
	}
	if _, err := im.GetWarehouse(ctx, freeA, pB); err != nil {
		t.Fatalf("拿 A 的作用域删 B 的空仓：必须仍在，实际 %v", err)
	}
	if err := im.DeleteWarehouse(ctx, freeA, pB); err != nil {
		t.Fatalf("工程 B 删自己的空仓应成功，实际 %v", err)
	}
	if _, err := im.GetWarehouse(ctx, freeA, pB); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("删掉之后应读不到该仓，实际 %v", err)
	}

	if _, err := pm.Get(ctx, aIDs["products"], pA); err != nil {
		t.Fatalf("工程 A 的商品不该受跨工程删除影响，实际 %v", err)
	}
	if _, err := im.GetWarehouse(ctx, aIDs["warehouse"], pA); err != nil {
		t.Fatalf("工程 A 的仓不该受跨工程删除影响，实际 %v", err)
	}
}

func TestRLS_ProductScope_StockGuardSeesOwnProject(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	im := inventorymodel.NewModel(db)

	pA, pB, aIDs, _ := productScopeFixture(t, db)

	byWarehouse, err := im.CountNonZeroStocks(ctx, aIDs["warehouse"], pA)
	if err != nil {
		t.Fatalf("数本工程仓的非零库存失败: %v", err)
	}
	if byWarehouse != 1 {
		t.Errorf("本工程仓内有 1 行 7 件库存，守卫应数出 1，实际 %d", byWarehouse)
	}
	factsA, err := im.ListStockFactsByIDs(ctx, pA, []string{aIDs["stock"]})
	if err != nil {
		t.Fatalf("读本工程货行事实失败: %v", err)
	}
	if len(factsA) != 1 || factsA[0].Quantity != 7 {
		t.Errorf("本工程货行有 1 行 7 件，应读到 7，实际 %+v", factsA)
	}

	if n, err := im.CountNonZeroStocks(ctx, aIDs["warehouse"], pB); err != nil || n != 0 {
		t.Errorf("拿 B 的作用域数 A 的仓应得 0（行不可见），实际 n=%d err=%v", n, err)
	}
	if rows, err := im.ListStockFactsByIDs(ctx, pB, []string{aIDs["stock"]}); err != nil || len(rows) != 0 {
		t.Errorf("拿 B 的作用域读 A 的货行应得 0 行（行不可见），实际 n=%d err=%v", len(rows), err)
	}
}

func TestRLS_ProductScope_RatingWriteAndReadInsideProject(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	pm := productmodel.NewModel(db)

	pA, _, aIDs, _ := productScopeFixture(t, db)

	ratingID := uuid.NewString()
	now := time.Now()
	err := pm.CreateRating(ctx, &productmodel.ProductRatingEntity{
		ID: ratingID, ProjectID: pA, ProductID: aIDs["products"],
		Score: 4.5, Source: "manual", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("写入评分失败: %v", err)
	}

	rows, err := pm.ListRatings(ctx, aIDs["products"], pA)
	if err != nil {
		t.Fatalf("读评分明细失败: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != ratingID {
		t.Fatalf("应读到刚写入的那一条评分，实际 %d 行", len(rows))
	}

	if err = pm.DeleteRating(ctx, ratingID, pA); err != nil {
		t.Fatalf("删评分失败: %v", err)
	}
	rows, err = pm.ListRatings(ctx, aIDs["products"], pA)
	if err != nil {
		t.Fatalf("删后读评分明细失败: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("删掉的评分不该还在，实际 %d 行", len(rows))
	}
}

func TestRLS_ProductScope_CollectionRespectsProject(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	pm := productmodel.NewModel(db)

	pA, pB, aIDs, _ := productScopeFixture(t, db)

	rows, err := pm.ListForCollection(ctx, productmodel.CollectionFilter{ProjectID: pA}, 100, 0)
	if err != nil {
		t.Fatalf("集合源取数失败: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != aIDs["products"] {
		t.Fatalf("集合源应只出工程 A 的 1 个商品，实际 %d 行", len(rows))
	}

	n, err := pm.CountForCollection(ctx, productmodel.CollectionFilter{ProjectID: pA})
	if err != nil {
		t.Fatalf("集合源计数失败: %v", err)
	}
	if n != 1 {
		t.Errorf("工程 A 的集合总量应为 1（与取数一致），实际 %d", n)
	}

	others, err := pm.ListForCollection(ctx, productmodel.CollectionFilter{ProjectID: pB}, 100, 0)
	if err != nil {
		t.Fatalf("集合源取 B 失败: %v", err)
	}
	if len(others) != 1 {
		t.Errorf("工程 B 的集合总量应为 1，实际 %d 行", len(others))
	}
}

func TestRLS_ProductScope_BatchByIDsRequiresScope(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	pm := productmodel.NewModel(db)

	pA, _, aIDs, bIDs := productScopeFixture(t, db)

	rows, err := pm.ListByIDs(ctx, []string{aIDs["products"]}, pA)
	if err != nil {
		t.Fatalf("本工程批量按 id 读应成功，实际 %v", err)
	}
	if len(rows) != 1 || rows[0].ID != aIDs["products"] {
		t.Fatalf("本工程应读到 1 行，实际 %d 行", len(rows))
	}

	mixed, err := pm.ListByIDs(ctx, []string{aIDs["products"], bIDs["products"]}, pA)
	if err != nil {
		t.Fatalf("混合入参读取不应报错，实际 %v", err)
	}
	if len(mixed) != 1 || mixed[0].ID != aIDs["products"] {
		t.Fatalf("混合入参应只出工程 A 的 1 行，实际 %d 行", len(mixed))
	}

	for _, bad := range []string{"", "not-a-uuid"} {
		if _, err := pm.ListByIDs(ctx, []string{aIDs["products"]}, bad); !errors.Is(err, rls.ErrInvalidProjectID) {
			t.Errorf("工程 id 为 %q 时应返回 rls.ErrInvalidProjectID，实际 %v", bad, err)
		}
	}
}

func TestRLS_ProductScope_NarrowProjectionsRequireScope(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	pm := productmodel.NewModel(db)

	pA, _, aIDs, bIDs := productScopeFixture(t, db)
	productA, productB := aIDs["products"], bIDs["products"]

	readers := []struct {
		name  string
		batch bool
		read  func(projectID string, ids []string) (idsRead []string, err error)
	}{
		{"GetRefFacts", false, func(projectID string, ids []string) ([]string, error) {
			row, err := pm.GetRefFacts(ctx, ids[0], projectID)
			if err != nil {
				return nil, err
			}
			return []string{row.ID}, nil
		}},
		{"GetSummaryFacts", false, func(projectID string, ids []string) ([]string, error) {
			row, err := pm.GetSummaryFacts(ctx, ids[0], projectID)
			if err != nil {
				return nil, err
			}
			return []string{row.ID}, nil
		}},
		{"GetPublicOptionFacts", false, func(projectID string, ids []string) ([]string, error) {
			row, err := pm.GetPublicOptionFacts(ctx, ids[0], projectID)
			if err != nil {
				return nil, err
			}
			return []string{row.ID}, nil
		}},
		{"ListStatusFactsByIDs", true, func(projectID string, ids []string) ([]string, error) {
			rows, err := pm.ListStatusFactsByIDs(ctx, ids, projectID)
			return narrowIDsAndProject(rows, projectID, err)
		}},
		{"ListNameFactsByIDs", true, func(projectID string, ids []string) ([]string, error) {
			rows, err := pm.ListNameFactsByIDs(ctx, ids, projectID)
			return narrowIDsAndProject(rows, projectID, err)
		}},
		{"ListIDFactsByIDs", true, func(projectID string, ids []string) ([]string, error) {
			rows, err := pm.ListIDFactsByIDs(ctx, ids, projectID)
			return narrowIDsAndProject(rows, projectID, err)
		}},
	}

	for _, r := range readers {
		t.Run(r.name, func(t *testing.T) {

			got, err := r.read(pA, []string{productA})
			if err != nil {
				t.Fatalf("本工程读取应成功，实际 %v", err)
			}
			assertIDsEqual(t, r.name+" 本工程", got, []string{productA})

			got, err = r.read(pA, []string{productB})
			if r.batch {
				if err != nil {
					t.Fatalf("批量读单独点名外工程不该报错，实际 %v", err)
				}
				assertIDsEqual(t, r.name+" 外工程单独点名", got, nil)
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("单行读外工程的 id 应 ErrRecordNotFound，实际 %v", err)
			}

			got, err = r.read(pA, []string{productA, productB})
			if err != nil {
				t.Fatalf("混合入参读取不应报错，实际 %v", err)
			}
			assertIDsEqual(t, r.name+" 混合入参", got, []string{productA})

			for _, bad := range []string{"", "not-a-uuid"} {
				if _, err := r.read(bad, []string{productA}); !errors.Is(err, rls.ErrInvalidProjectID) {
					t.Errorf("工程 id 为 %q 时应返回 rls.ErrInvalidProjectID，实际 %v", bad, err)
				}
			}
		})
	}

	for _, bad := range []string{"", "not-a-uuid"} {
		if _, err := pm.ListVariantPublicFacts(ctx, []string{aIDs["variant"]}, bad); !errors.Is(err, rls.ErrInvalidProjectID) {
			t.Errorf("ListVariantPublicFacts 工程 id 为 %q 时应返回 rls.ErrInvalidProjectID，实际 %v", bad, err)
		}
	}
	if _, err := pm.ListVariantPublicFacts(ctx, []string{aIDs["variant"]}, pA); err != nil {
		t.Errorf("ListVariantPublicFacts 本工程读取应成功，实际 %v", err)
	}
}

func TestRLS_ProductScope_BulkNarrowReadsHitExactSet(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	pm := productmodel.NewModel(db)

	pA, pB, aIDs, bIDs := productScopeFixture(t, db)

	extraA, _ := seedProductWithVariant(t, db, pA, "A 补充商品")
	extraB, _ := seedProductWithVariant(t, db, pB, "B 补充商品")

	readers := []struct {
		name string
		read func(projectID string, ids []string) ([]string, error)
	}{
		{"ListStatusFactsByIDs", func(projectID string, ids []string) ([]string, error) {
			rows, err := pm.ListStatusFactsByIDs(ctx, ids, projectID)
			return narrowIDsAndProject(rows, projectID, err)
		}},
		{"ListNameFactsByIDs", func(projectID string, ids []string) ([]string, error) {
			rows, err := pm.ListNameFactsByIDs(ctx, ids, projectID)
			return narrowIDsAndProject(rows, projectID, err)
		}},
		{"ListIDFactsByIDs", func(projectID string, ids []string) ([]string, error) {
			rows, err := pm.ListIDFactsByIDs(ctx, ids, projectID)
			return narrowIDsAndProject(rows, projectID, err)
		}},
	}
	all := []string{aIDs["products"], extraA, bIDs["products"], extraB}
	for _, r := range readers {
		got, err := r.read(pA, all)
		if err != nil {
			t.Fatalf("%s 作用域 A 整批读取失败：%v", r.name, err)
		}
		assertIDsEqual(t, r.name+" 作用域 A 整批", got, []string{aIDs["products"], extraA})

		got, err = r.read(pA, []string{bIDs["products"], extraB})
		if err != nil {
			t.Fatalf("%s 只点名外工程不该报错：%v", r.name, err)
		}
		assertIDsEqual(t, r.name+" 只点名外工程", got, nil)
	}
}

func narrowIDsAndProject(rows []*productmodel.ProductEntity, projectID string, err error) ([]string, error) {
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}

		if row.ProjectID == "" {
			return nil, fmt.Errorf("返回行的 project_id 为空（投影缺 project_id 列？）")
		}
		if row.ProjectID != projectID {
			return nil, fmt.Errorf("读到工程 %q 的行（期望只出 %q）", row.ProjectID, projectID)
		}
		ids = append(ids, row.ID)
	}
	return ids, nil
}

func assertIDsEqual(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s 命中 id 数应为 %d（%v），实际 %d（%v）", label, len(want), want, len(got), got)
	}
	seen := make(map[string]int, len(got))
	for _, id := range got {
		seen[id]++
	}
	for _, id := range want {
		if seen[id] == 0 {
			t.Fatalf("%s 缺少 id %s（实际 %v）", label, id, got)
		}
		seen[id]--
	}
	for id, n := range seen {
		if n != 0 {
			t.Fatalf("%s 多出 id %s（实际 %v）", label, id, got)
		}
	}
}

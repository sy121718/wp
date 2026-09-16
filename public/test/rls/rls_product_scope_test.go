package rlstest

// rls_product_scope_test.go — product 域（含 product/inventory）的工程作用域护栏（DB-009 第三批）。
//
// 这一批把 product 域的**读路径**补齐了：在它之前，该域只有写路径（Create/Update 与
// 少数 *Tx 变体）包了作用域，全部按 id 单查与列表 / 计数都是裸查询。裸查询在超级用户
// 连接下看不出任何问题（superuser 无条件绕过 RLS），换非超级角色之后才会现形 ——
// 而且是**静默返回 0 行**，不是报错。
//
// 因此这里的每一条断言都必须是「有失败能力」的，而不是「调用没报错」：
//
//   - 按 id 单查：本工程读得到、他工程读不到（拿 A 的作用域读 B 的行 ⇒ ErrRecordNotFound）。
//     把 model 里的 rls.InProjectScope 摘掉，这条在非超级角色下立刻红；
//   - 不设作用域的裸句柄读 0 行：这就是「漏包 scope」在换角色后的真实表现（fail closed 不报错），
//     也是本批要消灭的那类路径的判据。
//
// 挑这四张表是因为它们分别代表四类路径：商品主表（products）、分类学表（product_tags）、
// 库存真源（inventory_stocks）、采购单（inventory_purchase_orders）—— 且都在迁移 215 名单里。
//
// 全程跑在**非超级角色**下（rlsFixture 保证，并用 rls.BypassedRole 自检），否则断言全绿而无意义。

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	inventorymodel "go_wp/internal/module/product/inventory/model"
	productmodel "go_wp/internal/module/product/model"
)

// seedProductWithVariant 经 model 的写入路径落一行商品 + 一行变体（变体表不在 215 名单，无策略）。
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
		// 114 的 products_bundle_items_shape_check 要求它是对象且带 options 数组。
		BundleItems: []byte(`{"options":[]}`),
		CreatedAt:   now, UpdatedAt: now,
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

// seedTag 经 model 写入路径落一行手工标签。
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

// seedWarehouse 经 model 写入路径落一行仓库。
func seedWarehouse(t *testing.T, db *gorm.DB, projectID, code string) string {
	t.Helper()
	now := time.Now()
	id := uuid.NewString()
	err := inventorymodel.NewModel(db).CreateWarehouse(context.Background(), &inventorymodel.WarehouseEntity{
		ID: id, ProjectID: projectID, Code: code, Name: "护栏仓 " + code,
		Status: "active", IsDefault: false, Sort: 0,
		Metadata: []byte("{}"), CreatedAt: now, UpdatedAt: now,
	}, false)
	if err != nil {
		t.Fatalf("写入仓库失败: %v", err)
	}
	return id
}

// seedSource 经 model 写入路径落一行外部货源。
func seedSource(t *testing.T, db *gorm.DB, projectID, code string) string {
	t.Helper()
	now := time.Now()
	id := uuid.NewString()
	err := inventorymodel.NewModel(db).CreateSource(context.Background(), &inventorymodel.SourceEntity{
		ID: id, ProjectID: projectID, Code: code, Name: "护栏货源 " + code,
		Type: "external", Status: "active", Config: []byte("{}"),
		Metadata: []byte("{}"), CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("写入货源失败: %v", err)
	}
	return id
}

// seedStock 经 model 写入路径落一行库存真源（维度：SKU × 仓库）。
func seedStock(t *testing.T, db *gorm.DB, projectID, productID, variantID, warehouseID string) string {
	t.Helper()
	now := time.Now()
	id := uuid.NewString()
	out, err := inventorymodel.NewModel(db).EnsureStock(context.Background(), &inventorymodel.StockEntity{
		ID: id, ProjectID: projectID, WarehouseID: warehouseID,
		ProductID: productID, VariantID: variantID, SKUCode: "SKU-" + variantID[:8],
		Quantity: 7, Metadata: []byte("{}"), CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("写入库存行失败: %v", err)
	}
	return out.ID
}

// seedPurchaseOrder 经 model 写入路径落一行采购单（需要预置货源与收货仓）。
func seedPurchaseOrder(t *testing.T, db *gorm.DB, projectID, sourceID, warehouseID, code string) string {
	t.Helper()
	now := time.Now()
	id := uuid.NewString()
	err := inventorymodel.NewModel(db).Transaction(context.Background(), func(tx *gorm.DB) error {
		return inventorymodel.NewModel(db).CreatePurchaseOrderTx(context.Background(), tx, &inventorymodel.PurchaseOrderEntity{
			ID: id, ProjectID: projectID, Code: code,
			SourceID: sourceID, WarehouseID: warehouseID, Status: "pending",
			OrderedAt: now, Remark: "", OperatorID: "",
			Metadata: []byte("{}"), CreatedAt: now, UpdatedAt: now,
		}, nil)
	})
	if err != nil {
		t.Fatalf("写入采购单失败: %v", err)
	}
	return id
}

// productScopeFixture 一次性备好四张表各自的一行（分属工程 A / B）。
//
// 返回：本工程 A 的四行 id、工程 B 的四行 id。
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
		out["tag"] = seedTag(t, db, pid, "护栏标签 "+sfx)
		whID := seedWarehouse(t, db, pid, "WH"+sfx)
		out["warehouse"] = whID
		out["stock"] = seedStock(t, db, pid, productID, variantID, whID)
		srcID := seedSource(t, db, pid, "SRC"+sfx)
		out["purchaseOrder"] = seedPurchaseOrder(t, db, pid, srcID, whID, "PO-"+sfx)
	}
	return pA, pB, aIDs, bIDs
}

// TestRLS_ProductScope_ReadsOwnProjectOnly 按 id 单查只在工程作用域内可见。
//
// 这是「跨工程读」最直接的入口：拿工程 A 的作用域按 id 读工程 B 的行，必须
// ErrRecordNotFound —— 而不是把 B 的数据读出来。
func TestRLS_ProductScope_ReadsOwnProjectOnly(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	pm := productmodel.NewModel(db)
	im := inventorymodel.NewModel(db)

	pA, pB, aIDs, bIDs := productScopeFixture(t, db)

	// 本工程：四条路径都读得到。
	if _, err := pm.Get(ctx, aIDs["products"], pA); err != nil {
		t.Fatalf("本工程按 id 读商品应成功，实际 %v", err)
	}
	if _, err := pm.GetTag(ctx, aIDs["tag"], pA); err != nil {
		t.Fatalf("本工程按 id 读标签应成功，实际 %v", err)
	}
	if _, err := im.GetStock(ctx, aIDs["stock"], pA); err != nil {
		t.Fatalf("本工程按 id 读库存行应成功，实际 %v", err)
	}
	if _, err := im.GetPurchaseOrder(ctx, aIDs["purchaseOrder"], pA); err != nil {
		t.Fatalf("本工程按 id 读采购单应成功，实际 %v", err)
	}

	// 跨工程：拿 A 的作用域读 B 的行，一律「不存在」。
	cross := []struct {
		table string
		run   func() error
	}{
		{"products", func() error { _, err := pm.Get(ctx, bIDs["products"], pA); return err }},
		{"product_tags", func() error { _, err := pm.GetTag(ctx, bIDs["tag"], pA); return err }},
		{"inventory_stocks", func() error { _, err := im.GetStock(ctx, bIDs["stock"], pA); return err }},
		{"inventory_purchase_orders", func() error { _, err := im.GetPurchaseOrder(ctx, bIDs["purchaseOrder"], pA); return err }},
	}
	for _, c := range cross {
		if err := c.run(); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Errorf("拿 A 的作用域按 id 读 B 的 %s，应 ErrRecordNotFound（隔离生效），实际 %v", c.table, err)
		}
	}

	// 反向再确认一次：B 的作用域读得到 B 自己的行（证明上面的红不是因为行没写进去）。
	if _, err := pm.Get(ctx, bIDs["products"], pB); err != nil {
		t.Fatalf("工程 B 读自己的商品应成功，实际 %v", err)
	}
}

// TestRLS_ProductScope_FailClosedWithoutScope 未设作用域时这四张表一行都读不到。
//
// 用**裸句柄 + 同一个 WHERE 条件**读：可见性由会话变量决定，不由 WHERE 决定。
// 这条把「漏包 scope 的路径」在换角色后的真实表现钉住 —— 0 行且不报错。
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

	// 对照：经 model 的作用域路径读得到同一批行 —— 差别只在会话变量，不在 WHERE。
	if row, err := inventorymodel.NewModel(db).GetStock(ctx, aIDs["stock"], pA); err != nil || row == nil {
		t.Fatalf("设了作用域应读得到库存行，实际 err=%v", err)
	}
	// 事务结束后变量已还原（同一条连接，池里只有一条）：再次裸查仍 0 行。
	var raw int64
	if err := db.Table("products").Count(&raw).Error; err != nil {
		t.Fatalf("裸查失败: %v", err)
	}
	if raw != 0 {
		t.Fatalf("作用域事务结束后应重新 fail closed（0 行），实际 %d 行 —— 会话变量泄漏", raw)
	}
}

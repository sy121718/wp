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
		// Type（迁移 240 的 inventory_warehouses_type_check）与 Config（NOT NULL）都必须显式给：
		// gorm 会把 string 零值写空串、把 nil 的 json.RawMessage 写成 NULL，两者都不会落到 DDL 默认值上。
		Type:     "self",
		Config:   []byte("{}"),
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

// seedStock 经 model 写入路径落一行库存真源（维度：SKU × 仓库）。
func seedStock(t *testing.T, db *gorm.DB, projectID, productID, variantID, warehouseID string) string {
	t.Helper()
	now := time.Now()
	id := uuid.NewString()
	// 带具体数量的行必须是「跟踪」行：迁移 261 的 CHECK (track_quantity OR quantity = 0)
	// 不允许「不跟踪（无限）却又带着数字」。
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

// seedPurchaseOrder 经 model 写入路径落一行采购单（需要预置货源与收货仓）。
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

// TestRLS_ProductScope_ReadsOwnProjectOnly 按 id 单查只在工程作用域内可见。
//
// 这是「跨工程读」最直接的入口：拿工程 A 的作用域按 id 读工程 B 的行，必须
// ErrRecordNotFound —— 而不是把 B 的数据读出来。
func TestRLS_ProductScope_ReadsOwnProjectOnly(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	pm := productmodel.NewModel(db)
	im := inventorymodel.NewModel(db)
	pm2 := purchasemodel.NewModel(db)

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
	if _, err := pm2.GetPurchaseOrder(ctx, aIDs["purchaseOrder"], pA); err != nil {
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
		{"inventory_purchase_orders", func() error { _, err := pm2.GetPurchaseOrder(ctx, bIDs["purchaseOrder"], pA); return err }},
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

// —— 第三批 6：写路径 / 守卫计数 / 集合源 / 契约链的护栏 ——
//
// 上面两条钉的是「按 id 单查」。这一节补的是同批另一类入口 —— 它们的失效形态各不相同，
// 逐类断言才有意义：
//
//   · **写路径**（Delete / DeleteRating）：策略挡写时**不报错、影响 0 行** —— 页面会说
//     「删除成功」，刷新一看行还在。所以断言必须是「B 的行仍在」，而不是「调用没报错」；
//   · **守卫计数**（CountNonZeroStocks*）：反向失效 —— 数出 0 ⇒ 把有货的仓 / SKU 判成
//     可以删，外键级联把库存一起清掉。这是这一批里唯一会**丢数据**的一条；
//   · **集合源**（ListForCollection / CountForCollection）：工程是必填作用域，
//     缺它时必须 ErrInvalidProjectID（显式报错），不接受「不限工程」那种读法；
//   · **批量按 id 取商品**（ListByIDs，变体快照链的最后一环）：这条入口曾是本批唯一的
//     「显式例外」（契约里没有工程）。VariantSnapshotPort 契约补完之后它改回带作用域的
//     ListByIDs，断言随之从「它没假装有隔离」换成「缺工程当场报错 + 跨工程读不到」
//     （见下方 TestRLS_ProductScope_BatchByIDsRequiresScope，
//     端到端的变体快照护栏在 rls_variant_snapshot_scope_test.go）。

// TestRLS_ProductScope_RejectsMissingScope 缺工程作用域时这批入口显式报 ErrInvalidProjectID。
//
// 与上面那条「裸查 0 行」是两种不同的失效形态，都要钉住：裸句柄是**静默** fail closed，
// 而这里的新入口有 rls.InProjectScope 把关，缺工程时**当场报错**。后者才是想要的 ——
// 调用方一眼看出是调用点漏传工程，而不是在生产上排查「功能突然查不到数据」。
//
// 空串与非法 uuid 各测一次：前者对应「调用方忘了传」，后者对应「传了个 id 形状的东西」
// （策略谓词里有 ::uuid 强转，不先校验会把 PG 的语法错误抛给调用方，错误归属变得难判断）。
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

	// 对照：把作用域换成 A 自己，同一批调用全部成功 —— 上面的红只来自作用域，不来自数据。
	if n, err := im.CountNonZeroStocks(ctx, aIDs["warehouse"], pA); err != nil || n != 1 {
		t.Fatalf("工程 A 数自己仓的非零库存行应得 1，实际 n=%d err=%v", n, err)
	}
}

// TestRLS_ProductScope_DeleteStaysInProject 删除类入口只动本工程的行。
//
// 判据是**行还在不在**，不是「调用返回 nil」：策略挡写时 DELETE 影响 0 行、不报错 ——
// 只断言 err == nil 的话，没包 scope 的版本会照样绿。
func TestRLS_ProductScope_DeleteStaysInProject(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	pm := productmodel.NewModel(db)
	im := inventorymodel.NewModel(db)

	pA, pB, aIDs, bIDs := productScopeFixture(t, db)

	// 拿 A 的作用域删 B 的行：不报错，但一行都不该动。
	if err := pm.Delete(ctx, bIDs["products"], pA); err != nil {
		t.Fatalf("跨工程删除不该报错（策略是「不可见」而非「拒绝」），实际 %v", err)
	}
	if _, err := pm.Get(ctx, bIDs["products"], pB); err != nil {
		t.Fatalf("拿 A 的作用域删 B 的商品：B 的行必须仍在，实际读不到 %v", err)
	}
	// 仓与库存同理。B 的 fixture 仓被 B 自己的采购单引用（PO-B 的 warehouse_id），
	// 所以「删不动」在这里有个自带的第二重证据：真删成了会先撞外键报错 —— 两种失败
	// 都能被下面两行抓到。
	if err := im.DeleteWarehouse(ctx, bIDs["warehouse"], pA); err != nil {
		t.Fatalf("跨工程删仓不该报错，实际 %v", err)
	}
	if _, err := im.GetWarehouse(ctx, bIDs["warehouse"], pB); err != nil {
		t.Fatalf("拿 A 的作用域删 B 的仓：B 的仓必须仍在，实际 %v", err)
	}
	if _, err := im.GetStock(ctx, bIDs["stock"], pB); err != nil {
		t.Fatalf("拿 A 的作用域删 B 的仓：B 的库存行必须仍在（否则就是级联丢账），实际 %v", err)
	}

	// 对照：同一个调用换成 B 自己的作用域，**真的删得掉** —— 证明上面的「还在」不是因为
	// 调用本身是空操作，而是作用域把它挡在了外面。对照用另造的空仓：fixture 仓被采购单
	// 引用，删它会先撞外键，那是「引用完整性」而不是「作用域」在起作用，会污染判别。
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

	// A 的两行从头到尾没被上面任何一步碰到。
	if _, err := pm.Get(ctx, aIDs["products"], pA); err != nil {
		t.Fatalf("工程 A 的商品不该受跨工程删除影响，实际 %v", err)
	}
	if _, err := im.GetWarehouse(ctx, aIDs["warehouse"], pA); err != nil {
		t.Fatalf("工程 A 的仓不该受跨工程删除影响，实际 %v", err)
	}
}

// TestRLS_ProductScope_StockGuardSeesOwnProject 删仓 / 删 SKU 前的非零库存守卫在自己的工程里数得准。
//
// 这条是本批唯一会**丢数据**的失效路径：守卫数出 0 ⇒ 有货被判成可以删 ⇒ 外键级联把
// inventory_stocks 一起清掉。所以两个方向都要断言：
//
//	· 本工程（仓里真有 7 件）⇒ 数得到，删仓守卫拦得住；
//	· 拿**别的**工程的作用域数 ⇒ 0 —— 这正是「守卫反向失效」的样子，也是为什么守卫
//	  必须带上工程：它绝不能靠「不限工程」来数（那种读法换非超级角色后恒为 0）。
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

	// 拿 B 的作用域数 A 的仓 / SKU：数不到（策略让那些行不可见）。
	// 这不是 bug 而是隔离在生效 —— 但正因为**数不到就是 0**，守卫必须显式带工程，
	// 绝不能靠「不限工程」兜底：那样在多工程下会把每一行都数成 0。
	if n, err := im.CountNonZeroStocks(ctx, aIDs["warehouse"], pB); err != nil || n != 0 {
		t.Errorf("拿 B 的作用域数 A 的仓应得 0（行不可见），实际 n=%d err=%v", n, err)
	}
	if rows, err := im.ListStockFactsByIDs(ctx, pB, []string{aIDs["stock"]}); err != nil || len(rows) != 0 {
		t.Errorf("拿 B 的作用域读 A 的货行应得 0 行（行不可见），实际 n=%d err=%v", len(rows), err)
	}
}

// TestRLS_ProductScope_RatingWriteAndReadInsideProject 评分明细的读写都落在工程作用域内。
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

// TestRLS_ProductScope_CollectionRespectsProject 集合源只能取到本工程的商品。
//
// 集合源是构建期唯一一处商品列表读库（产物零查库，不变量 1），它的作用域来自
// core.BuildProjectID(ctx)。取数（ListForCollection）与分页总量（CountForCollection）
// 必须同一把作用域 —— 少包任何一个都会让「翻到最后一页少数几条」这种漂移重新出现。
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

	// B 的商品不会因为「名字也是护栏商品」而被算进来（两条 fixture 同名，只有工程不同）。
	others, err := pm.ListForCollection(ctx, productmodel.CollectionFilter{ProjectID: pB}, 100, 0)
	if err != nil {
		t.Fatalf("集合源取 B 失败: %v", err)
	}
	if len(others) != 1 {
		t.Errorf("工程 B 的集合总量应为 1，实际 %d 行", len(others))
	}
}

// TestRLS_ProductScope_BatchByIDsRequiresScope 批量按 id 取商品必须带工程作用域。
//
// 曾经是 ListByIDsWithoutScope（本批最后一条不带作用域的读数入口，唯一调用方是跨模块
// 只读端口 VariantSnapshotPort）。契约补完之后入口改回 ListByIDs(ctx, ids, projectID)。
// 现在 ListByIDs 已无生产调用方（三条链各自改走窄投影），它留着就是为了这条断言：
// **缺工程当场报错**（调用点漏传工程必须一眼看出来，而不是在生产上排查「功能突然查不到
// 数据」），**跨工程读不到**（隔离真的生效）。整行读取与窄投影共用同一个作用域原语，
// 所以这条断言同时也是窄投影那条用例的对照。
//
// 这条测试是「先补 scope、再换连接角色」那个顺序的可执行证据：摘掉 model 里的
// rls.InProjectScope，跨工程那次读取会读到 B 的商品，这里立刻红。
func TestRLS_ProductScope_BatchByIDsRequiresScope(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	pm := productmodel.NewModel(db)

	pA, _, aIDs, bIDs := productScopeFixture(t, db)

	// 本工程：读得到（证明作用域没有把正常读取一起挡掉）。
	rows, err := pm.ListByIDs(ctx, []string{aIDs["products"]}, pA)
	if err != nil {
		t.Fatalf("本工程批量按 id 读应成功，实际 %v", err)
	}
	if len(rows) != 1 || rows[0].ID != aIDs["products"] {
		t.Fatalf("本工程应读到 1 行，实际 %d 行", len(rows))
	}

	// 跨工程：0 行 —— 不报错，但也绝不把别的工程的数据交出来。
	// 混合入参同样只出本工程的那条（调用方一次可能传多个 id）。
	mixed, err := pm.ListByIDs(ctx, []string{aIDs["products"], bIDs["products"]}, pA)
	if err != nil {
		t.Fatalf("混合入参读取不应报错，实际 %v", err)
	}
	if len(mixed) != 1 || mixed[0].ID != aIDs["products"] {
		t.Fatalf("混合入参应只出工程 A 的 1 行，实际 %d 行", len(mixed))
	}

	// 缺工程：显式报错。空串 = 调用方忘了传，非法形状 = 传进来的不是工程 id。
	for _, bad := range []string{"", "not-a-uuid"} {
		if _, err := pm.ListByIDs(ctx, []string{aIDs["products"]}, bad); !errors.Is(err, rls.ErrInvalidProjectID) {
			t.Errorf("工程 id 为 %q 时应返回 rls.ErrInvalidProjectID，实际 %v", bad, err)
		}
	}
}

// TestRLS_ProductScope_NarrowProjectionsRequireScope 窄投影同样必须带工程作用域。
//
// 商品行有不少「只问一个事实」的入口（判断是否发布、补商品名、画调价行的名字与链接）。
// 它们共用 rls.InProjectScope，但风险形状与整行读取不同：投影越窄，越容易在评审时被
// 当成「只读几列、不用管作用域」。这条把每个入口的两件事一起钉住：
// **缺工程当场报错**（漏传一眼看出来），**跨工程点名读不到**（隔离真的生效）。
//
// 判据是「准确的命中 id 集合 + 显式报错」，不是「调用没崩」：摘掉任一方法的
// rls.InProjectScope，非超级角色下混入 B 工程的那个 id 就会出现在结果里；而「本工程
// 读到了什么」这一侧必须是**就是那一个 id**，不能靠 len > 0 或「没报错」敷衍 ——
// 那两种写法在「作用域把正常读取一起挡掉」时同样是绿的。
//
// 三个单行读者各自拿 ids[0] 读：单独点名外国工程的 id 是真实入口（页面拿着别的
// 工程的 id 请求详情），必须有 ErrRecordNotFound，而不是「碰巧没被混合入参覆盖」。
func TestRLS_ProductScope_NarrowProjectionsRequireScope(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	pm := productmodel.NewModel(db)

	pA, _, aIDs, bIDs := productScopeFixture(t, db)
	productA, productB := aIDs["products"], bIDs["products"]

	// 同一个读取抽象覆盖两类形状：单行读（按 id 取一行）与批量读（按 id 集合取一行）。两边都对
	// 「落在本工程时读到了什么」给出**准确的 id 集合**，而不是「没报错 / len > 0」——
	// 那种断言既证明不了隔离，也证明不了投影真的取到了行。
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
			// 本工程：**准确命中**这一个 id。空结果或错命中都红。
			got, err := r.read(pA, []string{productA})
			if err != nil {
				t.Fatalf("本工程读取应成功，实际 %v", err)
			}
			assertIDsEqual(t, r.name+" 本工程", got, []string{productA})

			// 外国工程单独点名：单行读是 ErrRecordNotFound；批量读是「空集 + 不报错」——
			// 两种形状都不许把 B 的那条交出来。缺少 OLD 之前那一步「只读 ids[0]」的
			// 混合测试在这里补上了「单独点名外国工程」这个真实入口。
			got, err = r.read(pA, []string{productB})
			if r.batch {
				if err != nil {
					t.Fatalf("批量读单独点名外工程不该报错，实际 %v", err)
				}
				assertIDsEqual(t, r.name+" 外工程单独点名", got, nil)
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("单行读外工程的 id 应 ErrRecordNotFound，实际 %v", err)
			}

			// 混合入参：只出本工程的那一条，且就是那一条。
			got, err = r.read(pA, []string{productA, productB})
			if err != nil {
				t.Fatalf("混合入参读取不应报错，实际 %v", err)
			}
			assertIDsEqual(t, r.name+" 混合入参", got, []string{productA})

			// 缺工程：显式报错，不是静默 0 行。
			for _, bad := range []string{"", "not-a-uuid"} {
				if _, err := r.read(bad, []string{productA}); !errors.Is(err, rls.ErrInvalidProjectID) {
					t.Errorf("工程 id 为 %q 时应返回 rls.ErrInvalidProjectID，实际 %v", bad, err)
				}
			}
		})
	}

	// 变体侧只钉「作用域必填」，不钉隔离：product_variants 没有 project_id 列、
	// 也不在迁移 215 的策略名单里（见 productmodel.ListVariantsByIDs 的注释），
	// 变体的跨工程可见性由归属商品兜底。把它塞进上面的循环会得到一条假绿断言。
	for _, bad := range []string{"", "not-a-uuid"} {
		if _, err := pm.ListVariantPublicFacts(ctx, []string{aIDs["variant"]}, bad); !errors.Is(err, rls.ErrInvalidProjectID) {
			t.Errorf("ListVariantPublicFacts 工程 id 为 %q 时应返回 rls.ErrInvalidProjectID，实际 %v", bad, err)
		}
	}
	if _, err := pm.ListVariantPublicFacts(ctx, []string{aIDs["variant"]}, pA); err != nil {
		t.Errorf("ListVariantPublicFacts 本工程读取应成功，实际 %v", err)
	}
}

// TestRLS_ProductScope_BulkNarrowReadsHitExactSet 批量窄读三方法的**整批求值**要准确：
// 用一批两边都有的 id，作用域 A 时命中集合必须恰好是「A 的那几个」，一个不多一个不少。
//
// 为什么单拎出来：上面那条循环每次只喂一两个 id，所谓「混合入参」也只有一条本工程的
// 命中；多 id 场景下「作用域部分失效」的表现（混进一条外工程行）与「投影漏排」都
// 看不出来。这里给 A 两条、B 两条，命中集合逐条核对，同时断言外工程行一条都没混进来。
func TestRLS_ProductScope_BulkNarrowReadsHitExactSet(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	pm := productmodel.NewModel(db)

	pA, pB, aIDs, bIDs := productScopeFixture(t, db)
	// 两个工程各补一条商品：fixture 只播了一条 A / 一条 B，多 id 场景需要两条才看得出
	// 「混进外工程行」与「漏了本工程行」。
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

// narrowIDsAndProject 批量窄投影的读取结果 → 命中的 id 集合（同时校验每行的 project_id）。
//
// 两件事一起判：行必须属于作用域工程，命中的 id 也要**被调用方拿来比对**。
// 过去这里只回答「出现外工程行就报错」，空结果也能通过 —— 于是「作用域把正常读取
// 一起挡掉了」和「隔离生效」长得一模一样。
func narrowIDsAndProject(rows []*productmodel.ProductEntity, projectID string, err error) ([]string, error) {
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		// project_id 恒空串说明投影漏了这一列（不是「工程错了」）：分开报，免得
		// 把投影缺陷说成隔离缺陷。
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

// assertIDsEqual 比对命中 id 集合（顺序无关：两条批量读带 ORDER BY，单行读只有一条）。
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

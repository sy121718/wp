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
//   · **显式例外**（ListByIDsWithoutScope）：契约里没有工程，是不带作用域的入口 ——
//     要钉住的是「它没被作用域校验拦下、也没假装有隔离」，而不是「它能读到数据」
//     （非超级角色下它本来就 fail closed，见该条测试）。

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
			{"CountNonZeroStocksByVariant(inventory_stocks)", func() error {
				_, err := im.CountNonZeroStocksByVariant(ctx, aIDs["variant"], bad)
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
	byVariant, err := im.CountNonZeroStocksByVariant(ctx, aIDs["variant"], pA)
	if err != nil {
		t.Fatalf("数本工程 SKU 的非零库存失败: %v", err)
	}
	if byVariant != 1 {
		t.Errorf("本工程 SKU 有 1 行 7 件库存，守卫应数出 1，实际 %d", byVariant)
	}

	// 拿 B 的作用域数 A 的仓 / SKU：数不到（策略让那些行不可见）。
	// 这不是 bug 而是隔离在生效 —— 但正因为**数不到就是 0**，守卫必须显式带工程，
	// 绝不能靠「不限工程」兜底：那样在多工程下会把每一行都数成 0。
	if n, err := im.CountNonZeroStocks(ctx, aIDs["warehouse"], pB); err != nil || n != 0 {
		t.Errorf("拿 B 的作用域数 A 的仓应得 0（行不可见），实际 n=%d err=%v", n, err)
	}
	if n, err := im.CountNonZeroStocksByVariant(ctx, aIDs["variant"], pB); err != nil || n != 0 {
		t.Errorf("拿 B 的作用域数 A 的 SKU 应得 0（行不可见），实际 n=%d err=%v", n, err)
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

// TestRLS_ProductScope_ExplicitWithoutScopeEntryUnaffected 显式例外入口按「无隔离」的形状固定住。
//
// ListByIDsWithoutScope 是这一批里**唯一**保留的裸读入口（唯一调用方 VariantSnapshotPort
// 的契约里没有工程，为什么补不上见 model 上的长注释）。与 rls_product_taxonomy_scope_test.go
// 里那批 GetXxxWithoutScope 同一口径：断言的是**形状**，不是「它能读到数据」。
//
//   - 不能被作用域校验拦下 —— 报 ErrInvalidProjectID 等于把「结构上拿不到工程」换成
//     一个更难懂的错误，而不是诚实表达「这条路径没有隔离」；
//   - 在非超级角色下必须 fail closed：0 行、**且不报错**。这正是换角色之后订单落快照 /
//     加购 / 价格核对片段的真实表现，也是「先补 VariantSnapshotPort 契约再换角色」
//     这条待办的依据（model 注释里列了要动的三处）。
//
// 这条绿不代表该路径安全，只代表它**没有骗人**：既不静默给出别的工程的数据，
// 也不假装自己带了作用域。后来者若给它包上 scope，这里会立刻红 —— 那说明契约链
// 已经补完，应当同时把方法名改回 ListByIDs 并删掉这段。
func TestRLS_ProductScope_ExplicitWithoutScopeEntryUnaffected(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	pm := productmodel.NewModel(db)

	pA, _, aIDs, bIDs := productScopeFixture(t, db)

	rows, err := pm.ListByIDsWithoutScope(ctx, []string{aIDs["products"], bIDs["products"]})
	if errors.Is(err, rls.ErrInvalidProjectID) {
		t.Fatalf("显式例外入口不应被作用域校验拦下，实际 %v", err)
	}
	if err != nil {
		t.Fatalf("显式例外入口在非超级角色下应静默 fail closed（不报错），实际 %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("非超级角色 + 未设工程变量时例外入口应 0 行（fail closed），实际 %d 行", len(rows))
	}

	// 对照：同样两个 id 走带作用域的 Get —— 本工程读得到、跨工程读不到。
	// 两条路径的差别是「有没有工程作用域」，不是数据本身。
	if _, err := pm.Get(ctx, aIDs["products"], pA); err != nil {
		t.Fatalf("本工程按 id 读应成功，实际 %v", err)
	}
	if _, err := pm.Get(ctx, bIDs["products"], pA); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("带作用域的 Get 读别的工程的商品应 ErrRecordNotFound，实际 %v", err)
	}
}

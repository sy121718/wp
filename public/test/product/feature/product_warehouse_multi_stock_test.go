// product_warehouse_multi_stock_test.go — 商品侧第一批（2026-09-19 用户拍板口径）的链路用例。
//
// 覆盖五组口径（docs/14-product-sku-and-cost-model.md §1.1 / §1.4 / §9.3）：
//
//  1. **仓库侧裸码 / 商品侧带前缀**：同一商品多仓各一行，仓库里那行的 sku_code 是裸码；
//  2. **多仓逐仓建行 + 认领仓定前缀**：勾了哪些仓就在哪些仓各建一行，第一个仓决定主体 SKU 前缀；
//  3. **认领 / 复用**：该仓已有同裸码的行 → 复用不新建（同仓同码两行是这批要消灭的东西）；
//  4. **数量默认无限**：不传数量 = 不跟踪（track_quantity = false）；显式填数字 = 跟踪并写入
//     （0 是合法值，与「没填」严格区分）；
//  5. **主体 SKU 预检**（工程内唯一）与**列表库存三态聚合**（∞ / 求和 / 未入库，混合绝不求和），
//     以及一次商品保存的**事务回滚**（留痕失败 → 三张表都不留痕）。
//
// 表结构一律来自生产迁移（support.NewMigratedPGTestDB），断言直查真源列。
package feature

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	producthttp "go_wp/internal/module/product/inbound/http"
	"go_wp/internal/templates"

	"go_wp/public/migrations"
)

// wstockRow 库存真源的一行（断言用的最小投影）。
type wstockRow struct {
	WarehouseID   string `gorm:"column:warehouse_id"`
	SKUCode       string `gorm:"column:sku_code"`
	TrackQuantity bool   `gorm:"column:track_quantity"`
	Quantity      int    `gorm:"column:quantity"`
}

// wstockRows 某商品在各仓的全部库存行。
func (f *wpickFixture) wstockRows(t *testing.T, productID string) []wstockRow {
	t.Helper()
	var rows []wstockRow
	if err := f.db.Raw("SELECT warehouse_id, sku_code, track_quantity, quantity FROM inventory_stocks "+
		"WHERE product_id = ? ORDER BY warehouse_id", productID).Scan(&rows).Error; err != nil {
		t.Fatalf("读库存行失败: %v", err)
	}
	return rows
}

// wstockRowCount 该 (仓库, 裸码) 有几行 —— 「复用不新建」的判据就是它恒为 1。
func (f *wpickFixture) wstockRowCount(t *testing.T, warehouseID, skuCode string) int {
	t.Helper()
	var n int
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_stocks WHERE warehouse_id = ? AND sku_code = ?",
		warehouseID, skuCode).Scan(&n).Error; err != nil {
		t.Fatalf("统计库存行失败: %v", err)
	}
	return n
}

// wvariantCount 某商品的变体数。
func (f *wpickFixture) wvariantCount(t *testing.T, productID string) int {
	t.Helper()
	var n int
	if err := f.db.Raw("SELECT COUNT(*) FROM product_variants WHERE product_id = ?", productID).Scan(&n).Error; err != nil {
		t.Fatalf("统计变体失败: %v", err)
	}
	return n
}

// assertNoTraceOfSKU 断言三张表里都没有这个商品的任何痕迹（事务整体回滚的判据）。
func assertNoTraceOfSKU(t *testing.T, f *wpickFixture, containerSKU, bareCode string) {
	t.Helper()
	var products, variants, stocks int
	if err := f.db.Raw("SELECT COUNT(*) FROM products WHERE sku_code = ?", containerSKU).Scan(&products).Error; err != nil {
		t.Fatalf("统计商品失败: %v", err)
	}
	if err := f.db.Raw("SELECT COUNT(*) FROM product_variants WHERE sku_code IN ?",
		[]string{containerSKU, bareCode}).Scan(&variants).Error; err != nil {
		t.Fatalf("统计变体失败: %v", err)
	}
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_stocks WHERE sku_code = ?", bareCode).Scan(&stocks).Error; err != nil {
		t.Fatalf("统计库存行失败: %v", err)
	}
	if products != 0 || variants != 0 || stocks != 0 {
		t.Fatalf("失败的商品不该留任何痕迹：products=%d product_variants=%d inventory_stocks=%d",
			products, variants, stocks)
	}
}

// failingChangesPort 变更记录端口替身：永远返回错误（故障注入用）。
//
// 嵌入 MasterDataService 只为了满足接口的其余方法（本用例不会调用它们）；
// 事务性由 RecordChangesTx 这一条被覆写的方法决定 —— 正是「商品保存的最后一步失败」。
type failingChangesPort struct {
	masterdatacontract.MasterDataService
	err error
}

func (p *failingChangesPort) RecordChanges(ctx context.Context, inputs []*masterdatacontract.ChangeInput) error {
	return p.err
}

func (p *failingChangesPort) RecordChangesTx(ctx context.Context, tx *gorm.DB, inputs []*masterdatacontract.ChangeInput) error {
	return p.err
}

// TestProductCreateMultiWarehouseBareCodes 多仓逐仓建行：仓库侧是**同一条裸码**，
// 商品侧（主体 + 首个变体）带认领仓的仓码前缀。
func TestProductCreateMultiWarehouseBareCodes(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	price := 12.5
	p, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: "多仓商品", Slug: "multi-wh",
		DefaultPrice: &price, SKUCode: "DRAWERSMOKE_001",
		// 勾两个仓：苏州（默认仓）在前 = 认领仓。
		WarehouseIDs: []string{f.sz.ID, f.sh.ID},
	})
	if err != nil {
		t.Fatalf("多仓创建失败: %v", err)
	}
	if got := f.wpickContainerSKU(t, p.ID); got != "SZ_DRAWERSMOKE_001" {
		t.Fatalf("主体 SKU 应带**认领仓**（第一个勾选的仓）前缀，实际 %q", got)
	}
	if len(p.Variants) != 1 || p.Variants[0].SKUCode != "SZ_DRAWERSMOKE_001" {
		t.Fatalf("首个变体应等于容器主体，实际 %+v", p.Variants)
	}
	rows := f.wstockRows(t, p.ID)
	if len(rows) != 2 {
		t.Fatalf("勾了两个仓应各建一行，实际 %d 行：%+v", len(rows), rows)
	}
	for _, row := range rows {
		// 仓库侧永远是**裸码**：带上前缀就会出现「同一条货在仓里有两个名字」。
		if row.SKUCode != "DRAWERSMOKE_001" {
			t.Fatalf("仓库侧编码应是裸码 DRAWERSMOKE_001，实际 %q（仓 %s）", row.SKUCode, row.WarehouseID)
		}
		// 不传数量 = 不跟踪（无限）。
		if row.TrackQuantity || row.Quantity != 0 {
			t.Fatalf("不传数量应是不跟踪（无限），实际 track=%v qty=%d", row.TrackQuantity, row.Quantity)
		}
	}
	if got := f.wstockRowCount(t, f.sz.ID, "DRAWERSMOKE_001"); got != 1 {
		t.Fatalf("默认仓应有且只有一行，实际 %d", got)
	}
	if got := f.wstockRowCount(t, f.sh.ID, "DRAWERSMOKE_001"); got != 1 {
		t.Fatalf("第二仓应有且只有一行，实际 %d", got)
	}
}

// TestProductCreateClaimWarehouseDecidesPrefix 认领仓 = 显式列表的第一个仓（与勾选顺序一致）。
func TestProductCreateClaimWarehouseDecidesPrefix(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	p, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: "上海认领", Slug: "claim-sh",
		SKUCode: "TEE-9001", WarehouseIDs: []string{f.sh.ID, f.sz.ID},
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if got := f.wpickContainerSKU(t, p.ID); got != "SH_TEE-9001" {
		t.Fatalf("认领仓应是列表第一个仓（SH），实际主体 SKU %q", got)
	}
	// 单值兼容形态（warehouseId）依旧可用：等价于只勾一个仓。
	p2, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: "单值兼容", Slug: "claim-single",
		SKUCode: "TEE-9002", WarehouseID: f.sh.ID,
	})
	if err != nil {
		t.Fatalf("单值创建失败: %v", err)
	}
	if got := f.wpickContainerSKU(t, p2.ID); got != "SH_TEE-9002" {
		t.Fatalf("单值形态应等价于只勾一个仓，实际主体 SKU %q", got)
	}
	if len(f.wstockRows(t, p2.ID)) != 1 {
		t.Fatalf("单值形态应只建一行库存，实际 %+v", f.wstockRows(t, p2.ID))
	}
}

// TestProductCreateReusesExistingWarehouseCode 认领 / 复用：该仓已有同裸码的行 → **不新建**。
//
// 「从仓库选」这条来路的全部语义就在这里 —— 旧实现会给新变体再建一行（仓库侧还带前缀），
// 于是同一条货在同一个仓有两行、SKU 串两边对不上。
func TestProductCreateReusesExistingWarehouseCode(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	host := f.wpickProduct(t, "已有货的宿主", "reuse-host")
	f.wpickSeedWarehouseSKU(t, f.sh.ID, "REUSE-1", host.ID, host.Variants[0].ID)

	p, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "认领已有编码", Slug: "reuse-claim",
		// 认领仓是 SH（第一个）：那里已有 REUSE-1 这一行。
		SKUCode: "REUSE-1", WarehouseIDs: []string{f.sh.ID},
	})
	if err != nil {
		t.Fatalf("认领已有编码应成功（复用那一行）: %v", err)
	}
	if got := f.wpickContainerSKU(t, p.ID); got != "SH_REUSE-1" {
		t.Fatalf("主体 SKU 应带认领仓前缀，实际 %q", got)
	}
	if got := f.wstockRowCount(t, f.sh.ID, "REUSE-1"); got != 1 {
		t.Fatalf("同仓同码必须只有一行（复用不新建），实际 %d 行", got)
	}
	// 复用不改写既有行：宿主那一行还在，且仍指向宿主的变体。
	var owner string
	if err := f.db.Raw("SELECT variant_id FROM inventory_stocks WHERE warehouse_id = ? AND sku_code = ?",
		f.sh.ID, "REUSE-1").Scan(&owner).Error; err != nil {
		t.Fatalf("读宿主库存行失败: %v", err)
	}
	if owner != host.Variants[0].ID {
		t.Fatalf("复用不该改写既有行的归属：应为宿主变体 %s，实际 %s", host.Variants[0].ID, owner)
	}
	if _, rows := f.wpickExternalSKU(t, p.Variants[0].ID, f.sh.ID); rows != 0 {
		t.Fatalf("认领不该为新变体建行，实际 %d 行", rows)
	}
}

// TestProductCreateQuantityTracking 数量口径：不传 = 不跟踪（无限）；显式填 = 跟踪并写入
// （0 是合法值，与「没填」严格区分 —— DDL 的 CHECK (track_quantity OR quantity = 0) 也是这条）。
func TestProductCreateQuantityTracking(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	mk := func(name, slug, sku string, qty *int) *productdto.ProductResp {
		t.Helper()
		p, err := f.products.Create(ctx, &productdto.CreateReq{
			ProjectID: f.projectID, Name: name, Slug: slug, SKUCode: sku,
			WarehouseIDs: []string{f.sz.ID}, Quantity: qty,
		})
		if err != nil {
			t.Fatalf("建商品 %s 失败: %v", name, err)
		}
		return p
	}

	untracked := mk("不跟踪", "qty-none", "QTY-NONE", nil)
	if rows := f.wstockRows(t, untracked.ID); len(rows) != 1 || rows[0].TrackQuantity || rows[0].Quantity != 0 {
		t.Fatalf("不传数量应是不跟踪（无限），实际 %+v", rows)
	}

	zero := 0
	trackedZero := mk("跟踪零", "qty-zero", "QTY-ZERO", &zero)
	if rows := f.wstockRows(t, trackedZero.ID); len(rows) != 1 || !rows[0].TrackQuantity || rows[0].Quantity != 0 {
		t.Fatalf("显式填 0 应是「跟踪且 0」（明确没货），实际 %+v", rows)
	}

	five := 5
	trackedFive := mk("跟踪五", "qty-five", "QTY-FIVE", &five)
	if rows := f.wstockRows(t, trackedFive.ID); len(rows) != 1 || !rows[0].TrackQuantity || rows[0].Quantity != 5 {
		t.Fatalf("显式填 5 应是「跟踪且 5」，实际 %+v", rows)
	}
}

// TestProductListStockAggregationThreeStates 列表「库存」列的三态聚合（docs/14 §1.4）。
//
// 混合状态**绝不求和**：任一仓不跟踪就整体显示「无限」—— 求和等于把无限当 0，
// 页面会显示成「有货」，而实际是「卖不完」。这条是本文件里最值钱的断言。
func TestProductListStockAggregationThreeStates(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	// ① 未入库：捆绑容器不生成库存行（它不分销自己的 SKU）。
	bundlePrice := 99.0
	bundle, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "捆绑容器", Slug: "agg-bundle", Type: "bundle",
		DefaultPrice: &bundlePrice, SKUCode: "AGG-BUNDLE",
	})
	if err != nil {
		t.Fatalf("建捆绑商品失败: %v", err)
	}
	got, err := f.products.Get(ctx, &productdto.GetReq{ID: bundle.ID})
	if err != nil {
		t.Fatalf("读捆绑商品失败: %v", err)
	}
	if got.StockState != productenums.StockStateNone || got.StockTotal != 0 {
		t.Fatalf("没有任何库存行应显示未入库，实际 state=%q total=%d", got.StockState, got.StockTotal)
	}

	// ② 全部跟踪：两仓各 5 → 求和 10。
	five := 5
	tracked, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "全部跟踪", Slug: "agg-tracked", SKUCode: "AGG-TRACKED",
		WarehouseIDs: []string{f.sz.ID, f.sh.ID}, Quantity: &five,
	})
	if err != nil {
		t.Fatalf("建跟踪商品失败: %v", err)
	}
	got, err = f.products.Get(ctx, &productdto.GetReq{ID: tracked.ID})
	if err != nil {
		t.Fatalf("读跟踪商品失败: %v", err)
	}
	if got.StockState != productenums.StockStateTracked || got.StockTotal != 10 {
		t.Fatalf("两仓各 5 应求和成 10，实际 state=%q total=%d", got.StockState, got.StockTotal)
	}
	if len(got.StockWarehouses) != 2 {
		t.Fatalf("分仓明细应有两行，实际 %+v", got.StockWarehouses)
	}

	// ③ 混合：SZ 不跟踪 + SH 跟踪 5 —— 商品级必须是 ∞，且**总数不得是 5**。
	mixed, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "混合状态", Slug: "agg-mixed", SKUCode: "AGG-MIXED",
		WarehouseIDs: []string{f.sz.ID},
	})
	if err != nil {
		t.Fatalf("建混合商品失败: %v", err)
	}
	ref, rerr := f.inventory.ResolveWarehouse(ctx, f.projectID, f.sh.ID)
	if rerr != nil {
		t.Fatalf("解析第二仓失败: %v", rerr)
	}
	if err := f.inventory.EnsureVariantStock(ctx, ref, mixed.ID, mixed.Variants[0].ID, "AGG-MIXED", &five); err != nil {
		t.Fatalf("为第二仓建跟踪行失败: %v", err)
	}
	got, err = f.products.Get(ctx, &productdto.GetReq{ID: mixed.ID})
	if err != nil {
		t.Fatalf("读混合商品失败: %v", err)
	}
	if got.StockState != productenums.StockStateInfinite {
		t.Fatalf("任一仓不跟踪就应显示无限，实际 state=%q total=%d", got.StockState, got.StockTotal)
	}
	if got.StockTotal != 0 {
		t.Fatalf("混合状态**绝不求和**：total 应是 0（页面按 ∞ 显示），实际 %d", got.StockTotal)
	}
	var shQty int
	var shState string
	for _, row := range got.StockWarehouses {
		if row.WarehouseID == f.sh.ID {
			shQty, shState = row.Quantity, row.State
		}
	}
	if shState != productenums.StockStateTracked || shQty != 5 {
		t.Fatalf("分仓明细要能区分三态：SH 应是跟踪 5，实际 state=%q qty=%d", shState, shQty)
	}
}

// TestProductCreateReuseInSecondWarehouse 多仓创建时第二个仓已有同裸码 → **复用**（不是错误）。
//
// 这一条刻意写清楚：docs/14 §1.1 的多仓口径是「该仓已有同裸码的行 → 复用那一行，不新建（幂等）」，
// 所以「第 N 个仓已有该编码」是**正常路径**，不是冲突；真正的冲突只剩两处 ——
// 主体 SKU 在工程内重复（预检 + 唯一索引兜底）与仓内外部编码指向了另一个商品（N:1 弱校验）。
func TestProductCreateReuseInSecondWarehouse(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	host := f.wpickProduct(t, "第二仓的既有货", "second-wh-host")
	f.wpickSeedWarehouseSKU(t, f.sh.ID, "SHARE-1", host.ID, host.Variants[0].ID)

	p, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "两仓一旧一新", Slug: "second-wh-reuse",
		SKUCode: "SHARE-1", WarehouseIDs: []string{f.sz.ID, f.sh.ID},
	})
	if err != nil {
		t.Fatalf("一半新建一半复用应成功: %v", err)
	}
	// 默认仓（SZ）原本没有这条码 → 建一行；上海仓已有 → 复用，不新建。
	if got := f.wstockRowCount(t, f.sz.ID, "SHARE-1"); got != 1 {
		t.Fatalf("第一个仓应新建一行，实际 %d 行", got)
	}
	if got := f.wstockRowCount(t, f.sh.ID, "SHARE-1"); got != 1 {
		t.Fatalf("已有同裸码的仓必须复用（仍是一行），实际 %d 行", got)
	}
	if got := f.wstockRows(t, p.ID); len(got) != 1 {
		t.Fatalf("该商品自己的库存行只有 SZ 那一条，实际 %+v", got)
	}
}

// TestProductPageCreateContainerSKUTakenChineseError 后台新建抽屉的重复主体 SKU：
// 页面必须给**可读的中文**结论，绝不出现索引名 / SQLSTATE / 裸 key（CQ-009 的实测缺陷）。
func TestProductPageCreateContainerSKUTakenChineseError(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	if _, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "占位商品", Slug: "taken-holder", SKUCode: "TAKEN-1",
	}); err != nil {
		t.Fatalf("建占位商品失败: %v", err)
	}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	grantProductPerms(engine)
	handle := producthttp.NewProductPageHandle(f.products, f.projects)
	engine.POST("/admin/products/create", handle.ProductsCreate)

	form := url.Values{
		"projectId": {f.projectID}, "name": {"重复编码的商品"}, "slug": {"taken-dup"},
		"type": {"variant"}, "sku": {"TAKEN-1"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/products/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("业务失败应 302 回列表页，实际 %d", rec.Code)
	}
	location, derr := url.QueryUnescape(rec.Header().Get("Location"))
	if derr != nil {
		t.Fatalf("回跳地址解码失败: %v", derr)
	}
	if !strings.Contains(location, "已被其它商品占用") {
		t.Fatalf("页面应给出可读的中文业务文案，实际 %s", location)
	}
	for _, leak := range []string{"uq_products", "23505", "SQLSTATE", "ErrContainerSKUTaken", "duplicate key"} {
		if strings.Contains(location, leak) {
			t.Fatalf("回跳地址不得泄漏内部细节 %q：%s", leak, location)
		}
	}
}

// TestProductUpdateSKUPrecheckRejectsDuplicate 编辑路径同样先预检（excludeID = 自身）：
// 撞别人的主体编码 → 业务错误；改回自己的编码 → 放行。
func TestProductUpdateSKUPrecheckRejectsDuplicate(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	other, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "别人", Slug: "upd-other", SKUCode: "TAKEN-2",
	})
	if err != nil {
		t.Fatalf("建「别人」失败: %v", err)
	}
	mine, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "我自己", Slug: "upd-mine", SKUCode: "MINE-1",
	})
	if err != nil {
		t.Fatalf("建「我自己」失败: %v", err)
	}
	foreign := f.wpickContainerSKU(t, other.ID)
	_, err = f.products.Update(ctx, &productdto.UpdateReq{
		ID: mine.ID, ProjectID: f.projectID, SKUCode: &foreign,
	})
	if err == nil || err.Error() != productenums.ErrContainerSKUTaken {
		t.Fatalf("撞别人的主体编码应返回 %s，实际 %v", productenums.ErrContainerSKUTaken, err)
	}
	// 自己那一份编码重存不该被自己撞下（excludeID 的作用域就是这一条）。
	own := f.wpickContainerSKU(t, mine.ID)
	if _, err = f.products.Update(ctx, &productdto.UpdateReq{
		ID: mine.ID, ProjectID: f.projectID, SKUCode: &own,
	}); err != nil {
		t.Fatalf("改回自己的编码不该被拒: %v", err)
	}
	if got := f.wpickContainerSKU(t, mine.ID); got != own {
		t.Fatalf("编码应保持 %q，实际 %q", own, got)
	}
}

// TestProductCreateRollsBackWhenChangeRecordFails 事务回滚（用例 A）：商品 + 首个变体 +
// 各仓库存行都写进同一个事务，最后一步「变更记录」失败时**三张表都不留痕**。
func TestProductCreateRollsBackWhenChangeRecordFails(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	f.products.SetMasterDataChanges(&failingChangesPort{err: errors.New("留痕端口故障（故障注入）")})
	_, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: "留痕失败的商品", Slug: "rollback-1", SKUCode: "ROLLBACK-1",
		WarehouseIDs: []string{f.sz.ID, f.sh.ID},
	})
	if err == nil {
		t.Fatal("留痕失败时创建必须整体失败")
	}
	var bySlug int
	if derr := f.db.Raw("SELECT COUNT(*) FROM products WHERE slug = ?", "rollback-1").Scan(&bySlug).Error; derr != nil {
		t.Fatalf("统计商品失败: %v", derr)
	}
	if bySlug != 0 {
		t.Fatalf("事务应整体回滚：products 里不该有这一行，实际 %d", bySlug)
	}
	assertNoTraceOfSKU(t, f, "SZ_ROLLBACK-1", "ROLLBACK-1")
}

// TestSaveVariantListRollsBackOnFailure 事务回滚（用例 C）：变体清单的「新增 + 清单外删除」
// 是一个整体动作，留痕失败时**既没有新增也没有删除**。
func TestSaveVariantListRollsBackOnFailure(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	yes := true
	color, err := f.products.CreateAttribute(ctx, &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: "颜色", Key: "color", IsVariation: &yes,
		Values: []productdto.AttributeValueReq{{Label: "红", Key: "red"}},
	})
	if err != nil {
		t.Fatalf("建属性组失败: %v", err)
	}
	p, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "清单回滚", Slug: "list-rollback",
		SKUCode: "LIST-1", AttributeIDs: []string{color.ID},
	})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}
	originalSKU := f.wpickContainerSKU(t, p.ID)
	if got := f.wvariantCount(t, p.ID); got != 1 {
		t.Fatalf("初始应有一个变体，实际 %d", got)
	}

	f.products.SetMasterDataChanges(&failingChangesPort{err: errors.New("留痕端口故障（故障注入）")})
	_, err = f.products.SaveVariantList(ctx, &productdto.SaveVariantListReq{
		ProductID: p.ID, ProjectID: f.projectID,
		Rows: []productdto.VariantListRow{
			// 清单里只有这个新组合：既要求新建它，也要求删掉清单外的无规格占位变体。
			{SKUCode: "LIST-1_RED_V", OptionValues: json.RawMessage(`{"color":"red"}`)},
		},
	})
	if err == nil {
		t.Fatal("留痕失败时保存清单必须整体失败")
	}
	if got := f.wvariantCount(t, p.ID); got != 1 {
		t.Fatalf("事务应整体回滚：既不该新增也不该删除，变体数仍应是 1，实际 %d", got)
	}
	var kept int
	if derr := f.db.Raw("SELECT COUNT(*) FROM product_variants WHERE product_id = ? AND sku_code = ?",
		p.ID, originalSKU).Scan(&kept).Error; derr != nil {
		t.Fatalf("统计变体失败: %v", derr)
	}
	if kept != 1 {
		t.Fatalf("原有的那个变体应还在（清单外的删除也回滚），实际 %d", kept)
	}
}

// TestProductPageCreateMultiWarehouseForm 页面表单的多仓勾选 + 数量开关直通 service：
// 字段名一旦分叉（模板写 warehouseIds、handler 读 warehouseId），运营勾的仓与填的数量就会**静默丢失**
// —— 商品建出来了，仓库里却没有对应的行。这条用例钉的就是这条链路。
func TestProductPageCreateMultiWarehouseForm(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	grantProductPerms(engine)
	handle := producthttp.NewProductPageHandle(f.products, f.projects)
	handle.SetInventoryDeps(f.inventory)
	engine.GET("/admin/products", handle.ProductsPage)
	// 建表单的落点已是独立整页（列表页抽屉退役，见 getProductsNewPage 的注释）——
	// 断言表单控件存在与否的用例要把这条路由注册上，否则请求 404、页面为空、断言全红。
	engine.GET("/admin/products/new", handle.ProductNewPage)
	engine.POST("/admin/products/create", handle.ProductsCreate)

	// 新建整页里的三组新控件必须真的渲染出来（字段名分叉在这里就会暴露）：
	// 多仓勾选（同名多值）、数量开关与数量框、可搜索下拉的候选 datalist。
	// 落点从列表页抽屉改为整页：那个 template 已退役（见 getProductsNewPage 的注释）。
	pageRec := httptest.NewRecorder()
	engine.ServeHTTP(pageRec, httptest.NewRequest(http.MethodGet, "/admin/products/new?project="+f.projectID, nil))
	page := pageRec.Body.String()
	for _, want := range []string{
		`name="warehouseIds"`, `data-warehouse-check`, `name="trackQuantity"`,
		`data-quantity-input`, `data-sku-candidates`, `data-sku-input`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("新建抽屉缺少 %q（多仓 / 数量 / SKU 候选控件没渲染出来）", want)
		}
	}

	form := url.Values{
		"projectId": {f.projectID}, "name": {"表单多仓"}, "slug": {"form-multi-wh"},
		"type": {"variant"}, "sku": {"FORM-1"},
		// 勾两个仓（同名多值）+ 显式跟踪数量 3。
		"warehouseIds":  {f.sz.ID, f.sh.ID},
		"trackQuantity": {"1"}, "quantity": {"3"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/products/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("创建应 302（成功去编辑页），实际 %d：%s", rec.Code, rec.Body.String())
	}
	location := rec.Header().Get("Location")
	if !strings.Contains(location, "/admin/products/edit") {
		t.Fatalf("成功应去商品编辑页，实际 %s", location)
	}
	var productID string
	if derr := f.db.Raw("SELECT id FROM products WHERE slug = ?", "form-multi-wh").Scan(&productID).Error; derr != nil {
		t.Fatalf("读商品失败: %v", derr)
	}
	if productID == "" {
		t.Fatal("表单单仓字段分叉：商品没建出来")
	}
	rows := f.wstockRows(t, productID)
	if len(rows) != 2 {
		t.Fatalf("勾了两个仓应各建一行（表单字段名分叉会让数量静默丢掉），实际 %+v", rows)
	}
	for _, row := range rows {
		if row.SKUCode != "FORM-1" || !row.TrackQuantity || row.Quantity != 3 {
			t.Fatalf("数量 3 应写成跟踪行（裸码 FORM-1），实际 %+v", row)
		}
	}
}

// TestProductPageCreateUntrackedQuantityNeverPrefilled 表单的「不跟踪」语义：
// 勾了跟踪但数量框留空 → 写 0（明确没货）；不勾跟踪 → 不传数量（无限）。
// 「无限时把数量框预填 0」是这条用例要挡住的那个错 —— 0 与无限在页面上看起来一样，
// 在库里的含义却一个是「卖光」一个是「要多少有多少」。
func TestProductPageCreateUntrackedQuantityNeverPrefilled(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	grantProductPerms(engine)
	handle := producthttp.NewProductPageHandle(f.products, f.projects)
	engine.POST("/admin/products/create", handle.ProductsCreate)

	post := func(name, slug, sku string, form url.Values) {
		t.Helper()
		form.Set("projectId", f.projectID)
		form.Set("name", name)
		form.Set("slug", slug)
		form.Set("sku", sku)
		form.Set("type", "variant")
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/products/create", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "/admin/products/edit") {
			t.Fatalf("创建 %s 失败：%d %s", name, rec.Code, rec.Header().Get("Location"))
		}
	}
	productIDOf := func(slug string) string {
		t.Helper()
		var id string
		if err := f.db.Raw("SELECT id FROM products WHERE slug = ?", slug).Scan(&id).Error; err != nil {
			t.Fatalf("读商品失败: %v", err)
		}
		return id
	}

	// ① 不勾「跟踪数量」：数量字段不提交（模板里数量框此时是 disabled 的）。
	post("不跟踪", "form-untracked", "FORM-U1", url.Values{"warehouseIds": {f.sz.ID}})
	if rows := f.wstockRows(t, productIDOf("form-untracked")); len(rows) != 1 || rows[0].TrackQuantity || rows[0].Quantity != 0 {
		t.Fatalf("不跟踪应是不传数量（无限），实际 %+v", rows)
	}

	// ② 勾了跟踪但数量框留空：写 0（跟踪 + 明确没货）。
	post("跟踪零", "form-zero", "FORM-Z1", url.Values{
		"warehouseIds": {f.sz.ID}, "trackQuantity": {"1"}, "quantity": {""},
	})
	if rows := f.wstockRows(t, productIDOf("form-zero")); len(rows) != 1 || !rows[0].TrackQuantity || rows[0].Quantity != 0 {
		t.Fatalf("跟踪但留空应写 0（明确没货），实际 %+v", rows)
	}

	// ③ 负数直接拒绝（不落库）。
	rec := httptest.NewRecorder()
	bad := url.Values{
		"projectId": {f.projectID}, "name": {"负数"}, "slug": {"form-neg"},
		"type": {"variant"}, "sku": {"FORM-N1"}, "warehouseIds": {f.sz.ID},
		"trackQuantity": {"1"}, "quantity": {"-1"},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/products/create", strings.NewReader(bad.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	engine.ServeHTTP(rec, req)
	if strings.Contains(rec.Header().Get("Location"), "/admin/products/detail") {
		t.Fatalf("负数数量应被拒绝，实际去了详情页: %s", rec.Header().Get("Location"))
	}
	if id := productIDOf("form-neg"); id != "" {
		t.Fatalf("被拒的请求不该落库，实际商品 %s", id)
	}
}

// TestProductsPageStockColumnRendersThreeStates 列表「库存」列的三态**渲染**：
// 服务端算出的三态必须在页面上真的长成 ∞ / 数字 / 未入库（模板分支写错、
// i18n key 写错都会在这里暴露），并且每个商品一行详情（可展开的分仓明细）。
func TestProductsPageStockColumnRendersThreeStates(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	five := 5
	if _, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "有货商品", Slug: "col-tracked", SKUCode: "COL-TRACKED",
		WarehouseIDs: []string{f.sz.ID, f.sh.ID}, Quantity: &five,
	}); err != nil {
		t.Fatalf("建有货商品失败: %v", err)
	}
	if _, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "无限商品", Slug: "col-infinite", SKUCode: "COL-INFINITE",
		WarehouseIDs: []string{f.sz.ID},
	}); err != nil {
		t.Fatalf("建无限商品失败: %v", err)
	}
	bundlePrice := 9.0
	if _, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "未入库容器", Slug: "col-none", Type: "bundle",
		DefaultPrice: &bundlePrice, SKUCode: "COL-NONE",
	}); err != nil {
		t.Fatalf("建捆绑容器失败: %v", err)
	}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	grantProductPerms(engine)
	handle := producthttp.NewProductPageHandle(f.products, f.projects)
	handle.SetInventoryDeps(f.inventory)
	engine.GET("/admin/products", handle.ProductsPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/products?project="+f.projectID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("列表页应 200，实际 %d", rec.Code)
	}
	page := rec.Body.String()
	if got := strings.Count(page, `class="stock-cell"`); got != 3 {
		t.Fatalf("三个商品应各有一个库存单元格，实际 %d 个", got)
	}
	for _, want := range []string{
		"∞ 无限",       // 不跟踪（无限）
		"未入库",        // 没有任何库存行
		"stock-rows", // 分仓明细（details 展开的内容）
		"10",         // 两仓各 5 的求和
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("库存列缺少 %q", want)
		}
	}
}

// TestContainerSKUTakenI18nSeeded 迁移 264 的词条必须真的进库。
//
// enums 常量的值就是 i18n key：缺词条时页面会把 ErrContainerSKUTaken 这个裸 key
// 原样显示给运营（「该主体 SKU 已被占用」这句可行动的话就丢了）。
func TestContainerSKUTakenI18nSeeded(t *testing.T) {
	f := newWPickFixture(t)
	if f == nil {
		return
	}
	if err := migrations.RunSeeds(f.db); err != nil {
		t.Fatalf("执行生产数据种子失败: %v", err)
	}
	for _, lang := range []string{"zh-CN", "en-US"} {
		var n int
		if err := f.db.Raw("SELECT COUNT(*) FROM sys_i18n WHERE item_key = 'ErrContainerSKUTaken' AND lang = ?", lang).Scan(&n).Error; err != nil {
			t.Fatalf("查词条失败: %v", err)
		}
		if n == 0 {
			t.Fatalf("%s 的 ErrContainerSKUTaken 词条缺失（迁移 264 未生效）", lang)
		}
	}
}

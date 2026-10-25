// inventory_stock_claim_test.go — W1（迁移 621）的验收：仓库商品 SKU 主档化。
//
// 覆盖任务书 §4.7 的会红断言：
//
//	4  未认领行可建，同仓同裸码插第二行撞 uq_inventory_stocks_warehouse_sku
//	5  认领冲突打回，且两行 quantity 一字未变
//	6  解除认领守卫（有数量就打回）
//	7  预占不写流水
//	8  可售不足整单拒绝（第一行 reserved 回到原值）
//	9  并发预占不超卖（-race）
//	10 未认领行不进商品侧聚合（VariantStockTotals 不出现空串 key）
//	11 删仓守卫仍拦得住未认领存货
//	12 页面渲染（未认领徽标 + ?claimed=unclaimed 筛选）
//
// 全部走真实 PG + 生产迁移 + 真实 service（不手抄 CREATE TABLE）。
package feature

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/adminctx"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventoryhttp "go_wp/internal/module/inventory/inbound/http"
	inventorymodel "go_wp/internal/module/inventory/model"
	"go_wp/internal/templates"
)

// createUnclaimed 建一条未认领存货行（走 service 的真实建档路径）。
func createUnclaimed(t *testing.T, f *invFixture, warehouseID, sku string, qty int, track bool) *inventorydto.StockResp {
	t.Helper()
	res, err := f.inventory.CreateUnclaimedStock(context.Background(), &inventorydto.CreateUnclaimedStockReq{
		ProjectID: f.projectID, WarehouseID: warehouseID, SKUCode: sku,
		Quantity: qty, TrackQuantity: track,
	})
	if err != nil {
		t.Fatalf("建未认领存货 %s 失败: %v", sku, err)
	}
	return res
}

// stockReserved 直读真源表的预占数量（断言落库结果，不经 service）。
func (f *invFixture) stockReserved(t *testing.T, variantID, warehouseID string) int {
	t.Helper()
	var reserved int
	if err := f.db.Raw("SELECT reserved_quantity FROM inventory_stocks "+
		"WHERE variant_id = ? AND warehouse_id = ?", variantID, warehouseID).Scan(&reserved).Error; err != nil {
		t.Fatalf("读预占数量失败: %v", err)
	}
	return reserved
}

// movementCount 流水总条数（预占 / 释放不写流水，靠它断言）。
func (f *invFixture) movementCount(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_stock_movements WHERE project_id = ?",
		f.projectID).Scan(&n).Error; err != nil {
		t.Fatalf("读流水条数失败: %v", err)
	}
	return n
}

// TestUnclaimedStockCreateAndDuplicateCode 验收 4：
// 未认领行（variant_id / product_id 均 NULL）可建；同仓同裸码插第二行被拒。
func TestUnclaimedStockCreateAndDuplicateCode(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	row := createUnclaimed(t, f, wh.ID, "DRAWERSMOKE_001", 10, true)
	if row.Claimed {
		t.Fatalf("新建的存货行不该是已认领状态：%+v", row)
	}
	if row.Quantity != 10 || row.ReservedQuantity != 0 || row.AvailableQuantity != 10 {
		t.Fatalf("建档后应是 10 件、预占 0、可售 10，实际 %d/%d/%d",
			row.Quantity, row.ReservedQuantity, row.AvailableQuantity)
	}
	// 落库路径也要一致：NULL 必须是 NULL，不是空串。
	var variantID, productID *string
	if err := f.db.Raw("SELECT variant_id, product_id FROM inventory_stocks WHERE id = ?", row.ID).
		Row().Scan(&variantID, &productID); err != nil {
		t.Fatalf("读未认领行失败: %v", err)
	}
	if variantID != nil || productID != nil {
		t.Fatalf("未认领行的 variant_id / product_id 必须是 NULL，实际 %v / %v", variantID, productID)
	}

	// 同仓同裸码第二行：提前拦截成可读的业务错误（不是 23505）。
	if _, err := f.inventory.CreateUnclaimedStock(context.Background(), &inventorydto.CreateUnclaimedStockReq{
		ProjectID: f.projectID, WarehouseID: wh.ID, SKUCode: "DRAWERSMOKE_001",
		Quantity: 3, TrackQuantity: true,
	}); !errors.Is(err, inventoryenums.Err(inventoryenums.ErrWarehouseSKUCodeTaken)) {
		t.Fatalf("同仓同码应被拒为 ErrWarehouseSKUCodeTaken，实际 %v", err)
	}
	// 加工前缀也要能归一：`SZ_DRAWERSMOKE_002` 在 SZ 仓里应落成裸码。
	if got := createUnclaimed(t, f, wh.ID, "SZ_DRAWERSMOKE_002", 1, true); got.SKUCode != "DRAWERSMOKE_002" {
		t.Fatalf("仓码前缀应被剥成裸码，实际 %q", got.SKUCode)
	}
}

// TestClaimConflictKeepsBothRows 验收 5：
// 目标 (变体, 仓) 已有库存行时认领被拒，两行数量一字未变。
func TestClaimConflictKeepsBothRows(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SH", "上海仓", true)
	p := mustProduct(t, f, "Clash")
	v := f.firstVariant(t, p.ID)
	// 让这个变体在该仓有一行（入库 4 件）。
	changeIn(t, f, p, v, wh.ID, 4, "purchase_in")
	// 另建一条未认领的货，尝试认领到同一个变体。
	loose := createUnclaimed(t, f, wh.ID, "LOOSE_001", 9, true)

	_, err := f.inventory.ClaimStock(ctx, &inventorydto.ClaimStockReq{
		ProjectID: f.projectID, StockID: loose.ID, ProductID: p.ID, VariantID: v.ID,
	})
	if !errors.Is(err, inventoryenums.Err(inventoryenums.ErrStockAlreadyClaimed)) {
		t.Fatalf("目标已有库存行时应报 ErrStockAlreadyClaimed，实际 %v", err)
	}
	// 两行都还在原位：已认领那行仍 4 件，未认领那行仍 9 件且仍未认领。
	if got := f.stockQty(t, v.ID, wh.ID); got != 4 {
		t.Fatalf("冲突认领不得改动既有行数量，期望 4，实际 %d", got)
	}
	var qty int
	var variantID *string
	if err := f.db.Raw("SELECT quantity, variant_id FROM inventory_stocks WHERE id = ?", loose.ID).
		Row().Scan(&qty, &variantID); err != nil {
		t.Fatalf("读未认领行失败: %v", err)
	}
	if qty != 9 || variantID != nil {
		t.Fatalf("冲突认领不得改动待认领行：期望 9 件且仍未认领，实际 %d 件 / variant=%v", qty, variantID)
	}
}

// TestClaimThenUnclaimGuard 认领成功 + 解除认领守卫（验收 6）。
func TestClaimThenUnclaimGuard(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	// 商品变体在**默认仓**已自动有一行库存（建变体即生成），所以未认领行放在另一个仓 ——
	// 否则认领会撞「目标 (变体, 仓) 已有库存行」这条守卫（那正是 TestClaimConflictKeepsBothRows 的对象）。
	f.createWarehouse(t, "SZ", "苏州仓", true)
	wh := f.createWarehouse(t, "NB", "宁波仓", false)
	p := mustProduct(t, f, "Claimable")
	v := f.firstVariant(t, p.ID)
	loose := createUnclaimed(t, f, wh.ID, "CLAIM_ME_001", 5, true)

	claimed, err := f.inventory.ClaimStock(ctx, &inventorydto.ClaimStockReq{
		ProjectID: f.projectID, StockID: loose.ID, ProductID: p.ID, VariantID: v.ID,
	})
	if err != nil {
		t.Fatalf("认领失败: %v", err)
	}
	if !claimed.Claimed || claimed.VariantID != v.ID || claimed.Quantity != 5 {
		t.Fatalf("认领后应是「已认领 / 该变体 / 仍 5 件」，实际 %+v", claimed)
	}
	// 带数量时解除认领必须被拒。
	if _, err := f.inventory.UnclaimStock(ctx, &inventorydto.UnclaimStockReq{
		ProjectID: f.projectID, StockID: loose.ID,
	}); !errors.Is(err, inventoryenums.Err(inventoryenums.ErrStockHasQuantity)) {
		t.Fatalf("有数量时解除认领应报 ErrStockHasQuantity，实际 %v", err)
	}
	// 把数量清零（盘点调整到 0）之后解除成功。
	if _, err := f.inventory.UpdateStockTracking(ctx, &inventorydto.UpdateStockTrackingReq{
		ProjectID: f.projectID, WarehouseID: wh.ID, VariantID: v.ID,
		TrackQuantity: true, Quantity: 0,
	}); err != nil {
		t.Fatalf("数量清零失败: %v", err)
	}
	unclaimed, err := f.inventory.UnclaimStock(ctx, &inventorydto.UnclaimStockReq{
		ProjectID: f.projectID, StockID: loose.ID,
	})
	if err != nil {
		t.Fatalf("清零后解除认领应成功: %v", err)
	}
	if unclaimed.Claimed {
		t.Fatalf("解除之后不该还是已认领状态：%+v", unclaimed)
	}
}

// TestReserveWritesNoMovement 验收 7 / 9：
// 预占只改 reserved_quantity、不写流水；并发预占不超卖。
func TestReserveWritesNoMovement(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "Reserve")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, wh.ID, 10, "purchase_in")

	before := f.movementCount(t)
	if _, err := f.inventory.ReserveStock(ctx, &inventorydto.ReserveStockReq{
		ProjectID: f.projectID, SourceType: "order", SourceRef: "SO-1",
		Lines: []inventorydto.ReserveStockLine{{VariantID: v.ID, Quantity: 3, WarehouseID: wh.ID}},
	}); err != nil {
		t.Fatalf("预占失败: %v", err)
	}
	if after := f.movementCount(t); after != before {
		t.Fatalf("预占不得写流水：之前 %d 条，之后 %d 条", before, after)
	}
	if got := f.stockReserved(t, v.ID, wh.ID); got != 3 {
		t.Fatalf("预占后 reserved 应为 3，实际 %d", got)
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 10 {
		t.Fatalf("预占不得改 quantity：期望 10，实际 %d", got)
	}

	// 并发：两 goroutine 各预占 6，行上 quantity = 10 → 恰一成一败，reserved = 6。
	// 先把预占清回 0（各自独立），再跑并发。
	if err := f.db.Exec("UPDATE inventory_stocks SET reserved_quantity = 0 "+
		"WHERE variant_id = ? AND warehouse_id = ?", v.ID, wh.ID).Error; err != nil {
		t.Fatalf("重置预占失败: %v", err)
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.inventory.ReserveStock(ctx, &inventorydto.ReserveStockReq{
				ProjectID: f.projectID, SourceType: "order", SourceRef: "SO-concurrent",
				Lines: []inventorydto.ReserveStockLine{{VariantID: v.ID, Quantity: 6, WarehouseID: wh.ID}},
			})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	ok, failed := 0, 0
	for err := range results {
		if err == nil {
			ok++
		} else {
			failed++
		}
	}
	if ok != 1 || failed != 1 {
		t.Fatalf("并发预占应恰一成一败，实际成功 %d / 失败 %d", ok, failed)
	}
	if got := f.stockReserved(t, v.ID, wh.ID); got != 6 {
		t.Fatalf("并发后 reserved 应为 6（只成功一次），实际 %d", got)
	}
}

// TestReserveInsufficientRollsBackAllLines 验收 8：
// 两行预占请求（第一行够、第二行不够）→ 报错后第一行 reserved 回到原值。
func TestReserveInsufficientRollsBackAllLines(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p1 := mustProduct(t, f, "Enough")
	p2 := mustProduct(t, f, "Short")
	v1 := f.firstVariant(t, p1.ID)
	v2 := f.firstVariant(t, p2.ID)
	changeIn(t, f, p1, v1, wh.ID, 10, "purchase_in")
	changeIn(t, f, p2, v2, wh.ID, 2, "purchase_in")

	_, err := f.inventory.ReserveStock(ctx, &inventorydto.ReserveStockReq{
		ProjectID: f.projectID, SourceType: "order", SourceRef: "SO-short",
		Lines: []inventorydto.ReserveStockLine{
			{VariantID: v1.ID, Quantity: 5, WarehouseID: wh.ID},
			{VariantID: v2.ID, Quantity: 9, WarehouseID: wh.ID}, // 只有 2 件
		},
	})
	if !errors.Is(err, inventoryenums.Err(inventoryenums.ErrStockInsufficient)) {
		t.Fatalf("第二行不足应报 ErrStockInsufficient，实际 %v", err)
	}
	if got := f.stockReserved(t, v1.ID, wh.ID); got != 0 {
		t.Fatalf("整单拒绝时第一行的预占必须回到原值 0，实际 %d", got)
	}
	if got := f.stockReserved(t, v2.ID, wh.ID); got != 0 {
		t.Fatalf("不足的那一行不该留下预占，实际 %d", got)
	}
}

// TestReleaseReservation 释放预占：只改 reserved，不改 quantity、不写流水；超释放打回。
func TestReleaseReservation(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "Release")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, wh.ID, 10, "purchase_in")
	if _, err := f.inventory.ReserveStock(ctx, &inventorydto.ReserveStockReq{
		ProjectID: f.projectID, SourceType: "order", SourceRef: "SO-release",
		Lines: []inventorydto.ReserveStockLine{{VariantID: v.ID, Quantity: 4, WarehouseID: wh.ID}},
	}); err != nil {
		t.Fatalf("预占失败: %v", err)
	}
	before := f.movementCount(t)
	if _, err := f.inventory.ReleaseStock(ctx, &inventorydto.ReserveStockReq{
		ProjectID: f.projectID, SourceType: "order", SourceRef: "SO-release",
		Lines: []inventorydto.ReserveStockLine{{VariantID: v.ID, Quantity: 4, WarehouseID: wh.ID}},
	}); err != nil {
		t.Fatalf("释放失败: %v", err)
	}
	if got := f.stockReserved(t, v.ID, wh.ID); got != 0 {
		t.Fatalf("释放后 reserved 应为 0，实际 %d", got)
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 10 {
		t.Fatalf("释放不得改 quantity：期望 10，实际 %d", got)
	}
	if after := f.movementCount(t); after != before {
		t.Fatalf("释放不得写流水：之前 %d 条，之后 %d 条", before, after)
	}
	// 超释放：reserved 已经是 0，再释放 1 件必须打回（不静默夹到 0）。
	if _, err := f.inventory.ReleaseStock(ctx, &inventorydto.ReserveStockReq{
		ProjectID: f.projectID, SourceType: "order", SourceRef: "SO-release",
		Lines: []inventorydto.ReserveStockLine{{VariantID: v.ID, Quantity: 1, WarehouseID: wh.ID}},
	}); !errors.Is(err, inventoryenums.Err(inventoryenums.ErrReserveUnderflow)) {
		t.Fatalf("超释放应报 ErrReserveUnderflow，实际 %v", err)
	}
}

// TestUnclaimedRowsStayOutOfVariantTotals 验收 10：
// 未认领行不进商品侧聚合（VariantStockTotals 的返回 map 里不出现空串 key）。
func TestUnclaimedRowsStayOutOfVariantTotals(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "Totals")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, wh.ID, 5, "purchase_in")
	// 一条未认领的 7 件货。
	createUnclaimed(t, f, wh.ID, "ORPHAN_007", 7, true)

	totals, err := inventorymodel.NewModel(f.db).VariantStockTotals(ctx, f.projectID, nil)
	if err != nil {
		t.Fatalf("读变体合计失败: %v", err)
	}
	if _, ok := totals[""]; ok {
		t.Fatalf("未认领行不得聚成空串 key 的「变体」：%+v", totals)
	}
	if got := totals[v.ID]; got != 5 {
		t.Fatalf("已知变体的合计不该受未认领行影响：期望 5，实际 %d", got)
	}
}

// TestDeleteWarehouseBlockedByUnclaimedStock 验收 11：
// 仓里只有一条未认领且有数量的行 → 删仓被拒（不因 variant_id 为空而漏判）。
func TestDeleteWarehouseBlockedByUnclaimedStock(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	// 先建一个默认仓（删仓守卫对默认仓另有拒绝，故被测的仓不是默认仓）。
	first := f.createWarehouse(t, "SZ", "苏州仓", true)
	_ = first
	wh := f.createWarehouse(t, "SH", "上海仓", false)
	createUnclaimed(t, f, wh.ID, "ONLY_ORPHAN", 3, true)

	if err := f.inventory.DeleteWarehouse(ctx, &inventorydto.DeleteWarehouseReq{
		ID: wh.ID, ProjectID: f.projectID,
	}); !errors.Is(err, inventoryenums.Err(inventoryenums.ErrWarehouseHasStock)) {
		t.Fatalf("仓里只有未认领存货时删仓应报 ErrWarehouseHasStock，实际 %v", err)
	}
}

// TestUnclaimedRequiresTrackForQuantity 建档的自相矛盾输入被拒（数量 > 0 却不跟踪）。
func TestUnclaimedRequiresTrackForQuantity(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	_, err := f.inventory.CreateUnclaimedStock(context.Background(), &inventorydto.CreateUnclaimedStockReq{
		ProjectID: f.projectID, WarehouseID: wh.ID, SKUCode: "CONTRADICT_001",
		Quantity: 3, TrackQuantity: false,
	})
	if !errors.Is(err, inventoryenums.Err(inventoryenums.ErrStockUntrackedQuantity)) {
		t.Fatalf("有数量却不跟踪应被拒为 ErrStockUntrackedQuantity，实际 %v", err)
	}
}

// TestSKULedgerPageRendersUnclaimed 验收 12：页面渲染（徽标 + 认领状态筛选）。
func TestSKULedgerPageRendersUnclaimed(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "Ledger")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, wh.ID, 2, "purchase_in")
	createUnclaimed(t, f, wh.ID, "LEDGER_ORPHAN", 6, true)

	body := f.renderSKULedgerPage(t, "")
	if !strings.Contains(body, "未认领") {
		t.Fatalf("台账页应出现「未认领」徽标，实际 HTML（截断）：%s", truncate(body, 800))
	}
	if !strings.Contains(body, "LEDGER_ORPHAN") {
		t.Fatalf("台账页应列出未认领行 LEDGER_ORPHAN")
	}
	// 只筛未认领：已认领行（该变体的 SKU）不得出现。
	onlyUnclaimed := f.renderSKULedgerPage(t, "&claimed=unclaimed")
	if !strings.Contains(onlyUnclaimed, "LEDGER_ORPHAN") {
		t.Fatalf("?claimed=unclaimed 应仍能看到未认领行")
	}
	// 已认领行的 SKU 在表格单元格里是 `<code>…</code>`；认领抽屉的变体下拉把
	// 「商品名 · SKU」当 option 文本渲染（那是合法的，未认领行要用它来选目标），
	// 所以判据必须落在**单元格**上，不能用裸子串。
	if strings.Contains(onlyUnclaimed, "<code>"+bareSKU(v.SKUCode, wh.Code)+"</code>") {
		t.Fatalf("?claimed=unclaimed 不该出现已认领的行 %q", bareSKU(v.SKUCode, wh.Code))
	}
	// 反向确认：不筛选时那一行确实在（否则上一条断言是空转）。
	if !strings.Contains(body, "<code>"+bareSKU(v.SKUCode, wh.Code)+"</code>") {
		t.Fatalf("不筛选时台账页应列出已认领行 %q", bareSKU(v.SKUCode, wh.Code))
	}
}

// newSKULedgerRouter 装配只挂台账页的测试引擎（真实 Jet 模板 + 真实 service）。
//
// 不挂鉴权中间件：登录态与权限属装配层（另有 check-page-get-authz.sh 守），
// 本用例的对象是**页面形状**（未认领徽标与筛选是否真的按状态过滤）。
func newSKULedgerRouter(t *testing.T, f *invFixture) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(templateRoot(), true)
	engine.Use(func(c *gin.Context) {
		c.Set(adminctx.PermSetKey, map[string]bool{"inventory:stock_change": true})
		c.Set(adminctx.ButtonsKey, map[string]bool{"inventory.stock_change": true})
	})
	handle := inventoryhttp.NewInventoryPageHandle(f.inventory, f.projects, f.products)
	engine.GET("/admin/inventory/skus",
		adminui.PageSlot(inventoryhttp.InventorySKUsPageTemplate), handle.InventorySKUsPage)
	return engine
}

// renderSKULedgerPage 经真实 HTTP 链路渲染台账页。
func (f *invFixture) renderSKULedgerPage(t *testing.T, extraQuery string) string {
	t.Helper()
	router := newSKULedgerRouter(t, f)
	req := httptest.NewRequest(http.MethodGet, "/admin/inventory/skus?project="+f.projectID+extraQuery, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("台账页应 200，实际 %d：%s", w.Code, truncate(w.Body.String(), 400))
	}
	return w.Body.String()
}

// truncate 截断长串（失败信息里贴整页 HTML 没有意义）。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("…（共 %d 字节）", len(s))
}

// TestStockAlertCounts 验收（W4 §10.5 ③）：库存预警四卡口径。
//
// 构造四行：quantity=0（跟踪）/ quantity=5 reserved=5 / quantity=5 reserved=0 /
// track_quantity=false → OutOfStock=1, LowStock=1, Alert=2, Total=3。
func TestStockAlertCounts(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)

	// ① quantity = 0（跟踪）—— 缺货。
	p1 := mustProduct(t, f, "预警缺货")
	v1 := f.firstVariant(t, p1.ID)
	changeIn(t, f, p1, v1, wh.ID, 1, "purchase_in")
	if err := f.db.Exec("UPDATE inventory_stocks SET quantity = 0 WHERE variant_id = ? AND warehouse_id = ?",
		v1.ID, wh.ID).Error; err != nil {
		t.Fatalf("把库存改成 0 失败: %v", err)
	}

	// ② quantity = 5、reserved = 5 —— 货在但全被占住（可售为 0）。
	p2 := mustProduct(t, f, "预警被占")
	v2 := f.firstVariant(t, p2.ID)
	changeIn(t, f, p2, v2, wh.ID, 5, "purchase_in")
	if _, err := f.inventory.ReserveStock(ctx, &inventorydto.ReserveStockReq{
		ProjectID: f.projectID, SourceType: "order", SourceRef: "SO-alert",
		Lines: []inventorydto.ReserveStockLine{{VariantID: v2.ID, Quantity: 5, WarehouseID: wh.ID}},
	}); err != nil {
		t.Fatalf("预占失败: %v", err)
	}

	// ③ quantity = 5、reserved = 0 —— 健康（不进预警）。
	p3 := mustProduct(t, f, "预警健康")
	v3 := f.firstVariant(t, p3.ID)
	changeIn(t, f, p3, v3, wh.ID, 5, "purchase_in")

	// ④ 跟踪关闭（无限）—— 永远「有货」，不进预警。
	loose := createUnclaimed(t, f, wh.ID, "ALERT_UNTRACKED", 0, false)
	if loose.TrackQuantity {
		t.Fatalf("未跟踪行不该是跟踪状态")
	}
	// 商品侧自动建的那些初始 0 行也是在跟踪状态 —— 它们会被算进 Total / OutOfStock，
	// 所以断言用「增量」而不是绝对值：本用例另外造了 4 行，其中 3 行是跟踪的。
	before, err := f.inventory.StockAlert(ctx, f.projectID)
	if err != nil {
		t.Fatalf("读库存预警失败: %v", err)
	}
	// 已建商品（p1 起）各有一行跟踪库存；p1 的那行是 0（缺货）、p2 的是可售 0（低库存）、
	// p3 的是健康。所以三个增量分别是 +1 / +1 / +0。
	if before.OutOfStock != 1 {
		t.Fatalf("缺货应为 1（p1 那一行 quantity=0），实际 %d", before.OutOfStock)
	}
	if before.LowStock != 1 {
		t.Fatalf("可售为 0 应为 1（p2 那一行 5/−5），实际 %d", before.LowStock)
	}
	if before.Alert != 2 {
		t.Fatalf("要处理应为 2（缺货 +1、可售为 0 +1），实际 %d", before.Alert)
	}
	if before.Total != 3 {
		t.Fatalf("跟踪数量的行应为 3（p1/p2/p3 各一行；未跟踪行不计），实际 %d", before.Total)
	}
}

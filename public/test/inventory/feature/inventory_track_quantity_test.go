// inventory_track_quantity_test.go — 无限库存（不跟踪数量，迁移 261/262/263）的定向测试。
//
// 本文件覆盖库存域第一批的完成判据（用户 2026-09-19 拍板的口径，逐条对应）：
//
//  1. 无限行不受扣减与超卖校验影响（不校验、不扣减、不写数量流水，订单照卖）；
//  2. 跟踪行的既有行为不变（超卖仍整体拒绝、卖光仍是 0 而不是无限）；
//  3. 给无限行写数量会把它切成跟踪（入库 / 调整 / 行内编辑三条入口）；
//  4. CHECK (track_quantity OR quantity = 0) 拒绝「不跟踪但有数字」；
//  5. 存量行在迁移 261 之后一律 track_quantity = true（保守口径）；
//  6. 库存页三态渲染（∞ 无限 / 数字 / 未入库）与行内编辑表单（无限时数量框留空且禁用）；
//  7. WarehouseStocksByProducts 一次取回多商品多仓（含无限标记）；
//  8. 迁移 262 剥前缀：先扫描、撞了就把**可定位的明细**打回给人（不合并、不改码）。
//
// 断言一律直查真源列（inventory_stocks.track_quantity / quantity）与页面 HTML，
// 不走 service 自己返回的响应 —— 那只能证明「service 以为自己写了」。
package feature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	productdto "go_wp/internal/module/product/dto"
	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventoryenums "go_wp/internal/module/product/inventory/enums"
	inventoryhttp "go_wp/internal/module/product/inventory/inbound/http"
	"go_wp/internal/templates"
	"go_wp/internal/web/shell"
	"go_wp/public/test/support"
)

// —— 直查真源的小工具 ——

// stockTrack 直读某 (变体, 仓库) 库存行的跟踪开关；行不存在时返回 false。
func stockTrack(t *testing.T, f *invFixture, variantID, warehouseID string) bool {
	t.Helper()
	var track bool
	if err := f.db.Raw("SELECT track_quantity FROM inventory_stocks "+
		"WHERE variant_id = ? AND warehouse_id = ?", variantID, warehouseID).Scan(&track).Error; err != nil {
		t.Fatalf("读跟踪开关失败: %v", err)
	}
	return track
}

// trackRowCount 某 (变体, 仓库) 的库存行数（0 = 未入库）。
func trackRowCount(t *testing.T, f *invFixture, variantID, warehouseID string) int {
	t.Helper()
	var n int
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_stocks "+
		"WHERE variant_id = ? AND warehouse_id = ?", variantID, warehouseID).Scan(&n).Error; err != nil {
		t.Fatalf("统计库存行失败: %v", err)
	}
	return n
}

// stockSKUOf 直读库存行的 sku_code（仓库侧裸码；测试不跟踪商品侧的编码口径，
// 页面按这个值查询，免得被另一批的前缀改动牵连）。
func stockSKUOf(t *testing.T, f *invFixture, variantID, warehouseID string) string {
	t.Helper()
	var code string
	if err := f.db.Raw("SELECT sku_code FROM inventory_stocks "+
		"WHERE variant_id = ? AND warehouse_id = ?", variantID, warehouseID).Scan(&code).Error; err != nil {
		t.Fatalf("读库存 sku_code 失败: %v", err)
	}
	return code
}

// changeInBareSKU 入库到指定仓，并显式指定**仓库侧裸码**。
//
// 为什么不能直接用共享的 changeIn：它传的是变体表的 sku_code（商品侧身份，可能带仓码前缀），
// 而库存行的 sku_code 是仓库侧裸码 —— 混用会让同一个 SKU 在不同仓落下两种编码，
// 页面按 SKU 查询时就只查得到一半（这条用例正是要三态同时出现，必须编码一致）。
func changeInBareSKU(t *testing.T, f *invFixture, p *productdto.ProductResp, v *productdto.VariantResp,
	warehouseID string, quantity int, skuCode string) {
	t.Helper()
	if _, err := f.inventory.ChangeStock(context.Background(), &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionIn, ReasonCode: "purchase_in",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: skuCode,
			WarehouseID: warehouseID, Quantity: quantity,
		}},
	}); err != nil {
		t.Fatalf("入库 %d 失败: %v", quantity, err)
	}
}

// changeOut 出库（返回 error 而不 t.Fatal：有几条用例就是来断言它被拒的）。
func changeOut(f *invFixture, p *productdto.ProductResp, v *productdto.VariantResp,
	warehouseID string, quantity int, sourceRef string) error {
	_, err := f.inventory.ChangeStock(context.Background(), &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionOut, ReasonCode: "sale_out",
		SourceType: "order", SourceRef: sourceRef,
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode,
			WarehouseID: warehouseID, Quantity: quantity,
		}},
	})
	return err
}

// adjustTo 盘点调整到目标绝对量（返回 error）。
func adjustTo(f *invFixture, p *productdto.ProductResp, v *productdto.VariantResp,
	warehouseID string, target int) error {
	_, err := f.inventory.ChangeStock(context.Background(), &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionAdjust, ReasonCode: "stocktake_adjust",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode,
			WarehouseID: warehouseID, Quantity: target,
		}},
	})
	return err
}

// TestInfiniteStockSkipsDeductAndOverSellCheck 判据 1：
// 无限行（track_quantity = false）不校验可用量、不扣减 —— 订单照卖。
func TestInfiniteStockSkipsDeductAndOverSellCheck(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "无限商品")
	v := f.firstVariant(t, p.ID)

	// 新建行默认无限（新建行才默认无限；存量行由迁移 261 保守置 true）。
	if stockTrack(t, f, v.ID, wh.ID) {
		t.Fatalf("新建库存行应默认不跟踪（无限），实际 track_quantity = true")
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 0 {
		t.Fatalf("无限行的数量应为 0（CHECK 保证），实际 %d", got)
	}

	// 卖 3 件：无限行不校验可用量（数量是 0 也不报「可用量不足」）、不扣减。
	if err := changeOut(f, p, v, wh.ID, 3, "SO-INF-1"); err != nil {
		t.Fatalf("无限行的出库不该被拒（不校验可用量）：%v", err)
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 0 {
		t.Fatalf("无限行的出库不该扣减数量，实际 %d", got)
	}
	if stockTrack(t, f, v.ID, wh.ID) {
		t.Fatalf("出库不该把无限行切成跟踪")
	}
	// 没有数量变动 ⇒ 没有变动 ⇒ 不写流水（与「调整到当前值」同一口径）。
	if n := countMovements(t, f, v.ID); n != 0 {
		t.Fatalf("无限行的出库不产生数量变动，不该写流水，实际 %d 条", n)
	}

	// 一次卖 999 件也一样：无限的定义就是数量不构成约束。
	if err := changeOut(f, p, v, wh.ID, 999, "SO-INF-2"); err != nil {
		t.Fatalf("无限行的大额出库同样不该被拒：%v", err)
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 0 {
		t.Fatalf("无限行的数量应保持 0，实际 %d", got)
	}

	// DeductStock（订单扣减入口）走同一套 applyStockChanges，同样跳过。
	if _, err := f.inventory.DeductStock(context.Background(), &inventorydto.DeductStockReq{
		ProjectID: f.projectID, ReasonCode: "sale_out", SourceType: "order", SourceRef: "SO-INF-3",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, WarehouseID: wh.ID, Quantity: 50,
		}},
	}); err != nil {
		t.Fatalf("无限行的 DeductStock 不该被拒：%v", err)
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 0 {
		t.Fatalf("无限行的 DeductStock 不该扣减，实际 %d", got)
	}
}

// TestTrackedStockKeepsExistingBehavior 判据 2：
// 跟踪行的既有行为一字不变 —— 超卖整体拒绝、卖光仍是 0（不是无限）。
func TestTrackedStockKeepsExistingBehavior(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "跟踪商品")
	v := f.firstVariant(t, p.ID)

	// 入库 5：显式给数量 ⇒ 切成跟踪。
	changeIn(t, f, p, v, wh.ID, 5, "purchase_in")
	if !stockTrack(t, f, v.ID, wh.ID) {
		t.Fatalf("入库给了具体数量，该行应被切成跟踪")
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 5 {
		t.Fatalf("入库 5 后应为 5，实际 %d", got)
	}
	before := countMovements(t, f, v.ID)

	// 出库 6 > 5：整体拒绝，数量与流水都不动。
	err := changeOut(f, p, v, wh.ID, 6, "SO-OVER")
	if err == nil || err.Error() != inventoryenums.ErrStockInsufficient {
		t.Fatalf("跟踪行超卖应报 ErrStockInsufficient，实际 %v", err)
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 5 {
		t.Fatalf("超卖被拒后数量应保持 5，实际 %d", got)
	}
	if n := countMovements(t, f, v.ID); n != before {
		t.Fatalf("超卖被拒后不该多写流水：%d → %d", before, n)
	}

	// 卖光：数量 0 但仍是**跟踪**（0 与无限必须能分开）。
	if err := changeOut(f, p, v, wh.ID, 5, "SO-ALL"); err != nil {
		t.Fatalf("卖光应成功：%v", err)
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 0 {
		t.Fatalf("卖光后数量应为 0，实际 %d", got)
	}
	if !stockTrack(t, f, v.ID, wh.ID) {
		t.Fatalf("卖光的行必须仍然跟踪（0 ≠ 无限）")
	}
	// 卖光后再卖 1 件：仍然超卖拒绝（无限才不校验）。
	if err := changeOut(f, p, v, wh.ID, 1, "SO-AGAIN"); err == nil ||
		err.Error() != inventoryenums.ErrStockInsufficient {
		t.Fatalf("卖光的跟踪行再出库应报 ErrStockInsufficient，实际 %v", err)
	}
}

// TestQuantityWriteSwitchesInfiniteRowToTracked 判据 3：
// 入库 / 调整只要显式给了数量，就把无限行切成跟踪（否则 CHECK 会在写非 0 数量时拒绝）。
func TestQuantityWriteSwitchesInfiniteRowToTracked(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)

	// ① 入库方向：数量 > 0。
	pIn := mustProduct(t, f, "入库切跟踪")
	vIn := f.firstVariant(t, pIn.ID)
	changeIn(t, f, pIn, vIn, wh.ID, 4, "purchase_in")
	if !stockTrack(t, f, vIn.ID, wh.ID) || f.stockQty(t, vIn.ID, wh.ID) != 4 {
		t.Fatalf("入库应把无限行切成跟踪并写入 4，实际 track=%v qty=%d",
			stockTrack(t, f, vIn.ID, wh.ID), f.stockQty(t, vIn.ID, wh.ID))
	}

	// ② 调整方向：目标 0 —— 「给 0」也是显式给数量，同样要切成跟踪（0 = 卖光，不是无限）。
	pAdj := mustProduct(t, f, "调整切跟踪")
	vAdj := f.firstVariant(t, pAdj.ID)
	if stockTrack(t, f, vAdj.ID, wh.ID) {
		t.Fatalf("前置：该行应为无限")
	}
	if err := adjustTo(f, pAdj, vAdj, wh.ID, 0); err != nil {
		t.Fatalf("调整到 0 应成功：%v", err)
	}
	if !stockTrack(t, f, vAdj.ID, wh.ID) {
		t.Fatalf("调整到 0 也应把该行切成跟踪（0 是显式数量）")
	}
	if got := f.stockQty(t, vAdj.ID, wh.ID); got != 0 {
		t.Fatalf("调整到 0 后数量应为 0，实际 %d", got)
	}

	// ③ 调整方向：目标 > 0（同一行的跟踪状态保持）。
	if err := adjustTo(f, pAdj, vAdj, wh.ID, 9); err != nil {
		t.Fatalf("调整到 9 应成功：%v", err)
	}
	if !stockTrack(t, f, vAdj.ID, wh.ID) || f.stockQty(t, vAdj.ID, wh.ID) != 9 {
		t.Fatalf("调整到 9 后应为跟踪且数量 9，实际 track=%v qty=%d",
			stockTrack(t, f, vAdj.ID, wh.ID), f.stockQty(t, vAdj.ID, wh.ID))
	}
}

// TestCheckRejectsUntrackedRowWithQuantity 判据 4：
// CHECK (track_quantity OR quantity = 0) 拒绝「不跟踪但有数字」—— 库侧的硬兜底。
func TestCheckRejectsUntrackedRowWithQuantity(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "CHECK 商品")
	v := f.firstVariant(t, p.ID)

	if stockTrack(t, f, v.ID, wh.ID) {
		t.Fatalf("前置：新建行应为无限")
	}
	// 绕过 service 直接 UPDATE：库必须拒绝。
	err := f.db.Exec("UPDATE inventory_stocks SET quantity = 5 WHERE variant_id = ? AND warehouse_id = ?",
		v.ID, wh.ID).Error
	if err == nil {
		t.Fatalf("不跟踪的行写非 0 数量必须被 CHECK 拒绝")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "track_quantity") {
		t.Fatalf("拒绝原因应指向 track_quantity 约束（便于定位），实际 %v", err)
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 0 {
		t.Fatalf("被拒后数量应保持 0，实际 %d", got)
	}

	// 同一条 UPDATE 在跟踪行上是合法的（约束只针对「不跟踪却有数字」）。
	changeIn(t, f, p, v, wh.ID, 5, "purchase_in")
	if err := f.db.Exec("UPDATE inventory_stocks SET quantity = 6 WHERE variant_id = ? AND warehouse_id = ?",
		v.ID, wh.ID).Error; err != nil {
		t.Fatalf("跟踪行写数量不该被 CHECK 拒绝：%v", err)
	}
}

// —— 迁移 261 / 262 的最小旧 schema 用例（support.NewPGTestDB：故意构造旧结构）——

// splitMigrationSQL 把迁移文件拆成可逐条执行的语句。
//
// 为什么需要：迁移文件里有 DO $$ ... $$ 块（含分号），而 gorm 的 Exec 走扩展协议、
// 不接受一次多条语句。这里做一个**美元引用感知**的最小拆分器 —— 只处理本测试用到的
// 两个文件（普通 SQL + DO 块），不追求通用解析器。
func splitMigrationSQL(t *testing.T, src string) []string {
	t.Helper()
	var stmts []string
	var buf strings.Builder
	inDollar := false
	runes := []rune(src)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		// 行注释：整行丢掉（注释里的分号不能当语句边界）。
		if !inDollar && c == '-' && i+1 < len(runes) && runes[i+1] == '-' {
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
			continue
		}
		if c == '$' && i+1 < len(runes) && runes[i+1] == '$' {
			inDollar = !inDollar
			buf.WriteString("$$")
			i++
			continue
		}
		if c == ';' && !inDollar {
			if s := strings.TrimSpace(buf.String()); s != "" {
				stmts = append(stmts, s)
			}
			buf.Reset()
			continue
		}
		buf.WriteRune(c)
	}
	if s := strings.TrimSpace(buf.String()); s != "" {
		stmts = append(stmts, s)
	}
	return stmts
}

// runMigrationFile 逐条执行迁移文件（测试用；生产由 migrator 执行）。
func runMigrationFile(t *testing.T, db *gorm.DB, path string) error {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读迁移文件 %s 失败: %v", path, err)
	}
	for _, stmt := range splitMigrationSQL(t, string(raw)) {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

// oldInventorySchema 建一份**没有 track_quantity** 的旧形态库存表（迁移 261 之前的结构）。
//
// 这是「故意构造旧 schema」的用例：NewPGTestDB 给空库，表由本函数手抄 —— 这里要的就是
// 「迁移跑之前」的样子，塞完整生产结构反而测不到存量纠正。
func oldInventorySchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	stmts := []string{
		`CREATE TABLE inventory_warehouses (
			id text PRIMARY KEY,
			code text NOT NULL,
			name text NOT NULL
		)`,
		`CREATE TABLE inventory_stocks (
			id text PRIMARY KEY,
			project_id uuid NOT NULL,
			warehouse_id text NOT NULL,
			product_id text NOT NULL,
			variant_id text NOT NULL,
			sku_code text NOT NULL,
			quantity integer NOT NULL DEFAULT 0,
			create_time timestamptz NOT NULL DEFAULT now(),
			update_time timestamptz NOT NULL DEFAULT now()
		)`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("建旧 schema 失败: %v", err)
		}
	}
}

// TestMigration261MarksExistingRowsTracked 判据 5：
// 迁移 261 加列 + CHECK，并把**存量行一律置 track_quantity = true**（保守口径）。
func TestMigration261MarksExistingRowsTracked(t *testing.T) {
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("PG 不可用：%v", err)
	}
	oldInventorySchema(t, db)

	const migrationPath = "../../../../public/migrations/261_inventory_stock_track_quantity.sql"
	proj := "11111111-1111-1111-1111-111111111111"
	if err := db.Exec(`INSERT INTO inventory_warehouses (id, code, name) VALUES ('w1', 'SZ', '苏州仓')`).Error; err != nil {
		t.Fatalf("插入仓库失败: %v", err)
	}
	// 两行存量：quantity = 0（分不清是占位还是卖光）与 quantity = 5（明显有货）。
	for _, row := range []struct {
		id  string
		qty int
	}{
		{"s1", 0},
		{"s2", 5},
	} {
		if err := db.Exec(`INSERT INTO inventory_stocks
			(id, project_id, warehouse_id, product_id, variant_id, sku_code, quantity)
			VALUES (?, ?, 'w1', 'p1', 'v1', 'DRAWERSMOKE_001', ?)`,
			row.id, proj, row.qty).Error; err != nil {
			t.Fatalf("插入存量库存行失败: %v", err)
		}
	}

	if err := runMigrationFile(t, db, migrationPath); err != nil {
		t.Fatalf("执行迁移 261 失败: %v", err)
	}

	// ① 列已加、NOT NULL、默认 false（新行默认无限）。
	var isNullable, colDefault string
	if err := db.Raw(`SELECT is_nullable, COALESCE(column_default, '') FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'inventory_stocks' AND column_name = 'track_quantity'`).
		Row().Scan(&isNullable, &colDefault); err != nil {
		t.Fatalf("读 track_quantity 列定义失败: %v", err)
	}
	if isNullable != "NO" {
		t.Fatalf("track_quantity 应是 NOT NULL，实际 is_nullable=%s", isNullable)
	}
	if strings.TrimSpace(colDefault) != "false" {
		t.Fatalf("track_quantity 的列默认必须是 false（新建行默认无限），实际 %q", colDefault)
	}

	// ② 存量行**一律 true**（保守：那些 0 分不清占位还是卖光，判成无限会超卖）。
	var tracked int
	if err := db.Raw(`SELECT COUNT(*) FROM inventory_stocks WHERE track_quantity`).Scan(&tracked).Error; err != nil {
		t.Fatalf("统计 track_quantity 失败: %v", err)
	}
	if tracked != 2 {
		t.Fatalf("存量行应一律 track_quantity = true，实际 %d/2", tracked)
	}

	// ③ CHECK 在位：把 s1 明确改成无限（必须同时清零，否则约束本身就是拦路虎），
	//    再写数字必须被拒 —— 这正是 CHECK (track_quantity OR quantity = 0) 要表达的事。
	if err := db.Exec(`UPDATE inventory_stocks SET quantity = 0, track_quantity = false WHERE id = 's1'`).Error; err != nil {
		t.Fatalf("把 s1 改成无限失败: %v", err)
	}
	if err := db.Exec(`UPDATE inventory_stocks SET quantity = 7 WHERE id = 's1'`).Error; err == nil {
		t.Fatalf("CHECK (track_quantity OR quantity = 0) 必须拒绝「不跟踪但有数字」")
	}

	// ④ 幂等：s1 现在是无限行，重跑迁移不得把它掰回 true。
	if err := runMigrationFile(t, db, migrationPath); err != nil {
		t.Fatalf("重跑迁移 261 失败（应幂等）：%v", err)
	}
	var s1Tracked bool
	if err := db.Raw(`SELECT track_quantity FROM inventory_stocks WHERE id = 's1'`).Scan(&s1Tracked).Error; err != nil {
		t.Fatalf("重跑后读 s1 失败: %v", err)
	}
	if s1Tracked {
		t.Fatalf("重跑迁移 261 不该把运营手工改成无限的存量行掰回跟踪")
	}
}

// TestMigration262StripsPrefixAndReportsCollisions 判据 8：
// 剥前缀撞车时**打回给人**（明细可定位、不合并、不改码）；不撞则剥干净且幂等。
func TestMigration262StripsPrefixAndReportsCollisions(t *testing.T) {
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("PG 不可用：%v", err)
	}
	oldInventorySchema(t, db)

	const migrationPath = "../../../../public/migrations/262_inventory_stock_sku_strip_warehouse_prefix.sql"
	const proj = "22222222-2222-2222-2222-222222222222"
	if err := db.Exec(`INSERT INTO inventory_warehouses (id, code, name) VALUES ('w1', 'SZ', '苏州仓')`).Error; err != nil {
		t.Fatalf("插入仓库失败: %v", err)
	}
	insert := func(id, sku string, qty int, productID, variantID string) {
		t.Helper()
		if err := db.Exec(`INSERT INTO inventory_stocks
			(id, project_id, warehouse_id, product_id, variant_id, sku_code, quantity)
			VALUES (?, ?, 'w1', ?, ?, ?, ?)`, id, proj, productID, variantID, sku, qty).Error; err != nil {
			t.Fatalf("插入库存行 %s 失败: %v", id, err)
		}
	}
	// 撞车：A 带前缀、B 已是裸码 —— 剥完是同一个 (warehouse_id, sku_code)。
	insert("a1", "SZ_DRAWERSMOKE_001", 3, "p-a", "v-a")
	insert("b1", "DRAWERSMOKE_001", 7, "p-b", "v-b")

	err = runMigrationFile(t, db, migrationPath)
	if err == nil {
		t.Fatalf("剥前缀后同仓同码撞车时迁移必须显式失败（不合并、不改码）")
	}
	msg := err.Error()
	// 明细必须能把人直接带到那两行：仓短码 / 裸码 / 行 id / product_id / variant_id / 原 sku_code / 数量。
	for _, want := range []string{
		"仓短码=SZ",
		"DRAWERSMOKE_001",
		"id=a1", "id=b1",
		"product_id=p-a", "product_id=p-b",
		"variant_id=v-a",
		"原 sku_code='SZ_DRAWERSMOKE_001'",
		"原 sku_code='DRAWERSMOKE_001'",
		"数量=3", "数量=7",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("冲突明细里缺少可定位字段 %q，实际错误：%s", want, msg)
		}
	}
	// 报错时数据一字不动（绝不「先改一半再失败」）。
	var aSKU, bSKU string
	if err := db.Raw(`SELECT sku_code FROM inventory_stocks WHERE id = 'a1'`).Scan(&aSKU).Error; err != nil {
		t.Fatalf("读 a1 失败: %v", err)
	}
	if err := db.Raw(`SELECT sku_code FROM inventory_stocks WHERE id = 'b1'`).Scan(&bSKU).Error; err != nil {
		t.Fatalf("读 b1 失败: %v", err)
	}
	if aSKU != "SZ_DRAWERSMOKE_001" || bSKU != "DRAWERSMOKE_001" {
		t.Fatalf("迁移失败时不该改动任何一行，实际 a1=%q b1=%q", aSKU, bSKU)
	}

	// 人工处理后（删掉其中一行 —— 这正是「打回给人拍」的动作）重跑：剥干净。
	if err := db.Exec(`DELETE FROM inventory_stocks WHERE id = 'b1'`).Error; err != nil {
		t.Fatalf("删除 b1 失败: %v", err)
	}
	if err := runMigrationFile(t, db, migrationPath); err != nil {
		t.Fatalf("无冲突后迁移 262 应成功: %v", err)
	}
	if err := db.Raw(`SELECT sku_code FROM inventory_stocks WHERE id = 'a1'`).Scan(&aSKU).Error; err != nil {
		t.Fatalf("重跑后读 a1 失败: %v", err)
	}
	if aSKU != "DRAWERSMOKE_001" {
		t.Fatalf("带前缀的行应被剥成裸码，实际 %q", aSKU)
	}
	// 幂等：再跑一次不改变任何东西。
	if err := runMigrationFile(t, db, migrationPath); err != nil {
		t.Fatalf("重跑迁移 262 应幂等: %v", err)
	}
	if err := db.Raw(`SELECT sku_code FROM inventory_stocks WHERE id = 'a1'`).Scan(&aSKU).Error; err != nil {
		t.Fatalf("幂等重跑后读 a1 失败: %v", err)
	}
	if aSKU != "DRAWERSMOKE_001" {
		t.Fatalf("幂等重跑不该再改 sku_code，实际 %q", aSKU)
	}
}

// TestWarehouseStocksByProductsAcrossWarehouses 判据 7：
// WarehouseStocksByProducts 一次取回多商品多仓（含无限标记），字段与冻结签名逐项一致。
func TestWarehouseStocksByProductsAcrossWarehouses(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	sz := f.createWarehouse(t, "SZ", "苏州仓", true)
	sh := f.createWarehouse(t, "SH", "上海仓", false)

	// p1：SZ 默认无限；SH 入库 5（跟踪）。
	p1 := mustProductPriced(t, f, "分仓聚合一", 9.9)
	v1 := f.firstVariant(t, p1.ID)
	changeIn(t, f, p1, v1, sh.ID, 5, "purchase_in")

	// p2：只在 SZ 有一行（默认无限）。
	p2 := mustProductPriced(t, f, "分仓聚合二", 19.9)

	rows, err := f.inventory.WarehouseStocksByProducts(ctx, f.projectID, []string{p1.ID, p2.ID})
	if err != nil {
		t.Fatalf("WarehouseStocksByProducts 失败: %v", err)
	}
	// p1 两行（SZ 无限 + SH 跟踪）、p2 一行（SZ 无限）。
	if len(rows) != 3 {
		t.Fatalf("应一次取回 3 行（p1 两仓 + p2 一仓），实际 %d：%+v", len(rows), rows)
	}
	type key struct{ productID, warehouseID string }
	byKey := make(map[key]inventorydto.ProductWarehouseStock, len(rows))
	for _, r := range rows {
		if r.ProductID == "" || r.WarehouseID == "" || r.WarehouseCode == "" || r.WarehouseName == "" || r.SKUCode == "" {
			t.Fatalf("分仓聚合的元素字段必须齐（冻结签名八项）：%+v", r)
		}
		byKey[key{r.ProductID, r.WarehouseID}] = r
	}
	if r := byKey[key{p1.ID, sz.ID}]; r.TrackQuantity || r.Quantity != 0 {
		t.Fatalf("p1 在 SZ 应是无限（track=false, qty=0），实际 %+v", r)
	}
	if r := byKey[key{p1.ID, sh.ID}]; !r.TrackQuantity || r.Quantity != 5 {
		t.Fatalf("p1 在 SH 应是跟踪且数量 5，实际 %+v", r)
	}
	if r, ok := byKey[key{p2.ID, sh.ID}]; ok {
		t.Fatalf("p2 在 SH 没有库存行，不该出现在结果里：%+v", r)
	}
	if _, ok := byKey[key{p2.ID, sz.ID}]; !ok {
		t.Fatalf("p2 在 SZ 应有一行（新建变体即建行）")
	}
	// 仓码 / 仓名随行给出（商品列表不必再查一次仓库）。
	if r := byKey[key{p1.ID, sh.ID}]; r.WarehouseCode != "SH" || r.WarehouseName != "上海仓" {
		t.Fatalf("仓码 / 仓名应随行给出，实际 %+v", r)
	}

	// 空入参：不查库，返回空切片（调用方按 len 分组即可）。
	empty, err := f.inventory.WarehouseStocksByProducts(ctx, f.projectID, nil)
	if err != nil {
		t.Fatalf("空入参不该报错：%v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("空入参应返回空结果，实际 %d 行", len(empty))
	}
}

// —— 库存页三态与行内编辑 ——

// newTrackQuantityPageEngine 装配只挂库存页与「跟踪开关」行内编辑写入口的测试引擎。
func newTrackQuantityPageEngine(t *testing.T) (*gin.Engine, *invFixture) {
	t.Helper()
	f := newInvFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(templateRoot(), true)
	engine.Use(func(c *gin.Context) {
		// 仅供渲染（shell.Prepare 读 PermSetKey）；拦截由路由上的 Casbin 中间件负责。
		c.Set(shell.PermSetKey, map[string]bool{"inventory:stock_change": true})
	})
	handle := inventoryhttp.NewInventoryPageHandle(f.inventory, f.projects, f.products)
	engine.GET("/admin/inventory", handle.InventoryPage)
	engine.POST("/admin/inventory/stock/tracking", handle.InventoryStockTrackingUpdate)
	return engine, f
}

// TestInventoryPageRendersThreeQuantityStates 判据 6：
// 「该 SKU 的各仓库存」数量列三态（∞ 无限 / 数字 / 未入库），
// 且无限行的数量框留空且禁用（绝不预填 0）。
func TestInventoryPageRendersThreeQuantityStates(t *testing.T) {
	engine, f := newTrackQuantityPageEngine(t)
	if engine == nil {
		return
	}
	sz := f.createWarehouse(t, "SZ", "苏州仓", true)
	sh := f.createWarehouse(t, "SH", "上海仓", false)
	// NJ 自始至终没有该 SKU 的库存行 → 未入库。
	f.createWarehouse(t, "NJ", "南京仓", false)

	p := mustProduct(t, f, "三态商品")
	v := f.firstVariant(t, p.ID)
	// SZ 的行由商品模块建变体时生成（默认无限）；SH 用**同一个仓库侧裸码**入库 5 切成跟踪。
	sku := stockSKUOf(t, f, v.ID, sz.ID)
	changeInBareSKU(t, f, p, v, sh.ID, 5, sku)
	if stockTrack(t, f, v.ID, sz.ID) {
		t.Fatalf("前置：SZ 的行应是无限")
	}
	if trackRowCount(t, f, v.ID, sz.ID) != 1 {
		t.Fatalf("前置：SZ 应有一行")
	}
	if got := stockSKUOf(t, f, v.ID, sh.ID); got != sku {
		t.Fatalf("前置：两个仓的库存行应共用同一个仓库侧裸码，实际 %q vs %q", got, sku)
	}
	rec := httptestGet(engine, "/admin/inventory?project="+url.QueryEscape(f.projectID)+"&sku="+url.QueryEscape(sku))
	if rec.Code != http.StatusOK {
		t.Fatalf("库存页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// 三态：∞ 无限 / 数字 / 未入库。
	if !strings.Contains(body, "∞ 无限") {
		t.Fatalf("无限行应渲染成 ∞ 无限（不能显示 0 —— 那看起来像没货）")
	}
	if !strings.Contains(body, "未入库") {
		t.Fatalf("没有库存行的仓应渲染成未入库（三态之一）")
	}
	if !strings.Contains(body, "<td>5</td>") {
		t.Fatalf("跟踪行的数量列应渲染数字 5")
	}

	// 行内表单：两个仓有库存行 ⇒ 两个表单；未入库的仓没有表单。
	if n := strings.Count(body, `action="/admin/inventory/stock/tracking"`); n != 2 {
		t.Fatalf("应只有 2 个可编辑的行（SZ / SH），实际 %d 个跟踪表单", n)
	}

	// 数量框：无限的那个留空且禁用；跟踪的那个带 5、不 disabled。
	inputs := regexp.MustCompile(`<input[^>]*stock-qty[^>]*>`).FindAllString(body, -1)
	if len(inputs) != 2 {
		t.Fatalf("应有 2 个行内数量框，实际 %d：%v", len(inputs), inputs)
	}
	disabled, filled := 0, 0
	for _, in := range inputs {
		if strings.Contains(in, "disabled") {
			disabled++
			if !strings.Contains(in, `value=""`) {
				t.Fatalf("无限行的数量框必须留空（绝不预填 0）：%s", in)
			}
		}
		if strings.Contains(in, `value="5"`) {
			filled++
		}
	}
	if disabled != 1 {
		t.Fatalf("应恰好 1 个数量框被禁用（无限行），实际 %d", disabled)
	}
	if filled != 1 {
		t.Fatalf("应恰好 1 个数量框预填 5（跟踪行），实际 %d", filled)
	}
}

// postTracking 提交行内编辑表单（字段与模板里那份表单一一对应）。
func postTracking(engine *gin.Engine, projectID, warehouseID, variantID, skuCode string,
	track bool, quantity string) *httptest.ResponseRecorder {
	form := url.Values{}
	form.Set("csrf_token", "test-csrf")
	form.Set("projectId", projectID)
	form.Set("warehouseId", warehouseID)
	form.Set("variantId", variantID)
	form.Set("skuCode", skuCode)
	if track {
		form.Set("trackQuantity", "1")
	}
	if quantity != "" {
		form.Set("quantity", quantity)
	}
	return postForm(engine, "/admin/inventory/stock/tracking", form)
}

// TestInventoryPageTrackingToggle 判据 3/6 的写路径：
// 行内编辑能把无限行切成跟踪（写数量 + 流水）、把跟踪行改回无限（清零 + 关开关），
// 且对无限行提交非 0 数量会被拒（不静默改数）。
func TestInventoryPageTrackingToggle(t *testing.T) {
	engine, f := newTrackQuantityPageEngine(t)
	if engine == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "行内编辑商品")
	v := f.firstVariant(t, p.ID)
	sku := stockSKUOf(t, f, v.ID, wh.ID)

	// ① 无限 → 跟踪 + 数量 7：走变动契约（手工调整），留一条流水。
	if rec := postTracking(engine, f.projectID, wh.ID, v.ID, sku, true, "7"); rec.Code != http.StatusFound {
		t.Fatalf("行内编辑应 302 回列表，实际 %d：%s", rec.Code, rec.Body.String())
	} else if !strings.Contains(rec.Header().Get("Location"), "done=1") {
		t.Fatalf("成功回跳应带回 done=1，实际 %q", rec.Header().Get("Location"))
	}
	if !stockTrack(t, f, v.ID, wh.ID) {
		t.Fatalf("提交跟踪 + 数量后该行应变跟踪")
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 7 {
		t.Fatalf("提交数量 7 后真源应为 7，实际 %d", got)
	}
	if n := countMovements(t, f, v.ID); n != 1 {
		t.Fatalf("行内编辑写数量必须留一条流水（有变动必有流水），实际 %d 条", n)
	}

	// ② 跟踪 → 无限：先把数量清成 0（同样留流水），再关开关。
	if rec := postTracking(engine, f.projectID, wh.ID, v.ID, sku, false, ""); rec.Code != http.StatusFound {
		t.Fatalf("置为无限应 302 回列表，实际 %d", rec.Code)
	} else if !strings.Contains(rec.Header().Get("Location"), "done=1") {
		t.Fatalf("置为无限应回带 done=1，实际 %q", rec.Header().Get("Location"))
	}
	if stockTrack(t, f, v.ID, wh.ID) {
		t.Fatalf("置为无限后 track_quantity 应为 false")
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 0 {
		t.Fatalf("无限行的数量必须被清成 0，实际 %d", got)
	}
	if n := countMovements(t, f, v.ID); n != 2 {
		t.Fatalf("清零也是一次变动，应再写一条流水，实际共 %d 条", n)
	}

	// ③ 无限行提交非 0 数量：拒绝（不静默按 0 处理），真源不动。
	rec := postTracking(engine, f.projectID, wh.ID, v.ID, sku, false, "5")
	if rec.Code != http.StatusFound {
		t.Fatalf("被拒也要 302 回列表（PRG），实际 %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if strings.Contains(loc, "done=1") {
		t.Fatalf("无限行带数量不该成功，实际 %q", loc)
	}
	if errText := errTextOf(t, loc); !strings.Contains(errText, "不跟踪") {
		t.Fatalf("错误文案应是中文业务提示（不跟踪的行不允许带数量），实际 %q", errText)
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 0 {
		t.Fatalf("被拒后真源不该被写，实际 %d", got)
	}
	if stockTrack(t, f, v.ID, wh.ID) {
		t.Fatalf("被拒后不该切成跟踪")
	}

	// ④ 跟踪态留空数量：拒绝（留空不等于 0，0 必须自己打）。
	rec = postTracking(engine, f.projectID, wh.ID, v.ID, sku, true, "")
	loc = rec.Header().Get("Location")
	if strings.Contains(loc, "done=1") {
		t.Fatalf("跟踪态留空数量不该成功，实际 %q", loc)
	}
	if errText := errTextOf(t, loc); !strings.Contains(errText, "必须填写数量") {
		t.Fatalf("错误文案应是中文业务提示（跟踪时必须填数量），实际 %q", errText)
	}
}

// TestTrackQuantityI18nSeeded 迁移 263 的词条真的落库了（中英成对）。
//
// 为什么单独测它：页面上的中文提示在线上来自 sys_i18n 取词，模板兜底只在**取不到词条**
// 时生效 —— 只测页面文字的话，缺 seed 也会被兜底掩盖（中文站点看起来正常，英文站点退回中文）。
func TestTrackQuantityI18nSeeded(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	keys := []string{
		"ErrStockQuantityRequired",
		"ErrStockUntrackedQuantity",
		"admin.inventory.stock.unlimited",
		"admin.inventory.stock.notStocked",
		"admin.inventory.stock.track",
		"admin.inventory.stock.col.tracking",
		"admin.inventory.stock.qtyPh",
		"admin.inventory.stock.save",
	}
	for _, key := range keys {
		var n int
		if err := f.db.Raw("SELECT COUNT(*) FROM sys_i18n WHERE item_key = ?", key).Scan(&n).Error; err != nil {
			t.Fatalf("查词条 %s 失败: %v", key, err)
		}
		if n != 2 {
			t.Fatalf("词条 %s 应有中英各一行（迁移 263），实际 %d 行", key, n)
		}
	}
	var value string
	if err := f.db.Raw("SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = 'zh-CN'",
		"admin.inventory.stock.unlimited").Scan(&value).Error; err != nil {
		t.Fatalf("读中文词条失败: %v", err)
	}
	if strings.TrimSpace(value) == "" || strings.Contains(value, "admin.inventory") {
		t.Fatalf("中文词条应是一条真文案，实际 %q", value)
	}
}

package feature

import (
	"context"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/internal/middleware/adminctx"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventoryhttp "go_wp/internal/module/inventory/inbound/http"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/internal/templates"
	"go_wp/public/test/support"
)

func stockTrack(t *testing.T, f *invFixture, variantID, warehouseID string) bool {
	t.Helper()
	var track bool
	if err := f.db.Raw("SELECT track_quantity FROM inventory_stocks "+
		"WHERE variant_id = ? AND warehouse_id = ?", variantID, warehouseID).Scan(&track).Error; err != nil {
		t.Fatalf("读跟踪开关失败: %v", err)
	}
	return track
}

func trackRowCount(t *testing.T, f *invFixture, variantID, warehouseID string) int {
	t.Helper()
	var n int
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_stocks "+
		"WHERE variant_id = ? AND warehouse_id = ?", variantID, warehouseID).Scan(&n).Error; err != nil {
		t.Fatalf("统计库存行失败: %v", err)
	}
	return n
}

func stockSKUOf(t *testing.T, f *invFixture, variantID, warehouseID string) string {
	t.Helper()
	var code string
	if err := f.db.Raw("SELECT sku_code FROM inventory_stocks "+
		"WHERE variant_id = ? AND warehouse_id = ?", variantID, warehouseID).Scan(&code).Error; err != nil {
		t.Fatalf("读库存 sku_code 失败: %v", err)
	}
	return code
}

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

func TestInfiniteStockSkipsDeductAndOverSellCheck(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "无限商品")
	v := f.firstVariant(t, p.ID)

	if stockTrack(t, f, v.ID, wh.ID) {
		t.Fatalf("新建库存行应默认不跟踪（无限），实际 track_quantity = true")
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 0 {
		t.Fatalf("无限行的数量应为 0（CHECK 保证），实际 %d", got)
	}

	if err := changeOut(f, p, v, wh.ID, 3, "SO-INF-1"); err != nil {
		t.Fatalf("无限行的出库不该被拒（不校验可用量）：%v", err)
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 0 {
		t.Fatalf("无限行的出库不该扣减数量，实际 %d", got)
	}
	if stockTrack(t, f, v.ID, wh.ID) {
		t.Fatalf("出库不该把无限行切成跟踪")
	}

	if n := countMovements(t, f, v.ID); n != 0 {
		t.Fatalf("无限行的出库不产生数量变动，不该写流水，实际 %d 条", n)
	}

	if err := changeOut(f, p, v, wh.ID, 999, "SO-INF-2"); err != nil {
		t.Fatalf("无限行的大额出库同样不该被拒：%v", err)
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 0 {
		t.Fatalf("无限行的数量应保持 0，实际 %d", got)
	}

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

func TestTrackedStockKeepsExistingBehavior(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "跟踪商品")
	v := f.firstVariant(t, p.ID)

	changeIn(t, f, p, v, wh.ID, 5, "purchase_in")
	if !stockTrack(t, f, v.ID, wh.ID) {
		t.Fatalf("入库给了具体数量，该行应被切成跟踪")
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 5 {
		t.Fatalf("入库 5 后应为 5，实际 %d", got)
	}
	before := countMovements(t, f, v.ID)

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

	if err := changeOut(f, p, v, wh.ID, 5, "SO-ALL"); err != nil {
		t.Fatalf("卖光应成功：%v", err)
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 0 {
		t.Fatalf("卖光后数量应为 0，实际 %d", got)
	}
	if !stockTrack(t, f, v.ID, wh.ID) {
		t.Fatalf("卖光的行必须仍然跟踪（0 ≠ 无限）")
	}

	if err := changeOut(f, p, v, wh.ID, 1, "SO-AGAIN"); err == nil ||
		err.Error() != inventoryenums.ErrStockInsufficient {
		t.Fatalf("卖光的跟踪行再出库应报 ErrStockInsufficient，实际 %v", err)
	}
}

func TestQuantityWriteSwitchesInfiniteRowToTracked(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)

	pIn := mustProduct(t, f, "入库切跟踪")
	vIn := f.firstVariant(t, pIn.ID)
	changeIn(t, f, pIn, vIn, wh.ID, 4, "purchase_in")
	if !stockTrack(t, f, vIn.ID, wh.ID) || f.stockQty(t, vIn.ID, wh.ID) != 4 {
		t.Fatalf("入库应把无限行切成跟踪并写入 4，实际 track=%v qty=%d",
			stockTrack(t, f, vIn.ID, wh.ID), f.stockQty(t, vIn.ID, wh.ID))
	}

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

	if err := adjustTo(f, pAdj, vAdj, wh.ID, 9); err != nil {
		t.Fatalf("调整到 9 应成功：%v", err)
	}
	if !stockTrack(t, f, vAdj.ID, wh.ID) || f.stockQty(t, vAdj.ID, wh.ID) != 9 {
		t.Fatalf("调整到 9 后应为跟踪且数量 9，实际 track=%v qty=%d",
			stockTrack(t, f, vAdj.ID, wh.ID), f.stockQty(t, vAdj.ID, wh.ID))
	}
}

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

	changeIn(t, f, p, v, wh.ID, 5, "purchase_in")
	if err := f.db.Exec("UPDATE inventory_stocks SET quantity = 6 WHERE variant_id = ? AND warehouse_id = ?",
		v.ID, wh.ID).Error; err != nil {
		t.Fatalf("跟踪行写数量不该被 CHECK 拒绝：%v", err)
	}
}

func splitMigrationSQL(t *testing.T, src string) []string {
	t.Helper()
	var stmts []string
	var buf strings.Builder
	inDollar := false
	runes := []rune(src)
	for i := 0; i < len(runes); i++ {
		c := runes[i]

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

	var tracked int
	if err := db.Raw(`SELECT COUNT(*) FROM inventory_stocks WHERE track_quantity`).Scan(&tracked).Error; err != nil {
		t.Fatalf("统计 track_quantity 失败: %v", err)
	}
	if tracked != 2 {
		t.Fatalf("存量行应一律 track_quantity = true，实际 %d/2", tracked)
	}

	if err := db.Exec(`UPDATE inventory_stocks SET quantity = 0, track_quantity = false WHERE id = 's1'`).Error; err != nil {
		t.Fatalf("把 s1 改成无限失败: %v", err)
	}
	if err := db.Exec(`UPDATE inventory_stocks SET quantity = 7 WHERE id = 's1'`).Error; err == nil {
		t.Fatalf("CHECK (track_quantity OR quantity = 0) 必须拒绝「不跟踪但有数字」")
	}

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

	insert("a1", "SZ_DRAWERSMOKE_001", 3, "p-a", "v-a")
	insert("b1", "DRAWERSMOKE_001", 7, "p-b", "v-b")

	err = runMigrationFile(t, db, migrationPath)
	if err == nil {
		t.Fatalf("剥前缀后同仓同码撞车时迁移必须显式失败（不合并、不改码）")
	}
	msg := err.Error()

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

func TestWarehouseStocksByProductsAcrossWarehouses(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	sz := f.createWarehouse(t, "SZ", "苏州仓", true)
	sh := f.createWarehouse(t, "SH", "上海仓", false)

	p1 := mustProductPriced(t, f, "分仓聚合一", 9.9)
	v1 := f.firstVariant(t, p1.ID)
	changeIn(t, f, p1, v1, sh.ID, 5, "purchase_in")

	p2 := mustProductPriced(t, f, "分仓聚合二", 19.9)

	rows, err := f.inventory.WarehouseStocksByProducts(ctx, f.projectID, []string{p1.ID, p2.ID})
	if err != nil {
		t.Fatalf("WarehouseStocksByProducts 失败: %v", err)
	}

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

	if r := byKey[key{p1.ID, sh.ID}]; r.WarehouseCode != "SH" || r.WarehouseName != "上海仓" {
		t.Fatalf("仓码 / 仓名应随行给出，实际 %+v", r)
	}

	empty, err := f.inventory.WarehouseStocksByProducts(ctx, f.projectID, nil)
	if err != nil {
		t.Fatalf("空入参不该报错：%v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("空入参应返回空结果，实际 %d 行", len(empty))
	}
}

func newTrackQuantityPageEngine(t *testing.T) (*gin.Engine, *invFixture) {
	t.Helper()
	f := newInvFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(templateRoot(), true)
	// 生产装配在基础层挂失败出口与请求取词；缺了它们片段渲染为空。
	engine.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	engine.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	engine.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.Use(func(c *gin.Context) {

		c.Set(adminctx.PermSetKey, map[string]bool{"inventory:stock_change": true})
		c.Set(adminctx.ButtonsKey, map[string]bool{"inventory.stock_change": true})
	})
	handle := inventoryhttp.NewInventoryPageHandle(f.inventory, f.projects, f.products)
	engine.GET("/admin/inventory", pageview.Slot(inventoryhttp.InventoryListPageTemplate, pageview.RenderPage), handle.InventoryPage)
	engine.POST("/admin/inventory/stock/tracking", pageview.HTMLFragment, handle.InventoryStockTrackingUpdate)
	return engine, f
}

func TestInventoryPageRendersThreeQuantityStates(t *testing.T) {
	engine, f := newTrackQuantityPageEngine(t)
	if engine == nil {
		return
	}
	sz := f.createWarehouse(t, "SZ", "苏州仓", true)
	sh := f.createWarehouse(t, "SH", "上海仓", false)

	f.createWarehouse(t, "NJ", "南京仓", false)

	p := mustProduct(t, f, "三态商品")
	v := f.firstVariant(t, p.ID)

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

	if !strings.Contains(body, "∞ 无限") {
		t.Fatalf("无限行应渲染成 ∞ 无限（不能显示 0 —— 那看起来像没货）")
	}
	if !strings.Contains(body, "未入库") {
		t.Fatalf("没有库存行的仓应渲染成未入库（三态之一）")
	}
	if !strings.Contains(body, "<td>5</td>") {
		t.Fatalf("跟踪行的数量列应渲染数字 5")
	}

	if n := strings.Count(body, `action="/admin/inventory/stock/tracking?`); n != 2 {
		t.Fatalf("应只有 2 个可编辑的行（SZ / SH），实际 %d 个跟踪表单", n)
	}

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
	path := "/admin/inventory/stock/tracking?project=" + url.QueryEscape(projectID) + "&sku=" + url.QueryEscape(skuCode)
	return postForm(engine, path, form)
}

func TestInventoryPageTrackingToggle(t *testing.T) {
	engine, f := newTrackQuantityPageEngine(t)
	if engine == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	p := mustProduct(t, f, "行内编辑商品")
	v := f.firstVariant(t, p.ID)
	sku := stockSKUOf(t, f, v.ID, wh.ID)

	assertInventoryJump(t, postTracking(engine, f.projectID, wh.ID, v.ID, sku, true, "7"), "ok")
	if !stockTrack(t, f, v.ID, wh.ID) {
		t.Fatalf("提交跟踪 + 数量后该行应变跟踪")
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 7 {
		t.Fatalf("提交数量 7 后真源应为 7，实际 %d", got)
	}
	if n := countMovements(t, f, v.ID); n != 1 {
		t.Fatalf("行内编辑写数量必须留一条流水（有变动必有流水），实际 %d 条", n)
	}

	assertInventoryJump(t, postTracking(engine, f.projectID, wh.ID, v.ID, sku, false, ""), "ok")
	if stockTrack(t, f, v.ID, wh.ID) {
		t.Fatalf("置为无限后 track_quantity 应为 false")
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 0 {
		t.Fatalf("无限行的数量必须被清成 0，实际 %d", got)
	}
	if n := countMovements(t, f, v.ID); n != 2 {
		t.Fatalf("清零也是一次变动，应再写一条流水，实际共 %d 条", n)
	}

	rec := postTracking(engine, f.projectID, wh.ID, v.ID, sku, false, "5")
	assertInventoryJump(t, rec, "err", "不跟踪")
	if got := f.stockQty(t, v.ID, wh.ID); got != 0 {
		t.Fatalf("被拒后真源不该被写，实际 %d", got)
	}
	if stockTrack(t, f, v.ID, wh.ID) {
		t.Fatalf("被拒后不该切成跟踪")
	}

	assertInventoryJump(t, postTracking(engine, f.projectID, wh.ID, v.ID, sku, true, ""), "err", "必须填写数量")
}

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

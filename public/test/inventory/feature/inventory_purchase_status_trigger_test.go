package feature

// inventory_purchase_status_trigger_test.go — 采购单状态由数据库兜底（审计 DB-008）。
//
// 审计原文：采购单 status 是应用维护的冗余列，可用生成列消除不一致风险。
// 核对后**前半句属实、后半句不成立**：status 不取决于本行的其它列，而取决于**明细行**
// （空行 → pending / 全部收满 → received / 其余 → partial，见 service 的 derivePurchaseStatus），
// PostgreSQL 的生成列只能引用同一行的列，表达不了这种跨行推导 —— 详见迁移
// 196_inventory_purchase_status_sync.sql 头部的推导。落地用的是触发器。
//
// 本文件钉住三件事（全部通过**绕过 service 的直接 SQL 写入**来验证：
// 只有绕过应用层，才能证明兜底真的在数据库里）：
//
//	1. 明细行变化后单头状态被重算（收满 / 新增一行 / 删行）；
//	2. 迁移的回填能修正历史不一致数据；
//	3. 迁移整份可重复执行（幂等），且不改变已经正确的状态。
//
// 迁移不由本测试负责登记（register.go 由父代理统一追加），所以这里直接按语句执行
// 迁移文件：既不依赖登记状态，又能顺带覆盖「同一条迁移跑第二遍」的幂等性。

import (
	"os"
	"strings"
	"testing"

	inventorydto "go_wp/internal/module/inventory/dto"

	"go_wp/public/migrations"
)

// statusSyncMigrationPath 迁移文件相对本包目录的路径（public/test/inventory/feature → public/migrations）。
const statusSyncMigrationPath = "../../../migrations/196_inventory_purchase_status_sync.sql"

// execStatusSyncMigration 逐语句执行 196 迁移（SplitStatements 与迁移器同一份实现：
// 它认得 $$ 美元引用块，触发器函数体里的分号不会被当语句边界切开）。
func execStatusSyncMigration(t *testing.T, f *invFixture) {
	t.Helper()
	raw, err := os.ReadFile(statusSyncMigrationPath)
	if err != nil {
		t.Fatalf("读取迁移文件失败: %v", err)
	}
	// 196 写于 205（DB-019 时间列改名）**之前**，它的两处回填 UPDATE 与 sync 函数体
	// 都写的是 updated_at。本包的表由生产迁移建出，列名已是 update_time ——
	// 所以要重放这份历史 SQL，得先按当前列名做一次等价替换。
	// 生产里不存在这层替换：196 在迁移序列中总是早于 205 执行。
	// 用原始文本跑的话，第一条回填就会报 column "updated_at" does not exist。
	sqlText := strings.ReplaceAll(string(raw), "updated_at", "update_time")
	stmts := migrations.SplitStatements(sqlText)
	if len(stmts) == 0 {
		t.Fatalf("迁移文件解析出 0 条语句，路径或分割逻辑有问题")
	}
	for i, stmt := range stmts {
		if err := f.db.Exec(stmt).Error; err != nil {
			t.Fatalf("执行第 %d 条迁移语句失败: %v；语句: %s", i+1, err, stmt)
		}
	}
}

// TestPurchaseStatusTriggerFollowsLineWrites 明细行的增删改之后，单头状态由数据库重算。
func TestPurchaseStatusTriggerFollowsLineWrites(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	execStatusSyncMigration(t, f)

	wh := f.createWarehouse(t, "TRG", "触发器测试仓", true)
	src := mustPurchaseSource(t, f, "TRG_SUP", "触发器测试货源", "external")
	p := mustProductPriced(t, f, "触发器商品", 199)
	v := f.firstVariant(t, p.ID)
	order := mustPurchaseOrder(t, f, "TRG-001", src.ID, wh.ID,
		[]inventorydto.PurchaseLineReq{purchaseLine(p, v, 10, 5)})

	// 建单后（服务层写回 pending）——这是基线，触发器不该把它改坏。
	if got := purchaseStatusIn(t, f, order.ID); got != "pending" {
		t.Fatalf("建单后状态应为 pending，实得 %s", got)
	}

	// ① 绕过 service 直接把明细收满 → 数据库应把单头推成 received。
	if err := f.db.Exec("UPDATE inventory_purchase_order_lines SET received_quantity = quantity WHERE order_id = ?",
		order.ID).Error; err != nil {
		t.Fatalf("直接写明细行失败: %v", err)
	}
	if got := purchaseStatusIn(t, f, order.ID); got != "received" {
		t.Errorf("明细全部收满后数据库应把状态推成 received，实得 %s", got)
	}

	// ② 手工把单头写歪（模拟应用层写错），再动一次明细 → 触发器把状态拉回一致。
	if err := f.db.Exec("UPDATE inventory_purchase_orders SET status = 'partial' WHERE id = ?",
		order.ID).Error; err != nil {
		t.Fatalf("伪造不一致状态失败: %v", err)
	}
	if err := f.db.Exec("UPDATE inventory_purchase_order_lines SET received_quantity = 4 WHERE order_id = ?",
		order.ID).Error; err != nil {
		t.Fatalf("直接改明细行失败: %v", err)
	}
	if got := purchaseStatusIn(t, f, order.ID); got != "partial" {
		t.Errorf("4/10 应为 partial，实得 %s", got)
	}

	// ③ 绕过 service 直接插一行未收的明细 → 从「可能收满」跌回 partial。
	//    迁移 134 起明细表带 fk_inventory_purchase_lines_product/_variant 真外键，
	//    且 (order_id, variant_id) 上有 uq_inventory_purchase_lines_order_variant 唯一约束：
	//    附加行必须挂「另一个真实变体」——随机 uuid 过不了外键，复用本单变体过不了唯一约束。
	extraProduct := mustProductPriced(t, f, "触发器商品-附加行", 299)
	extraVariant := f.firstVariant(t, extraProduct.ID)
	if err := f.db.Exec(
		"INSERT INTO inventory_purchase_order_lines "+
			"(id, order_id, project_id, product_id, variant_id, sku_code, quantity, received_quantity, unit_price, sort, remark, metadata, create_time, update_time) "+
			"VALUES (gen_random_uuid(), ?, ?, ?, ?, 'TRG-EXTRA', 1, 0, 1, 9, '', '{}'::jsonb, now(), now())",
		order.ID, f.projectID, extraProduct.ID, extraVariant.ID).Error; err != nil {
		t.Fatalf("直接插入明细行失败: %v", err)
	}
	if got := purchaseStatusIn(t, f, order.ID); got != "partial" {
		t.Errorf("新增一行未收明细后应为 partial，实得 %s", got)
	}

	// ④ 再把这行删掉（连同它之前把已有行收满）→ 回到 received。
	if err := f.db.Exec("DELETE FROM inventory_purchase_order_lines WHERE order_id = ? AND sku_code = 'TRG-EXTRA'",
		order.ID).Error; err != nil {
		t.Fatalf("直接删除明细行失败: %v", err)
	}
	if err := f.db.Exec("UPDATE inventory_purchase_order_lines SET received_quantity = quantity WHERE order_id = ?",
		order.ID).Error; err != nil {
		t.Fatalf("直接改写明细行失败: %v", err)
	}
	if got := purchaseStatusIn(t, f, order.ID); got != "received" {
		t.Errorf("删掉未收行并收满后应为 received，实得 %s", got)
	}

	// ⑤ 直接改写单头 status（明细行一行不动，上面那个 AFTER 触发器不会醒）→
	//    写入守卫在落库前把它换成推导值。
	if err := f.db.Exec("UPDATE inventory_purchase_orders SET status = 'pending' WHERE id = ?",
		order.ID).Error; err != nil {
		t.Fatalf("直接改写单头状态失败: %v", err)
	}
	if got := purchaseStatusIn(t, f, order.ID); got != "received" {
		t.Errorf("直改单头状态应被守卫换回推导值 received，实得 %s", got)
	}
}

// TestPurchaseStatusMigrationBackfillsAndIsIdempotent 迁移回填修正历史不一致，
// 且整份迁移可重复执行（迁移器会多次跑同一份文件：每个环境的启动路径都会经过它）。
func TestPurchaseStatusMigrationBackfillsAndIsIdempotent(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	execStatusSyncMigration(t, f)

	wh := f.createWarehouse(t, "BF", "回填测试仓", true)
	src := mustPurchaseSource(t, f, "BF_SUP", "回填测试货源", "external")
	p := mustProductPriced(t, f, "回填商品", 88)
	v := f.firstVariant(t, p.ID)

	// 两张单：一张明细未收（真值 pending），一张明细收满（真值 received）。
	pendingOrder := mustPurchaseOrder(t, f, "BF-PENDING", src.ID, wh.ID,
		[]inventorydto.PurchaseLineReq{purchaseLine(p, v, 3, 2)})
	receivedOrder := mustPurchaseOrder(t, f, "BF-RECEIVED", src.ID, wh.ID,
		[]inventorydto.PurchaseLineReq{purchaseLine(p, v, 3, 2)})
	if err := f.db.Exec("UPDATE inventory_purchase_order_lines SET received_quantity = quantity WHERE order_id = ?",
		receivedOrder.ID).Error; err != nil {
		t.Fatalf("收满明细失败: %v", err)
	}
	if got := purchaseStatusIn(t, f, receivedOrder.ID); got != "received" {
		t.Fatalf("前置：收满后应为 received，实得 %s", got)
	}

	// 制造历史脏数据：这是迁移 196 落地**之前**才可能的局面 —— 那时没有守卫触发器，
	// 直改单头的状态能落库。所以先摘掉守卫，再写脏值（顺带验证「对象缺失时整份迁移能重建」）。
	if err := f.db.Exec(
		"DROP TRIGGER IF EXISTS trg_inventory_purchase_orders_status_guard ON inventory_purchase_orders").Error; err != nil {
		t.Fatalf("摘除守卫失败: %v", err)
	}
	if err := f.db.Exec("UPDATE inventory_purchase_orders SET status = 'received' WHERE id IN (?, ?)",
		pendingOrder.ID, receivedOrder.ID).Error; err != nil {
		t.Fatalf("伪造脏数据失败: %v", err)
	}

	// 整份迁移再跑一遍：守卫与 AFTER 触发器重建 + 回填把两张单拉回各自明细行对应的真值。
	execStatusSyncMigration(t, f)
	if got := purchaseStatusIn(t, f, pendingOrder.ID); got != "pending" {
		t.Errorf("回填后未收明细的单应为 pending，实得 %s", got)
	}
	if got := purchaseStatusIn(t, f, receivedOrder.ID); got != "received" {
		t.Errorf("回填后收满明细的单应为 received，实得 %s", got)
	}

	// 幂等：第三遍执行不报错，也不改变已经正确的状态（含 update_time 不被反复推动）。
	var before string
	if err := f.db.Raw("SELECT status || '|' || update_time::text FROM inventory_purchase_orders WHERE id = ?",
		receivedOrder.ID).Scan(&before).Error; err != nil {
		t.Fatalf("读取回填后状态失败: %v", err)
	}
	execStatusSyncMigration(t, f)
	var after string
	if err := f.db.Raw("SELECT status || '|' || update_time::text FROM inventory_purchase_orders WHERE id = ?",
		receivedOrder.ID).Scan(&after).Error; err != nil {
		t.Fatalf("读取二次回填后状态失败: %v", err)
	}
	if before != after {
		t.Errorf("重复执行迁移不应改动已经正确的行：%s → %s", before, after)
	}
}

// TestPurchaseStatusTriggerKeepsServicePathIntact 触发器与应用层写回并存时结果一致：
// 走 service 的收货路径（见 inventory_purchase_test.go 的验收 2）结束后的状态，
// 必须与绕过 service 直接驱动明细行得到的数据库推导值一致。
func TestPurchaseStatusTriggerKeepsServicePathIntact(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	execStatusSyncMigration(t, f)

	wh := f.createWarehouse(t, "SVC", "服务路径测试仓", true)
	src := mustPurchaseSource(t, f, "SVC_SUP", "服务路径测试货源", "external")
	p := mustProductPriced(t, f, "服务路径商品", 66)
	v := f.firstVariant(t, p.ID)
	order := mustPurchaseOrder(t, f, "SVC-001", src.ID, wh.ID,
		[]inventorydto.PurchaseLineReq{purchaseLine(p, v, 8, 3)})

	lineID := order.Lines[0].ID
	mustReceiveLine(t, f, order.ID, lineID, 3, "trg-svc-1")
	if got := purchaseStatusIn(t, f, order.ID); got != "partial" {
		t.Fatalf("收 3/8 后应为 partial，实得 %s", got)
	}
	mustReceiveLine(t, f, order.ID, lineID, 5, "trg-svc-2")
	if got := purchaseStatusIn(t, f, order.ID); got != "received" {
		t.Fatalf("收满后应为 received，实得 %s", got)
	}

	// 数据库侧的独立推导（触发器用的那条 SQL）必须与服务层写回的结论一致。
	var derived string
	if err := f.db.Raw(
		"SELECT CASE "+
			"WHEN COUNT(*) = 0 THEN 'pending' "+
			"WHEN COUNT(*) FILTER (WHERE received_quantity < quantity) = 0 THEN 'received' "+
			"WHEN COUNT(*) FILTER (WHERE received_quantity > 0) > 0 THEN 'partial' "+
			"ELSE 'pending' END "+
			"FROM inventory_purchase_order_lines WHERE order_id = ?", order.ID).Scan(&derived).Error; err != nil {
		t.Fatalf("独立推导状态失败: %v", err)
	}
	if got := purchaseStatusIn(t, f, order.ID); got != derived {
		t.Errorf("服务层写回的状态与数据库推导不一致：单头 %s / 推导 %s", got, derived)
	}
}

// statusSyncRegistryConditionSQL 迁移注册判据：按**本批自己的四个对象名**枚举计数
// （不用总量 —— 总量会被别的批次的同类对象满足而静默跳过本批的迁移）。
// 里面没有 ? 占位符，迁移器因此不会往它传表名（见 migrator.go 的 apply）。
// register.go 里追加的那一块用的就是这一条，本测试负责让它不会是错的。
const statusSyncRegistryConditionSQL = "SELECT CASE WHEN COUNT(*) = 4 THEN 1 ELSE 0 END FROM (" +
	"SELECT p.proname AS name FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace " +
	"WHERE p.proname IN ('fn_inventory_purchase_order_status_sync', 'fn_inventory_purchase_order_status_guard') " +
	"AND n.nspname = current_schema() " +
	"UNION ALL " +
	"SELECT t.tgname FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid " +
	"JOIN pg_namespace n2 ON n2.oid = c.relnamespace " +
	"WHERE t.tgname IN ('trg_inventory_purchase_lines_status_sync', 'trg_inventory_purchase_orders_status_guard') " +
	"AND n2.nspname = current_schema() " +
	"AND NOT t.tgisinternal" +
	") AS batch_196"

// TestPurchaseStatusTriggerRegistryCondition 注册判据与触发器属性：
// 判据必须恰好认得本批的 4 个对象（少一个就返回 0，注册后整份迁移会被静默跳过）；
// 触发器必须是明细行表上的**行级 AFTER** 触发器，且 INSERT/UPDATE/DELETE 都在事件里。
//
// 判据的期望值是 1 而不是 0：196 已由 register.go 登记（本批父代理统一追加，见
// register_admin_i18n.go 的 "196-inventory-purchase-status-sync"），newInvFixture 的全量迁移
// 会先执行它 —— 进到本测试时 4 个对象已经存在，这是**有意改变**的语义（此前本测试自己按语句
// 执行迁移、不走登记）。它仍然是对判据的严格验证：对象名与注册块里写的不一致（少建一个、
// 名字拼错、被 DROP 掉）就会落回 0，测试随之变红。
func TestPurchaseStatusTriggerRegistryCondition(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}

	var before int64
	if err := f.db.Raw(statusSyncRegistryConditionSQL).Scan(&before).Error; err != nil {
		t.Fatalf("判据 SQL 执行失败: %v", err)
	}
	if before != 1 {
		t.Errorf("196 已登记并随全量迁移执行，四个对象应齐备（判据 1），实得 %d（对象名与注册块不一致，或 196 未登记）", before)
	}

	execStatusSyncMigration(t, f)

	var after int64
	if err := f.db.Raw(statusSyncRegistryConditionSQL).Scan(&after).Error; err != nil {
		t.Fatalf("判据 SQL 执行失败: %v", err)
	}
	if after != 1 {
		t.Errorf("196 执行后判据应为 1，实得 %d（函数或触发器没建全，或注册块里的名字与本文件不一致）", after)
	}

	// tgtype 位：1=ROW / 2=BEFORE / 4=INSERT / 8=DELETE / 16=UPDATE。
	// 时机错了（例如把重算写成 BEFORE、或写成语句级）都不会在状态漂移时把单头拉回来。
	var tgtype int64
	if err := f.db.Raw("SELECT t.tgtype::bigint FROM pg_trigger t " +
		"JOIN pg_class c ON c.oid = t.tgrelid " +
		"WHERE t.tgname = 'trg_inventory_purchase_lines_status_sync' AND c.relname = 'inventory_purchase_order_lines'").
		Scan(&tgtype).Error; err != nil {
		t.Fatalf("读触发器属性失败: %v", err)
	}
	if tgtype&1 == 0 || tgtype&2 != 0 || tgtype&4 == 0 || tgtype&8 == 0 || tgtype&16 == 0 {
		t.Errorf("明细行触发器应为行级 AFTER INSERT/UPDATE/DELETE，tgtype=%d", tgtype)
	}

	// 守卫必须是单头表上的行级 BEFORE UPDATE —— 它靠改 NEW.status 生效，
	// 落成 AFTER 就永远改不动要写的值（那时只能再发一条 UPDATE，锁与语义都变了）。
	var guardType int64
	if err := f.db.Raw("SELECT t.tgtype::bigint FROM pg_trigger t " +
		"JOIN pg_class c ON c.oid = t.tgrelid " +
		"WHERE t.tgname = 'trg_inventory_purchase_orders_status_guard' AND c.relname = 'inventory_purchase_orders'").
		Scan(&guardType).Error; err != nil {
		t.Fatalf("读守卫触发器属性失败: %v", err)
	}
	if guardType&1 == 0 || guardType&2 == 0 || guardType&16 == 0 {
		t.Errorf("守卫应为行级 BEFORE UPDATE，tgtype=%d", guardType)
	}
}

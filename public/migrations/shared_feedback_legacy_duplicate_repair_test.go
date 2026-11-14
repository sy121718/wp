package migrations_test

import (
	"strings"
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"

	"gorm.io/gorm"
)

// artex（真实开发库）上的形态是：退役 key 与已存在的新 key 都在，且文案不同 ——
// 早期短键迁移复制出一行，之后 407 又用另一套文案重新播种了退役 key。
// 747 的冲突守卫按设计整批拒绝，这是正确行为；746a 只修文案不同、恰好一边为规范值，
// 且待删的非规范副本没有编辑痕迹的形态，其余交给 747 保持严格检查。
//
// 这里的反例按 artex 的**真实行**（文案、category、remark）构造，不是简化夹具。

// apply747 单独执行 747（不经过 746a）：用来证明「未修复时 747 必须拒绝」这个前提。
func apply747(t *testing.T, db *gorm.DB) error {
	t.Helper()
	for _, m := range migrations.All() {
		if m.Version == "747-shared-feedback-short-keys-i18n" {
			return migrations.ApplyStatements(db, m.Version, "sys_i18n", m.CheckSQL,
				migrations.SplitStatements(m.SQL))
		}
	}
	t.Fatal("747 未登记")
	return nil
}

// seedPair 建出「退役 key + 新 key 同语言并存」这一形态。
// 测试库只跑迁移不跑种子，因此新 key 行需要显式创建。
func seedLegacyAndNewRow(t *testing.T, db *gorm.DB, oldValue, newValue string) {
	t.Helper()
	mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key IN ('MsgInternalError','uiInternalErr')")
	mustExec(t, db, `INSERT INTO sys_i18n (item_key,lang,item_value,http_code,category,remark,status)
VALUES ('MsgInternalError','en-US',?,'200','ui','legacy',1),('uiInternalErr','en-US',?,'200','block','current',1)`,
		oldValue, newValue)
}

func repairMigration(t *testing.T) migrations.Migration {
	t.Helper()
	for _, m := range migrations.All() {
		if m.Version == "746a-shared-feedback-legacy-duplicate-repair" {
			return m
		}
	}
	t.Fatal("746a 重复行修复迁移未登记")
	return migrations.Migration{}
}

// TestSharedFeedbackDuplicateRepairSettlesArtexShape 复现 artex 的真实冲突并证明修复后 747 能通过。
//
// 关键断言不是「删了一行」，而是：修复后 `migrations.Run` 整条链不再报冲突，
// 且终态里退役 key 一行不剩、新 key 保留规范文案。
func TestSharedFeedbackDuplicateRepairSettlesArtexShape(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)

	// 与 artex 同形：退役 key 是规范文案，新 key 是陈旧副本。
	seedLegacyAndNewRow(t, db, "Internal error, please try again later",
		"Internal server error, please try again later")
	// 种子可能省略时间列（NULL）；「无编辑痕迹」判据必须对 NULL 成立。
	mustExec(t, db, "UPDATE sys_i18n SET create_time = NULL, update_time = NULL WHERE item_key IN ('MsgInternalError','uiInternalErr')")

	// 前提：**单独执行 747** 时这个形态必须整批拒绝。
	if err := apply747(t, db); err == nil {
		t.Fatal("前提不成立：这个形态本该被 747 判为冲突")
	} else if !strings.Contains(err.Error(), "MsgInternalError -> uiInternalErr/en-US") {
		t.Fatalf("冲突必须定位 old -> new/lang，实际：%v", err)
	}

	// 修复迁移本身：只删陈旧副本。
	if err := migrations.ApplyStatements(db,
		repairMigration(t).Version, "sys_i18n", repairMigration(t).CheckSQL,
		migrations.SplitStatements(repairMigration(t).SQL)); err != nil {
		t.Fatalf("746a 执行失败：%v", err)
	}

	var rows []struct {
		ItemKey   string
		Lang      string
		ItemValue string
	}
	if err := db.Raw(`SELECT item_key, lang, item_value FROM sys_i18n
WHERE item_key IN ('MsgInternalError', 'uiInternalErr') ORDER BY item_key, lang`).Scan(&rows).Error; err != nil {
		t.Fatalf("读取终态失败：%v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("修复后应只剩一行规范新 key，实际：%+v", rows)
	}
	// 陈旧副本被删，退役行（本来就是规范文案）保留，成为 747 的搬迁源。
	if rows[0].ItemKey != "MsgInternalError" || rows[0].ItemValue != "Internal error, please try again later" {
		t.Fatalf("应保留规范文案的退役行作为搬迁源，实际：%+v", rows[0])
	}

	// 修复后整条迁移链必须能跑通，并且终态只剩规范新 key。
	if err := migrations.Run(db); err != nil {
		t.Fatalf("修复后整条迁移链仍失败：%v", err)
	}
	assertNoRetiredSharedFeedbackKeys(t, db)
	var final string
	if err := db.Raw(`SELECT item_value FROM sys_i18n WHERE item_key='uiInternalErr' AND lang='en-US'`).
		Scan(&final).Error; err != nil {
		t.Fatalf("读取终态新 key 失败：%v", err)
	}
	if final != "Internal error, please try again later" {
		t.Fatalf("终态文案应为规范值，实际 %q", final)
	}
}

// TestSharedFeedbackDuplicateRepairKeepsCanonicalNewRow 反方向：新 key 已是规范值、
// 退役行是陈旧文案时，只删退役行，不动已在位的新行（连同它自己的 id 与时间）。
func TestSharedFeedbackDuplicateRepairKeepsCanonicalNewRow(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	seedLegacyAndNewRow(t, db, "Stale legacy text", "Internal error, please try again later")
	mustExec(t, db, "UPDATE sys_i18n SET create_time = NULL, update_time = NULL WHERE item_key IN ('MsgInternalError','uiInternalErr')")

	var beforeID uint
	if err := db.Raw(`SELECT id FROM sys_i18n WHERE item_key='uiInternalErr' AND lang='en-US'`).Scan(&beforeID).Error; err != nil {
		t.Fatalf("读取修复前 id 失败：%v", err)
	}

	m := repairMigration(t)
	if err := migrations.ApplyStatements(db, m.Version, "sys_i18n", m.CheckSQL,
		migrations.SplitStatements(m.SQL)); err != nil {
		t.Fatalf("746a 执行失败：%v", err)
	}

	var afterID uint
	if err := db.Raw(`SELECT id FROM sys_i18n WHERE item_key='uiInternalErr' AND lang='en-US'`).Scan(&afterID).Error; err != nil {
		t.Fatalf("读取修复后 id 失败：%v", err)
	}
	if afterID != beforeID {
		t.Fatalf("规范化新行不应被替换：before=%d after=%d", beforeID, afterID)
	}
	var retired int64
	db.Raw(`SELECT COUNT(*) FROM sys_i18n WHERE item_key='MsgInternalError'`).Scan(&retired)
	if retired != 0 {
		t.Fatalf("退役行应被删除，实际仍有 %d 行", retired)
	}
}

// TestSharedFeedbackDuplicateRepairRefusesOperatorEdit 两边都不是规范值时，
// 746a 保持原记录，747 必须报可定位冲突并回滚，绝不替运营选一条。
func TestSharedFeedbackDuplicateRepairRefusesOperatorEdit(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	seedLegacyAndNewRow(t, db, "运营改写的文案 B", "运营改写的文案 A")
	mustExec(t, db, "UPDATE sys_i18n SET create_time = NULL, update_time = NULL WHERE item_key IN ('MsgInternalError','uiInternalErr')")

	m := repairMigration(t)
	if err := migrations.ApplyStatements(db, m.Version, "sys_i18n", m.CheckSQL,
		migrations.SplitStatements(m.SQL)); err != nil {
		t.Fatalf("746a 本身不应报错（它只是不动这两行）：%v", err)
	}
	if err := apply747(t, db); err == nil {
		t.Fatal("两边都不是规范值时必须拒绝，不能静默二选一")
	} else if !strings.Contains(err.Error(), "MsgInternalError -> uiInternalErr/en-US") {
		t.Fatalf("冲突错误必须定位 old -> new/lang，实际：%v", err)
	}
	var n int64
	db.Raw(`SELECT COUNT(*) FROM sys_i18n WHERE item_key IN ('MsgInternalError','uiInternalErr') AND lang='en-US'`).Scan(&n)
	if n != 2 {
		t.Fatalf("拒绝后不得留下部分变更，实际剩 %d 行", n)
	}
}

// TestSharedFeedbackDuplicateRepairSkipsWhenNoDuplicate 没有重复行时不执行（幂等）。
func TestSharedFeedbackDuplicateRepairSkipsWhenNoDuplicate(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	m := repairMigration(t)

	var count int64
	if err := db.Raw(m.CheckSQL).Scan(&count).Error; err != nil {
		t.Fatalf("求值 746a 守门失败：%v", err)
	}
	if count != 1 {
		t.Fatalf("没有重复行时守门应为 1（跳过），实际 %d", count)
	}
	before := sharedFeedbackSnapshot(t, db)
	if err := migrations.ApplyStatements(db, m.Version, "sys_i18n", m.CheckSQL,
		migrations.SplitStatements(m.SQL)); err != nil {
		t.Fatalf("无重复行时执行失败：%v", err)
	}
	if after := sharedFeedbackSnapshot(t, db); after != before {
		t.Fatal("没有重复行时不得改动任何词条")
	}
}

// 陈旧副本若带编辑痕迹（update_time 有值且不等于 create_time），就不能当作
// 可丢弃的机器播种行删除：它可能是运营手动改过的。
//
// 这里让**新行**（陈旧文案那一行）带上编辑痕迹，退役行保持规范值：
// 若只看文案就会删新行，恰好吃掉运营改动。
func TestSharedFeedbackDuplicateRepairRefusesEditedRows(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	seedLegacyAndNewRow(t, db, "Internal error, please try again later",
		"Internal server error, please try again later")
	// 新行被 admin 改过：update_time 有值、且与 create_time 不同。
	mustExec(t, db, "UPDATE sys_i18n SET create_time = now(), update_time = now() + interval '1 minute' WHERE item_key='uiInternalErr'")

	m := repairMigration(t)
	if err := migrations.ApplyStatements(db, m.Version, "sys_i18n", m.CheckSQL,
		migrations.SplitStatements(m.SQL)); err != nil {
		t.Fatalf("746a 本身不应报错（它只是不动这一行）：%v", err)
	}
	var n int64
	db.Raw(`SELECT COUNT(*) FROM sys_i18n WHERE item_key IN ('MsgInternalError','uiInternalErr') AND lang='en-US'`).Scan(&n)
	if n != 2 {
		t.Fatalf("带编辑痕迹的行不得被当作陈旧副本删除，实际剩 %d 行", n)
	}
	// 至于整链升不升级得上去，取决于 747 对「值不同」的严格语义 —— 那是 747 的事，
	// 本用例只证明 746a 没有越权删除。
}

// TestSharedFeedbackFullChainSettlesArtexShape 端到端：整条 migrations.Run
// （含 746a + 747）在 artex 形态上必须通过，且终态只剩规范新 key。
//
// 前面几条只验 746a 本身；这条验「接回主链后确实不再阻塞升级」——
// 否则修复迁移可能自己是对的，却因为排序或守门没接上而白写。
func TestSharedFeedbackFullChainSettlesArtexShape(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	seedLegacyAndNewRow(t, db, "Internal error, please try again later",
		"Internal server error, please try again later")
	mustExec(t, db, `INSERT INTO sys_i18n (item_key,lang,item_value,http_code,category,remark,status)
VALUES ('uiInternalErr','zh-CN','系统内部错误，请稍后重试',200,'ui','zh',1)`)
	mustExec(t, db, "UPDATE sys_i18n SET create_time = NULL, update_time = NULL")

	if err := migrations.Run(db); err != nil {
		t.Fatalf("整条迁移链必须能升级 artex 形态，实际失败：%v", err)
	}
	assertNoRetiredSharedFeedbackKeys(t, db)

	var rows []struct{ ItemKey, Lang, ItemValue string }
	if err := db.Raw(`SELECT item_key, lang, item_value FROM sys_i18n
WHERE item_key = 'uiInternalErr' ORDER BY lang`).Scan(&rows).Error; err != nil {
		t.Fatalf("读取终态失败：%v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("终态应只有 uiInternalErr 的中英两行，实际：%+v", rows)
	}
	for _, r := range rows {
		if r.Lang == "en-US" && r.ItemValue != "Internal error, please try again later" {
			t.Fatalf("en-US 终态文案应为规范值，实际 %q", r.ItemValue)
		}
	}
}

// TestSharedFeedbackArtexRowsUpgrade 固化的回归：用**从 artex 开发库导出的真实行**
// （真实 id、文案、category、remark 与时间）重放整条迁移链。
//
// 前几条用例是按形态构造的；这一条是真实数据 —— 两者都要留，缺一条就无法区分
// 「规则对」和「规则对但这个库恰好长得不一样」。
func TestSharedFeedbackArtexRowsUpgrade(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	mustExec(t, db, "DELETE FROM sys_i18n")
	mustExec(t, db, `INSERT INTO sys_i18n (id,item_key,lang,item_value,http_code,category,remark,status,create_time,update_time) VALUES
(250548,'uiInternalErr','en-US','Internal server error, please try again later',200,'block','block/inbound/http: 归口文案（本批补 en-US，http_code 与 058 的 zh-CN 行一致）',1,'2026-10-24 21:19:48.346555+08','2026-10-24 21:19:48.346555+08'),
(250549,'uiInternalErr','zh-CN','系统内部错误，请稍后重试',200,'ui','internal/module/block/enums/block_enums.go',1,'2026-10-24 21:19:47.97849+08','2026-10-24 21:19:47.97849+08'),
(382861,'MsgInternalError','en-US','Internal error, please try again later',200,'ui','shell.PageInternalText 的页面级归口文案（zh-CN 见 058；本批补英文）',1,'2026-11-05 18:24:13.218658+08','2026-11-05 18:24:13.218658+08')`)

	// 前提：这份真实数据确实会让 747 拒绝升级（否则本用例证明不了什么）。
	if err := apply747(t, db); err == nil {
		t.Fatal("前提不成立：artex 行本该被 747 判为冲突")
	} else if !strings.Contains(err.Error(), "MsgInternalError -> uiInternalErr/en-US") {
		t.Fatalf("747 必须以可定位冲突拒绝，实际：%v", err)
	}

	if err := migrations.Run(db); err != nil {
		t.Fatalf("整条迁移链必须能升级 artex 的真实数据，实际：%v", err)
	}
	assertNoRetiredSharedFeedbackKeys(t, db)

	var rows []struct{ Lang, ItemValue string }
	if err := db.Raw(`SELECT lang, item_value FROM sys_i18n WHERE item_key='uiInternalErr' ORDER BY lang`).
		Scan(&rows).Error; err != nil {
		t.Fatalf("读取终态失败：%v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("终态应有中英两行，实际：%+v", rows)
	}
	if rows[0].Lang == "en-US" && rows[0].ItemValue != "Internal error, please try again later" {
		t.Fatalf("en-US 终态应为规范文案，实际 %q", rows[0].ItemValue)
	}
}

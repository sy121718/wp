package migrations_test

import (
	"regexp"
	"strings"
	"testing"

	"go_wp/pkg/i18n"
	"go_wp/public/migrations"
	"go_wp/public/test/support"

	"gorm.io/gorm"
)

type sharedFeedbackGroup struct {
	oldKey string
	newKey string
}

// 本批唯一收口的六组共享反馈词条。brandNoneSel 是 738 原本就负责播种的附带词条，
// 保留但不扩到其它域。
var sharedFeedbackGroups = []sharedFeedbackGroup{
	{"MsgInternalError", "uiInternalErr"},
	{"ErrInvalidParams", "sysErrBadParam"},
	{"ErrPermissionDenied", "sysErrNoPerm"},
	{"ErrRequestEntityTooLarge", "sysErrTooLarge"},
	{"msg_operation_success", "uiOpSuccess"},
	{"msg_delete_confirm", "uiDelConfirm"},
}

var sharedFeedbackNewPairs = [][2]string{
	{"uiInternalErr", "zh-CN"}, {"uiInternalErr", "en-US"},
	{"sysErrBadParam", "zh-CN"}, {"sysErrBadParam", "en-US"},
	{"sysErrNoPerm", "zh-CN"}, {"sysErrNoPerm", "en-US"},
	{"sysErrTooLarge", "zh-CN"}, {"sysErrTooLarge", "en-US"},
	{"uiOpSuccess", "zh-CN"}, {"uiOpSuccess", "en-US"},
	{"uiDelConfirm", "zh-CN"}, {"uiDelConfirm", "en-US"},
	{"brandNoneSel", "zh-CN"}, {"brandNoneSel", "en-US"},
}

// mappedNewPairs 只含六组「旧→新」映射的目标语言对；brandNoneSel 没有旧 key，
// 不参与「升级不得先种默认」的前提判断。
var mappedNewPairs = sharedFeedbackNewPairs[:12]

func sharedFeedbackOldKeys() []string {
	keys := make([]string, 0, len(sharedFeedbackGroups))
	for _, g := range sharedFeedbackGroups {
		keys = append(keys, g.oldKey)
	}
	return keys
}

func sharedFeedbackNewKeys() []string {
	keys := make([]string, 0, len(sharedFeedbackGroups)+1)
	for _, g := range sharedFeedbackGroups {
		keys = append(keys, g.newKey)
	}
	return append(keys, "brandNoneSel")
}

func allSharedFeedbackKeys() []string {
	return append(sharedFeedbackOldKeys(), sharedFeedbackNewKeys()...)
}

var shortKeyPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,14}$`)

// TestSharedFeedbackPrepareNewSQL 在真实 PG 上 PREPARE 本批全部现行 SQL 与守门，
// 语法与列引用错误在测试期暴露，不等到部署时才报。
// 覆盖 058/405/407/720/738 的现行 SQL 与 ConditionSQL，以及 747 的 SQL 与 CheckSQL。
func TestSharedFeedbackPrepareNewSQL(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	seedVersions := []string{
		"058-i18n-seed-enums",
		"405-i18n-block-page-err",
		"407-i18n-product-inventory-page-err",
		"720-product-taxonomy-short-keys-i18n",
		"738-shared-error-short-keys",
	}
	var migration747 migrations.Migration
	for _, m := range migrations.All() {
		if m.Version == "747-shared-feedback-short-keys-i18n" {
			migration747 = m
		}
	}
	if migration747.Version == "" {
		t.Fatal("747 正式安全迁移未登记")
	}

	var sources, conditions []string
	for _, version := range seedVersions {
		s := findSeedByVersion(t, version)
		sources = append(sources, s.SQL)
		conditions = append(conditions, s.ConditionSQL)
	}
	sources = append(sources, migration747.SQL)
	conditions = append(conditions, migration747.CheckSQL)

	if err := db.Connection(func(conn *gorm.DB) error {
		for _, sqlText := range append(sources, conditions...) {
			for _, stmt := range migrations.SplitStatements(sqlText) {
				if err := conn.Exec("PREPARE shared_feedback_source AS " + stmt).Error; err != nil {
					return err
				}
				if err := conn.Exec("DEALLOCATE shared_feedback_source").Error; err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("PREPARE 共享反馈 SQL 失败：%v", err)
	}

	for _, key := range sharedFeedbackNewKeys() {
		if !shortKeyPattern.MatchString(key) {
			t.Errorf("新目标 key 不合规（ASCII 首字母、1–15 位）：%s", key)
		}
	}
}

// sharedFeedbackSnapshot 比较本批全部旧/新 key 的完整行（含 id 与时间），
// 用于证明「冲突整批回滚」与「升级保留运营值」时没有任何一行被改动。
func sharedFeedbackSnapshot(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var got string
	err := db.Raw(`SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY item_key, lang)::text, '[]')
FROM sys_i18n t WHERE item_key IN ?`, allSharedFeedbackKeys()).Scan(&got).Error
	if err != nil {
		t.Fatalf("读取共享反馈词条快照失败：%v", err)
	}
	return got
}

// assertNoRetiredSharedFeedbackKeys 证明六组旧 key 在终态里一行都不剩。
func assertNoRetiredSharedFeedbackKeys(t *testing.T, db *gorm.DB) {
	t.Helper()
	var rows []struct {
		ItemKey string
		Lang    string
	}
	if err := db.Raw(`SELECT item_key, lang FROM sys_i18n WHERE item_key IN ? ORDER BY item_key, lang`,
		sharedFeedbackOldKeys()).Scan(&rows).Error; err != nil {
		t.Fatalf("查询退役共享反馈 key 失败：%v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("退役共享反馈 key 仍然存在：%v", rows)
	}
}

func countSharedFeedbackPairs(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := db.Raw(`SELECT COUNT(*) FROM sys_i18n WHERE (item_key, lang) IN ?`, sharedFeedbackNewPairs).
		Scan(&n).Error; err != nil {
		t.Fatalf("统计共享反馈新 key 语言对失败：%v", err)
	}
	return n
}

// TestSharedFeedback058GuardMatchesExactPairs 钉住守门只认精确 (item_key, lang)：
// 用 key 数 × 两语言或全库数量代替真实语言对时，额外语言/无关 key 会抵消缺口。
func TestSharedFeedback058GuardMatchesExactPairs(t *testing.T) {
	seed := findSeedByVersion(t, "058-i18n-seed-enums")

	sqlPairs := regexp.MustCompile(`(?m)^\('([^']+)', '([^']+)',`).FindAllStringSubmatch(seed.SQL, -1)
	condPairs := regexp.MustCompile(`\('([^']+)', '([^']+)'\)`).FindAllStringSubmatch(seed.ConditionSQL, -1)
	if len(sqlPairs) != 263 {
		t.Fatalf("058 现行 SQL 的语言对数量改变：%d（应为 263）", len(sqlPairs))
	}
	if len(condPairs) != len(sqlPairs) {
		t.Fatalf("058 守门语言对数 %d 与 SQL 行数 %d 不一致", len(condPairs), len(sqlPairs))
	}
	for i := range sqlPairs {
		want := sqlPairs[i][1] + "\x00" + sqlPairs[i][2]
		got := condPairs[i][1] + "\x00" + condPairs[i][2]
		if want != got {
			t.Fatalf("058 守门第 %d 对待核对不一致：SQL=%v 守门=%v", i+1, sqlPairs[i], condPairs[i])
		}
	}
	for _, key := range sharedFeedbackOldKeys() {
		if strings.Contains(seed.SQL, "('"+key+"',") {
			t.Errorf("058 仍在播种退役 key %s", key)
		}
	}
	if !strings.Contains(seed.ConditionSQL, "NOT EXISTS") {
		t.Error("058 守门应显式要求退役 key 不存在")
	}
}

// TestSharedFeedbackDangerousSQLNotRegistered 证明旧的 738/745 危险 SQL 不再登记执行，
// 历史原文只作为精确归档存在（原文哈希由 register_registry_test.go 钉住）。
func TestSharedFeedbackDangerousSQLNotRegistered(t *testing.T) {
	for _, m := range migrations.All() {
		if strings.Contains(m.Version, "745") {
			t.Fatalf("危险清理迁移 %s 仍被登记执行", m.Version)
		}
	}
	seed := findSeedByVersion(t, "738-shared-error-short-keys")
	if !strings.Contains(seed.SQL, "共享反馈词条冲突") || !strings.Contains(seed.SQL, "UPDATE sys_i18n") {
		t.Fatal("738 现行 SQL 必须走先检查冲突、再原位搬迁的安全路径")
	}
	if strings.Contains(seed.SQL, "DELETE FROM sys_i18n\nWHERE (item_key, lang) IN") {
		t.Fatal("738 仍在先 DO NOTHING 后直接删除旧值")
	}
	found := false
	for _, m := range migrations.All() {
		if m.Version == "747-shared-feedback-short-keys-i18n" {
			found = true
			if m.SQL != seed.SQL {
				t.Fatal("747 与 738 必须复用同一份安全搬迁 SQL，避免两套六组映射分叉")
			}
		}
	}
	if !found {
		t.Fatal("747 正式安全迁移未登记")
	}
}

// TestSharedFeedback058BackfillRejectsDecoyAndKeepsOldKeysGone 模拟 058 定向缺一条：
// 同一 key 的额外语言（fr-FR）与无关 key 都不能替代缺失的精确语言对，
// 补种后退役 key 不得复活。
func TestSharedFeedback058BackfillRejectsDecoyAndKeepsOldKeysGone(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("首轮种子失败：%v", err)
	}
	assertNoRetiredSharedFeedbackKeys(t, db)
	if got := countSharedFeedbackPairs(t, db); got != int64(len(sharedFeedbackNewPairs)) {
		t.Fatalf("首轮种子后本批新 key 语言对 = %d，want %d", got, len(sharedFeedbackNewPairs))
	}

	seed := findSeedByVersion(t, "058-i18n-seed-enums")
	mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key = 'ErrUploadEmpty' AND lang = 'zh-CN'")
	mustExec(t, db, "INSERT INTO sys_i18n (item_key, lang, item_value, category, remark, status) VALUES ('ErrUploadEmpty', 'fr-FR', 'Fichier requis', 'error', 'extra', 1)")
	mustExec(t, db, "INSERT INTO sys_i18n (item_key, lang, item_value, category, remark, status) VALUES ('decoyUnrelatedKey', 'zh-CN', '干扰项', 'ui', 'decoy', 1)")

	var done int64
	if err := db.Raw(seed.ConditionSQL).Scan(&done).Error; err != nil {
		t.Fatalf("求值 058 守门失败：%v", err)
	}
	if done != 0 {
		t.Fatalf("缺失一条精确语言对、但有额外语言和无关 key 时，058 守门仍返回 %d，seed 会永不回补", done)
	}

	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("补种失败：%v", err)
	}
	var zh, fr string
	if err := db.Raw("SELECT item_value FROM sys_i18n WHERE item_key = 'ErrUploadEmpty' AND lang = 'zh-CN'").Scan(&zh).Error; err != nil || zh == "" {
		t.Fatalf("没有回补 ErrUploadEmpty/zh-CN：%q %v", zh, err)
	}
	if err := db.Raw("SELECT item_value FROM sys_i18n WHERE item_key = 'ErrUploadEmpty' AND lang = 'fr-FR'").Scan(&fr).Error; err != nil || fr != "Fichier requis" {
		t.Fatalf("补种改动了额外语言译文：%q %v", fr, err)
	}
	assertNoRetiredSharedFeedbackKeys(t, db)

	if err := db.Raw(seed.ConditionSQL).Scan(&done).Error; err != nil || done != 1 {
		t.Fatalf("补种后 058 守门应为 1，实际 %d：%v", done, err)
	}
}

// TestSharedFeedbackUpgradeRunThenSeedsKeepsOperationalValues 走完整 Run → RunSeeds：
// 升级前仅存在旧 key 的运营中英值、额外 fr/ja、禁用状态与元数据；
// 747 在 seed 之前先搬旧值，随后 seed 不得用新默认覆盖，也不得复活旧 key。
func TestSharedFeedbackUpgradeRunThenSeedsKeepsOperationalValues(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("准备基线种子失败：%v", err)
	}

	langs := []string{"zh-CN", "en-US", "fr-FR", "ja-JP"}
	for _, g := range sharedFeedbackGroups {
		mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key = ?", g.newKey)
		for _, lang := range langs {
			mustExec(t, db, `INSERT INTO sys_i18n
(item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES (?, ?, ?, 409, 'operator', ?, 0, '2020-01-02 03:04:05', '2021-02-03 04:05:06')`,
				g.oldKey, lang, "operational-"+g.oldKey+"/"+lang, "kept-"+lang)
		}
	}
	expected := map[string]string{}
	for _, g := range sharedFeedbackGroups {
		var got string
		if err := db.Raw(`SELECT jsonb_agg(to_jsonb(t) - 'item_key' ORDER BY lang)::text FROM sys_i18n t WHERE item_key = ?`, g.oldKey).
			Scan(&got).Error; err != nil {
			t.Fatalf("读取 %s 升级前快照失败：%v", g.oldKey, err)
		}
		expected[g.newKey] = got
	}

	if err := migrations.Run(db); err != nil {
		t.Fatalf("升级 Run 失败：%v", err)
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("升级 RunSeeds 失败：%v", err)
	}
	assertNoRetiredSharedFeedbackKeys(t, db)

	for _, g := range sharedFeedbackGroups {
		var got string
		if err := db.Raw(`SELECT jsonb_agg(to_jsonb(t) - 'item_key' ORDER BY lang)::text FROM sys_i18n t WHERE item_key = ?`, g.newKey).
			Scan(&got).Error; err != nil {
			t.Fatalf("读取 %s 升级后快照失败：%v", g.newKey, err)
		}
		if got != expected[g.newKey] {
			t.Fatalf("%s 升级未完整保留运营译文/额外语言/id/状态/时间：\nbefore=%s\nafter=%s", g.newKey, expected[g.newKey], got)
		}
	}

	// 再跑一轮完整 seed，终态不得漂移。
	before := sharedFeedbackSnapshot(t, db)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("升级后重跑 seed 失败：%v", err)
	}
	if after := sharedFeedbackSnapshot(t, db); after != before {
		t.Fatal("完整 seed 重跑改变了升级后的运营值")
	}
}

// TestSharedFeedbackRunSeedsAloneMigratesOldValues 证明不依赖 747 先跑：
// 单独调用 RunSeeds 时，058/405/407/720 的现行 SQL 都不触这六组，
// 旧值原样存续到 738 的版本位，由同一条安全 SQL 原位更名，id 与全部元数据都保留。
func TestSharedFeedbackRunSeedsAloneMigratesOldValues(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("首轮种子失败：%v", err)
	}
	langs := []string{"zh-CN", "en-US", "fr-FR", "ja-JP"}
	for _, g := range sharedFeedbackGroups {
		mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key = ?", g.newKey)
		for _, lang := range langs {
			mustExec(t, db, `INSERT INTO sys_i18n
(item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES (?, ?, ?, 422, 'operator', ?, 0, '2020-01-02 03:04:05', '2021-02-03 04:05:06')`,
				g.oldKey, lang, "custom-"+g.oldKey+"/"+lang, "kept-"+lang)
		}
	}
	// 原位改名必须保留原 id；快照只排除 item_key，id 与全部元数据都必须逐项相等。
	expected := map[string]string{}
	for _, g := range sharedFeedbackGroups {
		var snapshot string
		if err := db.Raw(`SELECT jsonb_agg(to_jsonb(t) - 'item_key' ORDER BY lang)::text
FROM sys_i18n t WHERE item_key = ?`, g.oldKey).Scan(&snapshot).Error; err != nil {
			t.Fatalf("读取 %s 搬迁前完整快照失败：%v", g.oldKey, err)
		}
		expected[g.newKey] = snapshot
	}

	// 前序 seed 不得抢先落目标默认值，否则 738 会因冲突拒绝整批。
	var planted int64
	if err := db.Raw(`SELECT COUNT(*) FROM sys_i18n WHERE (item_key, lang) IN ?`, mappedNewPairs).
		Scan(&planted).Error; err != nil {
		t.Fatalf("统计前序默认值失败：%v", err)
	}
	if planted != 0 {
		t.Fatalf("第二轮 RunSeeds 前已存在 %d 行六组映射目标默认值，说明前序 seed 抢先种默认，测试前提不成立", planted)
	}

	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("单独 RunSeeds 失败：%v", err)
	}
	assertNoRetiredSharedFeedbackKeys(t, db)

	for _, g := range sharedFeedbackGroups {
		var after string
		if err := db.Raw(`SELECT jsonb_agg(to_jsonb(t) - 'item_key' ORDER BY lang)::text
FROM sys_i18n t WHERE item_key = ?`, g.newKey).Scan(&after).Error; err != nil {
			t.Fatalf("读取 %s 搬迁后完整快照失败：%v", g.newKey, err)
		}
		if after != expected[g.newKey] {
			t.Fatalf("%s 独立 RunSeeds 未原位保留（含 id）四语言运营值与元数据：\nbefore=%s\nafter=%s",
				g.newKey, expected[g.newKey], after)
		}
		for _, lang := range langs {
			var row struct {
				Value     string
				HTTPCode  int
				Category  string
				Remark    string
				Status    int
				TimeMatch bool `gorm:"column:time_match"`
			}
			if err := db.Raw(`SELECT item_value AS value, http_code, category, remark, status,
       create_time = TIMESTAMP '2020-01-02 03:04:05'
         AND update_time = TIMESTAMP '2021-02-03 04:05:06' AS time_match
FROM sys_i18n WHERE item_key = ? AND lang = ?`, g.newKey, lang).Scan(&row).Error; err != nil {
				t.Fatalf("读取 %s/%s 失败：%v", g.newKey, lang, err)
			}
			if row.Value != "custom-"+g.oldKey+"/"+lang || row.HTTPCode != 422 || row.Category != "operator" ||
				row.Remark != "kept-"+lang || row.Status != 0 || !row.TimeMatch {
				t.Fatalf("%s/%s 未完整保留运营值与元数据：%+v", g.newKey, lang, row)
			}
		}
	}
}

// TestSharedFeedbackConflictRollsBackWholeBatch 目标已存在且内容或元数据不同时，
// 整批回滚：连本可以搬迁的其它组也必须零变动，错误必须定位 old -> new/lang。
func TestSharedFeedbackConflictRollsBackWholeBatch(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("准备基线种子失败：%v", err)
	}

	// 一组制造「同值不同元数据」冲突：值相同，remark 不同。
	mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key = 'sysErrBadParam'")
	mustExec(t, db, `INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status)
VALUES ('ErrInvalidParams', 'zh-CN', '请求参数错误', 400, 'error', 'old-remark', 1)`)
	mustExec(t, db, `INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status)
VALUES ('ErrInvalidParams', 'en-US', 'Invalid request parameters', 400, 'error', 'old-remark', 1)`)
	mustExec(t, db, `INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status)
VALUES ('ErrInvalidParams', 'fr-FR', 'Paramètres invalides', 400, 'error', 'old-remark', 1)`)
	mustExec(t, db, `INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status)
VALUES ('sysErrBadParam', 'en-US', 'Invalid request parameters', 400, 'error', 'target-remark', 1)`)

	// 另一组本可完整搬迁，用来证明失败时不会发生部分迁移。
	mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key = 'uiInternalErr'")
	mustExec(t, db, `INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status)
VALUES ('MsgInternalError', 'zh-CN', '自定义内部错误', 500, 'operator', 'kept', 0)`)

	before := sharedFeedbackSnapshot(t, db)
	err := migrations.Run(db)
	if err == nil {
		t.Fatal("冲突必须拒绝整批迁移")
	}
	if !strings.Contains(err.Error(), "ErrInvalidParams -> sysErrBadParam/en-US") {
		t.Fatalf("冲突错误必须定位 old -> new/lang，实际：%v", err)
	}
	if after := sharedFeedbackSnapshot(t, db); after != before {
		t.Fatal("冲突后发生部分迁移，本批词条不是整批回滚")
	}
}

// TestSharedFeedbackIdenticalRowsDedupe 旧/新目标全部业务字段与时间完全相同（仅 id 不同）时，
// 允许清理旧重复行；比较语义不得因 id 天然不同而误判冲突。
func TestSharedFeedbackIdenticalRowsDedupe(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("准备基线种子失败：%v", err)
	}
	// 重复行连 create_time/update_time 都与目标一致，只允许 id 天然不同。
	for _, g := range sharedFeedbackGroups {
		mustExec(t, db, `INSERT INTO sys_i18n
(item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
SELECT ?, lang, item_value, http_code, category, remark, status, create_time, update_time
FROM sys_i18n WHERE item_key = ?`, g.oldKey, g.newKey)
	}
	var targetIDs string
	if err := db.Raw(`SELECT jsonb_agg(jsonb_build_object('k', item_key, 'l', lang, 'id', id) ORDER BY item_key, lang)::text
FROM sys_i18n WHERE item_key IN ?`, sharedFeedbackNewKeys()).Scan(&targetIDs).Error; err != nil {
		t.Fatalf("读取目标行 id 失败：%v", err)
	}

	if err := migrations.Run(db); err != nil {
		t.Fatalf("完全相同重复行应被安全清理：%v", err)
	}
	assertNoRetiredSharedFeedbackKeys(t, db)
	var afterIDs string
	if err := db.Raw(`SELECT jsonb_agg(jsonb_build_object('k', item_key, 'l', lang, 'id', id) ORDER BY item_key, lang)::text
FROM sys_i18n WHERE item_key IN ?`, sharedFeedbackNewKeys()).Scan(&afterIDs).Error; err != nil {
		t.Fatalf("读取目标行 id 失败：%v", err)
	}
	if afterIDs != targetIDs {
		t.Fatal("去重不得删除或重建目标行")
	}
}

// TestSharedFeedbackTimeOnlyDifferenceRollsBack 目标与旧行值及业务元数据相同、
// 仅 create_time/update_time 不同：必须整批拒绝，不能借「忽略时间」去重丢掉旧行历史。
func TestSharedFeedbackTimeOnlyDifferenceRollsBack(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("准备基线种子失败：%v", err)
	}
	for _, g := range sharedFeedbackGroups {
		mustExec(t, db, `INSERT INTO sys_i18n
(item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
SELECT ?, lang, item_value, http_code, category, remark, status,
       create_time - interval '1 day', update_time - interval '1 day'
FROM sys_i18n WHERE item_key = ?`, g.oldKey, g.newKey)
	}
	before := sharedFeedbackSnapshot(t, db)
	err := migrations.Run(db)
	if err == nil {
		t.Fatal("仅时间不同也必须拒绝，不能去重删除旧行")
	}
	if !strings.Contains(err.Error(), "MsgInternalError -> uiInternalErr/") {
		t.Fatalf("时间冲突也必须定位 old -> new/lang，实际：%v", err)
	}
	if after := sharedFeedbackSnapshot(t, db); after != before {
		t.Fatal("时间冲突后发生部分迁移")
	}
}

// TestSharedFeedbackRunSeedsAloneConflictRollsBackWholeBatch 独立 RunSeeds 时若 738 遇到冲突，
// 整批（含另一组本可搬迁的行）必须零变动；不能因为前序 seed 已提交而留下部分结果。
func TestSharedFeedbackRunSeedsAloneConflictRollsBackWholeBatch(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("准备基线种子失败：%v", err)
	}

	// 冲突组：ErrInvalidParams 的 en-US 目标已存在但 remark 不同。
	mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key = 'sysErrBadParam'")
	for _, lang := range []string{"zh-CN", "fr-FR"} {
		mustExec(t, db, `INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status)
VALUES ('ErrInvalidParams', ?, '请求参数错误', 400, 'error', 'old-remark', 1)`, lang)
	}
	mustExec(t, db, `INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status)
VALUES ('ErrInvalidParams', 'en-US', 'Invalid request parameters', 400, 'error', 'old-remark', 1)`)
	mustExec(t, db, `INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status)
VALUES ('sysErrBadParam', 'en-US', 'Invalid request parameters', 400, 'error', 'target-remark', 1)`)

	// 另一组本可完整搬迁，用来证明独立播种失败时没有部分迁移。
	mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key = 'uiInternalErr'")
	mustExec(t, db, `INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status)
VALUES ('MsgInternalError', 'zh-CN', '自定义内部错误', 500, 'operator', 'kept', 0)`)

	before := sharedFeedbackSnapshot(t, db)
	err := migrations.RunSeeds(db)
	if err == nil {
		t.Fatal("独立 RunSeeds 遇到冲突必须拒绝整批")
	}
	if !strings.Contains(err.Error(), "ErrInvalidParams -> sysErrBadParam/en-US") {
		t.Fatalf("冲突错误必须定位 old -> new/lang，实际：%v", err)
	}
	if after := sharedFeedbackSnapshot(t, db); after != before {
		t.Fatal("独立 RunSeeds 冲突后发生部分迁移，本批词条不是整批回滚")
	}
}

// TestSharedFeedbackRunSeedsAloneReplays405NonSharedItem 独立 RunSeeds 缺 405 的非共享项时，
// 只补该项，不制造六组旧/新冲突，也不改变共享反馈词条终态。
func TestSharedFeedbackRunSeedsAloneReplays405NonSharedItem(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("准备基线种子失败：%v", err)
	}
	before := sharedFeedbackSnapshot(t, db)
	mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key = 'ErrBlockNotFound' AND lang = 'en-US'")

	var replay int64
	if err := db.Raw(findSeedByVersion(t, "405-i18n-block-page-err").ConditionSQL).Scan(&replay).Error; err != nil {
		t.Fatalf("求值 405 守门失败：%v", err)
	}
	if replay != 0 {
		t.Fatalf("缺 ErrBlockNotFound/en-US 时 405 守门应为 0，实际 %d", replay)
	}

	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("缺非共享项时独立 RunSeeds 失败：%v", err)
	}
	var value string
	if err := db.Raw(`SELECT item_value FROM sys_i18n WHERE item_key = 'ErrBlockNotFound' AND lang = 'en-US'`).Scan(&value).Error; err != nil || value == "" {
		t.Fatalf("没有回补 ErrBlockNotFound/en-US：%q %v", value, err)
	}
	assertNoRetiredSharedFeedbackKeys(t, db)
	if after := sharedFeedbackSnapshot(t, db); after != before {
		t.Fatal("回补 405 非共享项改变了共享反馈词条")
	}
}

// TestSharedFeedbackFullRunAndSeedsTwice 覆盖从旧安装完整 Run → RunSeeds 的两轮幂等，
// 终态不得出现退役 key，也不得改变本批词条。
func TestSharedFeedbackFullRunAndSeedsTwice(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if err := migrations.Run(db); err != nil {
		t.Fatalf("Run 失败：%v", err)
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("RunSeeds 失败：%v", err)
	}
	assertNoRetiredSharedFeedbackKeys(t, db)
	before := sharedFeedbackSnapshot(t, db)
	for round := 1; round <= 2; round++ {
		if err := migrations.Run(db); err != nil {
			t.Fatalf("第 %d 轮 Run 失败：%v", round, err)
		}
		if err := migrations.RunSeeds(db); err != nil {
			t.Fatalf("第 %d 轮 RunSeeds 失败：%v", round, err)
		}
		if after := sharedFeedbackSnapshot(t, db); after != before {
			t.Fatalf("第 %d 轮完整 Run→RunSeeds 改变了本批终态", round)
		}
		assertNoRetiredSharedFeedbackKeys(t, db)
	}
}

// TestSharedFeedbackI18nGetTextHitsRealTranslations 用隔离缓存验证中英真实命中，
// 不依赖全局 database 单例，也不污染其它用例。
func TestSharedFeedbackI18nGetTextHitsRealTranslations(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("种子失败：%v", err)
	}
	var rows []struct {
		ItemKey string `gorm:"column:item_key"`
		Lang    string
		Value   string `gorm:"column:item_value"`
	}
	if err := db.Raw(`SELECT item_key, lang, item_value FROM sys_i18n
WHERE item_key IN ? AND lang IN ('zh-CN', 'en-US')`, sharedFeedbackNewKeys()).Scan(&rows).Error; err != nil {
		t.Fatalf("读取词条失败：%v", err)
	}
	data := map[string]map[string]string{}
	for _, r := range rows {
		if data[r.ItemKey] == nil {
			data[r.ItemKey] = map[string]string{}
		}
		data[r.ItemKey][r.Lang] = r.Value
	}
	i18n.InjectForTest(data, nil)
	t.Cleanup(func() {
		i18n.InjectForTest(map[string]map[string]string{}, map[string]int{})
		_ = i18n.Close()
	})

	for _, pair := range sharedFeedbackNewPairs {
		key, lang := pair[0], pair[1]
		want := data[key][lang]
		if want == "" {
			t.Fatalf("%s/%s 词条缺失，i18n.GetText 会退成裸 key", key, lang)
		}
		if got := i18n.GetText(key, lang); got != want {
			t.Fatalf("i18n.GetText(%s, %s) = %q，want %q", key, lang, got, want)
		}
	}
	for _, tc := range []struct{ key, lang, want string }{
		{"uiOpSuccess", "zh-CN", "操作成功"},
		{"uiOpSuccess", "en-US", "Operation successful"},
		{"uiDelConfirm", "zh-CN", "确定要删除吗？"},
		{"sysErrNoPerm", "zh-CN", "无权限访问"},
		{"sysErrTooLarge", "en-US", "Request entity too large"},
		{"brandNoneSel", "en-US", "No brands selected; the list is unchanged."},
	} {
		if got := i18n.GetText(tc.key, tc.lang); got != tc.want {
			t.Fatalf("i18n.GetText(%s, %s) = %q，want %q", tc.key, tc.lang, got, tc.want)
		}
	}
}

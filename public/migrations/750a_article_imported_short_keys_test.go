package migrations_test

import (
	"strings"
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// 750a 现行执行版本必须只播短 key：历史 SQL 播种的是点分长 key，
// 完整 seed 重跑会把这些退役 key 重新补回。
func TestArticleImportedSeedRegistersShortKeysOnly(t *testing.T) {
	var seed *migrations.Seed
	for _, s := range migrations.AllSeeds() {
		if s.Version == "750a-article-imported-i18n" {
			seed = &s
			break
		}
	}
	if seed == nil {
		t.Fatal("文章导入词条 seed 未注册")
	}
	// 只看 VALUES 里的元组：( 'key', 'lang', ... )。注释里提到旧 key 是在说明它们已退役，
	// 不能把说明文字当成播种事实。
	values := insertValues(seed.SQL)
	for _, retired := range []string{"admin.article.ok.imported", "admin.article.edit.visualOpen"} {
		if strings.Contains(values, "'"+retired+"'") {
			t.Errorf("750a 仍在播种退役长 key：%s", retired)
		}
	}
	for _, short := range []string{"artImportOk", "artVisualOpen"} {
		if !contains(seed.SQL, short) {
			t.Errorf("750a 没有播种短 key：%s", short)
		}
	}
}

// 完整 seed 连跑两次后，退役长 key 不得复活，短 key 齐全。
func TestArticleImportedRetiredKeysStayGoneAfterRepeatedSeeds(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("首次完整 seed 失败：%v", err)
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("第二次完整 seed 失败：%v", err)
	}
	for _, retired := range []string{"admin.article.ok.imported", "admin.article.edit.visualOpen"} {
		var n int64
		if err := db.Table("sys_i18n").Where("item_key = ?", retired).Count(&n).Error; err != nil {
			t.Fatalf("查询退役 key 失败：%v", err)
		}
		if n != 0 {
			t.Errorf("完整 seed 重跑后退役 key 复活：%s（%d 行）", retired, n)
		}
	}
	for _, short := range []string{"artImportOk", "artVisualOpen"} {
		var n int64
		if err := db.Table("sys_i18n").Where("item_key = ?", short).Count(&n).Error; err != nil {
			t.Fatalf("查询短 key 失败：%v", err)
		}
		if n != 2 {
			t.Errorf("短 key %s 应有中英两行，实际 %d", short, n)
		}
	}
}

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }

// insertValues 截出 INSERT 语句 VALUES 之后的部分，排除文件头注释。
func insertValues(sql string) string {
	idx := strings.Index(strings.ToUpper(sql), "VALUES")
	if idx < 0 {
		return sql
	}
	return sql[idx:]
}

// 存量升级：旧库只有长 key（带运营自定义译文与额外语言）。
//
// 这条链路以前是坏的：750a 先补默认短 key → 753 看到短 key 已齐而跳过 →
// 旧译文留在长 key 上、新 key 用默认值。断言必须落在「值真的搬过来了」，
// 不能只断言「没有任何旧 key」（那样删掉旧行也能过）。
func TestArticleImportedUpgradeMovesLegacyTranslations(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("首次完整 seed 失败：%v", err)
	}

	// 造出「只升级到 750a 的旧库」形状：删掉短 key，只留带自定义译文的长 key，
	// 另加一条额外语言与不同元数据，验证它们是**搬**过来的，不是重新播种的。
	if err := db.Exec(`DELETE FROM sys_i18n WHERE item_key IN ('artImportOk','artVisualOpen')`).Error; err != nil {
		t.Fatalf("清理短 key 失败：%v", err)
	}
	if err := db.Exec(`INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('admin.article.ok.imported', 'zh-CN', '运营自定义：草稿已建', 200, 'admin', '自定义备注', 1, now(), now()),
    ('admin.article.ok.imported', 'en-US', 'Custom draft created', 200, 'admin', '自定义备注', 1, now(), now()),
    ('admin.article.ok.imported', 'ja-JP', 'カスタム', 200, 'admin', '额外语言', 1, now(), now()),
    ('admin.article.edit.visualOpen', 'zh-CN', '打开编辑器（自定义）', 200, 'admin', '自定义备注', 1, now(), now())`).Error; err != nil {
		t.Fatalf("构造旧库数据失败：%v", err)
	}

	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("升级重跑完整 seed 失败：%v", err)
	}

	type row struct {
		ItemKey   string
		Lang      string
		ItemValue string
		Remark    string
	}
	var rows []row
	if err := db.Raw(`SELECT item_key, lang, item_value, remark FROM sys_i18n
WHERE item_key IN ('artImportOk','artVisualOpen') ORDER BY item_key, lang`).Scan(&rows).Error; err != nil {
		t.Fatalf("查询搬迁结果失败：%v", err)
	}
	want := map[string]string{
		"artImportOk/zh-CN":   "运营自定义：草稿已建",
		"artImportOk/en-US":   "Custom draft created",
		"artImportOk/ja-JP":   "カスタム",
		"artVisualOpen/zh-CN": "打开编辑器（自定义）",
		"artVisualOpen/en-US": "Open the visual editor",
	}
	got := map[string]string{}
	for _, r := range rows {
		got[r.ItemKey+"/"+r.Lang] = r.ItemValue
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s 的译文应为 %q（搬迁或补默认），实际 %q（全部结果：%v）", key, value, got[key], got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("短 key 行数应为 %d，实际 %d：%v", len(want), len(got), got)
	}

	for _, retired := range []string{"admin.article.ok.imported", "admin.article.edit.visualOpen"} {
		var n int64
		if err := db.Table("sys_i18n").Where("item_key = ?", retired).Count(&n).Error; err != nil {
			t.Fatalf("查询退役 key 失败：%v", err)
		}
		if n != 0 {
			t.Errorf("升级后退役 key 仍残留：%s（%d 行）", retired, n)
		}
	}

	// 再跑一次完整 seed：已升级库不得回退，也不得复活旧 key。
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("第二次重跑完整 seed 失败：%v", err)
	}
	var after int64
	if err := db.Table("sys_i18n").Where("item_key IN ('artImportOk','artVisualOpen') AND item_value = '运营自定义：草稿已建'").Count(&after).Error; err != nil {
		t.Fatalf("复查自定义译文失败：%v", err)
	}
	if after != 1 {
		t.Fatalf("重跑后自定义译文被覆盖：匹配行数 %d", after)
	}
}

// 旧新并存且元数据一致：迁移去重后只留短 key，不丢值。
func TestArticleImportedUpgradeDeduplicatesIdenticalRows(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("首次完整 seed 失败：%v", err)
	}
	// 让长 key 与短 key 的元数据完全一致（模拟「已有人先复制过一份」）。
	if err := db.Exec(`INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
SELECT 'admin.article.ok.imported', lang, item_value, http_code, category, remark, status, create_time, update_time
FROM sys_i18n WHERE item_key = 'artImportOk'`).Error; err != nil {
		t.Fatalf("构造并存数据失败：%v", err)
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("并存升级失败：%v", err)
	}
	var retired int64
	if err := db.Table("sys_i18n").Where("item_key = 'admin.article.ok.imported'").Count(&retired).Error; err != nil {
		t.Fatalf("查询退役 key 失败：%v", err)
	}
	if retired != 0 {
		t.Errorf("元数据一致的并存行应被去重，实际仍残留 %d 行", retired)
	}
	var kept int64
	if err := db.Table("sys_i18n").Where("item_key = 'artImportOk'").Count(&kept).Error; err != nil {
		t.Fatalf("查询短 key 失败：%v", err)
	}
	if kept != 2 {
		t.Errorf("短 key 应保留中英两行，实际 %d", kept)
	}
}

// 旧新并存但元数据冲突：整批回滚并给出可定位原因，不覆盖、不静默选择。
func TestArticleImportedUpgradeConflictRollsBack(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("首次完整 seed 失败：%v", err)
	}
	// 长 key 用不同译文：与已有的短 key 冲突，必须整批拒绝。
	if err := db.Exec(`INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES ('admin.article.ok.imported', 'zh-CN', '运营改过的旧译文', 200, 'admin', '冲突来源', 1, now(), now())`).Error; err != nil {
		t.Fatalf("构造冲突数据失败：%v", err)
	}
	err := migrations.RunSeeds(db)
	if err == nil {
		t.Fatal("旧新译文冲突时整批 seed 必须失败，实际成功")
	}
	if !strings.Contains(err.Error(), "admin.article.ok.imported") {
		t.Errorf("冲突错误应可定位到具体 key，实际：%v", err)
	}
	// 回滚语义：短 key 与被拒的长 key 都保持原样，没有被覆盖或删除。
	var shortValue string
	if err := db.Raw(`SELECT item_value FROM sys_i18n WHERE item_key = 'artImportOk' AND lang = 'zh-CN'`).Scan(&shortValue).Error; err != nil {
		t.Fatalf("复查短 key 失败：%v", err)
	}
	if shortValue != "页面草稿已创建。" {
		t.Errorf("冲突时不应改写短 key，实际 %q", shortValue)
	}
	var retired int64
	if err := db.Table("sys_i18n").Where("item_key = 'admin.article.ok.imported'").Count(&retired).Error; err != nil {
		t.Fatalf("复查旧 key 失败：%v", err)
	}
	if retired != 1 {
		t.Errorf("冲突时旧行应原地保留（等待人工处理），实际 %d 行", retired)
	}
}

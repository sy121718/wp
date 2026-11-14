package migrations_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"

	"gorm.io/gorm"
)

type productKeyRename struct {
	old, key string
	retire   bool
}

func productShortKeySeed(t *testing.T) (migrations.Seed, []productKeyRename) {
	t.Helper()
	for _, seed := range migrations.AllSeeds() {
		if seed.Version != "718-product-short-keys-i18n" {
			continue
		}
		var pairs []productKeyRename
		seen := map[string]bool{}
		for _, match := range regexp.MustCompile(`\('([^']+)', '([^']+)', (true|false)\)`).FindAllStringSubmatch(seed.SQL, -1) {
			if !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,14}$`).MatchString(match[2]) || seen[match[2]] {
				t.Fatalf("目标 key 格式非法或重复: %s", match[2])
			}
			seen[match[2]] = true
			pairs = append(pairs, productKeyRename{match[1], match[2], match[3] == "true"})
		}
		if len(pairs) != 149 {
			t.Fatalf("商品业务词条迁移范围改变: %d", len(pairs))
		}
		return seed, pairs
	}
	t.Fatal("商品短词条迁移未注册")
	return migrations.Seed{}, nil
}

func productShortKeySnapshot(t *testing.T, db *gorm.DB, pairs []productKeyRename) string {
	t.Helper()
	var keys []string
	for _, pair := range pairs {
		keys = append(keys, pair.old, pair.key)
	}
	var got string
	if err := db.Raw("SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY item_key, lang)::text, '[]') FROM sys_i18n t WHERE item_key IN ?", keys).Scan(&got).Error; err != nil {
		t.Fatal(err)
	}
	return got
}

func TestProductShortKeysCurrentSourcesPrepare(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	var sources, conditions []string
	for _, seed := range migrations.AllSeeds() {
		if strings.HasPrefix(seed.SQL, "-- 当前词条回补源；历史原文 ") {
			sources = append(sources, seed.SQL)
			conditions = append(conditions, seed.ConditionSQL)
		}
	}
	for _, migration := range migrations.All() {
		if strings.HasPrefix(migration.SQL, "-- 当前词条回补源；历史原文 ") {
			sources = append(sources, migration.SQL)
		}
	}
	// 405/407/720 的现行版切到 747c/747d/747e 后各多一份当前回补源，
	// 723x 退出执行、原文归档；758a 是 058 的新现行执行源（原 747a 转归档），
	// 758b 是 447 的新现行执行源（原文按「历史 SQL 不改」归档）。
	if len(sources) != 51 {
		t.Fatalf("当前回补源数量改变: %d", len(sources))
	}
	if err := db.Connection(func(conn *gorm.DB) error {
		for _, source := range append(sources, conditions...) {
			for _, stmt := range migrations.SplitStatements(source) {
				if err := conn.Exec("PREPARE product_current_source AS " + stmt).Error; err != nil {
					return err
				}
				if err := conn.Exec("DEALLOCATE product_current_source").Error; err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestProductShortKeysFreshAndUpgrade(t *testing.T) {
	seed, pairs := productShortKeySeed(t)
	db := support.NewMigratedPGTestDB(t)
	if err := db.Connection(func(conn *gorm.DB) error {
		for _, stmt := range migrations.SplitStatements(seed.SQL) {
			if err := conn.Exec("PREPARE product_short_key_seed AS " + stmt).Error; err != nil {
				return err
			}
			if err := conn.Exec("DEALLOCATE product_short_key_seed").Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatal(err)
	}
	for _, pair := range pairs {
		var count int64
		if err := db.Raw("SELECT COUNT(*) FROM sys_i18n WHERE item_key = ? AND lang IN ('zh-CN', 'en-US')", pair.key).Scan(&count).Error; err != nil || count != 2 {
			t.Fatalf("目标词条缺基础语言: %s: %d %v", pair.key, count, err)
		}
		if err := db.Raw("SELECT COUNT(*) FROM sys_i18n WHERE item_key = ?", pair.old).Scan(&count).Error; err != nil {
			t.Fatal(err)
		}
		if pair.retire && count != 0 || !pair.retire && count == 0 {
			t.Fatalf("旧 key 清理范围错误: %+v count=%d", pair, count)
		}
	}
	// 模拟升级前的运营译文及额外语言，包含禁用状态、备注和原始时刻。
	if err := db.Exec("UPDATE sys_i18n SET item_key = 'ErrVariantHasStock' WHERE item_key = 'pVarHasStock'").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE sys_i18n SET item_value = 'Custom stock message', category = 'operator', remark = 'kept', status = 0 WHERE item_key = 'ErrVariantHasStock' AND lang = 'en-US'").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time) VALUES ('ErrVariantHasStock', 'fr-FR', 'Stock personnalisé', 409, 'operator', 'extra language', 0, '2020-01-02', '2021-02-03')").Error; err != nil {
		t.Fatal(err)
	}
	var before string
	if err := db.Raw("SELECT jsonb_agg(to_jsonb(t) - 'item_key' ORDER BY lang)::text FROM sys_i18n t WHERE item_key = 'ErrVariantHasStock'").Scan(&before).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(seed.SQL).Error; err != nil {
		t.Fatal(err)
	}
	var after string
	if err := db.Raw("SELECT jsonb_agg(to_jsonb(t) - 'item_key' ORDER BY lang)::text FROM sys_i18n t WHERE item_key = 'pVarHasStock'").Scan(&after).Error; err != nil || before != after {
		t.Fatalf("升级改变了译文或元数据: %v\n%s\n%s", err, before, after)
	}
	before = productShortKeySnapshot(t, db, pairs)
	for round := 0; round < 2; round++ {
		if err := migrations.RunSeeds(db); err != nil {
			t.Fatal(err)
		}
		if after = productShortKeySnapshot(t, db, pairs); before != after {
			t.Fatalf("完整 RunSeeds 第 %d 次重跑改变了终态", round+1)
		}
	}
}

func TestProductShortKeysPartialBackfillPreservesCompletedRows(t *testing.T) {
	_, pairs := productShortKeySeed(t)
	db := support.NewMigratedPGTestDB(t)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "UPDATE sys_i18n SET item_value = 'Custom range message', category = 'operator', remark = 'keep', status = 0 WHERE item_key = 'pPriceRangeBad' AND lang = 'en-US'")
	mustExec(t, db, "INSERT INTO sys_i18n (item_key, lang, item_value, category, remark, status) VALUES ('pPriceRangeBad', 'fr-FR', 'Plage personnalisée', 'operator', 'keep', 0)")
	for _, key := range []string{"pPriceCostMiss", "pTagArgReq", "prAtomicHint"} {
		mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key = ? AND lang = 'en-US'", key)
		before := productShortKeySnapshot(t, db, pairs)
		if err := migrations.RunSeeds(db); err != nil {
			t.Fatalf("缺少 %s/en-US 时完整回补失败: %v", key, err)
		}
		var count int64
		if err := db.Raw("SELECT COUNT(*) FROM sys_i18n WHERE item_key = ? AND lang = 'en-US'", key).Scan(&count).Error; err != nil || count != 1 {
			t.Fatalf("没有回补 %s/en-US: %d %v", key, count, err)
		}
		for _, pair := range pairs {
			if pair.retire {
				if err := db.Raw("SELECT COUNT(*) FROM sys_i18n WHERE item_key = ?", pair.old).Scan(&count).Error; err != nil || count != 0 {
					t.Fatalf("回补重建了退役 key %s: %d %v", pair.old, count, err)
				}
			}
		}
		// 去掉刚回补的一行后，其他行（包括 ID、译文、元数据和时刻）必须完全不变。
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("DELETE FROM sys_i18n WHERE item_key = ? AND lang = 'en-US'", key).Error; err != nil {
				return err
			}
			if after := productShortKeySnapshot(t, tx, pairs); after != before {
				t.Error("缺词回补改变了其他已完成词条")
			}
			return fmt.Errorf("只比较快照，不保留测试删除")
		}); err == nil {
			t.Fatal("比较快照的事务应回滚")
		}
	}
}

func TestProductShortKeysConflictRollsBackWholeBatch(t *testing.T) {
	seed, pairs := productShortKeySeed(t)
	db := support.NewMigratedPGTestDB(t)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE sys_i18n SET item_key = 'ErrSkuTaken' WHERE item_key = 'pSkuTaken'").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO sys_i18n (item_key, lang, item_value) VALUES ('pSkuTaken', 'en-US', 'Unrelated target')").Error; err != nil {
		t.Fatal(err)
	}
	before := productShortKeySnapshot(t, db, pairs)
	err := db.Exec(seed.SQL).Error
	if err == nil || !strings.Contains(err.Error(), "ErrSkuTaken -> pSkuTaken/en-US") {
		t.Fatalf("冲突必须拒绝并定位旧 key、新 key 和语言: %v", err)
	}
	if after := productShortKeySnapshot(t, db, pairs); after != before {
		t.Fatal("冲突后发生了部分词条迁移")
	}
	var complete int64
	if err := db.Raw(seed.ConditionSQL).Scan(&complete).Error; err != nil || complete != 0 {
		t.Fatal(fmt.Sprintf("冲突批次不能标为完成: %d %v", complete, err))
	}
}

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

func productTranslationShortKeySeed(t *testing.T) (migrations.Seed, []productKeyRename) {
	t.Helper()
	for _, seed := range migrations.AllSeeds() {
		if seed.Version != "722-product-translation-short-keys-i18n" {
			continue
		}
		var pairs []productKeyRename
		seen := map[string]bool{}
		for _, match := range regexp.MustCompile(`\('([^']+)', '([^']+)', (true|false)\)`).FindAllStringSubmatch(seed.SQL, -1) {
			if !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,14}$`).MatchString(match[2]) || seen[match[2]] {
				t.Fatalf("商品翻译 目标 key 格式非法或重复: %s", match[2])
			}
			seen[match[2]] = true
			pairs = append(pairs, productKeyRename{match[1], match[2], match[3] == "true"})
		}
		if len(pairs) != 30 {
			t.Fatalf("商品翻译 词条迁移范围改变: %d", len(pairs))
		}
		_, businessPairs := productShortKeySeed(t)
		_, seoPairs := productSEOShortKeySeed(t)
		_, taxonomyPairs := productTaxonomyShortKeySeed(t)
		_, tagPairs := productTagShortKeySeed(t)
		for _, pair := range append(append(append(businessPairs, seoPairs...), taxonomyPairs...), tagPairs...) {
			if seen[pair.key] {
				t.Fatalf("商品翻译 与已迁词条撞名: %s", pair.key)
			}
		}
		return seed, pairs
	}
	t.Fatal("商品 商品翻译 短词条迁移未注册")
	return migrations.Seed{}, nil
}

func productTranslationShortKeySnapshot(t *testing.T, db *gorm.DB, pairs []productKeyRename) string {
	t.Helper()
	return productShortKeySnapshot(t, db, append(pairs, productKeyRename{key: "trScopeInfo"}, productKeyRename{key: "trProgress"}, productKeyRename{key: "trGroupInfo"}, productKeyRename{key: "trNoTargetDesc"}, productKeyRename{key: "trSourceOnly"}))
}

func TestProductTranslationShortKeysPrepareFreshAndUpgrade(t *testing.T) {
	seed, pairs := productTranslationShortKeySeed(t)
	db := support.NewMigratedPGTestDB(t)
	if err := db.Connection(func(conn *gorm.DB) error {
		for _, sql := range append(migrations.SplitStatements(seed.SQL), seed.ConditionSQL) {
			if err := conn.Exec("PREPARE product_translation_short_key AS " + sql).Error; err != nil {
				return err
			}
			if err := conn.Exec("DEALLOCATE product_translation_short_key").Error; err != nil {
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
			t.Fatalf("商品翻译 词条缺基础语言: %s: %d %v", pair.key, count, err)
		}
		if pair.retire {
			if err := db.Raw("SELECT COUNT(*) FROM sys_i18n WHERE item_key = ?", pair.old).Scan(&count).Error; err != nil || count != 0 {
				t.Fatalf("仍存在退役 商品翻译 key: %s: %d %v", pair.old, count, err)
			}
		}
	}
	for key, parameter := range map[string]string{"trProgress": "{done}", "trGroupInfo": "{count}", "trSourceOnly": "{lang}"} {
		for _, lang := range []string{"zh-CN", "en-US"} {
			var value string
			if err := db.Raw("SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = ?", key, lang).Scan(&value).Error; err != nil || !strings.Contains(value, parameter) {
				t.Fatalf("商品翻译完整句缺少语言或具名参数: %s: %q %v", lang, value, err)
			}
		}
	}
	// 升级同时覆盖两个原位改名来源；旧商品翻译碎片译文仅留作数据，不再用于拼句。
	for _, tc := range []struct {
		old, key string
	}{
		{"admin.product_translations.title", "trTitle"},
		{"admin.product_translations.col.field", "trField"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			mustExec(t, db, "UPDATE sys_i18n SET item_key = ? WHERE item_key = ?", tc.old, tc.key)
			mustExec(t, db, "UPDATE sys_i18n SET item_value = 'Custom tag title', category = 'operator', remark = 'kept', status = 0 WHERE item_key = ? AND lang = 'en-US'", tc.old)
			mustExec(t, db, "INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time) VALUES (?, 'fr-FR', 'Titre des étiquettes', 409, 'operator', 'extra', 0, '2020-01-02', '2021-02-03')", tc.old)
			columns := "to_jsonb(t) - 'item_key'"
			query := "SELECT jsonb_agg(" + columns + " ORDER BY lang)::text FROM sys_i18n t WHERE item_key = ?"
			var before, after string
			if err := db.Raw(query, tc.old).Scan(&before).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec(seed.SQL).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Raw(query, tc.key).Scan(&after).Error; err != nil || before != after {
				t.Fatalf("升级改变了译文或元数据: %v\n%s\n%s", err, before, after)
			}
		})
	}
	before := productTranslationShortKeySnapshot(t, db, pairs)
	for round := 0; round < 2; round++ {
		if err := migrations.RunSeeds(db); err != nil {
			t.Fatal(err)
		}
		if after := productTranslationShortKeySnapshot(t, db, pairs); before != after {
			t.Fatalf("商品翻译 完整 seed 第 %d 次重跑改变终态", round+1)
		}
	}
}

func TestProductTranslationShortKeysBackfillDoesNotRecreateRetiredKeys(t *testing.T) {
	_, pairs := productTranslationShortKeySeed(t)
	db := support.NewMigratedPGTestDB(t)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "UPDATE sys_i18n SET item_value = 'Custom title', category = 'operator', remark = 'keep', status = 0 WHERE item_key = 'trTitle' AND lang = 'en-US'")
	mustExec(t, db, "UPDATE sys_i18n SET item_value = 'Custom {done} / {total} entries', category = 'operator', remark = 'keep', status = 0 WHERE item_key = 'trProgress' AND lang = 'en-US'")
	mustExec(t, db, "INSERT INTO sys_i18n (item_key, lang, item_value, category, remark, status) VALUES ('trProgress', 'fr-FR', '{done} / {total} traductions', 'operator', 'extra', 0)")
	for _, key := range []string{"trTitle", "trField", "trStateDone", "trSourceOnly", "trProgress", "trGroupInfo", "trNoTargetDesc"} {
		mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key = ? AND lang = 'zh-CN'", key)
		before := productTranslationShortKeySnapshot(t, db, pairs)
		if err := migrations.RunSeeds(db); err != nil {
			t.Fatalf("缺少 %s/zh-CN 时回补失败: %v", key, err)
		}
		var count int64
		if err := db.Raw("SELECT COUNT(*) FROM sys_i18n WHERE item_key = ? AND lang = 'zh-CN'", key).Scan(&count).Error; err != nil || count != 1 {
			t.Fatalf("没有回补 %s/zh-CN: %d %v", key, count, err)
		}
		for _, pair := range pairs {
			if pair.retire {
				if err := db.Raw("SELECT COUNT(*) FROM sys_i18n WHERE item_key = ?", pair.old).Scan(&count).Error; err != nil || count != 0 {
					t.Fatalf("回补重建了退役 key %s: %d %v", pair.old, count, err)
				}
			}
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("DELETE FROM sys_i18n WHERE item_key = ? AND lang = 'zh-CN'", key).Error; err != nil {
				return err
			}
			if after := productTranslationShortKeySnapshot(t, tx, pairs); before != after {
				t.Error("商品翻译 回补改变了其他词条")
			}
			return fmt.Errorf("回滚比较用删除")
		}); err == nil {
			t.Fatal("比较事务应回滚")
		}
	}
}

func TestProductTranslationShortKeysConflictRollsBackWholeBatch(t *testing.T) {
	seed, pairs := productTranslationShortKeySeed(t)
	db := support.NewMigratedPGTestDB(t)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "UPDATE sys_i18n SET item_key = 'admin.product_translations.title' WHERE item_key = 'trTitle'")
	mustExec(t, db, "UPDATE sys_i18n SET item_key = 'admin.product_translations.list.title' WHERE item_key = 'trField'")
	mustExec(t, db, "INSERT INTO sys_i18n (item_key, lang, item_value) VALUES ('trTitle', 'en-US', 'Unrelated target')")
	// 新完整句也必须受同一冲突事务保护，不能在改名失败时单独落地。
	mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key = 'trProgress' AND lang = 'en-US'")
	before := productTranslationShortKeySnapshot(t, db, pairs)
	err := db.Exec(seed.SQL).Error
	if err == nil || !strings.Contains(err.Error(), "admin.product_translations.title -> trTitle/en-US") {
		t.Fatalf("冲突必须拒绝并定位: %v", err)
	}
	if after := productTranslationShortKeySnapshot(t, db, pairs); after != before {
		t.Fatal("冲突后发生部分迁移")
	}
	var complete int64
	if err := db.Raw(seed.ConditionSQL).Scan(&complete).Error; err != nil || complete != 0 {
		t.Fatalf("冲突批次不能标为完成: %d %v", complete, err)
	}
}

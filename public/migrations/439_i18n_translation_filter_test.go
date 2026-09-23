package migrations_test

import (
	"strings"
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

func TestTranslationFilterSeedBilingualAndIdempotent(t *testing.T) {
	const version = "439-i18n-translation-filter"
	var seed migrations.Seed
	for _, item := range migrations.AllSeeds() {
		if item.Version == version {
			seed = item
			break
		}
	}
	if seed.Version == "" {
		t.Fatal("439 译文筛选种子未注册")
	}
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	for round := 1; round <= 2; round++ {
		if err := migrations.RunSeeds(db); err != nil {
			t.Fatalf("第 %d 次执行种子失败: %v", round, err)
		}
		var applied int64
		if err := db.Raw(seed.ConditionSQL).Scan(&applied).Error; err != nil {
			t.Fatal(err)
		}
		if applied != 1 {
			t.Fatalf("第 %d 次执行后应判定已应用", round)
		}
		var count int64
		if err := db.Raw("SELECT COUNT(*) FROM sys_i18n WHERE lang IN ('zh-CN','en-US') AND item_key IN (" +
			"'admin.article.translations.filterKeyword','admin.article.translations.filterPlaceholder'," +
			"'admin.article.translations.noMatch.title','admin.article.translations.noMatch.desc'," +
			"'admin.article.translations.noMatch.action','admin.article.translations.empty.action'," +
			"'admin.navigation_translations.filter_keyword','admin.navigation_translations.filter_placeholder'," +
			"'admin.navigation_translations.no_project.title','admin.navigation_translations.no_project.desc'," +
			"'admin.navigation_translations.no_project.action','admin.navigation_translations.no_match.title'," +
			"'admin.navigation_translations.no_match.desc','admin.navigation_translations.no_match.action'," +
			"'admin.navigation_translations.empty.action')").Scan(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 30 {
			t.Fatalf("新增 15 键应各有中英文一行，实际 %d", count)
		}
	}
	var value string
	if err := db.Raw("SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = ?", "admin.navigation_translations.no_match.title", "en-US").Scan(&value).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(value, "matching") {
		t.Fatalf("英文词条读取异常：%q", value)
	}
}

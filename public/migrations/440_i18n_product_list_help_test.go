package migrations_test

import (
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

func TestProductListHelpSeedUpdatesOnlyHistoricalValues(t *testing.T) {
	var old, fix migrations.Seed
	for _, seed := range migrations.AllSeeds() {
		switch seed.Version {
		case "233-i18n-seed-product-detail":
			old = seed
		case "440-i18n-product-list-help":
			fix = seed
		}
	}
	if old.Version == "" || fix.Version == "" {
		t.Fatalf("商品说明 seed 缺失: 233=%q, 440=%q", old.Version, fix.Version)
	}

	for _, tc := range []struct {
		name, customKey, customLang string
	}{
		{name: "historical values"},
		{name: "custom Chinese tail", customKey: "admin.products.hint.detailTail", customLang: "zh-CN"},
		{name: "custom English tail", customKey: "admin.products.hint.detailTail", customLang: "en-US"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := support.NewMigratedPGTestDB(t)
			if db == nil {
				return
			}
			for _, stmt := range migrations.SplitStatements(old.SQL) {
				if err := db.Exec(stmt).Error; err != nil {
					t.Fatalf("初始化历史词条: %v", err)
				}
			}
			const key = "admin.products.hint.detailTail"
			const custom = "Operator-defined product help"
			if tc.customKey != "" {
				if err := db.Exec("UPDATE sys_i18n SET item_value = ? WHERE item_key = ? AND lang = ?", custom, tc.customKey, tc.customLang).Error; err != nil {
					t.Fatal(err)
				}
			}
			var applied int
			if err := db.Raw(fix.ConditionSQL).Scan(&applied).Error; err != nil || applied != 0 {
				t.Fatalf("历史值仍存在时判定应执行: %d / %v", applied, err)
			}
			want := map[string]map[string]string{
				key: {
					"zh-CN": "页维护（点行的「编辑」进入）。评分是独立明细：平均值与条数由明细算出，改评分不改动商品字段；「没有评分」与「评分 0 分」是两回事。",
					"en-US": " page (open it via Edit in the row). Ratings live in their own detail table: the average and count are derived from it, and editing a rating never touches the product's own fields. No ratings and rated 0 are different things.",
				},
				"admin.products.hint.detailLead": {
					"zh-CN": "变体与评分属于单个商品，在商品的",
					"en-US": "Variants and ratings belong to a single product and are maintained on that product's",
				},
			}
			if tc.customKey != "" {
				want[tc.customKey][tc.customLang] = custom
			}
			for run := 0; run < 2; run++ {
				for _, stmt := range migrations.SplitStatements(fix.SQL) {
					if err := db.Exec(stmt).Error; err != nil {
						t.Fatalf("执行 440: %v", err)
					}
				}
				for itemKey, langs := range want {
					for lang, expected := range langs {
						var got string
						if err := db.Raw("SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = ?", itemKey, lang).Scan(&got).Error; err != nil {
							t.Fatal(err)
						}
						if got != expected {
							t.Errorf("第 %d 次执行后 %s/%s = %q，期望 %q", run+1, itemKey, lang, got, expected)
						}
					}
				}
				if err := db.Raw(fix.ConditionSQL).Scan(&applied).Error; err != nil || applied != 1 {
					t.Fatalf("执行后应跳过: %d / %v", applied, err)
				}
			}
			if err := migrations.RunSeeds(db); err != nil {
				t.Fatalf("重复运行全部 seed: %v", err)
			}
			for itemKey, langs := range want {
				for lang, expected := range langs {
					var got string
					if err := db.Raw("SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = ?", itemKey, lang).Scan(&got).Error; err != nil || got != expected {
						t.Errorf("重跑全部 seed 后 %s/%s = %q，期望 %q，错误 %v", itemKey, lang, got, expected, err)
					}
				}
			}
		})
	}
}

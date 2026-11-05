package migrations_test

import (
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

const adminI18nBulkDeleteVersion = "708-admin-i18n-bulk-delete-i18n"

// 708 的两条错误词条，中英各一份 —— 这就是注册门槛要精确枚举的 4 个 (key, lang)。
var adminI18nBulkDeleteWords = map[string]map[string]string{
	"admin.err.i18nEntryMissing": {
		"zh-CN": "选中的词条已不存在，请刷新后重选",
		"en-US": "Some selected entries no longer exist. Refresh and pick again.",
	},
	"admin.err.i18nSelectionEmpty": {
		"zh-CN": "没有选中任何词条",
		"en-US": "No entries were selected.",
	},
}

func findAdminI18nBulkDeleteSeed(t *testing.T) migrations.Seed {
	t.Helper()
	for _, s := range migrations.AllSeeds() {
		if s.Version == adminI18nBulkDeleteVersion {
			return s
		}
	}
	t.Fatalf("迁移 %s 未注册（register.go 里漏了 registerAdminI18nBulkDeleteI18n？）", adminI18nBulkDeleteVersion)
	return migrations.Seed{}
}

// TestAdminI18nBulkDeleteSeedCoversBothLanguages 守注册门槛的精确性。
//
// 背景：这批词条的判据最初写成「数 zh 的两条」，于是 en 缺失时门槛照样成立、
// seed 被跳过，英文界面就退回归口文案。门槛必须精确枚举 4 个 (key, lang) 组合：
// 少任何一条（包括只少 en）都要判为「尚未完成」，让 seed 真的执行。
//
// 这里不启动整库 seed，只对 ConditionSQL 求值：它是门槛本身，要能独立证明。
func TestAdminI18nBulkDeleteSeedCoversBothLanguages(t *testing.T) {
	seed := findAdminI18nBulkDeleteSeed(t)

	cases := []struct {
		name  string
		drop  [][2]string // 模拟缺失的 (key, lang)
		want  int64
		label string
	}{
		{name: "四条全齐", drop: nil, want: 1, label: "已完成，跳过重跑"},
		{
			name:  "缺 en 的 EntryMissing",
			drop:  [][2]string{{"admin.err.i18nEntryMissing", "en-US"}},
			want:  0,
			label: "英文缺一条也要重跑（旧门槛只数 zh，会漏掉）",
		},
		{
			name:  "缺 en 的 SelectionEmpty",
			drop:  [][2]string{{"admin.err.i18nSelectionEmpty", "en-US"}},
			want:  0,
			label: "同上",
		},
		{
			name:  "两个 en 都缺",
			drop:  [][2]string{{"admin.err.i18nEntryMissing", "en-US"}, {"admin.err.i18nSelectionEmpty", "en-US"}},
			want:  0,
			label: "只剩中文两条 —— 这正是旧门槛判成完成的场景",
		},
		{
			name:  "缺 zh 的 EntryMissing",
			drop:  [][2]string{{"admin.err.i18nEntryMissing", "zh-CN"}},
			want:  0,
			label: "任一语言缺失都要重跑",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			db := support.NewMigratedPGTestDB(t)
			if db == nil {
				return
			}

			// 铺齐本批 4 条，再按用例删掉指定组合。
			for key, langs := range adminI18nBulkDeleteWords {
				for lang, value := range langs {
					if err := db.Exec(
						"INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time) "+
							"VALUES (?, ?, ?, 400, 'error', 'test', 1, now(), now()) "+
							"ON CONFLICT (item_key, lang) DO UPDATE SET item_value = EXCLUDED.item_value",
						key, lang, value).Error; err != nil {
						t.Fatalf("铺词条 %s/%s: %v", key, lang, err)
					}
				}
			}
			for _, d := range tc.drop {
				if err := db.Exec(
					"DELETE FROM sys_i18n WHERE item_key = ? AND lang = ?", d[0], d[1]).Error; err != nil {
					t.Fatalf("删除 %s/%s: %v", d[0], d[1], err)
				}
			}

			var applied int64
			if err := db.Raw(seed.ConditionSQL).Scan(&applied).Error; err != nil {
				t.Fatalf("判据求值失败: %v", err)
			}
			if applied != tc.want {
				t.Fatalf("门槛判据 = %d，期望 %d（%s）", applied, tc.want, tc.label)
			}
		})
	}
}

// TestAdminI18nBulkDeleteSeedWritesBothLanguages 验实际写入：
// 门槛成立时执行 SQL，四条 (key, lang) 都要落库，且 http_code 按真实语义给
// （EntryMissing 404 不存在 / SelectionEmpty 400 参数问题）。
func TestAdminI18nBulkDeleteSeedWritesBothLanguages(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	seed := findAdminI18nBulkDeleteSeed(t)

	for _, stmt := range migrations.SplitStatements(seed.SQL) {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("执行 708 失败: %v", err)
		}
	}

	wantCodes := map[string]int64{
		"admin.err.i18nEntryMissing":   404,
		"admin.err.i18nSelectionEmpty": 400,
	}
	for key, langs := range adminI18nBulkDeleteWords {
		for lang, want := range langs {
			var got string
			var code int64
			if err := db.Raw(
				"SELECT item_value, http_code FROM sys_i18n WHERE item_key = ? AND lang = ?",
				key, lang).Row().Scan(&got, &code); err != nil {
				t.Fatalf("读取 %s/%s: %v", key, lang, err)
			}
			if got != want {
				t.Errorf("%s/%s 文案 = %q，期望 %q", key, lang, got, want)
			}
			if code != wantCodes[key] {
				t.Errorf("%s/%s http_code = %d，期望 %d", key, lang, code, wantCodes[key])
			}
		}
	}

	// 幂等：再跑一次不报错、不产生重复行。
	for _, stmt := range migrations.SplitStatements(seed.SQL) {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("重复执行 708 失败（应幂等）: %v", err)
		}
	}
	var total int64
	if err := db.Raw(
		"SELECT COUNT(*) FROM sys_i18n WHERE item_key IN ('admin.err.i18nEntryMissing','admin.err.i18nSelectionEmpty')").
		Scan(&total).Error; err != nil {
		t.Fatal(err)
	}
	if total != 4 {
		t.Fatalf("重复执行后应仍为 4 行，实际 %d", total)
	}
}

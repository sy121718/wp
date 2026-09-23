package migrations_test

import (
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

func TestInventoryMovesHeadingSeedIsIdempotent(t *testing.T) {
	const version = "442-i18n-inventory-moves-heading"
	const key = "admin.inventory.moves.title"
	var seed migrations.Seed
	for _, item := range migrations.AllSeeds() {
		if item.Version == version {
			seed = item
			break
		}
	}
	if seed.Version == "" {
		t.Fatalf("种子 %s 未注册", version)
	}

	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	const custom = "Operator-defined stock ledger"
	if err := db.Exec("INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time) VALUES (?, ?, ?, 200, 'admin', 'operator', 1, now(), now())", key, "zh-CN", custom).Error; err != nil {
		t.Fatal(err)
	}

	for round := 1; round <= 3; round++ {
		var applied int
		wantBefore := 0
		if round == 3 {
			wantBefore = 1
		}
		if err := db.Raw(seed.ConditionSQL).Scan(&applied).Error; err != nil || applied != wantBefore {
			t.Fatalf("第 %d 轮执行前判定 = %d, 期望 %d, 错误 %v", round, applied, wantBefore, err)
		}
		if err := migrations.RunSeeds(db); err != nil {
			t.Fatalf("第 %d 轮运行 seed: %v", round, err)
		}
		if err := db.Raw(seed.ConditionSQL).Scan(&applied).Error; err != nil || applied != 1 {
			t.Fatalf("第 %d 轮补齐后判定 = %d, 错误 %v", round, applied, err)
		}
		for _, tc := range []struct{ lang, want string }{
			{"zh-CN", custom},
			{"en-US", "Stock ledger"},
		} {
			var values []string
			if err := db.Raw("SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = ?", key, tc.lang).Scan(&values).Error; err != nil {
				t.Fatal(err)
			}
			if len(values) != 1 || values[0] != tc.want {
				t.Errorf("第 %d 轮 %s = %v, 期望单条 %q", round, tc.lang, values, tc.want)
			}
		}
		// The next pass must repair a missing language without overwriting the operator's value.
		if round == 1 {
			if err := db.Exec("DELETE FROM sys_i18n WHERE item_key = ? AND lang = ?", key, "en-US").Error; err != nil {
				t.Fatal(err)
			}
		}
	}
}

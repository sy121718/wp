package migrations_test

import (
	"strings"
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

var retiredEnumKeys = []string{
	"MsgInternalError", "ErrInvalidParams", "ErrPermissionDenied",
	"ErrRequestEntityTooLarge", "msg_operation_success", "msg_delete_confirm",
}

// TestSeed058DoesNotPlantRetiredKeys 钉住「已退役 key 不再被播种」：
// 守门按本批行数判断，只要 058 里还留着退役行就会被重新种回去。
func TestSeed058DoesNotPlantRetiredKeys(t *testing.T) {
	var seed migrations.Seed
	for _, s := range migrations.AllSeeds() {
		if s.Version == "058-i18n-seed-enums" {
			seed = s
			break
		}
	}
	if seed.Version == "" {
		t.Fatal("058-i18n-seed-enums 未注册")
	}
	for _, key := range retiredEnumKeys {
		if strings.Contains(seed.SQL, "('"+key+"',") {
			t.Errorf("058 仍在播种退役 key %s —— 清理后的下一个新库又会把它种回来", key)
		}
	}
	if !strings.Contains(seed.ConditionSQL, "NOT EXISTS") {
		t.Error("058 的守门应显式要求退役 key 不存在，避免清理后反复重播")
	}
}

// TestRetiredEnumKeysStayGoneAfterSeeds 在真实迁移库上连续跑两遍：
// 第一遍写入并清理，第二遍不得把退役 key 种回来，本批词条必须齐全。
func TestRetiredEnumKeysStayGoneAfterSeeds(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	for round := 1; round <= 2; round++ {
		if err := migrations.RunSeeds(db); err != nil {
			t.Fatalf("第 %d 轮种子失败：%v", round, err)
		}
		var n int64
		if err := db.Raw(`SELECT COUNT(*) FROM sys_i18n WHERE item_key IN ?`, retiredEnumKeys).
			Scan(&n).Error; err != nil {
			t.Fatalf("查退役 key 失败：%v", err)
		}
		if n != 0 {
			t.Errorf("第 %d 轮后仍有 %d 行退役 key —— seed 或清理迁移失效", round, n)
		}
	}

	var uiOp int64
	if err := db.Raw(`SELECT COUNT(*) FROM sys_i18n WHERE item_key = 'uiOpSuccess'`).Scan(&uiOp).Error; err != nil {
		t.Fatalf("查新 key 失败：%v", err)
	}
	if uiOp == 0 {
		t.Error("替代 key uiOpSuccess 缺失：成功提示会退成裸 key")
	}
}

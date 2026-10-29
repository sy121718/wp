package migrations_test

import (
	"testing"

	"go_wp/public/test/support"
	"gorm.io/gorm"
)

func TestOrderBulkRejectedSeedPreparesAndReplays(t *testing.T) {
	seed := findSeedByVersion(t, "694-order-bulk-rejected-i18n")
	db := support.NewMigratedPGTestDB(t)
	if err := db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("PREPARE order_bulk_rejected_seed AS " + seed.SQL).Error; err != nil {
			return err
		}
		return conn.Exec("DEALLOCATE order_bulk_rejected_seed").Error
	}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := db.Exec(seed.SQL).Error; err != nil {
			t.Fatal(err)
		}
	}
	var ready int
	if err := db.Raw(seed.ConditionSQL).Scan(&ready).Error; err != nil || ready != 1 {
		t.Fatalf("批量拒绝双语词条不完整: %d %v", ready, err)
	}
	var rows []struct {
		Lang      string
		ItemValue string
	}
	if err := db.Table("sys_i18n").Select("lang, item_value").Where("item_key = ?", "order.err.bulkRejected").Order("lang").Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Lang != "en-US" || rows[0].ItemValue != "The batch was not executed. Correct the items that cannot be processed and try again." || rows[1].Lang != "zh-CN" || rows[1].ItemValue != "整批未执行：有条目不能处理，请按明细修正后重试" {
		t.Fatalf("词条与声明事实不一致: %+v", rows)
	}
}

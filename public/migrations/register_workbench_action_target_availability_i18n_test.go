package migrations_test

import (
	"testing"

	workbenchenums "go_wp/internal/module/workbench/enums"
	"go_wp/public/test/support"
	"gorm.io/gorm"
)

func TestWorkbenchActionTargetAvailabilitySeedPreparesAndReplays(t *testing.T) {
	seed := findSeedByVersion(t, "695a-workbench-action-target-availability-i18n")
	db := support.NewMigratedPGTestDB(t)
	if err := db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("PREPARE workbench_action_target_seed AS " + seed.SQL).Error; err != nil {
			return err
		}
		return conn.Exec("DEALLOCATE workbench_action_target_seed").Error
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
		t.Fatalf("动作目标六条双语词条不完整: %d %v", ready, err)
	}
	var value string
	if err := db.Table("sys_i18n").Select("item_value").Where("item_key = ? AND lang = ?", workbenchenums.ErrPreviewActionTargetUnavailable, "zh-CN").Scan(&value).Error; err != nil {
		t.Fatal(err)
	}
	if value != workbenchenums.FallbackFor(workbenchenums.ErrPreviewActionTargetUnavailable) {
		t.Fatalf("重选提示与声明处兜底不同: %q", value)
	}
}

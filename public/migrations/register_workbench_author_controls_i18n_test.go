package migrations_test

import (
	"testing"

	"go_wp/public/test/support"
	"gorm.io/gorm"
)

func TestWorkbenchAuthorControlsSeedPreparesAndReplays(t *testing.T) {
	seed := findSeedByVersion(t, "690-workbench-author-controls-i18n")
	db := support.NewMigratedPGTestDB(t)
	if err := db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("PREPARE workbench_author_seed AS " + seed.SQL).Error; err != nil {
			return err
		}
		return conn.Exec("DEALLOCATE workbench_author_seed").Error
	}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := db.Exec(seed.SQL).Error; err != nil {
			t.Fatal(err)
		}
		var complete int
		if err := db.Raw(seed.ConditionSQL).Scan(&complete).Error; err != nil || complete != 1 {
			t.Fatalf("作者界面双语词条不完整：%d %v", complete, err)
		}
	}
}

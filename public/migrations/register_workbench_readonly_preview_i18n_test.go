package migrations_test

import (
	"go_wp/public/test/support"
	"gorm.io/gorm"
	"testing"
)

func TestWorkbenchReadonlySeedPrepares(t *testing.T) {
	seed := findSeedByVersion(t, "685-workbench-readonly-preview-i18n")
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	if err := db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("PREPARE workbench_readonly_seed AS " + seed.SQL).Error; err != nil {
			return err
		}
		return conn.Exec("DEALLOCATE workbench_readonly_seed").Error
	}); err != nil {
		t.Fatal(err)
	}
}

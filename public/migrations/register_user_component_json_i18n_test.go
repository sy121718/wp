package migrations_test

import (
	"go_wp/public/test/support"
	"gorm.io/gorm"
	"testing"
)

func TestUserComponentJSONSeedPrepares(t *testing.T) {
	seed := findSeedByVersion(t, "681-user-component-json-i18n")
	db := support.NewMigratedPGTestDB(t)
	if err := db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("PREPARE user_component_json_seed AS " + seed.SQL).Error; err != nil {
			return err
		}
		return conn.Exec("DEALLOCATE user_component_json_seed").Error
	}); err != nil {
		t.Fatal(err)
	}
}

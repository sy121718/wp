package migrations_test

import (
	"testing"

	"go_wp/public/test/support"
	"gorm.io/gorm"
)

func TestUserCodeFlowSeedPrepares(t *testing.T) {
	seed := findSeedByVersion(t, "687-user-code-flow-i18n")
	db := support.NewMigratedPGTestDB(t)
	if err := db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("PREPARE user_code_flow_seed AS " + seed.SQL).Error; err != nil {
			return err
		}
		return conn.Exec("DEALLOCATE user_code_flow_seed").Error
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(seed.SQL).Error; err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Raw(seed.ConditionSQL).Scan(&count).Error; err != nil || count != 1 {
		t.Fatalf("双语验证码词条未完整落库：%d %v", count, err)
	}
}

package migrations_test

import (
	"go_wp/public/test/support"
	"gorm.io/gorm"
	"testing"
)

func TestAddToCartUnavailableSeedPrepares(t *testing.T) {
	seed := findSeedByVersion(t, "693-add-to-cart-unavailable-i18n")
	db := support.NewMigratedPGTestDB(t)
	if err := db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("PREPARE add_to_cart_unavailable_seed AS " + seed.SQL).Error; err != nil {
			return err
		}
		return conn.Exec("DEALLOCATE add_to_cart_unavailable_seed").Error
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(seed.SQL).Error; err != nil {
		t.Fatal(err)
	}
	var ready int
	if err := db.Raw(seed.ConditionSQL).Scan(&ready).Error; err != nil || ready != 1 {
		t.Fatalf("禁用态词条不完整: %d %v", ready, err)
	}
}

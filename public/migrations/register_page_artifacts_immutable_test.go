package migrations_test

import (
	"go_wp/public/migrations"
	"go_wp/public/test/support"
	"gorm.io/gorm"
	"testing"
)

func TestPageArtifactImmutableMigrationIsIdempotent(t *testing.T) {
	var migration migrations.Migration
	for _, m := range migrations.All() {
		if m.Version == "691-page-artifacts-immutable" {
			migration = m
			break
		}
	}
	if migration.SQL == "" {
		t.Fatal("缺少不可变产物生产迁移")
	}
	db := support.NewMigratedPGTestDB(t)
	if err := db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("PREPARE artifact_immutable_check AS " + migration.CheckSQL).Error; err != nil {
			return err
		}
		return conn.Exec("DEALLOCATE artifact_immutable_check").Error
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := db.Exec(migration.SQL).Error; err != nil {
			t.Fatal(err)
		}
	}
	var ready int
	if err := db.Raw(migration.CheckSQL).Scan(&ready).Error; err != nil || ready != 1 {
		t.Fatalf("不可变身份约束未完整落地: %d %v", ready, err)
	}
}

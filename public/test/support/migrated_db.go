package support

import (
	"testing"

	"gorm.io/gorm"
)

func NewMigratedPGTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := newTestDatabase(t, localPGEndpoint(), true, false)
	if err != nil {
		FailIfRequiredPG(t, err)
		t.Skipf("本地 PostgreSQL 不可用，跳过测试：%v", err)
		return nil
	}
	return db
}

func NewMigratedPGTestDBTranslateError(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := newTestDatabase(t, localPGEndpoint(), true, true)
	if err != nil {
		FailIfRequiredPG(t, err)
		t.Skipf("本地 PostgreSQL 不可用，跳过测试：%v", err)
		return nil
	}
	return db
}

func SeedProjectRow(t *testing.T, db *gorm.DB, id, name string) {
	t.Helper()
	if name == "" {
		name = "测试站点"
	}
	if err := db.Exec(
		"INSERT INTO projects (id, name, settings, create_time, update_time) VALUES (?, ?, '{}'::jsonb, NOW(), NOW())",
		id, name,
	).Error; err != nil {
		t.Fatalf("准备工程行失败：%v", err)
	}
}

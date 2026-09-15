package support

import (
	"testing"

	"gorm.io/gorm"

	"go_wp/public/migrations"
)

// NewMigratedPGTestDB 建隔离 schema 并**跑生产迁移**建表，返回可直接使用的库。
//
// 业务链路测试一律从这里拿库，不要再手抄 CREATE TABLE：手抄的表结构与生产 schema
// 会静默分叉。实测过两次代价：publication 用例手抄的 publication_receipts 停在
// uuid + create_time，而生产迁移已改成 bigint + create_time，DB-019/020 批次推进时
// 该包 4 个用例整片变红，失败信息还被读成「迁移把库改坏了」。
//
// 迁移建的是完整 106 张表，测试只需再补真实父行（见 SeedProjectRow）。
// PG 不可用时 t.Skip，与 NewPGTestDB 行为一致。
func NewMigratedPGTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用，跳过测试：%v", err)
		return nil
	}
	if err := migrations.Run(db); err != nil {
		t.Fatalf("执行生产迁移失败：%v", err)
	}
	return db
}

// SeedProjectRow 插入一条真实工程行。
//
// 多数业务表都有 project_id → projects(id) 的外键（page_routes、themes、pages …），
// 真实迁移建表后只补这一行就能让测试跑起来；user_id / page_id 之类按需在各测试内补。
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

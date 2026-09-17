package support

import (
	"testing"

	"gorm.io/gorm"
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
	// 复制「跑完生产迁移的模板库」，而不是每次新建空库再跑一遍迁移：
	// 结构同样只由生产迁移产生（模板库就是这么建出来的），但每个用例约 65ms 而非约 1.1s。
	db, err := newTestDatabase(t, localPGEndpoint(), true, false)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用，跳过测试：%v", err)
		return nil
	}
	return db
}

// NewMigratedPGTestDBTranslateError 与 NewMigratedPGTestDB 相同，但按生产配置打开连接
// （gorm.Config{TranslateError:true}，见 pkg/database）：service 里依赖
// gorm.ErrDuplicatedKey 归一化唯一键冲突的分支只在这时命中 —— 默认连接返回原始 PG 23505
// （SQLSTATE 23505），errors.Is 判假，冲突会被当成未知持久化错误往上抛。
// 样板消费者：artifact service 的 mapPersistenceError。
func NewMigratedPGTestDBTranslateError(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := newTestDatabase(t, localPGEndpoint(), true, true)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用，跳过测试：%v", err)
		return nil
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

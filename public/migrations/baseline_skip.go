package migrations

import (
	"strings"

	baseline "go_wp/schema/baseline"

	"gorm.io/gorm"
)

// baselineSkipSet 返回基线已覆盖、可以跳过的迁移 version 集合。
//
// 判定链（任一不满足即回落全量迁移，这是安全默认）：
//  1. 仓库 manifest 非空；
//  2. 迁移库里有 sys_baseline 表与最新一行；
//  3. 该行 manifest_sha256 与仓库 manifest 的 SHA-256 一致。
//
// 为什么比哈希而不是比 version 列表：manifest、structure.sql 与 system_data.sql
// 是同一批产物，哈希一致才说明「这个库确实是按当前产物载入的基线」。只比 version
// 的话，基线 SQL 内容变了也会被误判成已覆盖，跳过的迁移就永远不会补上。
func baselineSkipSet(db *gorm.DB) map[string]bool {
	versions := baseline.Versions()
	if len(versions) == 0 {
		return nil
	}
	if !db.Migrator().HasTable("sys_baseline") {
		return nil
	}
	var row struct {
		ManifestSHA256 string `gorm:"column:manifest_sha256"`
	}
	if err := db.Raw("SELECT manifest_sha256 FROM sys_baseline ORDER BY applied_at DESC, id DESC LIMIT 1").Scan(&row).Error; err != nil {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(row.ManifestSHA256), baseline.ManifestSHA256()) {
		return nil
	}
	set := make(map[string]bool, len(versions))
	for _, v := range versions {
		set[v] = true
	}
	return set
}

package migrations

import (
	"fmt"
	"strings"

	baseline "go_wp/schema/baseline"

	"gorm.io/gorm"
)

// baselineState 返回基线覆盖的迁移与 seed 集合。
//
// 三种情形：
//   - 无 manifest / 无 sys_baseline 表 / 无水位行：nil, nil, nil —— 新库照常走全量；
//   - 水位摘要与产物内容摘要一致：返回两段集合，段内全部跳过，段外的新版本照常执行；
//   - 水位存在但与当前产物不一致：返回错误。
//
// 第三种**不回落全量**。历史迁移假定从空库开始：164 建的索引已被 223 DROP、它引用的
// updated_at 已被 201/205 改名，在已有终态结构的库上重放必然执行到一半报
// column "updated_at" does not exist。基线内容变了意味着迁移链的形状变了，
// 正确处置是在新库重建，或人工核对后删掉水位行 —— 而不是静默把 180 多条历史迁移
// 重新跑在一个已经不是空库的库上。
//
// 只比 version 清单不够：基线 SQL 内容变了、清单没变时会被误判成已覆盖，
// 跳过的迁移就永远不会补上。
func baselineState(db *gorm.DB) (map[string]bool, map[string]bool, error) {
	manifest := baseline.ParseManifest()
	if len(manifest.Migrations) == 0 || len(manifest.Seeds) == 0 {
		return nil, nil, nil
	}
	if !db.Migrator().HasTable("sys_baseline") {
		return nil, nil, nil
	}
	var row struct {
		Version        string `gorm:"column:version"`
		ManifestSHA256 string `gorm:"column:manifest_sha256"`
	}
	if err := db.Raw("SELECT version, manifest_sha256 FROM sys_baseline ORDER BY applied_at DESC, id DESC LIMIT 1").Scan(&row).Error; err != nil {
		return nil, nil, nil
	}
	recorded := strings.TrimSpace(row.ManifestSHA256)
	if recorded == "" {
		return nil, nil, nil
	}
	want := baseline.ContentSHA256()
	if !strings.EqualFold(recorded, want) {
		return nil, nil, fmt.Errorf(
			"基线产物已变更：库登记 %s（%s），仓库产物 %s。历史迁移不能在已有结构的库上重放；请在新库重建，或核对后删除 sys_baseline 水位行再迁移",
			shortDigest(recorded), row.Version, shortDigest(want))
	}
	return toSet(manifest.Migrations), toSet(manifest.Seeds), nil
}

func shortDigest(sum string) string {
	if len(sum) <= 8 {
		return sum
	}
	return sum[:8]
}

func toSet(versions []string) map[string]bool {
	set := make(map[string]bool, len(versions))
	for _, v := range versions {
		set[v] = true
	}
	return set
}

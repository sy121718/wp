package migrations

import (
	"fmt"
	"strings"

	baseline "go_wp/schema/baseline"

	"gorm.io/gorm"
)

// baselineState 返回基线覆盖的迁移与 seed 集合。
//
// 四种情形：
//   - 无 manifest / 无 sys_baseline 表 / 表在但水位为空：nil, nil, nil —— 新库与未接入的旧库照常走全量；
//   - 水位摘要与产物内容摘要一致：返回两段集合，段内全部跳过，段外的新版本照常执行；
//   - 水位存在但与当前产物不一致：返回错误；
//   - 水位查询本身失败：返回错误（不当作「没有基线」）。
//
// 第三、四种都**不回落全量**。历史迁移假定从空库开始：164 建的索引已被 223 DROP、它引用的
// updated_at 已被 201/205 改名，在已有终态结构的库上重放必然执行到一半报
// column "updated_at" does not exist。基线内容变了意味着迁移链的形状变了，
// 正确处置是用 baseline-adopt.sh 核验追平后刷新水位 —— 而不是静默把历史迁移
// 重新跑在一个已经不是空库的库上，也不是删掉水位行试跑。
//
// 只比 version 清单不够：基线 SQL 内容变了、清单没变时会被误判成已覆盖，
// 跳过的迁移就永远不会补上。
func baselineState(db *gorm.DB) (map[string]bool, map[string]bool, error) {
	manifest := baseline.ParseManifest()
	if len(manifest.Migrations) == 0 || len(manifest.Seeds) == 0 {
		return nil, nil, nil
	}
	// 存在性必须能与「查询失败」区分：HasTable 只有 bool，查不动时返回 false，
	// 已有结构的库就会被当成新库重新进入历史迁移链。
	var exists bool
	if err := db.Raw("SELECT to_regclass('public.sys_baseline') IS NOT NULL").Scan(&exists).Error; err != nil {
		return nil, nil, fmt.Errorf("无法确认 sys_baseline 是否存在：%w", err)
	}
	if !exists {
		return nil, nil, nil
	}
	var rows []struct {
		Version        string `gorm:"column:version"`
		ManifestSHA256 string `gorm:"column:manifest_sha256"`
	}
	if err := db.Raw("SELECT version, manifest_sha256 FROM sys_baseline ORDER BY applied_at DESC, id DESC LIMIT 1").Scan(&rows).Error; err != nil {
		return nil, nil, fmt.Errorf("读取 sys_baseline 水位失败：%w", err)
	}
	if len(rows) == 0 {
		// 表在、水位空：迁移跑过但从未接入基线（旧库升级路径），照常走全量。
		return nil, nil, nil
	}
	row := rows[0]
	recorded := strings.TrimSpace(row.ManifestSHA256)
	if recorded == "" {
		return nil, nil, nil
	}
	want := baseline.ContentSHA256()
	if !strings.EqualFold(recorded, want) {
		return nil, nil, fmt.Errorf(
			"基线产物已变更：库登记 %s（%s），仓库产物 %s。历史迁移不能在已有结构的库上重放；先核对差异，用 scripts/baseline-adopt.sh --db <库> --refresh --apply 追平结构后刷新水位 —— 直接删除水位行再跑迁移会半路失败",
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

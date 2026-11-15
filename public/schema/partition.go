package schema

import (
	_ "embed"

	pkgpartition "go_wp/pkg/database/partition"
)

// partitionRLSSQL 把父表策略逐字段复制到全部叶子分区；由启动期分区维护执行。
// 它不参与 Digest：这是运行期对账，不是结构产物。
//
//go:embed partition_rls.sql
var partitionRLSSQL string

// PartitionOptions 返回本应用的分区表目录与隔离对账 SQL。
// 通用分区机制在 pkg/database/partition；这里只声明本仓的三张分区父表。
func PartitionOptions() pkgpartition.Options {
	return pkgpartition.Options{
		Tables: []pkgpartition.Table{
			{Name: "page_views", TimeColumn: "viewed_at"},
			{Name: "inventory_stock_movements", TimeColumn: "create_time"},
			{Name: "master_data_changes", TimeColumn: "create_time"},
		},
		ReconcileRLSSQL: func() string { return partitionRLSSQL },
	}
}

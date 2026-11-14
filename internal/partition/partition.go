package partition

import (
	"context"
	"time"

	"gorm.io/gorm"

	pkgpartition "go_wp/pkg/database/partition"
	migrations "go_wp/public/migrations"
)

// Tables 是应用的表目录（保留在生产迁移所属的应用侧）。
var Tables = []pkgpartition.Table{
	{Name: "page_views", TimeColumn: "viewed_at"},
	{Name: "inventory_stock_movements", TimeColumn: "created_at"},
	{Name: "master_data_changes", TimeColumn: "created_at"},
}

const AheadMonths = pkgpartition.AheadMonths

// DefaultOptions 返回应用注入的选项：表目录与 RLS 对账 SQL。
func DefaultOptions() pkgpartition.Options {
	return pkgpartition.Options{
		Tables:          Tables,
		ReconcileRLSSQL: migrations.ReconcilePartitionRLSSQL,
	}
}

func EnsureAhead(ctx context.Context, db *gorm.DB, aheadMonths int) ([]string, error) {
	return pkgpartition.EnsureAhead(ctx, db, DefaultOptions(), aheadMonths)
}

func ReconcileRLS(ctx context.Context, db *gorm.DB) error {
	return pkgpartition.ReconcileRLS(ctx, db, migrations.ReconcilePartitionRLSSQL)
}

// 返回的 stop 等待循环退出：调用方必须在关闭数据库之前调用它。
func StartScheduler(ctx context.Context, db *gorm.DB) func() {
	return pkgpartition.StartScheduler(ctx, db, DefaultOptions())
}

type Table = pkgpartition.Table

type Partition = pkgpartition.Partition

type RLSGap = pkgpartition.RLSGap

func ListPartitions(ctx context.Context, db *gorm.DB, table string) ([]Partition, error) {
	return pkgpartition.ListPartitions(ctx, db, table)
}

func DetachBefore(ctx context.Context, db *gorm.DB, table string, cutoff time.Time) ([]string, error) {
	return pkgpartition.DetachBefore(ctx, db, table, cutoff)
}

func AuditPartitionRLSCoverage(ctx context.Context, db *gorm.DB) ([]RLSGap, int, error) {
	return pkgpartition.AuditPartitionRLSCoverage(ctx, db)
}

func AuditRLSCoverage(ctx context.Context, db *gorm.DB, exempt ...string) ([]RLSGap, int, error) {
	return pkgpartition.AuditRLSCoverage(ctx, db, exempt...)
}

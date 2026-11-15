package partition

import _ "embed"

//go:embed reconcile_rls.sql
var reconcilePartitionRLSSQL string

// ReconcilePartitionRLSSQL 返回把父表策略逐字段复制到全部叶子分区的 SQL。
// 原为迁移 507；结构改为 public/schema 后仍由分区维护在启动期执行。
func ReconcilePartitionRLSSQL() string { return reconcilePartitionRLSSQL }

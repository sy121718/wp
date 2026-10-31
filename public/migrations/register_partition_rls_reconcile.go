package migrations

import "sync"

func registerPartitionRLSReconcile() {
	registerPartitionRLSReconcileOnce.Do(func() {
		register(Migration{
			Version: "507-partition-rls-reconcile",
			SQL:     ReconcilePartitionRLSSQL(),
		})
	})
}

var registerPartitionRLSReconcileOnce sync.Once

func ReconcilePartitionRLSSQL() string {
	return mustSQL("507_partition_rls_reconcile.sql")
}

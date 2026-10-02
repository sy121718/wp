package migrations

import "sync"

// register_partition_rls_reconcile.go — 分区子表的工程隔离对账（结构 507，审计 DB-02）。
//
// 507：遍历**父表上有策略**的分区表，把父表的每一条策略逐字段复制到它的每个叶子分区
// （ENABLE + FORCE + polname / polpermissive / polcmd / polroles / qual / withcheck）。
// 215 用的是静态分区名数组，而 173 / EnsureAhead 按部署时间动态建分区 ——
// 部署月落在 215 名单之外时，那些分区从来没有策略。
//
// **刻意不带 TableName**：理由与 506 / 491 相同（见 register_page_schedule_index.go 的说明）——
// 目标不是某一张表，而是「当时库里存在哪些分区」这个动态集合。带上任何一个表名，
// 一旦那张表已存在，整条迁移就会被「迁移对象已存在，跳过」，对账永不执行。
//
// 幂等由 507 的 DO 块自己保证：已是 ENABLE + FORCE 且策略名集合覆盖父表的分区整体 CONTINUE
// （只读检查、零 DDL），需要处理时才 DROP POLICY IF EXISTS + CREATE。
//
// 注册方式：由 register.go 的 init() 显式调用（与 491 / 506 同）。
func registerPartitionRLSReconcile() {
	registerPartitionRLSReconcileOnce.Do(func() {
		register(Migration{
			Version: "507-partition-rls-reconcile",
			SQL:     ReconcilePartitionRLSSQL(),
		})
	})
}

// registerPartitionRLSReconcileOnce 让重复调用成为空操作（不会重复 register）。
var registerPartitionRLSReconcileOnce sync.Once

// ReconcilePartitionRLSSQL 分区工程隔离对账的 SQL 原文（迁移 507）。
//
// **为什么导出**：这不是「只在迁移期跑一次」的判据。分区是**持续被创建**的对象
// （internal/partition.EnsureAhead 按月建），刚建出来的分区必须立刻有策略 ——
// 而那与 507 是**同一件事**：把父表上的策略逐条复制到叶子分区。
// 所以它只有一份实现：运行时维护**直接执行这条 SQL**，而不是另写一份等价的 Go 逻辑。
//
// 两份等价的实现必然漂移，而漂移在这里的后果是**静默的**：某个分区少一条策略，
// 应用走父表一切正常，只有按分区名直查（排障 / 归档 / 报表 / 手写 SQL）才绕开隔离。
// 这类「判据分叉」在本仓已有前科（215 的静态名单 vs 173 的动态建表），不必再添一处。
//
// 文本取自 //go:embed *.sql（与迁移执行读的是同一个 FS）→ 不存在「SQL 文件改了、
// 运行时的副本没改」这种可能；改这一份，迁移与运行时**同时**变。
func ReconcilePartitionRLSSQL() string {
	return mustSQL("507_partition_rls_reconcile.sql")
}

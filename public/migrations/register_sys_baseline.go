package migrations

// registerSysBaseline 登记基线台账表（759）。
//
// 这张表只在载入初始化基线时写入一行（version + manifest_sha256 + 时间），
// 迁移器据此跳过 manifest 覆盖的历史迁移。表本身走普通迁移创建，
// 所以「全量迁移库」与「基线库 + 增量」的结构一致。
func registerSysBaseline() {
	register(Migration{
		Version:   "759-sys-baseline",
		TableName: "sys_baseline",
		SQL:       mustSQL("759_sys_baseline.sql"),
	})
}

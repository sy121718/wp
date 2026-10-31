package buildmodel

// build_job_seq.go — menu_stale 的输入版本号源（迁移 698）。
//
// 为什么单独一张表而不是 MAX(input_version)+1：
//   - MAX+1 在并发下会拿到同一个号（两个写入各自读到 N，都写 N+1），
//     于是「A→B→A」这类来回改动被去重成一条任务，新意图被旧任务吞掉；
//   - update_time / 内容 hash 充版本号同理：它们都不是「单调、按 (工程, 位置) 独立」的量，
//     hash 在 A→B→A 时回到旧值，与「又一次真实变化」这件事没有一一对应；
//   - 行级 UPSERT 递增在 PG 里是原子的，且同一 (project_id, kind) 的并发写入天然串行 ——
//     这正是版本号需要的语义：每次真实变化分配一个**严格更大**的号。
//
// 版本号只在调用方事务里分配：事务回滚时号一起回滚，不会出现「号被消耗但没有任务」。
//
// 表访问不走裸 SQL（raw_sql_boundary_test.go 的预算只减不增）：取号是标准的
// INSERT … ON CONFLICT DO UPDATE … RETURNING，gorm 的 clause.OnConflict + clause.Returning
// 能原样表达，所以这里不登记任何例外。

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const tableNameBuildJobSeq = "build_job_seq"

// NextMenuStaleVersionTx 在调用方事务里为 (projectID, kind) 分配下一个输入版本号。
//
// 只做「分配」这一件事：不判断 kind 合法性（那是 service 的规则）、不写任务行
// （CreateTx 负责）、不自开事务。调用方拿到的号直接进 build_jobs.build_input_hash 与
// input_version，两者同事务提交。
//
// 语句形状全部走 gorm（raw_sql_boundary_test.go 的预算只减不增，不新增裸 SQL）：
//   - INSERT … (project_id, kind)：首行由列的 DEFAULT 1 落下起点；
//   - clause.OnConflict{Columns: 主键}：PG 生成 ON CONFLICT (project_id, kind) DO UPDATE，
//     同键并发写入由行锁串行化 —— 这正是版本号要的「严格更大」；
//   - DoUpdates 只写两列：next_version = build_job_seq.next_version + 1 与
//     update_time = now()。右侧的表限定名是**我们显式写的**：gorm 只负责把左侧
//     列名拼进 SET，不会替你限定右侧引用，而不限定的 next_version 会同时匹配目标行
//     与 excluded，PG 报 42702（歧义）。
//     gorm 的自动时间戳只覆盖 Create 的零值、以及 Zeroer 类型的 Updates，
//     这是 UPSERT 的 UPDATE 分支，必须显式写；
//   - clause.Returning 列出 next_version：分配与读回是同一条语句，中间没有窗口。
//
// 不在插入映射里写 next_version：列 DEFAULT 1 已经表达了「首次插入的起点」，在 Go 里再写
// 一遍就是第二份真相（迁移改了默认值而这里不跟，会出现「首号从 5 开始」这种静默漂移）。
func (m *Model) NextMenuStaleVersionTx(tx *gorm.DB, projectID, kind string) (version int64, err error) {
	if err := requireTx(tx); err != nil {
		return 0, err
	}
	// 插入与结果用**同一个对象**：gorm 的 clause.Returning 不是另发一条 SELECT，而是把
	// RETURNING 的列扫回语句的目标对象（本仓既有形态：page_model.go:326-329 的
	// Model(&rows).Clauses(Returning).Updates、product_outbox_model.go:94-100 同理）。
	// 写成 Create(map…).Scan(&局部结构) 时目标不是语句对象，扫回与否取决于 gorm 内部实现，
	// 不是本模块能依赖的契约；取号是事务里第一步，读到零值等于把空版本号写进任务行。
	//
	// 目标字段与列型严格对应：next_version 是 bigint NOT NULL，所以是 int64 而不是指针
	// （指针留给可空列）。
	//
	// 两个主键列必须**随对象带入**：gorm 的 Create 把结构体的零值也显式写进 VALUES
	//（INSERT INTO build_job_seq (project_id, kind, next_version) VALUES ('','',0)），
	// 不会因为列上有 NOT NULL / DEFAULT 就跳过它 —— 空串因此直接撞上 uuid 列的
	// 22P02（invalid input syntax for type uuid: ""），而不是落到默认值。
	//
	// next_version 则相反：它**必须**留在对象之外（Omit）。零值 int64 同样会被写进
	// VALUES，于是首行插入显式写 0、覆盖列上的 DEFAULT 1，RETURNING 又把同一个 0 扫回
	// got.NextVersion，被下面的 <= 0 判据判成 ErrRecordNotFound ——「首建菜单报 record
	// not found」。列 DEFAULT 是首号唯一真相，这里只让它说话。
	//
	// Omit 只影响 INSERT 的列集合，不影响 RETURNING 的扫回目标：Create 回调在
	// ConvertToCreateValues 之前就把 clause.Returning 挂上（gorm v1.31.2、
	// callbacks/create.go:52-65），Scan 的落点始终是 db.Statement.Dest（即 &got）。
	got := seqRow{ProjectID: projectID, Kind: kind}
	res := tx.Table(tableNameBuildJobSeq).
		Omit("NextVersion").
		Clauses(
			clause.OnConflict{
				Columns: []clause.Column{{Name: "project_id"}, {Name: "kind"}},
				DoUpdates: clause.Assignments(map[string]any{
					// 右侧必须写表限定名：ON CONFLICT DO UPDATE 的 SET 右侧，未限定的
					// next_version 同时匹配目标表与 excluded 两行，PG 直接报
					// column reference "next_version" is ambiguous (SQLSTATE 42702)。
					"next_version": gorm.Expr(tableNameBuildJobSeq + ".next_version + 1"),
					"update_time":  gorm.Expr("now()"),
				}),
			},
			// 显式列出 next_version：空 Returning{} 会展开成 RETURNING *，把 create_time /
			// update_time 也扫回结果 —— 目标结构没有这两个字段。
			clause.Returning{Columns: []clause.Column{{Name: "next_version"}}},
		).
		Create(&got)
	if res.Error != nil {
		return 0, res.Error
	}
	if got.NextVersion <= 0 {
		// 读出 0 只可能是 RETURNING 没扫回，或列被写坏：静默返回 0 会让任务拿到一个
		// 空版本号，宁可显式失败。
		return 0, gorm.ErrRecordNotFound
	}
	return got.NextVersion, nil
}

// seqRow 取号语句的插入与 RETURNING 目标。
//
// 它只是「这一条语句用到的那几列」，不是 build_job_seq 的实体：本表在 model 侧只有
// 取号一个用途，行语义（kind 四值、工程外键、RLS）全部由迁移 698 表达，这里不重复。
type seqRow struct {
	ProjectID   string `gorm:"column:project_id;primaryKey"`
	Kind        string `gorm:"column:kind;primaryKey"`
	NextVersion int64  `gorm:"column:next_version"`
}

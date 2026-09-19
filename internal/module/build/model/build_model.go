// Package buildmodel 构建任务队列的表访问单元（build_jobs，审计 DB-007 / DB-01）。
//
// 队列引擎选 PG 表而不是外部队列：构建任务需要**可见性**（后台能看队列深度与失败原因）
// 与**可纠错**（失败任务能重试、僵尸任务能回收）。这两件事在内存队列或 asynq 里
// 都要额外搭一套存储才能做到，而 build_jobs 这张表的形状本来就是为它设计的。
package buildmodel

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/database"
)

const tableNameBuildJobs = "build_jobs"

// 允许的来源类型（与 DDL 的 build_jobs_source_type_check 逐字对应）。
//
// service 层也要有一份：数据库拒绝时的报错是约束名，到不了「看得懂的一句话」；
// 更重要的是入队前拒绝才不会有半条任务留在表里。
const (
	SourceTypePage         = "page"
	SourceTypePresentation = "presentation"
)

// 任务状态（与 DDL 的 CHECK 逐字对应）。
const (
	StatusPending    = "pending"
	StatusRunning    = "running"
	StatusSuperseded = "superseded"
	StatusFailed     = "failed"
	StatusSucceeded  = "succeeded"
)

// 构建意图（与迁移 295 的 build_jobs_intent_check 逐字对应）。
//
// 为什么要把它入库而不是从「谁调用入队」推断：ARCH-04 的异步重建要按意图分流
// （人工构建走一次，依赖重建按语言与发布状态决定要不要回写线上），而这两种任务
// 在表里长得一模一样 —— 没有这个字段，消费侧只能靠猜。
const (
	// IntentManual 人工发起（后台点「构建」这类由人驱动的工作）。
	IntentManual = "manual"
	// IntentDependency 依赖失效自动重建（内容 / 模板 / 译文变化扇出来的工作）。
	IntentDependency = "dependency"
)

// ErrLeaseLost 租约已失效：任务被回收、合并或作废后，旧 worker 的完成结果不能再落库。
//
// 它是**预期内**的结果而不是故障：worker 执行期间超过租约时长是允许发生的
// （回收先行、执行收尾），此时丢弃旧结果正是「完成归属」要保证的事。
var ErrLeaseLost = fmt.Errorf("构建任务租约已失效（任务已被回收或合并）")

// Entity build_jobs 表实体。
type Entity struct {
	ID         int64  `gorm:"column:id;primaryKey"`
	SourceType string `gorm:"column:source_type;not null"`
	SourceID   string `gorm:"column:source_id;not null"`
	// ProjectID 任务所属站点工程（迁移 295）。可空：来源模块拿不到工程时留空而不是编一个。
	ProjectID *string `gorm:"column:project_id"`
	// Lang 构建语言；空串 = 默认语言（ARCH-04 之前的生产者一律为空串）。
	Lang           string  `gorm:"column:lang;not null"`
	Intent         string  `gorm:"column:intent;not null"`
	DraftVersion   int64   `gorm:"column:draft_version;not null"`
	BuildInputHash string  `gorm:"column:build_input_hash;not null"`
	Status         string  `gorm:"column:status;not null"`
	ArtifactID     *string `gorm:"column:artifact_id"`
	ErrorMessage   *string `gorm:"column:error_message"`
	// Attempt 认领次数：回收后重新认领会累加，用于识别「反复被领取的任务」。
	Attempt          int        `gorm:"column:attempt;not null"`
	LeaseToken       *string    `gorm:"column:lease_token"`
	LeaseExpiresTime *time.Time `gorm:"column:lease_expires_time"`
	CreateTime       time.Time  `gorm:"column:create_time;not null"`
	StartedAt        *time.Time `gorm:"column:started_at"`
	CompletedAt      *time.Time `gorm:"column:completed_at"`
}

// TableName 表名。
func (Entity) TableName() string { return tableNameBuildJobs }

// Model build_jobs 表访问。
type Model struct{ db *gorm.DB }

// NewModel 构造。
func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

// DB 绑定本表的查询入口。
func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&Entity{})
}

// Enqueue 入队（幂等）。
//
// ON CONFLICT DO NOTHING 对着迁移 171 的部分唯一索引
// (source_type, source_id, build_input_hash) WHERE status='pending'：
// 依赖失效扇出会在一批里反复标记同一个页面，没有这层幂等，队列会被同一份工作填满。
// 返回 false 表示「同一份工作已经在队列里」。
//
// 注意它**不覆盖 running**：正在跑的那份工作与队列里的新待办是两回事
// （内容在构建期间又变了就该再排一份），互斥由 claim 侧的同来源条件负责。
func (m *Model) Enqueue(ctx context.Context, e *Entity) (created bool, err error) {
	res := m.DB(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(e)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// claimSQL 取一条待办任务、置为 running 并**原子发出租约令牌**。
//
// 五个要点，少一个都会出错（前三者是原实现，后两者是审计 DB-01 的整改）：
//   - FOR UPDATE SKIP LOCKED：多个 worker 同时取任务时各拿各的，不排队、不重复；
//   - ORDER BY create_time, id：先入先出，否则旧任务会被新任务无限插队；
//   - 同一语句里 SELECT + UPDATE：取与置位之间没有窗口，
//     否则两个 worker 可能都读到同一条 pending（看得到、抢不到的那种重复消费）；
//   - **同来源互斥**：只挑「本来源当前没有 running」的待办。SKIP LOCKED 只锁住 job 行，
//     锁不到「来源」这个概念 —— 把同来源的两条任务交给两个 worker，页面的两次构建
//     会在同一个产物目录上互相覆盖。这里的 NOT EXISTS 是前置过滤，
//     uq_build_jobs_running_source（迁移 295）是数据库层的最终保证；
//   - lease_token / lease_expires_time / attempt 在同一条语句里产生：
//     令牌由数据库生成，认领与发令牌之间没有应用层窗口；到期时间只看租约时长，
//     与 jobTimeout 解耦（执行超时由 worker 侧的 context 负责）。
const claimSQL = `UPDATE build_jobs
	SET status = 'running',
	    started_at = now(),
	    lease_token = gen_random_uuid(),
	    lease_expires_time = now() + make_interval(secs => ?),
	    attempt = attempt + 1,
	    error_message = NULL
	WHERE id = (
	    SELECT b.id FROM build_jobs b
	    WHERE b.status = 'pending'
	      AND NOT EXISTS (
	          SELECT 1 FROM build_jobs r
	          WHERE r.status = 'running'
	            AND r.source_type = b.source_type
	            AND r.source_id = b.source_id
	      )
	    ORDER BY b.create_time ASC, b.id ASC
	    LIMIT 1
	    FOR UPDATE SKIP LOCKED
	)
	RETURNING id, source_type, source_id, project_id, lang, intent,
	          draft_version, build_input_hash, status, attempt,
	          lease_token, lease_expires_time, create_time, started_at`

// claimMaxRetries 认领撞上「同来源已有 running」时的重试次数。
//
// 为什么会有这个窗口：NOT EXISTS 读的是语句快照，另一个 worker 刚把同来源的任务
// 置为 running 但还没提交时，双方都会通过前置过滤；先提交的那条占住唯一索引，
// 后者拿到 23505。重跑一次就能看到已提交的 running 行并改挑别的来源 —— 重试是
// 收敛的，不是「多试几次也许能过」。它依赖 autocommit：认领**不在**业务事务里
// （事务里一条语句失败之后整个事务已中止，重试没有意义）。
const claimMaxRetries = 3

// Claim 取一条待办任务并发出租约；队列为空或所有待办都被同来源的 running 挡住时返回 (nil, nil)。
//
// leaseTTL 为本次租约时长（服务层按 leaseTTL 传入，默认 15 分钟）。
func (m *Model) Claim(ctx context.Context, leaseTTL time.Duration) (e *Entity, err error) {
	if leaseTTL <= 0 {
		return nil, fmt.Errorf("构建任务租约时长必须为正数: %s", leaseTTL)
	}
	var lastErr error
	for i := 0; i < claimMaxRetries; i++ {
		var row Entity
		serr := m.db.WithContext(ctx).Raw(claimSQL, leaseTTL.Seconds()).Scan(&row).Error
		if serr == nil {
			if row.ID == 0 {
				return nil, nil
			}
			return &row, nil
		}
		if !database.IsUniqueViolation(serr) {
			return nil, serr
		}
		lastErr = serr
	}
	return nil, lastErr
}

// leaseGuard 完成写入的公共守卫：任务必须仍是 running 且租约令牌一致。
//
// 只按 id 匹配是审计 DB-01 的第三个缺陷：旧 worker 在一次回收 + 重新认领之后
// 仍然能把自己的结果写成这条任务的状态，把新 worker 正在做的活覆盖掉。
// 令牌由 claim 每次生成，因此「谁认领的谁才能结案」这件事不需要额外的表。
func (m *Model) leaseGuard(ctx context.Context, id int64, leaseToken string) (*gorm.DB, error) {
	if strings.TrimSpace(leaseToken) == "" {
		return nil, ErrLeaseLost
	}
	return m.DB(ctx).Where("id = ? AND status = ? AND lease_token = ?", id, StatusRunning, leaseToken), nil
}

// MarkSucceeded 标记任务成功（记录产物行 id）；返回 ErrLeaseLost 表示租约已不在本 worker 手上。
func (m *Model) MarkSucceeded(ctx context.Context, id int64, leaseToken, artifactID string, at time.Time) error {
	q, err := m.leaseGuard(ctx, id, leaseToken)
	if err != nil {
		return err
	}
	updates := map[string]any{
		"status": StatusSucceeded, "completed_at": at, "error_message": nil,
		// 结案即释放租约：保留令牌会让后台把「已完成的令牌」误读成「还占着来源」。
		"lease_token": nil, "lease_expires_time": nil,
	}
	if artifactID != "" {
		updates["artifact_id"] = artifactID
	}
	res := q.Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrLeaseLost
	}
	return nil
}

// MarkFailed 标记任务失败并记录原因（后台据此判断要不要重试）；租约不代表本 worker 时返回 ErrLeaseLost。
func (m *Model) MarkFailed(ctx context.Context, id int64, leaseToken, message string, at time.Time) error {
	q, err := m.leaseGuard(ctx, id, leaseToken)
	if err != nil {
		return err
	}
	res := q.Updates(map[string]any{
		"status": StatusFailed, "error_message": message, "completed_at": at,
		"lease_token": nil, "lease_expires_time": nil,
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrLeaseLost
	}
	return nil
}

// MarkSuperseded 标记任务作废（同一目标已有更新的构建输入，这份工作没有意义了）。
func (m *Model) MarkSuperseded(ctx context.Context, id int64, leaseToken, reason string, at time.Time) error {
	q, err := m.leaseGuard(ctx, id, leaseToken)
	if err != nil {
		return err
	}
	updates := map[string]any{
		"status": StatusSuperseded, "completed_at": at,
		"lease_token": nil, "lease_expires_time": nil,
	}
	if strings.TrimSpace(reason) != "" {
		updates["error_message"] = reason
	}
	res := q.Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrLeaseLost
	}
	return nil
}

// reclaimStaleSQL 回收租约已到期的任务：能重做的退回 pending，重复的合并为 superseded。
//
// 旧实现只有前半句（无条件 status='pending'），于是撞上 171 的 uq_build_jobs_pending
// 报 23505 —— 而且是**整条** UPDATE 失败：一条与队列里的 pending 同键的陈旧任务，
// 会把同批其它本该被回收的任务一起拖在 running 上（审计 DB-01 的实测复现）。
//
// 两条分支合并在同一条语句里，避免「先查重复、再改状态」之间被并发入队插进来：
//   - has_pending：同键已有待办 → 本行的工作已经排在队列里，标 superseded（合并，不丢工作）；
//   - 其余：退回 pending 重新排队。
//
// 仍有极小窗口：并发入队在本语句快照之后提交同键 pending 行，会让 UPDATE 撞 23505。
// 调用方对 23505 做有界重试，重试的那一次必然看得见那行 pending，改走 superseded 分支。
const reclaimStaleSQL = `WITH stale AS (
	    SELECT b.id,
	           EXISTS (
	               SELECT 1 FROM build_jobs p
	                WHERE p.status = 'pending'
	                  AND p.source_type = b.source_type
	                  AND p.source_id = b.source_id
	                  AND p.build_input_hash = b.build_input_hash
	           ) AS has_pending
	      FROM build_jobs b
	     WHERE b.status = 'running'
	       AND b.lease_expires_time IS NOT NULL
	       AND b.lease_expires_time < ?::timestamptz
	     ORDER BY b.id
	     FOR UPDATE SKIP LOCKED
	)
	UPDATE build_jobs j
	   SET status = CASE WHEN s.has_pending THEN 'superseded' ELSE 'pending' END,
	       started_at = CASE WHEN s.has_pending THEN j.started_at ELSE NULL END,
	       lease_token = NULL,
	       lease_expires_time = NULL,
	       completed_at = CASE WHEN s.has_pending THEN ?::timestamptz ELSE NULL END,
	       error_message = CASE WHEN s.has_pending
	                            THEN COALESCE(j.error_message, '陈旧任务：同目标同构建输入的待办已在队列中，本次合并为 superseded')
	                            ELSE COALESCE(j.error_message, 'worker 租约到期未结束，已退回队列') END
	  FROM stale s
	 WHERE j.id = s.id
	RETURNING j.status`

// ReclaimResult 一次回收的构成。
type ReclaimResult struct {
	// Reclaimed 退回 pending、可以重新被消费的行数。
	Reclaimed int64
	// Merged 与队列里的待办同键、被合并为 superseded 的行数。
	Merged int64
}

// ReclaimStale 回收租约已到期的任务（at 之前到期）。
//
// 单条任务的状态异常不会阻断其余任务：归并判定写在同一条语句里，
// 唯一的失败来源（并发入队造成的 23505）走有界重试。
func (m *Model) ReclaimStale(ctx context.Context, at time.Time) (res ReclaimResult, err error) {
	const maxAttempts = 3
	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		var rows []struct {
			Status string `gorm:"column:status"`
		}
		serr := m.db.WithContext(ctx).Raw(reclaimStaleSQL, at, at).Scan(&rows).Error
		if serr == nil {
			var out ReclaimResult
			for _, r := range rows {
				switch r.Status {
				case StatusPending:
					out.Reclaimed++
				case StatusSuperseded:
					out.Merged++
				}
			}
			return out, nil
		}
		if !database.IsUniqueViolation(serr) {
			return ReclaimResult{}, serr
		}
		lastErr = serr
	}
	return ReclaimResult{}, lastErr
}

// StatusCount 单个状态的行数。
type StatusCount struct {
	Status string `gorm:"column:status"`
	Count  int64  `gorm:"column:count"`
}

// CountByStatus 按状态统计队列深度。
func (m *Model) CountByStatus(ctx context.Context) (rows []StatusCount, err error) {
	err = m.DB(ctx).Select("status, COUNT(*) AS count").Group("status").Scan(&rows).Error
	return rows, err
}

// ListRecent 按状态列出最近的任务（空状态 = 全部）。
func (m *Model) ListRecent(ctx context.Context, status string, limit int) (list []*Entity, err error) {
	q := m.DB(ctx).Order("create_time DESC")
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if limit <= 0 {
		limit = 20
	}
	err = q.Limit(limit).Find(&list).Error
	return list, err
}

// Get 按 id 查询任务。
func (m *Model) Get(ctx context.Context, id string) (e *Entity, err error) {
	var row Entity
	if err = m.DB(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// RetryFailed 把失败任务退回队列（清空错误、完成时间与租约残留）。
//
// 与「重新入队一条新任务」不同：重试保留原来的 draft_version 与 build_input_hash，
// 因此它仍然与部分唯一索引对齐 —— 若同一份工作已被重新排上，这次重试会被索引挡下，
// 不会变成两份并行的工作。
//
// 守卫写在 WHERE 里（status='failed'）而不是先读后写：并发两次重试只有一次生效，
// 第二次 RowsAffected=0，由 service 翻成「任务不存在或不是失败态」。
func (m *Model) RetryFailed(ctx context.Context, id int64) (ok bool, err error) {
	res := m.DB(ctx).Where("id = ? AND status = ?", id, StatusFailed).Updates(map[string]any{
		"status": StatusPending, "started_at": nil, "completed_at": nil, "error_message": nil,
		"lease_token": nil, "lease_expires_time": nil,
	})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

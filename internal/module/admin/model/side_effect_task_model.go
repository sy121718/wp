package adminmodel

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 副作用任务种类。取值写死在表里，重放端按种类分发。
const (
	SideEffectPolicyReload  = "policy_reload"
	SideEffectRevokeSession = "revoke_session"
)

// SideEffectTaskEntity 是「业务已提交、副作用待确认」的一条持久事实。
//
// 为什么需要它：策略重载与会话吊销跨库或依赖进程内状态，PG 事务回滚不了它们。
// 此前做法是提交后同步调用、失败就报错 + 打日志 —— 日志不是可重放的持久事实，
// 进程重启即丢，而且「DB 已提交却谎报整批回滚」本身就是错的。
//
// 现在的语义：任务行与业务写在同一事务里（业务回滚任务一起没），提交后立刻尝试一次；
// 成功后 processed_time 落定，失败保留 attempts / last_error 由自己的 worker 重放。
//
// 全局账号与策略副作用，无项目维度：不声明 project_id，也不走项目域 RLS。
type SideEffectTaskEntity struct {
	ID            int64      `gorm:"column:id;primaryKey"`
	Kind          string     `gorm:"column:kind;not null"`
	Payload       string     `gorm:"column:payload;not null"`
	CreateTime    time.Time  `gorm:"column:create_time;not null"`
	ClaimedTime   *time.Time `gorm:"column:claimed_time"`
	ProcessedTime *time.Time `gorm:"column:processed_time"`
	Attempts      int        `gorm:"column:attempts;not null"`
	LastError     string     `gorm:"column:last_error;not null"`
}

func (SideEffectTaskEntity) TableName() string { return "admin_side_effect_tasks" }

type SideEffectTaskModel struct {
	db *gorm.DB
}

func NewSideEffectTaskModel(db *gorm.DB) *SideEffectTaskModel {
	return &SideEffectTaskModel{db: db}
}

func (m *SideEffectTaskModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx)
}

// Transaction 由 model 持有连接并下传事务句柄。
//
// service 不持 *gorm.DB：重放也一样走这里拿 tx，不为 worker 开第二套连接口子。
func (m *SideEffectTaskModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// AppendTx 在调用方事务里登记一条待执行的副作用。
//
// 必须和产生它的业务写在同一个事务：业务回滚时这条任务也要一起消失，
// 否则会出现「删失败了却留下重放任务」的悬挂记录。
func (m *SideEffectTaskModel) AppendTx(ctx context.Context, tx *gorm.DB, kind, payload string) (int64, error) {
	if tx == nil {
		return 0, gorm.ErrInvalidDB
	}
	row := SideEffectTaskEntity{Kind: kind, Payload: payload, CreateTime: time.Now().UTC()}
	if err := tx.WithContext(ctx).Create(&row).Error; err != nil {
		return 0, err
	}
	return row.ID, nil
}

// ClaimPendingTx 在一个事务里按 id 升序租用一批待执行的副作用。
//
// 租约语义与 product 的 outbox 一致：只领「未完成且 claimed_time 为空或已过期」的行，
// SKIP LOCKED 让多个实例各领各的，attempts 自增记录重试次数。
func (m *SideEffectTaskModel) ClaimPendingTx(ctx context.Context, tx *gorm.DB, lease time.Duration, limit int) (rows []SideEffectTaskEntity, err error) {
	if limit <= 0 {
		return nil, nil
	}
	secs := int64(lease / time.Second)
	if secs <= 0 {
		secs = 1
	}
	candidates := tx.WithContext(ctx).Model(&SideEffectTaskEntity{}).
		Select("id").
		Where("processed_time IS NULL").
		Where("claimed_time IS NULL OR claimed_time < now() - (? * interval '1 second')", secs).
		Order("id").
		Limit(limit).
		Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
	err = tx.WithContext(ctx).Model(&rows).
		Where("id IN (?)", candidates).
		Clauses(clause.Returning{}).
		Updates(map[string]any{
			"claimed_time": gorm.Expr("now()"),
			"attempts":     gorm.Expr("attempts + 1"),
		}).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// ClaimOneTx 在一个事务里租用「指定 id」的一条待执行任务。
//
// 与 ClaimPendingTx 的区别只有一个：它只领这一行，不看队列里更早的任务。
// 业务提交后要立刻把「自己刚才登记的那条」做完，不能用批量领取 ——
// 批量按 id 升序取，前面只要有一条历史失败任务，就会先执行别人的，
// 本次任务被推迟到下一轮，调用方以为「立刻做完了」而实际上没做。
//
// 返回 ok=false 表示这一行不可领（已完成、或被别的持有者用未过期租约占着）。
func (m *SideEffectTaskModel) ClaimOneTx(ctx context.Context, tx *gorm.DB, id int64, lease time.Duration) (row SideEffectTaskEntity, ok bool, err error) {
	if id <= 0 {
		return row, false, nil
	}
	secs := int64(lease / time.Second)
	if secs <= 0 {
		secs = 1
	}
	var rows []SideEffectTaskEntity
	err = tx.WithContext(ctx).Model(&rows).
		Where("id = ?", id).
		Where("processed_time IS NULL").
		Where("claimed_time IS NULL OR claimed_time < now() - (? * interval '1 second')", secs).
		Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Clauses(clause.Returning{}).
		Updates(map[string]any{
			"claimed_time": gorm.Expr("now()"),
			"attempts":     gorm.Expr("attempts + 1"),
		}).Error
	if err != nil {
		return row, false, err
	}
	if len(rows) == 0 {
		return row, false, nil
	}
	return rows[0], true, nil
}

// ClaimResult 是领取一条任务的结果：Task 是这次领到的行，Ok 表示真的领到了。
type ClaimResult struct {
	Task SideEffectTaskEntity
	Ok   bool
}

// ClaimOneTxInTest 是 ClaimOneTx 的便捷包装：自动开一个短事务再领取。
//
// 生产路径按 id 立刻执行时用 FlushSideEffect（它自己开事务），
// 需要单独观察「领取到了什么」时用这个方法。
func (m *SideEffectTaskModel) ClaimOneTxInTest(ctx context.Context, id int64, lease time.Duration) (ClaimResult, error) {
	var out ClaimResult
	err := m.Transaction(ctx, func(tx *gorm.DB) error {
		row, ok, err := m.ClaimOneTx(ctx, tx, id, lease)
		out = ClaimResult{Task: row, Ok: ok}
		return err
	})
	return out, err
}

// MarkDone 把一条「本次领取」的任务标记为已完成。
//
// attempts 是这次领取的令牌：领取时 attempts+1，写回时要求 attempts 仍然等于
// 领取到的值，说明这期间没有别人重新领过。少了这个条件，一个执行时间超过
// 租约的旧持有者会在别人已经重领、甚至已经做完之后，把结果覆盖成自己的 ——
// 那会把已经完成的行又写一遍，或者把新持有者的失败记录抹掉。
func (m *SideEffectTaskModel) MarkDone(ctx context.Context, id int64, attempts int) error {
	if id <= 0 {
		return nil
	}
	now := time.Now().UTC()
	return m.DB(ctx).Model(&SideEffectTaskEntity{}).
		Where("id = ? AND processed_time IS NULL AND attempts = ?", id, attempts).
		Updates(map[string]any{"processed_time": now, "last_error": ""}).Error
}

// MarkFailed 记下这次执行失败的原因，任务保持可被再次领取。
//
// 同样带 attempts 条件：只有仍持有本次租约的执行者才能清掉 claimed_time，
// 否则旧持有者失败退出时会把新持有者的租约清空，导致同一任务被并发执行两遍。
func (m *SideEffectTaskModel) MarkFailed(ctx context.Context, id int64, attempts int, msg string) error {
	if id <= 0 {
		return nil
	}
	return m.DB(ctx).Model(&SideEffectTaskEntity{}).
		Where("id = ? AND processed_time IS NULL AND attempts = ?", id, attempts).
		Updates(map[string]any{"last_error": msg, "claimed_time": nil}).Error
}

// PendingCount 返回还没完成的任务数，供测试与巡检读取。
func (m *SideEffectTaskModel) PendingCount(ctx context.Context) (int64, error) {
	var count int64
	err := m.DB(ctx).Model(&SideEffectTaskEntity{}).Where("processed_time IS NULL").Count(&count).Error
	return count, err
}

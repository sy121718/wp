package pagemodel

// page_schedule_model.go — 页面定时上下线待办（page_schedules，PIPE-7）。
//
// 本文件只做表访问：认领（含租约）、完成/失败的**归属**写入、超时回收、
// 排定写入/取消与只读列表。业务规则（什么时候该排定、到点该做什么、失败算不算终态）
// 全在 service 层 —— model 是 Repository，不是 Domain Model（AGENTS.md「model 层定位」）。
//
// 与 build_jobs 的租赁形状同源（internal/module/build/model/build_model.go）：认领与发令牌
// 在同一条 SQL 里、完成写入必须带令牌、超时回收先于执行收尾。差别只在于本表的互斥粒度是
// (page_id, lang, action)（同键同一时刻最多一条 running），由部分唯一索引
// uq_page_schedules_active（迁移 460）在数据库层保证。
//
// 无 RLS 作用域：page_schedules 与 page_stagings / page_publications / page_publication_plans
// 同属本模块内部台账，不在迁移 215 的清单里（见迁移 460 的表头论证）。工程作用域由调用方
// 经 page_id 反查 pages 得到，写 pages 的那一步才需要作用域。

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

const tableNamePageSchedules = "page_schedules"

// 到点动作（与迁移 460 的 page_schedules_action_check 逐字对应）。
const (
	// ScheduleActionPublish 到点上线：把该语言的符号链接原子切到排定时冻结的暂存产物。
	ScheduleActionPublish = "publish"
	// ScheduleActionOffline 到点下线：删符号链接（redirect_path 非空时落 301），
	// 再解除路由占用与发布指针。
	ScheduleActionOffline = "offline"
)

// 排定状态（与迁移 460 的 page_schedules_status_check 逐字对应）。
const (
	// ScheduleStatusPending 待执行（到点后被认领）。
	ScheduleStatusPending = "pending"
	// ScheduleStatusRunning 已认领，持有租约。
	ScheduleStatusRunning = "running"
	// ScheduleStatusDone 已执行完成（终态）。
	ScheduleStatusDone = "done"
	// ScheduleStatusFailed 执行失败（终态；last_error 记业务 key）。
	ScheduleStatusFailed = "failed"
	// ScheduleStatusCanceled 被取消或被同键新排定取代（终态）。
	ScheduleStatusCanceled = "canceled"
)

// ErrScheduleLeaseLost 租约已失效：排定被回收并重新认领后，旧执行者的完成结果不能再落库。
//
// 与 build 的 ErrLeaseLost 同一语义（那是「预期内」的结果而不是故障）：回收先行、
// 执行收尾时丢弃旧结果，正是「完成归属」要保证的事。
var ErrScheduleLeaseLost = errors.New("排定租约已失效（已被回收或取代，本次结果不落库）")

// ScheduleEntity 对应 page_schedules 表。
//
// 不声明列型（internal/architecture/model_gorm_tag_test.go 守门）：列型是迁移的事，
// model 只维护列名映射。
type ScheduleEntity struct {
	ID               int64      `gorm:"column:id;primaryKey"`
	PageID           string     `gorm:"column:page_id"`
	Lang             string     `gorm:"column:lang"`
	Action           string     `gorm:"column:action"`
	ScheduledAt      time.Time  `gorm:"column:scheduled_at"`
	Status           string     `gorm:"column:status"`
	ArtifactID       *string    `gorm:"column:artifact_id"`
	DraftVersion     int64      `gorm:"column:draft_version"`
	RedirectPath     *string    `gorm:"column:redirect_path"`
	LeaseToken       *string    `gorm:"column:lease_token"`
	LeaseExpiresTime *time.Time `gorm:"column:lease_expires_time"`
	Attempts         int        `gorm:"column:attempts"`
	LastError        *string    `gorm:"column:last_error"`
	CreateBy         *int64     `gorm:"column:create_by"`
	CreateTime       time.Time  `gorm:"column:create_time"`
	UpdateTime       time.Time  `gorm:"column:update_time"`
}

func (ScheduleEntity) TableName() string { return tableNamePageSchedules }

// ScheduleDB 返回已绑定 page_schedules 表的 GORM 实例。
func (m *Model) ScheduleDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ScheduleEntity{})
}

// ScheduleUpsert 排定写入参数（同键旧 pending 会被作废，见 UpsertPendingSchedule）。
type ScheduleUpsert struct {
	PageID       string
	Lang         string
	Action       string
	ScheduledAt  time.Time
	ArtifactID   string
	DraftVersion int64
	RedirectPath string
	// CreateBy 发起人 id（0 = 未登录 / 系统排定，落库为 NULL，与全库 create_by 同型）。
	CreateBy int64
}

// claimDueSQL 取一批到期待办、置为 running 并**原子发出租约令牌**。
//
// 五个要点（前三者与 build_jobs 的 claimSQL 同形，后两者是本表特有的）：
//   - FOR UPDATE SKIP LOCKED：多个实例同时扫描时各拿各的，不排队、不重复；
//   - ORDER BY scheduled_at, id：先到点先执行，否则早该上线的排定会被一直插队；
//   - 同一语句里 SELECT + UPDATE：取与置位之间没有窗口，两个执行者不会都读到同一条
//     pending（「看得到、抢不到」的重复消费）；
//   - **同键互斥前置过滤**（NOT EXISTS）：只挑「同 (page_id, lang, action) 当前没有
//     running」的待办。SKIP LOCKED 只锁待办行，锁不到「同一个页面的同一个动作」这个概念 ——
//     把同键的两条交给两个执行者，两次符号链接切换会在同一个路径上互相覆盖。
//     数据库层的最终保证是 uq_page_schedules_active（迁移 460）；
//   - lease_token / lease_expires_time / attempts 在同一条语句里产生：令牌由数据库生成，
//     认领与发令牌之间没有应用层窗口。attempts 自增是「这条到底试过几次」的唯一证据。
//
// 参数：租约秒数、单批上限。
const claimDueSQL = `UPDATE page_schedules s
	SET status = 'running',
	    lease_token = gen_random_uuid(),
	    lease_expires_time = now() + make_interval(secs => ?),
	    attempts = attempts + 1,
	    last_error = NULL,
	    update_time = now()
	WHERE s.id IN (
	    SELECT c.id FROM page_schedules c
	     WHERE c.status = 'pending'
	       AND c.scheduled_at <= now()
	       AND NOT EXISTS (
	           SELECT 1 FROM page_schedules r
	            WHERE r.status = 'running'
	              AND r.page_id = c.page_id
	              AND r.lang = c.lang
	              AND r.action = c.action
	       )
	     ORDER BY c.scheduled_at ASC, c.id ASC
	     LIMIT ?
	     FOR UPDATE SKIP LOCKED
	)
	RETURNING id, page_id, lang, action, scheduled_at, status, artifact_id,
	          draft_version, redirect_path, lease_token, lease_expires_time,
	          attempts, last_error, create_by, create_time, update_time`

// ClaimDueSchedules 认领一批到点的待办并发出租约；无到期项时返回空切片。
//
// leaseTTL 为本次租约时长（service 按常量传入）；limit 为单批上限（<=0 时按 1 处理，
// 避免 LIMIT 0 变成「永远扫不到」这种看起来正常实则不工作的状态）。
func (m *Model) ClaimDueSchedules(ctx context.Context, leaseTTL time.Duration, limit int) (list []ScheduleEntity, err error) {
	if leaseTTL <= 0 {
		return nil, errors.New("排定租约时长必须为正数")
	}
	if limit <= 0 {
		limit = 1
	}
	err = m.db.WithContext(ctx).Raw(claimDueSQL, leaseTTL.Seconds(), limit).Scan(&list).Error
	if err != nil {
		return nil, err
	}
	return list, nil
}

// scheduleLeaseGuard 完成写入的公共守卫：该行必须仍是 running 且令牌一致。
//
// 只按 id 匹配是 build_jobs 出现过的真实缺陷（审计 DB-01）：旧执行者被回收并重新认领之后，
// 仍能把自己的结果写成这条排定的状态，把新执行者正在做的事覆盖掉。
func (m *Model) scheduleLeaseGuard(ctx context.Context, id int64, leaseToken string) (*gorm.DB, error) {
	token := strings.TrimSpace(leaseToken)
	if token == "" {
		return nil, ErrScheduleLeaseLost
	}
	return m.db.WithContext(ctx).Model(&ScheduleEntity{}).
		Where("id = ? AND status = ? AND lease_token = ?", id, ScheduleStatusRunning, token), nil
}

// MarkScheduleDone 标记排定执行完成（结案即释放租约）。
// 租约不在本执行者手上时返回 ErrScheduleLeaseLost。
func (m *Model) MarkScheduleDone(ctx context.Context, id int64, leaseToken string, at time.Time) error {
	q, err := m.scheduleLeaseGuard(ctx, id, leaseToken)
	if err != nil {
		return err
	}
	res := q.Updates(map[string]any{
		"status": ScheduleStatusDone, "last_error": nil,
		"lease_token": nil, "lease_expires_time": nil, "update_time": at,
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrScheduleLeaseLost
	}
	return nil
}

// MarkScheduleFailed 标记排定失败并记录业务 key（终态，不再重试）。
//
// message 是**可翻译的业务 key**（page enums 常量）或归口 key：这一列会显示在后台，
// 原文只进日志（与门禁 check-no-internal-error-leak.sh 同一条判据）。
func (m *Model) MarkScheduleFailed(ctx context.Context, id int64, leaseToken, message string, at time.Time) error {
	q, err := m.scheduleLeaseGuard(ctx, id, leaseToken)
	if err != nil {
		return err
	}
	reason := strings.TrimSpace(message)
	updates := map[string]any{
		"status":      ScheduleStatusFailed,
		"lease_token": nil, "lease_expires_time": nil, "update_time": at,
	}
	if reason != "" {
		updates["last_error"] = reason
	}
	res := q.Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrScheduleLeaseLost
	}
	return nil
}

// ReleaseScheduleForRetry 把本次认领**退回待执行**（可重试失败），保留 attempts 计数。
//
// 与失败的区别：这是「这次执行的环境不对」（数据库抖动、文件系统瞬时错误），
// 下一轮扫描会再认领一次；attempts 达上限后由 service 改走 MarkScheduleFailed。
func (m *Model) ReleaseScheduleForRetry(ctx context.Context, id int64, leaseToken, message string, at time.Time) error {
	q, err := m.scheduleLeaseGuard(ctx, id, leaseToken)
	if err != nil {
		return err
	}
	updates := map[string]any{
		"status":      ScheduleStatusPending,
		"lease_token": nil, "lease_expires_time": nil, "update_time": at,
	}
	if reason := strings.TrimSpace(message); reason != "" {
		updates["last_error"] = reason
	}
	res := q.Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrScheduleLeaseLost
	}
	return nil
}

// reclaimExpiredSQL 回收超时未结案的 running 行。
//
// attempts 已达上限的直接判 failed（否则一条永远执行不完的排定会被无限重试：
// 每一次回收都把它退回 pending，再被认领，再超时 —— 后台看到的「待执行」永不减少）。
// 一条语句而不是两条：两条各自提交会留下「已回收成 pending、但 attempts 已超限」
// 的中间态，下一轮它又会被认领一次。
const reclaimExpiredSQL = `UPDATE page_schedules
	SET status = CASE WHEN attempts >= ? THEN 'failed' ELSE 'pending' END,
	    last_error = CASE WHEN attempts >= ? THEN COALESCE(last_error, ?) ELSE last_error END,
	    lease_token = NULL,
	    lease_expires_time = NULL,
	    update_time = now()
	WHERE status = 'running'
	  AND lease_expires_time IS NOT NULL
	  AND lease_expires_time < now()`

// ReclaimExpiredSchedules 回收超时的 running 行，返回被回收的行数（含判失败的那些）。
//
// maxAttempts 为允许的认领次数上限（service 按常量传入）；expiredMessage 是达上限时
// 写入 last_error 的归口 key。
func (m *Model) ReclaimExpiredSchedules(ctx context.Context, maxAttempts int, expiredMessage string) (reclaimed int64, err error) {
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	res := m.db.WithContext(ctx).Exec(reclaimExpiredSQL, maxAttempts, maxAttempts, expiredMessage)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// UpsertPendingSchedule 写入一条待执行排定，并作废同一 (page, lang, action) 的旧 pending。
//
// 为什么「作废旧 pending + 插新行」两步在同一事务里，而不是 ON CONFLICT DO UPDATE：
// 部分唯一索引的冲突目标只覆盖 pending / running 两种状态，DO UPDATE 一旦命中 running 行
// 就会改写一条**正在执行**的排定（执行者手上还拿着它的租约与快照）。这里的语义要说清楚：
// pending 被新排定取代（旧决策作废），running 则拒绝（返回唯一键冲突，service 映射成业务错误）。
//
// 返回新行的 id。
func (m *Model) UpsertPendingSchedule(ctx context.Context, in ScheduleUpsert) (id int64, err error) {
	err = m.Transaction(ctx, func(tx *gorm.DB) error {
		now := time.Now().UTC()
		if cerr := tx.WithContext(ctx).Model(&ScheduleEntity{}).
			Where("page_id = ? AND lang = ? AND action = ? AND status = ?",
				in.PageID, in.Lang, in.Action, ScheduleStatusPending).
			Updates(map[string]any{
				"status": ScheduleStatusCanceled, "update_time": now,
			}).Error; cerr != nil {
			return cerr
		}
		row := &ScheduleEntity{
			PageID: in.PageID, Lang: in.Lang, Action: in.Action,
			ScheduledAt: in.ScheduledAt, Status: ScheduleStatusPending,
			DraftVersion: in.DraftVersion, CreateTime: now, UpdateTime: now,
		}
		if strings.TrimSpace(in.ArtifactID) != "" {
			artifactID := in.ArtifactID
			row.ArtifactID = &artifactID
		}
		if strings.TrimSpace(in.RedirectPath) != "" {
			redirectPath := in.RedirectPath
			row.RedirectPath = &redirectPath
		}
		if in.CreateBy > 0 {
			createdBy := in.CreateBy
			row.CreateBy = &createdBy
		}
		if cerr := tx.WithContext(ctx).Create(row).Error; cerr != nil {
			return cerr
		}
		id = row.ID
		return nil
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// ListSchedulesByPages 按页面批量取排定（后台列表页的行内状态投影）。
//
// statuses 为空 = 不过滤状态；limit <= 0 = 不限（调用方负责给上限，
// 因为这是后台页面的投影而不是导出）。
func (m *Model) ListSchedulesByPages(ctx context.Context, pageIDs []string, statuses []string, limit int) (list []ScheduleEntity, err error) {
	if len(pageIDs) == 0 {
		return nil, nil
	}
	q := m.ScheduleDB(ctx).Where("page_id IN ?", pageIDs)
	if len(statuses) > 0 {
		q = q.Where("status IN ?", statuses)
	}
	q = q.Order("create_time DESC, id DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err = q.Find(&list).Error
	return list, err
}

// ListSchedulesByPage 按单个页面取排定（面板片段用；全部状态，按新到旧）。
func (m *Model) ListSchedulesByPage(ctx context.Context, pageID string, limit int) (list []ScheduleEntity, err error) {
	if strings.TrimSpace(pageID) == "" {
		return nil, nil
	}
	q := m.ScheduleDB(ctx).Where("page_id = ?", pageID).Order("create_time DESC, id DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err = q.Find(&list).Error
	return list, err
}

// ScheduleStatusAt 读一条排定的当前状态（不存在返回空串与 nil：调用方据此给 404）。
// pageID 非空时同时限定归属，避免「拿别人的排定 id 试状态」。
func (m *Model) ScheduleStatusAt(ctx context.Context, id int64, pageID string) (status string, err error) {
	q := m.ScheduleDB(ctx).Where("id = ?", id)
	if strings.TrimSpace(pageID) != "" {
		q = q.Where("page_id = ?", pageID)
	}
	var row ScheduleEntity
	if err = q.First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return row.Status, nil
}

// CancelSchedule 取消一条**待执行**的排定（条件更新，幂等语义由调用方决定）。
//
// 返回 false 表示「没有这样一条 pending 排定」——可能是 id 不存在，
// 也可能是它已被认领（running）或已是终态。调用方用 ScheduleStatusAt 区分两种成因。
func (m *Model) CancelSchedule(ctx context.Context, id int64, pageID string) (canceled bool, err error) {
	q := m.ScheduleDB(ctx).Where("id = ? AND status = ?", id, ScheduleStatusPending)
	if strings.TrimSpace(pageID) != "" {
		q = q.Where("page_id = ?", pageID)
	}
	res := q.Updates(map[string]any{
		"status": ScheduleStatusCanceled, "update_time": time.Now().UTC(),
	})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// DeleteFinishedSchedules 删除**终态**且到点时刻早于 cutoff 的排定行（保留期清理用）。
//
// 判据必须同时含 status（终态）与 scheduled_at（超期）：
//   - 只按时间删会把一条排在下周的排定（此刻仍是 pending）在三个月后删掉 —— 那是静默丢待办；
//   - 只按状态删会把昨天刚跑完的记录一并清掉，排障时什么都查不到。
//
// limit 为单批上限（0 或负数 = 不限批次，调用方按 retention 的批次约定传入）：
// 分批的意义是不制造长事务与锁表。
func (m *Model) DeleteFinishedSchedules(ctx context.Context, cutoff time.Time, limit int) (deleted int64, err error) {
	q := m.db.WithContext(ctx).
		Where("status IN ? AND scheduled_at < ?", []string{
			ScheduleStatusDone, ScheduleStatusFailed, ScheduleStatusCanceled,
		}, cutoff)
	if limit > 0 {
		// 用子查询限定批次而不是 LIMIT：PostgreSQL 的 DELETE 不支持 LIMIT。
		q = q.Where("id IN (?)", m.db.WithContext(ctx).Model(&ScheduleEntity{}).
			Select("id").
			Where("status IN ? AND scheduled_at < ?", []string{
				ScheduleStatusDone, ScheduleStatusFailed, ScheduleStatusCanceled,
			}, cutoff).
			Order("id ASC").Limit(limit))
	}
	res := q.Delete(&ScheduleEntity{})
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

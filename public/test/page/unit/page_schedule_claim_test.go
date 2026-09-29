package unit

// page_schedule_claim_test.go — 定时上下线（PIPE-7）的认领、完成归属与超时回收。
//
// 这一批守的是**静默错**：认领与结案写错时不会报错、不会 500，页面看起来一切正常，
// 只是「两次执行都生效了」或「旧执行者的结果把新执行者的状态覆盖了」。判据全部落在
// 表上的状态与令牌，因此需要真实 PostgreSQL（表结构来自生产迁移）。
//
// 三条判据与 build_jobs 的队列同源（审计 DB-01）：
//   1. claim 是「取 + 置 running + 发令牌」的同一条语句，同键同一时刻只有一条 running；
//   2. 完成 / 失败写入必须带上认领时发出的令牌 —— 回收并重新认领之后，旧令牌的结果被丢弃；
//   3. 超时回收把 running 退回 pending（attempts 达上限则判失败），回收后旧令牌彻底失效。

import (
	"context"
	"testing"
	"time"

	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pagemodel "go_wp/internal/module/page/model"

	"go_wp/pkg/sitetz"

	"gorm.io/gorm"
)

// scheduleTTL 用例里的租约时长（远大于用例自身耗时，判据只看令牌不看时间）。
const scheduleTTL = time.Minute

// seedSchedule 直接经 model 写入一条**已到点**的排定（绕开 service 的「未来时刻」校验）。
func seedSchedule(t *testing.T, m *pagemodel.Model, pageID, lang, action string) int64 {
	t.Helper()
	id, err := m.UpsertPendingSchedule(context.Background(), pagemodel.ScheduleUpsert{
		PageID: pageID, Lang: lang, Action: action,
		ScheduledAt: time.Now().Add(-time.Minute), DraftVersion: 1,
	})
	if err != nil {
		t.Fatalf("写入排定失败: %v", err)
	}
	return id
}

// leaseTokenOf 取行上的令牌（nil 转空串）。
func leaseTokenOf(e pagemodel.ScheduleEntity) string {
	if e.LeaseToken == nil {
		return ""
	}
	return *e.LeaseToken
}

// TestScheduleClaimCompletionOwnership 完成归属：旧令牌的结案结果被丢弃。
//
// 复现的是 build_jobs 出现过的真实缺陷（审计 DB-01）：执行者 A 认领 → 超时被回收 →
// 执行者 B 认领同一条 → A 收尾时把自己的结果写成这条排定的状态，把 B 的工作覆盖掉。
// 守卫是「带令牌的条件更新」（受影响 0 行 → ErrScheduleLeaseLost）。
func TestScheduleClaimCompletionOwnership(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	m := pagemodel.NewPageModel(db)

	pageID := createPage(t, svc, projectID, "/sched-claim", headingDocument).ID
	id := seedSchedule(t, m, pageID, "zh-CN", pagemodel.ScheduleActionPublish)

	claimed, err := m.ClaimDueSchedules(ctx, scheduleTTL, 10)
	if err != nil {
		t.Fatalf("认领失败: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != id || claimed[0].Attempts != 1 {
		t.Fatalf("认领结果错误: %+v", claimed)
	}
	staleToken := leaseTokenOf(claimed[0])
	if staleToken == "" {
		t.Fatal("认领必须发出租约令牌（没有令牌就没有完成归属）")
	}
	// 已认领的行不再是 pending：同一键的第二次认领拿不到它。
	again, err := m.ClaimDueSchedules(ctx, scheduleTTL, 10)
	if err != nil {
		t.Fatalf("二次认领失败: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("running 的行不该被再次认领: %+v", again)
	}
	// 模拟「超时回收 + 重新认领」：手工清掉租约让它回到待执行，再认领一次。
	if err = m.ReleaseScheduleForRetry(ctx, id, staleToken, "", time.Now().UTC()); err != nil {
		t.Fatalf("退回待执行失败: %v", err)
	}
	reclaimed, err := m.ClaimDueSchedules(ctx, scheduleTTL, 10)
	if err != nil || len(reclaimed) != 1 {
		t.Fatalf("重新认领失败: list=%+v err=%v", reclaimed, err)
	}
	freshToken := leaseTokenOf(reclaimed[0])
	if freshToken == "" || freshToken == staleToken {
		t.Fatalf("重新认领必须换新令牌: stale=%s fresh=%s", staleToken, freshToken)
	}
	// 旧执行者收尾：结果必须被丢弃。
	if err = m.MarkScheduleDone(ctx, id, staleToken, time.Now().UTC()); err != pagemodel.ErrScheduleLeaseLost {
		t.Fatalf("旧令牌结案应返回 ErrScheduleLeaseLost，实际 %v", err)
	}
	status, _, _ := scheduleStateOf(t, db, id)
	if status != pagemodel.ScheduleStatusRunning {
		t.Fatalf("旧令牌的结案不该改变状态，实际 %s", status)
	}
	// 新执行者收尾：正常落库。
	if err = m.MarkScheduleDone(ctx, id, freshToken, time.Now().UTC()); err != nil {
		t.Fatalf("新令牌结案失败: %v", err)
	}
	status, lastError, _ := scheduleStateOf(t, db, id)
	if status != pagemodel.ScheduleStatusDone || lastError != "" {
		t.Fatalf("结案后状态错误: status=%s lastError=%q", status, lastError)
	}
}

// TestScheduleReclaimExpiredLease 超时回收：租约过期的 running 行退回待执行，旧令牌失效。
//
// 另一种收尾是「attempts 达上限判失败」—— 否则一条永远执行不完的排定会被无限重试，
// 后台看到的「待执行」永不减少（回收一次、认领一次、再超时）。
func TestScheduleReclaimExpiredLease(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	m := pagemodel.NewPageModel(db)

	pageID := createPage(t, svc, projectID, "/sched-reclaim", headingDocument).ID
	id := seedSchedule(t, m, pageID, "zh-CN", pagemodel.ScheduleActionPublish)
	claimed, err := m.ClaimDueSchedules(ctx, scheduleTTL, 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("认领失败: list=%+v err=%v", claimed, err)
	}
	staleToken := leaseTokenOf(claimed[0])

	// 让租约过期（真实等待一分钟不现实，直接改到期时刻 —— 回收的判据只看这一列）。
	if err = db.Exec(`UPDATE page_schedules SET lease_expires_time = now() - interval '1 minute' WHERE id = ?`, id).Error; err != nil {
		t.Fatalf("制造超时租约失败: %v", err)
	}
	reclaimed, err := m.ReclaimExpiredSchedules(ctx, 3, pageenums.ErrScheduleApplyFailed)
	if err != nil {
		t.Fatalf("回收失败: %v", err)
	}
	if reclaimed != 1 {
		t.Fatalf("应回收 1 行，实际 %d", reclaimed)
	}
	status, _, _ := scheduleStateOf(t, db, id)
	if status != pagemodel.ScheduleStatusPending {
		t.Fatalf("回收后应回到待执行，实际 %s", status)
	}
	// 旧令牌在回收后彻底失效。
	if err = m.MarkScheduleFailed(ctx, id, staleToken, pageenums.ErrScheduleApplyFailed, time.Now().UTC()); err != pagemodel.ErrScheduleLeaseLost {
		t.Fatalf("回收后旧令牌应失效，实际 %v", err)
	}

	// 反复超时到 attempts 上限：回收直接把这条判失败，而不是永远退回去。
	if _, err = m.ClaimDueSchedules(ctx, scheduleTTL, 10); err != nil {
		t.Fatalf("认领失败: %v", err)
	}
	if err = db.Exec(`UPDATE page_schedules SET attempts = 3, lease_expires_time = now() - interval '1 minute' WHERE id = ?`, id).Error; err != nil {
		t.Fatalf("制造达上限的排定失败: %v", err)
	}
	if _, err = m.ReclaimExpiredSchedules(ctx, 3, pageenums.ErrScheduleApplyFailed); err != nil {
		t.Fatalf("回收失败: %v", err)
	}
	status, lastError, _ := scheduleStateOf(t, db, id)
	if status != pagemodel.ScheduleStatusFailed || lastError != pageenums.ErrScheduleApplyFailed {
		t.Fatalf("达上限应判失败并写归口 key: status=%s lastError=%q", status, lastError)
	}
}

// TestScheduleUpsertReplacesOnlyPending 同键重排：pending 被取代，running 被拒绝。
//
// 两条不同的语义，写错都会静默出错：
//   - 把 running 也一起改掉 → 抹掉执行者手上那一条的租约与快照（它还在跑）；
//   - 不取代旧 pending → 两条待执行同键并存，第二轮扫描会把同一个动作做两遍。
func TestScheduleUpsertReplacesOnlyPending(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()

	created := createPage(t, svc, projectID, "/sched-upsert", headingDocument)
	first, err := svc.SetPageSchedule(ctx, &pagedto.ScheduleSetReq{
		PageID: created.ID, Action: "publish",
		ScheduledAt: sitetz.FormatDateTime(time.Now().Add(2 * time.Hour)),
	})
	if err != nil {
		t.Fatalf("首次排定失败: %v", err)
	}
	// 同键重排：旧 pending 作废、新行生效，只有一条待执行。
	second, err := svc.SetPageSchedule(ctx, &pagedto.ScheduleSetReq{
		PageID: created.ID, Action: "publish",
		ScheduledAt: sitetz.FormatDateTime(time.Now().Add(3 * time.Hour)),
	})
	if err != nil {
		t.Fatalf("重排失败: %v", err)
	}
	if second.ID == first.ID {
		t.Fatal("重排应产生新行（旧决策作废、新决策生效），而不是改写旧行")
	}
	oldStatus, _, _ := scheduleStateOf(t, db, first.ID)
	if oldStatus != pagemodel.ScheduleStatusCanceled {
		t.Fatalf("被取代的旧排定应为 canceled，实际 %s", oldStatus)
	}
	var pendingCount int64
	db.Raw(`SELECT count(*) FROM page_schedules WHERE page_id = ? AND status = 'pending'`, created.ID).Scan(&pendingCount)
	if pendingCount != 1 {
		t.Fatalf("同键最多一条待执行，实际 %d", pendingCount)
	}
	// 已被认领（running）时不再接受同键新排定：不能改写执行者手上的那一条。
	if err = db.Exec(`UPDATE page_schedules SET status = 'running', scheduled_at = now() WHERE id = ?`, second.ID).Error; err != nil {
		t.Fatalf("制造 running 排定失败: %v", err)
	}
	if _, err = svc.SetPageSchedule(ctx, &pagedto.ScheduleSetReq{
		PageID: created.ID, Action: "publish",
		ScheduledAt: sitetz.FormatDateTime(time.Now().Add(4 * time.Hour)),
	}); err == nil {
		t.Fatal("同键已有 running 排定时应拒绝新排定")
	}
}

// scheduleStateOf 读一条排定的状态 / 失败原因 / 认领次数（判据用的最小投影）。
func scheduleStateOf(t *testing.T, db *gorm.DB, id int64) (status string, lastError string, attempts int) {
	t.Helper()
	var row struct {
		Status    string
		LastError *string
		Attempts  int
	}
	if err := db.Raw(`SELECT status, last_error, attempts FROM page_schedules WHERE id = ?`, id).Scan(&row).Error; err != nil {
		t.Fatalf("读取排定状态失败: %v", err)
	}
	if row.LastError != nil {
		lastError = *row.LastError
	}
	return row.Status, lastError, row.Attempts
}

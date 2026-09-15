package feature

// build_queue_test.go — 构建任务队列（审计 DB-007）的链路测试。
//
// 覆盖点对应审计的三条验收：
//   - 多 worker 不重复消费同一任务（SKIP LOCKED）；
//   - 崩在任务中途的 worker 留下的 running 行能被回收并重新消费；
//   - 队列状态可见（深度 + 最近失败任务与原因）。
//
// 另外两条边界同样固化：同一份工作不重复排队、没有执行器的来源类型显式失败而不是静默丢弃。

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	buildcontract "go_wp/internal/module/build/contract"
	builddto "go_wp/internal/module/build/dto"
	buildenums "go_wp/internal/module/build/enums"
	buildmodel "go_wp/internal/module/build/model"
	buildservice "go_wp/internal/module/build/service"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// newBuildFixture 隔离 PG schema + 生产迁移建表（build_jobs 来自 init_builder_schema）。
func newBuildFixture(t *testing.T) (*gorm.DB, *buildservice.Service) {
	t.Helper()
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil, nil
	}
	if err := migrations.Run(db); err != nil {
		t.Fatalf("执行生产迁移失败: %v", err)
	}
	return db, buildservice.NewService(buildmodel.NewModel(db))
}

// enqueue 入队一条任务并断言成功入队。
func enqueue(t *testing.T, svc *buildservice.Service, sourceType string) string {
	t.Helper()
	job, created, err := svc.Enqueue(context.Background(), &builddto.EnqueueReq{
		SourceType: sourceType, SourceID: uuid.NewString(), DraftVersion: 1,
	})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	if !created {
		t.Fatal("首次入队应创建任务")
	}
	return job.ID
}

// jobStatus 读任务状态与错误信息。
func jobStatus(t *testing.T, db *gorm.DB, id string) (status, errMsg string) {
	t.Helper()
	var row struct {
		Status string
		Msg    *string
	}
	if err := db.Raw("SELECT status, error_message AS msg FROM build_jobs WHERE id = ?", id).Scan(&row).Error; err != nil {
		t.Fatalf("读取任务失败: %v", err)
	}
	if row.Msg != nil {
		errMsg = *row.Msg
	}
	return row.Status, errMsg
}

// TestClaimIsExclusiveAcrossWorkers 多 worker 并发取任务时同一条只被执行一次。
func TestClaimIsExclusiveAcrossWorkers(t *testing.T) {
	db, svc := newBuildFixture(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	const jobs = 8
	for i := 0; i < jobs; i++ {
		enqueue(t, svc, "page")
	}

	var mu sync.Mutex
	executed := map[string]int{}
	svc.RegisterExecutor("page", func(_ context.Context, job *buildcontract.Job) error {
		mu.Lock()
		executed[job.ID]++
		mu.Unlock()
		return nil
	})

	// 8 个 worker 各取一次：若 SKIP LOCKED 未生效，会出现同一条任务被两个 worker 拿到。
	var wg sync.WaitGroup
	for i := 0; i < jobs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.RunOnce(ctx); err != nil {
				t.Errorf("取任务失败: %v", err)
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(executed) != jobs {
		t.Fatalf("应执行 %d 条任务，实际 %d 条被取到", jobs, len(executed))
	}
	for id, n := range executed {
		if n != 1 {
			t.Fatalf("任务 %s 被执行了 %d 次（应恰好 1 次）", id, n)
		}
	}
	// 队列已清空：再取一次应当没有活。
	if processed, err := svc.RunOnce(ctx); err != nil || processed {
		t.Fatalf("队列应已清空: processed=%v err=%v", processed, err)
	}
	_ = db
}

// TestReclaimStaleJob 崩在任务中途留下的 running 行会被回收并重新消费。
func TestReclaimStaleJob(t *testing.T) {
	db, svc := newBuildFixture(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	id := enqueue(t, svc, "page")
	// 模拟「worker 拿到任务后进程被杀」：行停在 running，started_at 是很久以前。
	if err := db.Exec("UPDATE build_jobs SET status = 'running', started_at = now() - interval '2 hours' WHERE id = ?", id).Error; err != nil {
		t.Fatalf("构造僵尸任务失败: %v", err)
	}
	// 僵尸行不会被正常取任务拿到（它不是 pending）——先确认这一点，否则回收测不出来。
	if processed, _ := svc.RunOnce(ctx); processed {
		t.Fatal("running 状态的任务不应被重复取走")
	}

	reclaimed, err := svc.ReclaimStale(ctx)
	if err != nil {
		t.Fatalf("回收失败: %v", err)
	}
	if reclaimed != 1 {
		t.Fatalf("应回收 1 条僵尸任务，实际 %d", reclaimed)
	}
	if status, _ := jobStatus(t, db, id); status != buildmodel.StatusPending {
		t.Fatalf("回收后应为 pending，实际 %q", status)
	}

	ran := false
	svc.RegisterExecutor("page", func(_ context.Context, job *buildcontract.Job) error {
		if job.ID != id {
			t.Errorf("取到的任务不是被回收那条: %s", job.ID)
		}
		ran = true
		return nil
	})
	if processed, err := svc.RunOnce(ctx); err != nil || !processed {
		t.Fatalf("回收后的任务应能被消费: processed=%v err=%v", processed, err)
	}
	if !ran {
		t.Fatal("回收后的任务没有被执行")
	}
	if status, _ := jobStatus(t, db, id); status != buildmodel.StatusSucceeded {
		t.Fatalf("执行后应为 succeeded，实际 %q", status)
	}
}

// TestQueueStatsVisible 队列状态与失败原因可见（后台可见性）。
func TestQueueStatsVisible(t *testing.T) {
	_, svc := newBuildFixture(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	okID := enqueue(t, svc, "page")
	badID := enqueue(t, svc, "page")
	_ = enqueue(t, svc, "presentation")
	svc.RegisterExecutor("page", func(_ context.Context, job *buildcontract.Job) error {
		if job.ID == badID {
			return fmt.Errorf("构建失败：组件注册表缺失")
		}
		return nil
	})

	// 三条任务全部消费掉（含没有执行器的那种）。
	for i := 0; i < 3; i++ {
		if _, err := svc.RunOnce(ctx); err != nil {
			t.Fatalf("消费失败: %v", err)
		}
	}

	res, err := svc.Stats(ctx)
	if err != nil {
		t.Fatalf("查询队列状态失败: %v", err)
	}
	if res.Total != 3 || res.Succeeded != 1 || res.Failed != 2 || res.Pending != 0 {
		t.Fatalf("队列状态不符: total=%d succeeded=%d failed=%d pending=%d",
			res.Total, res.Succeeded, res.Failed, res.Pending)
	}
	if len(res.RecentFailed) != 2 {
		t.Fatalf("应列出 2 条失败任务: %d", len(res.RecentFailed))
	}
	var sawBuildErr, sawExecutorMissing bool
	for _, j := range res.RecentFailed {
		switch j.ID {
		case badID:
			sawBuildErr = j.ErrorMessage == "构建失败：组件注册表缺失"
		default:
			sawExecutorMissing = j.ErrorMessage != ""
		}
	}
	if !sawBuildErr {
		t.Fatal("失败任务应带出执行器返回的原因")
	}
	if !sawExecutorMissing {
		t.Fatal("没有执行器的来源类型应显式失败并说明原因")
	}
	_ = okID
}

// TestEnqueueIsIdempotent 同一目标同一构建输入的待办任务不重复排队。
func TestEnqueueIsIdempotent(t *testing.T) {
	db, svc := newBuildFixture(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	sourceID := uuid.NewString()
	req := &builddto.EnqueueReq{SourceType: "page", SourceID: sourceID, DraftVersion: 3, BuildInputHash: "hash-1"}
	if _, created, err := svc.Enqueue(ctx, req); err != nil || !created {
		t.Fatalf("首次入队应成功: created=%v err=%v", created, err)
	}
	if _, created, err := svc.Enqueue(ctx, req); err != nil || created {
		t.Fatalf("同一份工作重复入队应被挡下: created=%v err=%v", created, err)
	}
	var n int64
	if err := db.Table("build_jobs").Where("source_id = ?", sourceID).Count(&n).Error; err != nil {
		t.Fatalf("统计任务失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("同一份工作只应有一行待办，实际 %d", n)
	}
}

// TestRetryFailedJob 失败任务可退回队列重试，且只有失败态能被重试。
func TestRetryFailedJob(t *testing.T) {
	db, svc := newBuildFixture(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	id := enqueue(t, svc, "page")
	svc.RegisterExecutor("page", func(_ context.Context, _ *buildcontract.Job) error {
		return fmt.Errorf("暂时性失败")
	})
	if _, err := svc.RunOnce(ctx); err != nil {
		t.Fatalf("消费失败: %v", err)
	}
	if status, msg := jobStatus(t, db, id); status != buildmodel.StatusFailed || msg == "" {
		t.Fatalf("应为 failed 且带原因: %q %q", status, msg)
	}
	if err := svc.Retry(ctx, id); err != nil {
		t.Fatalf("重试失败: %v", err)
	}
	if status, msg := jobStatus(t, db, id); status != buildmodel.StatusPending || msg != "" {
		t.Fatalf("重试后应回到 pending 且清空原因: %q %q", status, msg)
	}
	// 非失败态不允许重试（否则会把已成功的任务重新做一遍）。
	svc.RegisterExecutor("page", func(_ context.Context, _ *buildcontract.Job) error { return nil })
	if _, err := svc.RunOnce(ctx); err != nil {
		t.Fatalf("消费失败: %v", err)
	}
	if err := svc.Retry(ctx, id); err == nil || err.Error() != buildenums.ErrJobNotFound {
		t.Fatalf("成功态不应可重试: %v", err)
	}
}

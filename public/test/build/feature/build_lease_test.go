package feature

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	buildcontract "go_wp/internal/module/build/contract"
	builddto "go_wp/internal/module/build/dto"
	buildenums "go_wp/internal/module/build/enums"
	buildmodel "go_wp/internal/module/build/model"
	buildservice "go_wp/internal/module/build/service"
	"go_wp/pkg/database"
)

type jobRow struct {
	ID           int64      `gorm:"column:id"`
	Status       string     `gorm:"column:status"`
	Attempt      int        `gorm:"column:attempt"`
	LeaseToken   *string    `gorm:"column:lease_token"`
	LeaseExpires *time.Time `gorm:"column:lease_expires_time"`
	ProjectID    *string    `gorm:"column:project_id"`
	Lang         string     `gorm:"column:lang"`
	Intent       string     `gorm:"column:intent"`
	ArtifactID   *string    `gorm:"column:artifact_id"`
	ErrorMsg     *string    `gorm:"column:error_message"`
}

func loadJob(t *testing.T, db *gorm.DB, id string) jobRow {
	t.Helper()
	var row jobRow
	if err := db.Raw(`SELECT id, status, attempt, lease_token, lease_expires_time,
		project_id, lang, intent, artifact_id, error_message
		FROM build_jobs WHERE id = ?`, id).Scan(&row).Error; err != nil {
		t.Fatalf("读取任务 %s 失败: %v", id, err)
	}
	return row
}

func enqueueJob(t *testing.T, svc *buildservice.Service, req *builddto.EnqueueReq) string {
	t.Helper()
	job, created, err := svc.Enqueue(context.Background(), req)
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	if !created {
		t.Fatalf("入队未创建任务（同一份工作已存在）: %+v", req)
	}
	return job.ID
}

func expireLease(t *testing.T, db *gorm.DB, id string) {
	t.Helper()
	if err := db.Exec("UPDATE build_jobs SET lease_expires_time = now() - interval '1 hour' WHERE id = ?", id).Error; err != nil {
		t.Fatalf("构造过期租约失败: %v", err)
	}
}

func runningCount(t *testing.T, db *gorm.DB, sourceType, sourceID string) int64 {
	t.Helper()
	var n int64
	if err := db.Table("build_jobs").
		Where("source_type = ? AND source_id = ? AND status = 'running'", sourceType, sourceID).
		Count(&n).Error; err != nil {
		t.Fatalf("统计 running 失败: %v", err)
	}
	return n
}

func jobID(t *testing.T, id string) int64 {
	t.Helper()
	v, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		t.Fatalf("任务 id 不是整数: %q", id)
	}
	return v
}

func TestClaimIssuesLease(t *testing.T) {
	db, svc := newBuildFixture(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	id := enqueueJob(t, svc, &builddto.EnqueueReq{SourceType: "page", SourceID: uuid.NewString(), DraftVersion: 1})

	job, err := svc.Model().Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("认领失败: %v", err)
	}
	if job == nil {
		t.Fatal("队列里有一条待办，认领不应为空")
	}
	if job.LeaseToken == nil || *job.LeaseToken == "" {
		t.Fatal("认领必须产生租约令牌（完成写入的凭据）")
	}
	if job.LeaseExpiresTime == nil || !job.LeaseExpiresTime.After(time.Now()) {
		t.Fatalf("认领必须产生未来的租约到期时间: %v", job.LeaseExpiresTime)
	}
	if job.Attempt != 1 {
		t.Fatalf("首次认领 attempt 应为 1，实际 %d", job.Attempt)
	}

	if again, err := svc.Model().Claim(ctx, time.Minute); err != nil || again != nil {
		t.Fatalf("已认领的任务不应被再次认领: job=%v err=%v", again, err)
	}

	if err := svc.Model().MarkSucceeded(ctx, job.ID, *job.LeaseToken, "", time.Now().UTC()); err != nil {
		t.Fatalf("带令牌完成失败: %v", err)
	}
	row := loadJob(t, db, id)
	if row.Status != buildmodel.StatusSucceeded {
		t.Fatalf("任务应为 succeeded，实际 %q", row.Status)
	}
	if row.LeaseToken != nil || row.LeaseExpires != nil {
		t.Fatalf("结案应清空租约: token=%v expires=%v", row.LeaseToken, row.LeaseExpires)
	}
	if row.Attempt != 1 {
		t.Fatalf("attempt 应保留为 1，实际 %d", row.Attempt)
	}
}

func TestClaimSerializesSameSource(t *testing.T) {
	db, svc := newBuildFixture(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	src := uuid.NewString()
	first := enqueueJob(t, svc, &builddto.EnqueueReq{SourceType: "page", SourceID: src, DraftVersion: 1, BuildInputHash: "h1"})
	second := enqueueJob(t, svc, &builddto.EnqueueReq{SourceType: "page", SourceID: src, DraftVersion: 1, BuildInputHash: "h2"})

	other := enqueueJob(t, svc, &builddto.EnqueueReq{SourceType: "presentation", SourceID: uuid.NewString()})

	m := svc.Model()
	got1, err := m.Claim(ctx, time.Minute)
	if err != nil || got1 == nil {
		t.Fatalf("第一次认领失败: job=%v err=%v", got1, err)
	}
	if got1.ID != jobID(t, first) {
		t.Fatalf("先入先出被破坏: 认领到 %d，期望 %s", got1.ID, first)
	}
	got2, err := m.Claim(ctx, time.Minute)
	if err != nil || got2 == nil {
		t.Fatalf("同来源互斥不应阻塞其它来源: job=%v err=%v", got2, err)
	}
	if got2.ID == jobID(t, second) {
		t.Fatal("同来源的第二条任务被并发认领了 —— 同一来源同时只允许一条 running")
	}
	if got2.ID != jobID(t, other) {
		t.Fatalf("应认领到另一来源的任务 %s，实际 %d", other, got2.ID)
	}
	if n := runningCount(t, db, "page", src); n != 1 {
		t.Fatalf("同来源 running 数应为 1，实际 %d", n)
	}
	if row := loadJob(t, db, second); row.Status != buildmodel.StatusPending {
		t.Fatalf("被互斥挡下的同来源任务应保持 pending，实际 %q", row.Status)
	}

	if err := m.MarkSucceeded(ctx, got1.ID, *got1.LeaseToken, "", time.Now().UTC()); err != nil {
		t.Fatalf("带令牌完成失败: %v", err)
	}
	got3, err := m.Claim(ctx, time.Minute)
	if err != nil || got3 == nil {
		t.Fatalf("前一条结案后应能认领同来源的下一条: job=%v err=%v", got3, err)
	}
	if got3.ID != jobID(t, second) {
		t.Fatalf("应轮到同来源的第二条任务 %s，实际 %d", second, got3.ID)
	}
	if got3.Attempt != 1 {
		t.Fatalf("第二条任务是首次认领，attempt 应为 1，实际 %d", got3.Attempt)
	}
}

func TestClaimSameSourceConcurrent(t *testing.T) {
	db, svc := newBuildFixture(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	src := uuid.NewString()
	first := enqueueJob(t, svc, &builddto.EnqueueReq{SourceType: "page", SourceID: src, DraftVersion: 1, BuildInputHash: "h1"})
	second := enqueueJob(t, svc, &builddto.EnqueueReq{SourceType: "page", SourceID: src, DraftVersion: 1, BuildInputHash: "h2"})

	const workers = 8
	var mu sync.Mutex
	claimed := []int64{}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			job, err := svc.Model().Claim(ctx, time.Minute)
			if err != nil {
				t.Errorf("并发认领失败: %v", err)
				return
			}
			if job != nil {
				mu.Lock()
				claimed = append(claimed, job.ID)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(claimed) != 1 {
		t.Fatalf("8 个 worker 争抢同一来源的两条任务，应恰好 1 条被认领，实际 %d 条: %v", len(claimed), claimed)
	}
	if n := runningCount(t, db, "page", src); n != 1 {
		t.Fatalf("并发认领后同来源 running 数应为 1，实际 %d", n)
	}

	pendingID := second
	if strconv.FormatInt(claimed[0], 10) == second {
		pendingID = first
	}
	if row := loadJob(t, db, pendingID); row.Status != buildmodel.StatusPending {
		t.Fatalf("被挡下的同来源任务应保持 pending，实际 %q", row.Status)
	}
}

func TestRunningSourceIndexRejectsSecondRunning(t *testing.T) {
	db, svc := newBuildFixture(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	src := uuid.NewString()
	enqueueJob(t, svc, &builddto.EnqueueReq{SourceType: "page", SourceID: src, DraftVersion: 1, BuildInputHash: "h1"})
	second := enqueueJob(t, svc, &builddto.EnqueueReq{SourceType: "page", SourceID: src, DraftVersion: 1, BuildInputHash: "h2"})
	if _, err := svc.Model().Claim(ctx, time.Minute); err != nil {
		t.Fatalf("首次认领失败: %v", err)
	}
	err := db.Exec(
		"UPDATE build_jobs SET status = 'running', lease_token = gen_random_uuid(), "+
			"lease_expires_time = now() + interval '1 minute' WHERE id = ?", jobID(t, second)).Error
	if err == nil {
		t.Fatal("同来源的第二条 running 必须被 uq_build_jobs_running_source 拒绝（否则来源互斥只靠应用层自觉）")
	}
	if !database.IsUniqueViolation(err) {
		t.Fatalf("应撞唯一约束，实际: %v", err)
	}
	if row := loadJob(t, db, second); row.Status != buildmodel.StatusPending {
		t.Fatalf("被拒绝的那条应保持 pending，实际 %q", row.Status)
	}
}

func TestStaleWorkerCannotOverwrite(t *testing.T) {
	db, svc := newBuildFixture(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	id := enqueueJob(t, svc, &builddto.EnqueueReq{SourceType: "page", SourceID: uuid.NewString(), DraftVersion: 1})

	m := svc.Model()
	oldJob, err := m.Claim(ctx, time.Minute)
	if err != nil || oldJob == nil {
		t.Fatalf("首次认领失败: job=%v err=%v", oldJob, err)
	}
	oldToken := *oldJob.LeaseToken

	expireLease(t, db, id)
	res, err := m.ReclaimStale(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("回收失败: %v", err)
	}
	if res.Reclaimed != 1 || res.Merged != 0 {
		t.Fatalf("应回收 1 条、合并 0 条，实际 %+v", res)
	}
	newJob, err := m.Claim(ctx, time.Minute)
	if err != nil || newJob == nil {
		t.Fatalf("重新认领失败: job=%v err=%v", newJob, err)
	}
	if newJob.LeaseToken == nil || *newJob.LeaseToken == oldToken {
		t.Fatal("重新认领必须换发新的租约令牌")
	}
	if newJob.Attempt != 2 {
		t.Fatalf("重新认领 attempt 应累加到 2，实际 %d", newJob.Attempt)
	}

	jobPrimary := jobID(t, id)
	oldArtifact, newArtifact := uuid.NewString(), uuid.NewString()
	if err := m.MarkSucceeded(ctx, jobPrimary, oldToken, oldArtifact, time.Now().UTC()); !errors.Is(err, buildmodel.ErrLeaseLost) {
		t.Fatalf("旧令牌的成功结论应被拒绝，实际 err=%v", err)
	}
	if err := m.MarkFailed(ctx, jobPrimary, oldToken, "旧 worker 的失败", time.Now().UTC()); !errors.Is(err, buildmodel.ErrLeaseLost) {
		t.Fatalf("旧令牌的失败结论应被拒绝，实际 err=%v", err)
	}
	row := loadJob(t, db, id)
	if row.Status != buildmodel.StatusRunning {
		t.Fatalf("旧 worker 不得改动状态，实际 %q", row.Status)
	}
	if row.LeaseToken == nil || *row.LeaseToken != *newJob.LeaseToken {
		t.Fatal("旧 worker 不得改动新租约令牌")
	}
	if row.ArtifactID != nil {
		t.Fatalf("旧 worker 不得写入产物 id: %v", *row.ArtifactID)
	}

	if err := m.MarkSucceeded(ctx, jobPrimary, *newJob.LeaseToken, newArtifact, time.Now().UTC()); err != nil {
		t.Fatalf("新令牌完成失败: %v", err)
	}
	row = loadJob(t, db, id)
	if row.Status != buildmodel.StatusSucceeded || row.ArtifactID == nil || *row.ArtifactID != newArtifact {
		t.Fatalf("新 worker 的结论应落库: status=%q artifact=%v", row.Status, row.ArtifactID)
	}
}

func TestReclaimMergesDuplicateWithoutBlockingOthers(t *testing.T) {
	db, svc := newBuildFixture(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	m := svc.Model()

	srcBad := uuid.NewString()
	bad := enqueueJob(t, svc, &builddto.EnqueueReq{SourceType: "page", SourceID: srcBad, DraftVersion: 1, BuildInputHash: "same"})
	badJob, err := m.Claim(ctx, time.Minute)
	if err != nil || badJob == nil {
		t.Fatalf("认领坏任务失败: job=%v err=%v", badJob, err)
	}
	dup := enqueueJob(t, svc, &builddto.EnqueueReq{SourceType: "page", SourceID: srcBad, DraftVersion: 1, BuildInputHash: "same"})

	others := make([]string, 0, 2)
	for i := 0; i < 2; i++ {
		id := enqueueJob(t, svc, &builddto.EnqueueReq{SourceType: "presentation", SourceID: uuid.NewString()})
		if _, err := m.Claim(ctx, time.Minute); err != nil {
			t.Fatalf("认领僵尸任务 %s 失败: %v", id, err)
		}
		expireLease(t, db, id)
		others = append(others, id)
	}
	expireLease(t, db, bad)

	res, err := m.ReclaimStale(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("回收不应因一条撞唯一键的任务整条失败: %v", err)
	}
	if res.Reclaimed != int64(len(others)) {
		t.Fatalf("应有 %d 条退回 pending，实际 %d（其它僵尸任务被坏任务拖住了）", len(others), res.Reclaimed)
	}
	if res.Merged != 1 {
		t.Fatalf("应合并 1 条重复陈旧任务，实际 %d", res.Merged)
	}
	if row := loadJob(t, db, bad); row.Status != buildmodel.StatusSuperseded {
		t.Fatalf("重复的陈旧任务应标 superseded，实际 %q", row.Status)
	}
	if row := loadJob(t, db, dup); row.Status != buildmodel.StatusPending {
		t.Fatalf("队列里的待办不应被动到，实际 %q", row.Status)
	}
	for _, id := range others {
		if row := loadJob(t, db, id); row.Status != buildmodel.StatusPending {
			t.Fatalf("僵尸任务 %s 应退回 pending，实际 %q", id, row.Status)
		}
	}

	next, err := m.Claim(ctx, time.Minute)
	if err != nil || next == nil {
		t.Fatalf("回收后应能继续认领: job=%v err=%v", next, err)
	}
	if next.ID != jobID(t, dup) {
		t.Fatalf("被合并的工作应由队列里的待办承担（%s），实际认领到 %d", dup, next.ID)
	}
}

func TestEnqueueStoresExplicitScope(t *testing.T) {
	db, svc := newBuildFixture(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	projectID := uuid.NewString()

	manual := enqueueJob(t, svc, &builddto.EnqueueReq{
		SourceType: "page", SourceID: uuid.NewString(), ProjectID: projectID, Lang: "zh-CN",
		Intent: buildmodel.IntentManual, DraftVersion: 2,
	})
	row := loadJob(t, db, manual)
	if row.ProjectID == nil || *row.ProjectID != projectID {
		t.Fatalf("project_id 应显式入库: %v", row.ProjectID)
	}
	if row.Lang != "zh-CN" {
		t.Fatalf("lang 应显式入库，实际 %q", row.Lang)
	}
	if row.Intent != buildmodel.IntentManual {
		t.Fatalf("intent 应显式入库，实际 %q", row.Intent)
	}

	pageID := uuid.NewString()
	if err := svc.EnqueuePageBuild(ctx, pageID, projectID, "zh-CN", buildmodel.IntentDependency, 3, "h"); err != nil {
		t.Fatalf("EnqueuePageBuild 失败: %v", err)
	}

	if err := svc.EnqueuePageBuild(ctx, pageID, projectID, "zh-CN", buildmodel.IntentManual, 3, "h"); err != nil {
		t.Fatalf("EnqueuePageBuild(manual) 失败: %v", err)
	}

	if err := svc.EnqueuePageBuild(ctx, pageID, projectID, "en-US", buildmodel.IntentDependency, 3, "h"); err != nil {
		t.Fatalf("EnqueuePageBuild(en-US) 失败: %v", err)
	}

	presentationID := uuid.NewString()
	if err := svc.EnqueuePresentationBuild(ctx, presentationID, projectID); err != nil {
		t.Fatalf("EnqueuePresentationBuild 失败: %v", err)
	}

	var pageJobs []struct {
		SourceID  string  `gorm:"column:source_id"`
		ProjectID *string `gorm:"column:project_id"`
		Intent    string  `gorm:"column:intent"`
		Lang      string  `gorm:"column:lang"`
	}
	if err := db.Raw("SELECT source_id::text AS source_id, project_id, intent, lang FROM build_jobs WHERE source_id = ? ORDER BY id", pageID).Scan(&pageJobs).Error; err != nil {
		t.Fatalf("读取 page 端口入队的任务失败: %v", err)
	}
	wantPageJobs := map[string]string{"zh-CN|" + buildmodel.IntentDependency: "", "zh-CN|" + buildmodel.IntentManual: "", "en-US|" + buildmodel.IntentDependency: ""}
	if len(pageJobs) != len(wantPageJobs) {
		t.Fatalf("page 端口应入队 %d 条（两语言 + 手工/依赖两种意图），实际 %d 条: %+v", len(wantPageJobs), len(pageJobs), pageJobs)
	}
	for _, got := range pageJobs {
		if got.SourceID != pageID {
			t.Fatalf("读取到的任务不是期望的来源: %q", got.SourceID)
		}
		if got.ProjectID == nil || *got.ProjectID != projectID {
			t.Fatalf("端口入队应带工程作用域: %v", got.ProjectID)
		}
		if _, ok := wantPageJobs[got.Lang+"|"+got.Intent]; !ok {
			t.Fatalf("page 端口入队的语言/意图不在期望集合内: lang=%q intent=%q", got.Lang, got.Intent)
		}
		delete(wantPageJobs, got.Lang+"|"+got.Intent)
	}
	if len(wantPageJobs) != 0 {
		t.Fatalf("page 端口缺少这些语言/意图的任务: %v", wantPageJobs)
	}

	var presJob struct {
		ProjectID *string `gorm:"column:project_id"`
		Intent    string  `gorm:"column:intent"`
		Lang      string  `gorm:"column:lang"`
	}
	if err := db.Raw("SELECT project_id, intent, lang FROM build_jobs WHERE source_id = ?", presentationID).Scan(&presJob).Error; err != nil {
		t.Fatalf("读取 presentation 端口入队的任务失败: %v", err)
	}
	if presJob.ProjectID == nil || *presJob.ProjectID != projectID {
		t.Fatalf("presentation 端口入队应带工程作用域: %v", presJob.ProjectID)
	}
	if presJob.Intent != buildmodel.IntentDependency {
		t.Fatalf("presentation 端口入队的意图应为依赖重建，实际 %q", presJob.Intent)
	}
	if presJob.Lang != "" {
		t.Fatalf("presentation 没有语言维度，lang 应为空串，实际 %q", presJob.Lang)
	}

	if _, created, err := svc.Enqueue(ctx, &builddto.EnqueueReq{
		SourceType: "page", SourceID: uuid.NewString(), Intent: "whatever",
	}); err == nil || created {
		t.Fatalf("非法意图应被拒绝: created=%v err=%v", created, err)
	}
	var n int64
	if err := db.Table("build_jobs").Count(&n).Error; err != nil {
		t.Fatalf("统计任务失败: %v", err)
	}

	if n != 5 {
		t.Fatalf("非法意图不应留下任务行，期望 5 行，实际 %d", n)
	}
}

func TestRetryFailedIsAtomicUnderRace(t *testing.T) {
	db, svc := newBuildFixture(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	id := enqueueJob(t, svc, &builddto.EnqueueReq{SourceType: "page", SourceID: uuid.NewString(), DraftVersion: 1})
	svc.RegisterExecutor("page", func(_ context.Context, _ *buildcontract.Job) error {
		return errors.New("暂时性失败")
	})
	if _, err := svc.RunOnce(ctx); err != nil {
		t.Fatalf("消费失败: %v", err)
	}
	if row := loadJob(t, db, id); row.Status != buildmodel.StatusFailed || row.ErrorMsg == nil {
		t.Fatalf("应为 failed 且带原因: %+v", row)
	}

	const goroutines = 6
	var mu sync.Mutex
	okCount, notFound := 0, 0
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := svc.Retry(ctx, "", id)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				okCount++
			case err.Error() == buildenums.ErrJobNotFound:
				notFound++
			default:
				t.Errorf("并发重试出现意外错误: %v", err)
			}
		}()
	}
	wg.Wait()
	if okCount != 1 {
		t.Fatalf("并发重试应恰好生效一次，实际 %d 次", okCount)
	}
	if notFound != goroutines-1 {
		t.Fatalf("其余重试应报「任务不存在或不是失败态」，实际 %d 次", notFound)
	}

	row := loadJob(t, db, id)
	if row.Status != buildmodel.StatusPending || row.LeaseToken != nil || row.LeaseExpires != nil {
		t.Fatalf("重试后应为干净的 pending: %+v", row)
	}
	svc.RegisterExecutor("page", func(_ context.Context, _ *buildcontract.Job) error { return nil })
	if _, err := svc.RunOnce(ctx); err != nil {
		t.Fatalf("重试后消费失败: %v", err)
	}
	if row := loadJob(t, db, id); row.Status != buildmodel.StatusSucceeded {
		t.Fatalf("重试后应能跑成功，实际 %q", row.Status)
	}
}

func TestCancelIsExclusiveWithCompletion(t *testing.T) {
	db, svc := newBuildFixture(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	m := svc.Model()
	id := enqueueJob(t, svc, &builddto.EnqueueReq{SourceType: "page", SourceID: uuid.NewString(), DraftVersion: 1})
	job, err := m.Claim(ctx, time.Minute)
	if err != nil || job == nil {
		t.Fatalf("认领失败: job=%v err=%v", job, err)
	}
	token := *job.LeaseToken
	primary := jobID(t, id)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errs[0] = m.MarkSuperseded(ctx, primary, token, "上游已重新发布", time.Now().UTC())
	}()
	go func() {
		defer wg.Done()
		errs[1] = m.MarkSucceeded(ctx, primary, token, uuid.NewString(), time.Now().UTC())
	}()
	wg.Wait()

	applied := 0
	for _, e := range errs {
		switch {
		case e == nil:
			applied++
		case errors.Is(e, buildmodel.ErrLeaseLost):
		default:
			t.Fatalf("意外错误: %v", e)
		}
	}
	if applied != 1 {
		t.Fatalf("取消与完成应恰好一个生效，实际 %d 个", applied)
	}
	row := loadJob(t, db, id)
	if row.Status != buildmodel.StatusSuperseded && row.Status != buildmodel.StatusSucceeded {
		t.Fatalf("状态应为取消或完成之一，实际 %q", row.Status)
	}

	if err := m.MarkFailed(ctx, primary, token, "迟到的失败", time.Now().UTC()); !errors.Is(err, buildmodel.ErrLeaseLost) {
		t.Fatalf("已结案的任务不得再被改动，实际 err=%v", err)
	}
}

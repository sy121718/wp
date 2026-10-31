package feature

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	builddto "go_wp/internal/module/build/dto"
	buildmodel "go_wp/internal/module/build/model"
)

func TestPendingKeySeparatesLangAndIntent(t *testing.T) {
	db, svc := newBuildFixture(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	src := uuid.NewString()
	base := func(lang, intent string) *builddto.EnqueueReq {
		return &builddto.EnqueueReq{
			SourceType: "page", SourceID: src, Lang: lang, Intent: intent,
			DraftVersion: 1, BuildInputHash: "h1",
		}
	}

	zhDep := enqueueJob(t, svc, base("zh-CN", buildmodel.IntentDependency))
	zhManual := enqueueJob(t, svc, base("zh-CN", buildmodel.IntentManual))
	enDep := enqueueJob(t, svc, base("en-US", buildmodel.IntentDependency))

	if _, created, err := svc.Enqueue(ctx, base("zh-CN", buildmodel.IntentDependency)); err != nil || created {
		t.Fatalf("完全同键的重复待办应被去重: created=%v err=%v", created, err)
	}

	for _, id := range []string{zhDep, zhManual, enDep} {
		row := loadJob(t, db, id)
		if row.Status != buildmodel.StatusPending {
			t.Fatalf("任务 %s 应为 pending，实际 %q", id, row.Status)
		}
	}
	var n int64
	if err := db.Table("build_jobs").Where("source_id = ?", src).Count(&n).Error; err != nil {
		t.Fatalf("统计任务失败: %v", err)
	}
	if n != 3 {
		t.Fatalf("两语言 + 手工/依赖两种意图应入队 3 条，实际 %d 条", n)
	}
}

func TestReclaimMergeJudgementMatchesPendingKey(t *testing.T) {
	db, svc := newBuildFixture(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	m := svc.Model()

	langSrc, intentSrc, dupSrc := uuid.NewString(), uuid.NewString(), uuid.NewString()
	claimOne := func(id string) {
		t.Helper()
		job, err := m.Claim(ctx, time.Minute)
		if err != nil || job == nil {
			t.Fatalf("认领任务失败: job=%v err=%v", job, err)
		}
		if job.SourceID != id {
			t.Fatalf("认领到的不是期望的任务: got=%s want=%s", job.SourceID, id)
		}
		expireLease(t, db, strconv.FormatInt(job.ID, 10))
	}

	langRunning := enqueueJob(t, svc, &builddto.EnqueueReq{
		SourceType: "page", SourceID: langSrc, Lang: "zh-CN", Intent: buildmodel.IntentDependency,
		DraftVersion: 1, BuildInputHash: "shared"})
	claimOne(langSrc)
	langPending := enqueueJob(t, svc, &builddto.EnqueueReq{
		SourceType: "page", SourceID: langSrc, Lang: "en-US", Intent: buildmodel.IntentDependency,
		DraftVersion: 1, BuildInputHash: "shared"})

	intentRunning := enqueueJob(t, svc, &builddto.EnqueueReq{
		SourceType: "page", SourceID: intentSrc, Lang: "zh-CN", Intent: buildmodel.IntentManual,
		DraftVersion: 1, BuildInputHash: "shared"})
	claimOne(intentSrc)
	intentPending := enqueueJob(t, svc, &builddto.EnqueueReq{
		SourceType: "page", SourceID: intentSrc, Lang: "zh-CN", Intent: buildmodel.IntentDependency,
		DraftVersion: 1, BuildInputHash: "shared"})

	dupRunning := enqueueJob(t, svc, &builddto.EnqueueReq{
		SourceType: "page", SourceID: dupSrc, Lang: "zh-CN", Intent: buildmodel.IntentDependency,
		DraftVersion: 1, BuildInputHash: "same"})
	claimOne(dupSrc)
	dupPending := enqueueJob(t, svc, &builddto.EnqueueReq{
		SourceType: "page", SourceID: dupSrc, Lang: "zh-CN", Intent: buildmodel.IntentDependency,
		DraftVersion: 1, BuildInputHash: "same"})

	res, err := m.ReclaimStale(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("回收失败: %v", err)
	}
	if res.Reclaimed != 2 || res.Merged != 1 {
		t.Fatalf("语言 / 意图不同的两份陈旧工作应各退回 pending（Reclaimed=2），只有完全同键的那份该被合并（Merged=1）；实际 %+v", res)
	}
	for _, id := range []string{langRunning, intentRunning} {
		if row := loadJob(t, db, id); row.Status != buildmodel.StatusPending {
			t.Fatalf("语言 / 意图不同 ≠ 重复工作，任务 %s 应退回 pending，实际 %q", id, row.Status)
		}
	}
	if row := loadJob(t, db, dupRunning); row.Status != buildmodel.StatusSuperseded {
		t.Fatalf("完全同键的陈旧任务应被合并为 superseded，实际 %q", row.Status)
	}

	for _, id := range []string{langPending, intentPending, dupPending} {
		if row := loadJob(t, db, id); row.Status != buildmodel.StatusPending {
			t.Fatalf("队列里的待办不应被动到，任务 %s 实际 %q", id, row.Status)
		}
	}
}

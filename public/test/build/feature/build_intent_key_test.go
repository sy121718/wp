package feature

// build_intent_key_test.go — 待办去重键的语言 / 意图维度（迁移 307，审计 ARCH-04）。
//
// 两条不变量：
//  1. 入队侧：同来源同输入、**不同语言**或**不同意图**是不同工作，必须各自成行；
//     完全同键的重复入队仍然被去重（幂等不变）。
//  2. 回收侧：has_pending 的判据必须与索引同形 —— 少比语言 / 意图会把两份不同的工作
//     合并掉（一条租约到期的人工构建看到同键的依赖重建待办就自我作废）。

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	builddto "go_wp/internal/module/build/dto"
	buildmodel "go_wp/internal/module/build/model"
)

// TestPendingKeySeparatesLangAndIntent 同一来源同一输入，语言 / 意图不同即不同待办。
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

	// 同键重复：仍然幂等（不新建）。
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

// TestReclaimMergeJudgementMatchesPendingKey 回收的归并判定与新待办键同形。
//
// 这是**失败能力验证**的靶子：把 reclaimStaleSQL 的 has_pending 改回只比
// (source_type, source_id, build_input_hash)，两条陈旧任务会被误判为「工作已排在队列里」
// 而标 superseded（Merged=2 / Reclaimed=0），本用例立刻变红。
func TestReclaimMergeJudgementMatchesPendingKey(t *testing.T) {
	db, svc := newBuildFixture(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	m := svc.Model()

	// 三条 running 的构造顺序有讲究：先把每条**认领**成 running，再入队同键的待办 ——
	// 同键的两条待办会被 uq_build_jobs_pending 去重（索引只覆盖 pending 行），
	// 而 running + pending 同键是合法的（正是回收要合并的那种历史形态）。
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

	// 场景一：语言不同（zh-CN 在跑，队列里是同输入的 en-US）——不是同一份工作。
	langRunning := enqueueJob(t, svc, &builddto.EnqueueReq{
		SourceType: "page", SourceID: langSrc, Lang: "zh-CN", Intent: buildmodel.IntentDependency,
		DraftVersion: 1, BuildInputHash: "shared"})
	claimOne(langSrc)
	langPending := enqueueJob(t, svc, &builddto.EnqueueReq{
		SourceType: "page", SourceID: langSrc, Lang: "en-US", Intent: buildmodel.IntentDependency,
		DraftVersion: 1, BuildInputHash: "shared"})

	// 场景二：意图不同（人工构建在跑，队列里是同键的依赖重建）——同样不是同一份工作。
	intentRunning := enqueueJob(t, svc, &builddto.EnqueueReq{
		SourceType: "page", SourceID: intentSrc, Lang: "zh-CN", Intent: buildmodel.IntentManual,
		DraftVersion: 1, BuildInputHash: "shared"})
	claimOne(intentSrc)
	intentPending := enqueueJob(t, svc, &builddto.EnqueueReq{
		SourceType: "page", SourceID: intentSrc, Lang: "zh-CN", Intent: buildmodel.IntentDependency,
		DraftVersion: 1, BuildInputHash: "shared"})

	// 对照组：同来源同输入同语言同意图的陈旧 running —— 这才是真的重复工作，应当被合并。
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
	// 队列里那三条待办不受影响，四份工作都在（2 条退回 + 1 条被合并 + 1 条同键待办 + 2 条待办）。
	for _, id := range []string{langPending, intentPending, dupPending} {
		if row := loadJob(t, db, id); row.Status != buildmodel.StatusPending {
			t.Fatalf("队列里的待办不应被动到，任务 %s 实际 %q", id, row.Status)
		}
	}
}

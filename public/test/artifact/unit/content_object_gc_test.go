package unit

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	artifactdto "go_wp/internal/module/artifact/dto"
	artifactenums "go_wp/internal/module/artifact/enums"
	artifactservice "go_wp/internal/module/artifact/service"
)

// 审计 IDX-016：content_objects 只写不删 —— 产物行被回收（payload_state='deleted'）之后，
// 它闭包里引用的共享内容对象已无人引用，却永远留在表里。本文件的测试固化标记清除语义：
// 「谁还在用」判定要认得出已回收的产物行（否则孤儿永远产生不了），同时绝不动仍在用的对象。

// 保留窗口是 30 天，测试用 60 天前确保落在窗口之外。
const gcAgeDays = 60

func gcReq(dryRun bool) *artifactdto.ContentObjectGCReq {
	return &artifactdto.ContentObjectGCReq{DryRun: &dryRun}
}

// ageContentObjects 把所有内容对象的创建时间挪到保留窗口之外（模拟长期积累）。
func ageContentObjects(t *testing.T, svc *artifactservice.Service, days int) {
	t.Helper()
	cut := time.Now().UTC().AddDate(0, 0, -days)
	if err := svc.Model().DB(context.Background()).Table("content_objects").
		Where("1 = 1").Update("created_at", cut).Error; err != nil {
		t.Fatalf("调整内容对象创建时间失败: %v", err)
	}
}

// setArtifactState 把产物行改成指定负载状态，并把它的创建时间挪旧。
func setArtifactState(t *testing.T, svc *artifactservice.Service, id, state string, days int) {
	t.Helper()
	cut := time.Now().UTC().AddDate(0, 0, -days)
	if err := svc.Model().DB(context.Background()).Where("id = ?", id).
		Updates(map[string]any{"payload_state": state, "created_at": cut}).Error; err != nil {
		t.Fatalf("更新产物负载状态失败: %v", err)
	}
}

// seedKeptArtifact 归档第二个产物（仍在用），闭包是另外两个内容对象。
func seedKeptArtifact(t *testing.T, svc *artifactservice.Service) string {
	t.Helper()
	req := validReq()
	req.ArtifactID = testArtifactID2
	req.PageID = "bbbbbbbb-0000-0000-0000-000000000002"
	req.ArtifactHash = artifactHashV2
	req.ArtifactKey = "artifacts/" + artifactHashV2
	req.Manifest = json.RawMessage(`{"canonicalPath":"/keep.html","files":{"keep.html":"hash-keep","keep.json":"hash-keep-manifest"}}`)
	kept := mustRecord(t, svc, req)
	return kept.ID
}

// TestContentObjectGCDeletesOrphansKeepsReferenced 是本条审计的主用例：
// 已回收产物引用的对象被清掉，仍在用产物引用的对象一个都不能动。
func TestContentObjectGCDeletesOrphansKeepsReferenced(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	collected := mustRecord(t, svc, validReq())
	seedKeptArtifact(t, svc)
	ageContentObjects(t, svc, gcAgeDays)
	setArtifactState(t, svc, collected.ID, "deleted", gcAgeDays)

	// 预演：只数不动表。首次上线前正是靠这一步看数量。
	preview, err := svc.GarbageCollectContentObjects(ctx, gcReq(true))
	if err != nil {
		t.Fatalf("dryRun 预演失败: %v", err)
	}
	if preview.Orphans != 2 {
		t.Fatalf("孤儿内容对象应为 2 条: %d", preview.Orphans)
	}
	if preview.Deleted != 0 {
		t.Fatalf("dryRun 不应删除任何行: %d", preview.Deleted)
	}
	if n := contentObjectCount(t, svc); n != 4 {
		t.Fatalf("dryRun 后 content_objects 应仍为 4 行: %d", n)
	}
	if len(preview.Items) != 2 {
		t.Fatalf("预演应列出 2 条候选: %d", len(preview.Items))
	}

	// 真删：只有已回收产物的那两个对象被清掉。
	res, err := svc.GarbageCollectContentObjects(ctx, gcReq(false))
	if err != nil {
		t.Fatalf("回收失败: %v", err)
	}
	if res.Deleted != 2 {
		t.Fatalf("应删除 2 条孤儿对象: %d", res.Deleted)
	}
	if contentObjectExists(t, svc, "hash-html") || contentObjectExists(t, svc, "hash-manifest") {
		t.Fatalf("已回收产物引用的内容对象应被清理")
	}
	if !contentObjectExists(t, svc, "hash-keep") || !contentObjectExists(t, svc, "hash-keep-manifest") {
		t.Fatalf("仍在用产物引用的内容对象不得被删除")
	}
}

// TestContentObjectGCKeepsAvailableReferences 反向锚点：产物行只要还不是 deleted，
// 它的闭包对象就不是孤儿 —— 判定不能退化成「被引用过就删」。
func TestContentObjectGCKeepsAvailableReferences(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	mustRecord(t, svc, validReq())
	ageContentObjects(t, svc, gcAgeDays)

	res, err := svc.GarbageCollectContentObjects(ctx, gcReq(false))
	if err != nil {
		t.Fatalf("回收失败: %v", err)
	}
	if res.Orphans != 0 || res.Deleted != 0 {
		t.Fatalf("可用产物的闭包对象不应被判为孤儿: orphans=%d deleted=%d", res.Orphans, res.Deleted)
	}
	if n := contentObjectCount(t, svc); n != 2 {
		t.Fatalf("content_objects 应保持 2 行: %d", n)
	}
}

// TestContentObjectGCKeepsExternalReferences 覆盖「引用来源不止一处」：
// 外部模块（自动发布实例的 presentation_artifact_objects）声明的引用必须被尊重。
func TestContentObjectGCKeepsExternalReferences(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	rec := mustRecord(t, svc, validReq())
	ageContentObjects(t, svc, gcAgeDays)
	setArtifactState(t, svc, rec.ID, "deleted", gcAgeDays)

	svc.SetExternalContentRefs(func(_ context.Context, hashes []string) ([]string, error) {
		for _, h := range hashes {
			if h == "hash-manifest" {
				return []string{h}, nil
			}
		}
		return nil, nil
	})

	res, err := svc.GarbageCollectContentObjects(ctx, gcReq(false))
	if err != nil {
		t.Fatalf("回收失败: %v", err)
	}
	if res.Deleted != 1 || res.SkippedExtern != 1 {
		t.Fatalf("应删 1 条、因外部引用跳过 1 条: deleted=%d skipped=%d", res.Deleted, res.SkippedExtern)
	}
	if !contentObjectExists(t, svc, "hash-manifest") {
		t.Fatalf("被外部模块引用的内容对象不得删除")
	}
	if contentObjectExists(t, svc, "hash-html") {
		t.Fatalf("无任何引用的内容对象应被删除")
	}
}

// TestContentObjectGCFailsClosedOnExternalQueryError 固化失败口径：
// 问不到外部引用就整轮不删 —— 误删共享对象的代价高于多留一轮垃圾。
func TestContentObjectGCFailsClosedOnExternalQueryError(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	rec := mustRecord(t, svc, validReq())
	ageContentObjects(t, svc, gcAgeDays)
	setArtifactState(t, svc, rec.ID, "deleted", gcAgeDays)

	svc.SetExternalContentRefs(func(_ context.Context, _ []string) ([]string, error) {
		return nil, errors.New("外部引用来源不可用")
	})

	_, err := svc.GarbageCollectContentObjects(ctx, gcReq(false))
	if err == nil {
		t.Fatalf("外部引用查询失败时应返回错误")
	}
	if !strings.Contains(err.Error(), artifactenums.ErrContentRefQueryFailed) {
		t.Fatalf("错误应锚定 enums 常量 %s: %v", artifactenums.ErrContentRefQueryFailed, err)
	}
	if n := contentObjectCount(t, svc); n != 2 {
		t.Fatalf("引用关系不明时不得删除任何行: %d", n)
	}
}

// TestContentObjectGCRespectsRetentionWindow 固化保留窗口：
// 产物可以是旧的，但对象刚写入就不回收（可能只是还没被后续归档复用）。
func TestContentObjectGCRespectsRetentionWindow(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	rec := mustRecord(t, svc, validReq())
	setArtifactState(t, svc, rec.ID, "deleted", gcAgeDays)

	res, err := svc.GarbageCollectContentObjects(ctx, gcReq(false))
	if err != nil {
		t.Fatalf("回收失败: %v", err)
	}
	if res.Orphans != 0 || res.Deleted != 0 {
		t.Fatalf("窗口内的对象不应被回收: orphans=%d deleted=%d", res.Orphans, res.Deleted)
	}
	if n := contentObjectCount(t, svc); n != 2 {
		t.Fatalf("content_objects 应保持 2 行: %d", n)
	}
}

// TestContentObjectGCNilRequestUsesSafeDefaults 固化安全默认：
// nil 请求 = dryRun（不删），与产物 GC 同口径 —— 不传参数不该顺手删数据。
func TestContentObjectGCNilRequestUsesSafeDefaults(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	rec := mustRecord(t, svc, validReq())
	ageContentObjects(t, svc, gcAgeDays)
	setArtifactState(t, svc, rec.ID, "deleted", gcAgeDays)

	res, err := svc.GarbageCollectContentObjects(ctx, nil)
	if err != nil {
		t.Fatalf("回收失败: %v", err)
	}
	if !res.DryRun {
		t.Fatalf("nil 请求应落在 dryRun 上")
	}
	if res.Deleted != 0 {
		t.Fatalf("dryRun 不应删除任何行: %d", res.Deleted)
	}
	if n := contentObjectCount(t, svc); n != 2 {
		t.Fatalf("content_objects 应保持 2 行: %d", n)
	}
}

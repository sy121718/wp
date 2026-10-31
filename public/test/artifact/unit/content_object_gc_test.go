package unit

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"
	"time"

	artifactdto "go_wp/internal/module/artifact/dto"
	artifactenums "go_wp/internal/module/artifact/enums"
	artifactservice "go_wp/internal/module/artifact/service"
)

const gcAgeDays = 60

func gcReq(dryRun bool) *artifactdto.ContentObjectGCReq {
	return &artifactdto.ContentObjectGCReq{DryRun: &dryRun}
}

func ageContentObjects(t *testing.T, svc *artifactservice.Service, days int) {
	t.Helper()
	cut := time.Now().UTC().AddDate(0, 0, -days)
	if err := svc.Model().DB(context.Background()).Table("content_objects").
		Where("1 = 1").Update("create_time", cut).Error; err != nil {
		t.Fatalf("调整内容对象创建时间失败: %v", err)
	}
}

func setArtifactState(t *testing.T, svc *artifactservice.Service, id, state string, days int) {
	t.Helper()
	cut := time.Now().UTC().AddDate(0, 0, -days)
	if err := svc.Model().DB(context.Background()).Where("id = ?", id).
		Updates(map[string]any{"payload_state": state, "create_time": cut}).Error; err != nil {
		t.Fatalf("更新产物负载状态失败: %v", err)
	}
}

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

func TestContentObjectGCDeletesOrphansKeepsReferenced(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	collected := mustRecord(t, svc, validReq())
	seedKeptArtifact(t, svc)
	ageContentObjects(t, svc, gcAgeDays)
	setArtifactState(t, svc, collected.ID, "deleted", gcAgeDays)

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

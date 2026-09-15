package feature

import (
	"context"
	"encoding/json"
	"testing"

	"gorm.io/gorm"

	pagedto "go_wp/internal/module/page/dto"
)

// TestContentObjectGCSurvivesRollback 审计 IDX-016 的第二条验收：内容对象清理不得伤到回滚。
//
// 判据不是「GC 跑过没报错」，而是：保留窗口内仍处 available 的历史版本产物，所引用的
// 共享内容对象一个都不能少 —— 少一个，回滚出来的页面就指向不存在的内容对象。
// 因此用例在真实发布链路上跑两趟保留期任务，再回滚。
func TestContentObjectGCSurvivesRollback(t *testing.T) {
	db, svc, projectID := newPageService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/gc-rollback", DraftDocument: json.RawMessage(pageDocument),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}
	built1, err := svc.Build(ctx, &pagedto.BuildReq{ID: created.ID})
	if err != nil {
		t.Fatalf("构建 v1 失败: %v", err)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: created.ID}); err != nil {
		t.Fatalf("发布 v1 失败: %v", err)
	}

	// 第一趟：v1 正在线上。保留期任务不该回收任何东西（指针与窗口双重保护）。
	if _, perr := svc.PurgeRetention(ctx); perr != nil {
		t.Fatalf("保留期清理失败: %v", perr)
	}

	// v2 发布，v1 转历史（产物行仍 available、仍在保留窗口内）。
	if _, err = svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: created.ID, ExpectedVersion: created.DraftVersion,
		DraftPath: "/gc-rollback", DraftDocument: json.RawMessage(docV2),
	}); err != nil {
		t.Fatalf("保存 v2 失败: %v", err)
	}
	if _, err = svc.Build(ctx, &pagedto.BuildReq{ID: created.ID}); err != nil {
		t.Fatalf("构建 v2 失败: %v", err)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: created.ID}); err != nil {
		t.Fatalf("发布 v2 失败: %v", err)
	}

	// 第二趟：v1 已是历史版本，但它仍要能被回滚 —— 内容对象必须留着。
	if _, perr := svc.PurgeRetention(ctx); perr != nil {
		t.Fatalf("保留期清理失败: %v", perr)
	}

	v1ID := artifactIDByHash(t, db, created.ID, built1.StagedHash)
	if v1ID == "" {
		t.Fatalf("v1 产物行在保留窗口内不该消失")
	}
	hashes := closureHashes(t, db, v1ID)
	if len(hashes) == 0 {
		t.Fatalf("v1 产物行没有闭包记录，用例前提不成立")
	}
	for _, h := range hashes {
		if n := contentObjectRows(t, db, h); n != 1 {
			t.Fatalf("回滚目标的内容对象被误清: %s 剩 %d 行", h, n)
		}
	}

	// 最终判据：清理没有把回滚窗口削掉。
	rolledBack, err := svc.Rollback(ctx, &pagedto.RollbackReq{ID: created.ID, TargetHash: built1.StagedHash})
	if err != nil {
		t.Fatalf("清理后回滚失败: %v", err)
	}
	if rolledBack.ActiveHash != built1.StagedHash {
		t.Fatalf("回滚目标错误: %+v", rolledBack)
	}
}

// artifactIDByHash 按产物 hash 取产物行 ID（空串表示不存在）。
func artifactIDByHash(t *testing.T, db *gorm.DB, pageID, hash string) string {
	t.Helper()
	var id string
	if err := db.Raw("SELECT id FROM page_artifacts WHERE page_id = ? AND artifact_hash = ?", pageID, hash).
		Scan(&id).Error; err != nil {
		t.Fatalf("查询产物行失败: %v", err)
	}
	return id
}

// closureHashes 取某产物行的对象闭包 hash 列表。
func closureHashes(t *testing.T, db *gorm.DB, artifactID string) []string {
	t.Helper()
	hashes := []string{}
	if err := db.Table("page_artifact_objects").Where("artifact_id = ?", artifactID).
		Pluck("content_hash", &hashes).Error; err != nil {
		t.Fatalf("查询对象闭包失败: %v", err)
	}
	return hashes
}

// contentObjectRows 统计某个内容对象的行数。
func contentObjectRows(t *testing.T, db *gorm.DB, hash string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM content_objects WHERE content_hash = ?", hash).Scan(&n).Error; err != nil {
		t.Fatalf("统计内容对象失败: %v", err)
	}
	return n
}

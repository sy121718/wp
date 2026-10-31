package unit

import (
	"context"
	"fmt"
	json "go_wp/pkg/jsonx"
	"testing"
	"time"

	pagedto "go_wp/internal/module/page/dto"
)

const emptyRevDoc = `{"settings":{"layout":{"mode":"full"}},"root":[]}`

func TestSaveDraftPrunesRevisionHistory(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/rev", DraftDocument: json.RawMessage(emptyRevDoc),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}

	version := created.DraftVersion
	for i := 0; i < 30; i++ {
		doc := json.RawMessage(fmt.Sprintf("{\"settings\":{\"layout\":{\"mode\":\"full\"},\"seo\":{\"title\":\"第%d版\"}},\"root\":[]}", i))
		res, serr := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
			ID: created.ID, ExpectedVersion: version, DraftPath: "/rev", DraftDocument: doc,
		})
		if serr != nil {
			t.Fatalf("第 %d 次保存失败: %v", i, serr)
		}
		version = res.DraftVersion
	}
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM page_revisions WHERE page_id = ?", created.ID).Scan(&count).Error; err != nil {
		t.Fatalf("统计修订失败: %v", err)
	}

	if count > 21 {
		t.Fatalf("历史快照应被收敛到上限附近，实际 %d 条", count)
	}

	if version != int64(31) {
		t.Fatalf("版本号应为 31，实际 %d", version)
	}
}

func TestPurgeRetentionKeepsRecentRevisions(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/keep", DraftDocument: json.RawMessage(emptyRevDoc),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}
	version := created.DraftVersion
	for i := 0; i < 5; i++ {
		res, serr := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
			ID: created.ID, ExpectedVersion: version, DraftPath: "/keep",
			DraftDocument: json.RawMessage(fmt.Sprintf("{\"settings\":{\"layout\":{\"mode\":\"full\"},\"seo\":{\"title\":\"v%d\"}},\"root\":[]}", i)),
		})
		if serr != nil {
			t.Fatalf("保存失败: %v", serr)
		}
		version = res.DraftVersion
	}
	if _, derr := svc.PurgeRetention(ctx); derr != nil {
		t.Fatalf("保留期清理失败: %v", derr)
	}
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM page_revisions WHERE page_id = ?", created.ID).Scan(&count).Error; err != nil {
		t.Fatalf("统计修订失败: %v", err)
	}
	if count < 5 {
		t.Fatalf("保留期内的快照不该被删，实际剩 %d 条", count)
	}
}

func TestPurgeRetentionDeletesExpiredRevisions(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/stale", DraftDocument: json.RawMessage(emptyRevDoc),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}
	version := created.DraftVersion

	for i := 0; i < 30; i++ {
		res, serr := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
			ID: created.ID, ExpectedVersion: version, DraftPath: "/stale",
			DraftDocument: json.RawMessage(fmt.Sprintf("{\"settings\":{\"layout\":{\"mode\":\"full\"},\"seo\":{\"title\":\"old%d\"}},\"root\":[]}", i)),
		})
		if serr != nil {
			t.Fatalf("保存失败: %v", serr)
		}
		version = res.DraftVersion
	}

	if err := db.Exec(
		"UPDATE page_revisions SET create_time = NOW() - INTERVAL '200 days' WHERE page_id = ?", created.ID,
	).Error; err != nil {
		t.Fatalf("回拨修订时间失败: %v", err)
	}
	var before int64
	if err := db.Raw("SELECT COUNT(*) FROM page_revisions WHERE page_id = ?", created.ID).Scan(&before).Error; err != nil {
		t.Fatalf("统计修订失败: %v", err)
	}
	if _, err = svc.PurgeRetention(ctx); err != nil {
		t.Fatalf("保留期清理失败: %v", err)
	}
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM page_revisions WHERE page_id = ?", created.ID).Scan(&count).Error; err != nil {
		t.Fatalf("统计修订失败: %v", err)
	}

	if count > 20 {
		t.Fatalf("超期且超上限的历史快照应被清理，实际仍有 %d 条", count)
	}
	if before > 20 && count >= before {
		t.Fatalf("清理没有生效：清理前 %d 条、清理后仍 %d 条", before, count)
	}
	_ = time.Now()
}

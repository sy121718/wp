package unit

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	pagedto "go_wp/internal/module/page/dto"
)

const emptyRevDoc = `{"settings":{"layout":{"mode":"full"}},"root":[]}`

// TestSaveDraftPrunesRevisionHistory 保存草稿时把历史快照收敛到上限（IDX-005）。
//
// 一次保存 = 一份完整 draft_document。此前只增不减：高频编辑的页面能把
// page_revisions 撑到任意大小，而其中绝大多数版本永远不会被回退用到。
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
	// 连续保存 30 次（上限 20）：每次都要带上当前版本号。
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
	// 收敛在每次保存后进行，因此最终不会超过上限 + 1（本次新增的那条）。
	if count > 21 {
		t.Fatalf("历史快照应被收敛到上限附近，实际 %d 条", count)
	}
	// 版本号本身不回退：收敛的是行，不是版本序列。
	if version != int64(31) {
		t.Fatalf("版本号应为 31，实际 %d", version)
	}
}

// TestPurgeRetentionKeepsRecentRevisions 保留期内的快照一条都不删（IDX-005 的保守取舍）。
//
// 两个条件「超出条数」与「早于保留期」必须同时满足：刚编辑过的页面即使版本很多，
// 也应该能回退 —— 只按条数删掉刚存的版本是编辑者最不能接受的。
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

// TestPurgeRetentionDeletesExpiredRevisions 早于保留期的历史快照被清掉（IDX-005）。
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
	// 保存次数要超过每页保留上限，才能构造「超出条数」这一半条件 ——
	// 本用例验证两个条件**同时成立**时才删（少一半就不该删）。
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
	// 把全部快照推到保留期之外，模拟「久未编辑、库存了一堆老版本」的页面。
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
	// 超出每页保留上限的老版本被清掉，且清理后不超过上限。
	if count > 20 {
		t.Fatalf("超期且超上限的历史快照应被清理，实际仍有 %d 条", count)
	}
	if before > 20 && count >= before {
		t.Fatalf("清理没有生效：清理前 %d 条、清理后仍 %d 条", before, count)
	}
	_ = time.Now()
}

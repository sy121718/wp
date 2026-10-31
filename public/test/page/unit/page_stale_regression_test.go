package unit

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
)

type peerSetter interface {
	SetI18nStalePeer(pageservice.I18nStalePeer)
}

type fakeI18nPeer struct {
	fail   bool
	called int
}

func (f *fakeI18nPeer) MarkStaleForI18n(context.Context) error { return nil }

func (f *fakeI18nPeer) MarkStaleForI18nTx(_ context.Context, _ *gorm.DB, _ string) error {
	f.called++
	if f.fail {
		return errors.New("fake peer：注入的失败")
	}
	return nil
}

func staleCountOfProject(t *testing.T, db *gorm.DB, projectID string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM pages WHERE project_id = ? AND stale = true", projectID).Scan(&n).Error; err != nil {
		t.Fatalf("统计 stale 页面失败: %v", err)
	}
	return n
}

func TestMarkStaleByIDsReturnsActuallyMarked(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	page := createPage(t, svc, projectID, "/stale-by-ids", emptyRevDoc)

	if err := db.Exec("UPDATE pages SET stale = false WHERE project_id = ?", projectID).Error; err != nil {
		t.Fatalf("清理 stale 失败: %v", err)
	}

	m := pagemodel.NewPageModel(db)
	const ghost = "00000000-0000-0000-0000-0000000000ff"
	marked, err := m.MarkStaleByIDs(ctx, projectID, []string{page.ID, ghost}, time.Now().UTC())
	if err != nil {
		t.Fatalf("标记失败: %v", err)
	}
	if len(marked) != 1 || marked[0] != page.ID {
		t.Fatalf("返回值必须只含真实存在的行（入参里有 1 个幽灵 uuid）：got %v", marked)
	}
	if got := staleCountOfProject(t, db, projectID); got != 1 {
		t.Fatalf("库里应恰好 1 行被标记为 stale，实际 %d", got)
	}
}

func TestMarkStaleForI18nMarkedSetMatchesDB(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	first := createPage(t, svc, projectID, "/stale-i18n-1", emptyRevDoc)
	second := createPage(t, svc, projectID, "/stale-i18n-2", emptyRevDoc)

	if err := db.Exec("UPDATE pages SET stale = false WHERE project_id = ?", projectID).Error; err != nil {
		t.Fatalf("清理 stale 失败: %v", err)
	}

	m := pagemodel.NewPageModel(db)
	marked, err := m.MarkStaleForI18n(ctx, projectID)
	if err != nil {
		t.Fatalf("整站标记失败: %v", err)
	}
	got := map[string]bool{}
	for _, id := range marked {
		got[id] = true
	}
	if !got[first.ID] || !got[second.ID] {
		t.Fatalf("两个页面都应出现在返回集合里：%v", marked)
	}

	var staleIDs []string
	if err := db.Raw("SELECT id FROM pages WHERE project_id = ? AND stale = true", projectID).Scan(&staleIDs).Error; err != nil {
		t.Fatalf("查 stale 行失败: %v", err)
	}
	if len(staleIDs) != len(marked) {
		t.Fatalf("返回集合与库里被标记的集合大小不一致：marked=%v db=%v", marked, staleIDs)
	}
	for _, id := range staleIDs {
		if !got[id] {
			t.Fatalf("库里的 stale 行 %s 不在返回集合里：marked=%v", id, marked)
		}
	}
}

func TestMarkStaleForI18nRollsBackWithPeer(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	createPage(t, svc, projectID, "/stale-peer", emptyRevDoc)

	if err := db.Exec("UPDATE pages SET stale = false WHERE project_id = ?", projectID).Error; err != nil {
		t.Fatalf("清理 stale 失败: %v", err)
	}
	setter, ok := svc.(peerSetter)
	if !ok {
		t.Fatal("页面服务未暴露 SetI18nStalePeer 注入点")
	}

	peer := &fakeI18nPeer{fail: true}
	setter.SetI18nStalePeer(peer)
	if err := svc.MarkStaleForI18n(ctx); err == nil {
		t.Fatal("peer 失败时整站标记必须返回错误（静默半截正是要消除的形态）")
	}
	if peer.called == 0 {
		t.Fatal("peer 通道应被调用（否则这条用例没测到回滚）")
	}
	if got := staleCountOfProject(t, db, projectID); got != 0 {
		t.Fatalf("peer 失败应让页面侧的标记一并回滚，实际仍有 %d 行 stale", got)
	}

	peer.fail = false
	peer.called = 0
	if err := svc.MarkStaleForI18n(ctx); err != nil {
		t.Fatalf("peer 正常时整站标记不该报错: %v", err)
	}
	if peer.called == 0 {
		t.Fatal("peer 通道应被调用")
	}
	if got := staleCountOfProject(t, db, projectID); got == 0 {
		t.Fatal("peer 成功时页面应被标记为 stale")
	}
}

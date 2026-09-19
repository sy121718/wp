package unit

// page_stale_regression_test.go — 整站 / 批量 stale 标记的三条关键行为的长期回归。
//
// 为什么单独一个文件：这三条行为是 2026-09-19 第五批改的，改的时候只用**临时测试**验证过
//（跑完即删），没有任何长期守护 —— 而它们各自都出过或差点出过真问题：
//
//	1. MarkStaleByIDs 旧实现**原样返回入参**（不是真正被标记的行）—— 传一个不存在的 uuid
//	   也会被算进返回值，于是「日志说标了 8 个、实际只有 3 个存在」永远查不出来。
//	   改成 RETURNING id 之后，这条回归把「返回值必须来自数据库」钉住。
//	2. 三个整站标记方法改成 RETURNING id 供影响面回执使用 —— 返回值必须与「库里真的变成了
//	   stale 的那些行」逐字一致，否则影响面日志与实际标记面会各说各话。
//	3. MarkStaleForI18n 的 peer 扇出（其它发布来源，如自动发布实例）与 page 侧标记
//	   现在**同事务**：peer 失败必须让**页面侧的标记一并回滚**，不允许留下
//	   「页面已标、实例未标且不会自动补」的半截状态（现象是「改了译文，商品详情页仍是旧字节」
//	   且日志里什么都没有）。

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
)

// peerSetter 页面服务的 i18n peer 注入点（不进服务契约，走类型断言 —— 与既有用例同手法）。
type peerSetter interface {
	SetI18nStalePeer(pageservice.I18nStalePeer)
}

// fakeI18nPeer 可注入失败的其它发布来源。
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

// staleCountOfProject 本工程当前 stale=true 的页面数。
func staleCountOfProject(t *testing.T, db *gorm.DB, projectID string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM pages WHERE project_id = ? AND stale = true", projectID).Scan(&n).Error; err != nil {
		t.Fatalf("统计 stale 页面失败: %v", err)
	}
	return n
}

// TestMarkStaleByIDsReturnsActuallyMarked 返回值必须来自数据库，而不是原样回带入参。
//
// 反向验证（写这条用例时做过）：把 model 的实现临时换回「返回入参」，本用例必须变红 ——
// 这正是旧实现的破绽所在。
func TestMarkStaleByIDsReturnsActuallyMarked(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	page := createPage(t, svc, projectID, "/stale-by-ids", emptyRevDoc)

	// 先清干净，让「谁被标记」这件事在断言里没有歧义。
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

// TestMarkStaleForI18nMarkedSetMatchesDB 整站标记的返回值必须与库里真正变成 stale 的行一致。
//
// 影响面回执（日志里的「本次影响 N 个页面（前 K 个：标题/路径）」）用的就是这个返回值：
// 它若包含没被标记的行，日志会把无关页面算进影响面；漏掉被标记的行，运维会以为某页不受影响。
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

	// 与库里的真实集合逐字比对（返回集合 == 被标记集合）。
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

// TestMarkStaleForI18nRollsBackWithPeer peer 失败必须让页面侧标记一并回滚。
//
// 这条是「消除静默半截状态」的回归：peer 标记（自动发布实例那一侧）没有任何自动补的入口
// —— 页面标了、实例没标，就会长期停在旧字节，而且日志里什么都没有。
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

	// ① peer 失败：整体报错，且**页面侧的标记一并回滚**（before == after）。
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

	// ② peer 成功：两侧都标记。
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

package unit

// page_deactivation_receipt_test.go — 页面删除的「下线回执」链路（本 owner 的改动）。
//
// 覆盖需要真实 PG + 真实文件系统的那几段：
//   · 删除先登记 pending 回执，再在同一事务里置 businessCommitted（预登记存在 ≠ 业务已提交）；
//   · 撤链在提交之后、路径锁内复验归属；被新 owner 接管时绝不删别人的链接；
//   · 恢复：businessCommitted=false 只保 pending、不动链接（不误杀）；
//   · 账本上已被并发结案时 no-op，不改终态。

import (
	"context"
	"strings"
	"testing"
	"time"

	pagedto "go_wp/internal/module/page/dto"
	pubcontract "go_wp/internal/module/publication/contract"
	pubmodel "go_wp/internal/module/publication/model"

	"gorm.io/gorm"
)

// deactivationReceiptOf 取该页最新一条下线回执（id / state / businessCommitted / pathArtifacts）。
func deactivationReceiptOf(t *testing.T, db *gorm.DB, pageID string) *pubmodel.ReceiptEntity {
	t.Helper()
	var row pubmodel.ReceiptEntity
	err := db.Raw(`SELECT id, source_type, source_id, action, path, from_artifact_id, to_artifact_id,
		receipt_state, receipt_data, create_time, completed_at
		FROM publication_receipts
		WHERE source_type = 'page' AND source_id = ? AND action = 'deactivate'
		ORDER BY id DESC LIMIT 1`, pageID).Scan(&row).Error
	if err != nil {
		t.Fatalf("读取下线回执失败: %v", err)
	}
	if row.ID == 0 {
		t.Fatalf("页面 %s 没有下线回执（删除链应先登记意图）", pageID)
	}
	return &row
}

// TestDeleteRegistersDeactivationReceiptAndWithdrawsLinks 删除：先登记意图、同事务置提交、
// 提交后撤链并结案；账本上留下可追溯的成功身份。
func TestDeleteRegistersDeactivationReceiptAndWithdrawsLinks(t *testing.T) {
	db, svc, _, projectID := newPageServiceWithRoutes(t)
	ctx := context.Background()
	pageID := createPage(t, svc, projectID, "/del/one", pageDocument).ID
	buildAndPublish(t, svc, pageID)
	if kind := activeKind(t, "/del/one"); kind == "" || kind == "none" {
		t.Fatalf("前置条件不成立：发布后访问面应有激活链接，实际 %q", kind)
	}

	if err := svc.Delete(ctx, &pagedto.DeleteReq{ID: pageID}); err != nil {
		t.Fatalf("删除失败: %v", err)
	}

	rec := deactivationReceiptOf(t, db, pageID)
	if rec.ReceiptState != pubmodel.ReceiptCommitted {
		t.Fatalf("删除完成后下线回执应已结案（committed），实际 %q", rec.ReceiptState)
	}
	if !deactivationBusinessCommittedFromData(rec.ReceiptData) {
		t.Fatal("业务事务必须与 businessCommitted 同事务落库，否则恢复永远不敢撤链")
	}
	if ids := deactivationPathArtifactsFromData(rec.ReceiptData); len(ids) == 0 {
		t.Fatal("回执必须记下每条路径登记时刻的归属事实（重放时路由行已删，查不回来）")
	}
	// 访问面已撤：链接不再指向页面产物。
	if kind := activeKind(t, "/del/one"); kind != "" && kind != "none" {
		t.Fatalf("删除后访问面链接应被撤掉，实际仍为 %q", kind)
	}
	// 页面已软删。
	var deletedAt *time.Time
	if err := db.Raw(`SELECT deleted_at FROM pages WHERE id = ?`, pageID).Scan(&deletedAt).Error; err != nil {
		t.Fatalf("读取页面失败: %v", err)
	}
	if deletedAt == nil {
		t.Fatal("页面应已被软删")
	}
}

// TestDeleteKeepsForeignPathOnTakeoverRebound 路径在删除前被别的页面接管：不删别人的链接。
//
// 夹具直接改写路由归属来模拟"另一方已 Activate 成功"。这一步刻意做得"不完整"
// （链接仍指向原产物、账本没跟着换人）—— 它恰好覆盖了判据里最容易写错的一侧：
// 只看 Locator 会认为链接还是我的、于是照删；路由归属复验必须先把它挡下来。
func TestDeleteKeepsForeignPathOnTakeoverRebound(t *testing.T) {
	db, svc, _, projectID := newPageServiceWithRoutes(t)
	ctx := context.Background()
	pageID := createPage(t, svc, projectID, "/del/taken", pageDocument).ID
	buildAndPublish(t, svc, pageID)

	intruder := createPage(t, svc, projectID, "/del/intruder", pageDocument).ID
	if err := db.Exec(`UPDATE page_routes SET page_id = ? WHERE project_id = ? AND path = ?`,
		intruder, projectID, "/del/taken").Error; err != nil {
		t.Fatalf("改写路由归属失败: %v", err)
	}
	// 走真实撤链入口：归属复验应拒绝，链接保留。
	err := svc.Delete(ctx, &pagedto.DeleteReq{ID: pageID})
	if err == nil {
		// 归属被改写时业务事务里的 deactivationPathsUnchanged 不会失败（集合没变），
		// 撤链在提交后复验归属 → 拒绝 → 回执 pending、错误不返回给用户。
		// 因此这里断言的是"链接仍在 + 回执 pending"，而不是错误。
		t.Log("删除返回成功：撤链被归属复验拒绝，回执应保持 pending")
	}
	if kind := activeKind(t, "/del/taken"); kind != "page" {
		t.Fatalf("路径已被别人接管，删除链不得撤掉它，实际访问面状态 %q", kind)
	}
	rec2 := deactivationReceiptOf(t, db, pageID)
	if rec2.ReceiptState != pubmodel.ReceiptPending {
		t.Fatalf("撤链失败时回执必须保持 pending 供收敛重放，实际 %q", rec2.ReceiptState)
	}
	// 路由归属也必须还原到接管者：撤链不得顺带清掉别人的占用。
	var owner *string
	if qerr := db.Raw(`SELECT page_id FROM page_routes WHERE project_id = ? AND path = ?`,
		projectID, "/del/taken").Scan(&owner).Error; qerr != nil {
		t.Fatalf("读取路由归属失败: %v", qerr)
	}
	if owner == nil || *owner != intruder {
		t.Fatalf("路径归属应仍属于接管者 %s，实际 %v", intruder, owner)
	}
}

// TestWithdrawFailsClosedWithoutRegisteredIdentity 回执里没有路径身份时不得照删。
//
// 这条守的是「保守方向」：重放时路由行已被删除，唯一还能认出"这条链接是不是我登记时
// 那条"的证据就是 Locator / artifact / receipt 三项。三项全空时无从复核 —— 必须
// fail closed（保 pending、不动访问面），不能用"只剩路由判据"当理由照删。
func TestWithdrawFailsClosedWithoutRegisteredIdentity(t *testing.T) {
	db, svc, routes, projectID := newPageServiceWithRoutes(t)
	ctx := context.Background()
	pageID := createPage(t, svc, projectID, "/deact/no-identity", pageDocument).ID
	buildAndPublish(t, svc, pageID)

	id, err := routes.BeginPublishReceipt(ctx, &pubcontract.BeginPublishReceiptReq{
		ProjectID: projectID, Path: "/deact/no-identity", PageID: pageID,
		Action:   pubcontract.ReceiptActionDeactivate,
		OldPaths: []string{"/deact/no-identity"},
		// PathArtifacts 故意留空：模拟历史回执 / 登记端没捕获到身份。
	})
	if err != nil {
		t.Fatalf("登记下线回执失败: %v", err)
	}
	if _, _, cerr := recoverPending(t, svc, ctx); cerr != nil {
		t.Logf("收敛返回错误（预期：缺身份时拒绝撤链）: %v", cerr)
	}
	// 业务未提交 + 缺身份：两条都指向"不动链接"。
	if kind := activeKind(t, "/deact/no-identity"); kind != "page" {
		t.Fatalf("缺登记身份时不得撤链，实际访问面状态 %q", kind)
	}
	rec := deactivationReceiptOf(t, db, pageID)
	if rec.ReceiptState != pubmodel.ReceiptPending {
		t.Fatalf("缺身份时回执应保持 pending，实际 %q", rec.ReceiptState)
	}
	_ = id
}

// TestRecoverDeactivateKeepsPendingWhenBusinessNotCommitted 恢复：业务未提交时
// 保 pending、不动链接（预登记存在 ≠ 业务已提交）。
func TestRecoverDeactivateKeepsPendingWhenBusinessNotCommitted(t *testing.T) {
	db, svc, routes, projectID := newPageServiceWithRoutes(t)
	ctx := context.Background()
	pageID := createPage(t, svc, projectID, "/deact/pending", pageDocument).ID
	buildAndPublish(t, svc, pageID)

	id, err := routes.BeginPublishReceipt(ctx, &pubcontract.BeginPublishReceiptReq{
		ProjectID: projectID, Path: "/deact/pending", PageID: pageID,
		Action:   pubcontract.ReceiptActionDeactivate,
		OldPaths: []string{"/deact/pending"},
	})
	if err != nil {
		t.Fatalf("登记下线回执失败: %v", err)
	}
	if strings.TrimSpace(id) == "" {
		t.Fatal("登记下线回执未返回 id")
	}
	if _, _, cerr := recoverPending(t, svc, ctx); cerr != nil {
		t.Fatalf("收敛失败: %v", cerr)
	}
	// 业务未提交 → 不结案、不撤链。
	rec := deactivationReceiptOf(t, db, pageID)
	if rec.ReceiptState != pubmodel.ReceiptPending {
		t.Fatalf("业务未提交时回执必须保持 pending（不能把未开始的有效意图判死），实际 %q", rec.ReceiptState)
	}
	if kind := activeKind(t, "/deact/pending"); kind != "page" {
		t.Fatalf("业务未提交时不得动访问面，实际 %q", kind)
	}
}

package unit

import (
	"context"
	publish "go_wp/pkg/publish"
	"strconv"
	"strings"
	"testing"
	"time"

	pagedto "go_wp/internal/module/page/dto"
	pageservice "go_wp/internal/module/page/service"
	pubcontract "go_wp/internal/module/publication/contract"
	pubmodel "go_wp/internal/module/publication/model"

	"gorm.io/gorm"
)

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
	if !pageservice.DeactivationBusinessCommittedFromData(rec.ReceiptData) {
		t.Fatal("业务事务必须与 businessCommitted 同事务落库，否则恢复永远不敢撤链")
	}
	if ids := pageservice.DeactivationPathArtifactsFromData(rec.ReceiptData); len(ids) == 0 {
		t.Fatal("回执必须记下每条路径登记时刻的归属事实（重放时路由行已删，查不回来）")
	}

	if kind := activeKind(t, "/del/one"); kind != "" && kind != "none" {
		t.Fatalf("删除后访问面链接应被撤掉，实际仍为 %q", kind)
	}

	var deletedAt *time.Time
	if err := db.Raw(`SELECT deleted_at FROM pages WHERE id = ?`, pageID).Scan(&deletedAt).Error; err != nil {
		t.Fatalf("读取页面失败: %v", err)
	}
	if deletedAt == nil {
		t.Fatal("页面应已被软删")
	}
}

func TestDeleteNeverTouchesPathOwnedByAnotherPage(t *testing.T) {
	db, svc, _, projectID := newPageServiceWithRoutes(t)
	ctx := context.Background()
	pageID := createPage(t, svc, projectID, "/del/taken", pageDocument).ID
	buildAndPublish(t, svc, pageID)
	if kind := activeKind(t, "/del/taken"); kind != "page" {
		t.Fatalf("前置失败：发布后访问面应有页面链接，实际 %q", kind)
	}

	intruder := createPage(t, svc, projectID, "/del/intruder", pageDocument).ID

	res := db.Exec(`UPDATE page_routes SET page_id = ? WHERE project_id = ? AND path = ?`,
		intruder, projectID, "/del/taken")
	if res.Error != nil {
		t.Fatalf("改写路由归属失败: %v", res.Error)
	}
	if res.RowsAffected != 1 {
		t.Fatalf("前置失败：/del/taken 的 active 行应恰好一条，实际影响 %d 行", res.RowsAffected)
	}

	if err := svc.Delete(ctx, &pagedto.DeleteReq{ID: pageID}); err != nil {
		t.Fatalf("本页已无在服务的路径，删除应成功: %v", err)
	}

	if kind := activeKind(t, "/del/taken"); kind != "page" {
		t.Fatalf("路径已被别人接管，删除链不得撤掉它，实际访问面状态 %q", kind)
	}

	var owner *string
	if qerr := db.Raw(`SELECT page_id FROM page_routes WHERE project_id = ? AND path = ?`,
		projectID, "/del/taken").Scan(&owner).Error; qerr != nil {
		t.Fatalf("读取路由归属失败: %v", qerr)
	}
	if owner == nil || *owner != intruder {
		t.Fatalf("路径归属应仍属于接管者 %s，实际 %v", intruder, owner)
	}

	var deletedAt *time.Time
	if qerr := db.Raw(`SELECT deleted_at FROM pages WHERE id = ?`, pageID).Scan(&deletedAt).Error; qerr != nil {
		t.Fatalf("读取页面失败: %v", qerr)
	}
	if deletedAt == nil {
		t.Fatal("页面应已被软删")
	}

	var receiptCount int64
	if qerr := db.Raw(`SELECT count(*) FROM publication_receipts WHERE source_type = 'page' AND source_id = ? AND action = 'deactivate'`,
		pageID).Scan(&receiptCount).Error; qerr != nil {
		t.Fatalf("统计下线回执失败: %v", qerr)
	}
	if receiptCount != 0 {
		t.Fatalf("本页已无在服务的路径，不该登记下线回执，实际 %d 条", receiptCount)
	}
}

func TestWithdrawFailsClosedWithoutRegisteredIdentity(t *testing.T) {
	db, svc, routes, projectID := newPageServiceWithRoutes(t)
	ctx := context.Background()

	pageID := createPage(t, svc, projectID, "/deact/no-identity", pageDocument).ID
	buildAndPublish(t, svc, pageID)
	if kind := activeKind(t, "/deact/no-identity"); kind != "page" {
		t.Fatalf("前置失败：发布后访问面应有页面链接，实际 %q", kind)
	}

	id, err := routes.BeginPublishReceipt(ctx, &pubcontract.BeginPublishReceiptReq{
		ProjectID: projectID, Path: "/deact/no-identity", PageID: pageID,
		Action:   pubcontract.ReceiptActionDeactivate,
		OldPaths: []string{"/deact/no-identity"},
	})
	if err != nil {
		t.Fatalf("登记下线回执失败: %v", err)
	}
	if _, _, cerr := recoverPending(t, svc, ctx); cerr != nil {
		t.Logf("收敛返回错误（预期：缺身份时拒绝撤链）: %v", cerr)
	}

	if rec := deactivationReceiptOf(t, db, pageID); strconv.FormatInt(rec.ID, 10) != strings.TrimSpace(id) {
		t.Fatalf("回执身份不符：登记返回 %q，库内最新一条 %d", id, rec.ID)
	}
	if kind := activeKind(t, "/deact/no-identity"); kind != "page" {
		t.Fatalf("缺登记身份时不得撤链，实际访问面状态 %q", kind)
	}
	rec := deactivationReceiptOf(t, db, pageID)
	if rec.ReceiptState != pubmodel.ReceiptPending {
		t.Fatalf("缺身份时回执应保持 pending，实际 %q", rec.ReceiptState)
	}
	if pageservice.DeactivationBusinessCommittedFromData(rec.ReceiptData) {
		t.Fatal("本用例没有走业务事务，businessCommitted 不得为 true")
	}
	if len(pageservice.DeactivationPathArtifactsFromData(rec.ReceiptData)) != 0 {
		t.Fatal("前置失败：本用例要的是「登记端没捕获到身份」，PathArtifacts 应为空")
	}

	var routeRows int
	if qerr := db.Raw(`SELECT count(*) FROM page_routes WHERE project_id = ? AND page_id = ?`,
		projectID, pageID).Scan(&routeRows).Error; qerr != nil {
		t.Fatalf("读取路由行失败: %v", qerr)
	}
	if routeRows == 0 {
		t.Fatal("缺身份时不得清理路由行")
	}
}

func TestDeleteWithRedirectFailsClosedWithoutPathLock(t *testing.T) {
	db, svc, _, projectID := newPageServiceWithRoutes(t)
	ctx := context.Background()
	concrete, ok := svc.(*pageservice.Service)
	if !ok {
		t.Fatalf("页面服务具体类型不是 *pageservice.Service，实际 %T", svc)
	}
	pageID := createPage(t, svc, projectID, "/del/lock", pageDocument).ID
	buildAndPublish(t, svc, pageID)
	if kind := activeKind(t, "/del/lock"); kind != "page" {
		t.Fatalf("前置失败：发布后访问面应有页面链接，实际 %q", kind)
	}

	concrete.SetPublicationPathLocker(nil)
	defer concrete.SetPublicationPathLocker(publish.DefaultPathLocker())

	if derr := svc.Delete(ctx, &pagedto.DeleteReq{ID: pageID}); derr != nil {

		t.Logf("删除返回错误（允许：撤链失败也会保 pending）：%v", derr)
	}

	if kind := activeKind(t, "/del/lock"); kind != "page" {
		t.Fatalf("路径锁关闭时访问面链接不得被撤掉，实际 %q", kind)
	}

	rec := deactivationReceiptOf(t, db, pageID)
	if rec.ReceiptState != pubmodel.ReceiptPending {
		t.Fatalf("撤链被 fail closed 时回执必须保持 pending，实际 %q", rec.ReceiptState)
	}

	if !pageservice.DeactivationBusinessCommittedFromData(rec.ReceiptData) {
		t.Fatal("业务事务应已提交（软删与清路由已落库），businessCommitted 不得为 false")
	}
	var deletedAt *time.Time
	if qerr := db.Raw(`SELECT deleted_at FROM pages WHERE id = ?`, pageID).Scan(&deletedAt).Error; qerr != nil {
		t.Fatalf("读取页面失败: %v", qerr)
	}
	if deletedAt == nil {
		t.Fatal("业务事务已提交，页面应已被软删")
	}

	concrete.SetPublicationPathLocker(publish.DefaultPathLocker())
	if _, _, cerr := recoverPending(t, svc, ctx); cerr != nil {
		t.Logf("收敛返回错误（允许：单条失败保 pending）：%v", cerr)
	}
	rec2 := deactivationReceiptOf(t, db, pageID)
	if rec2.ReceiptState != pubmodel.ReceiptCommitted {
		t.Fatalf("锁恢复后重放应收口为 committed，实际 %q", rec2.ReceiptState)
	}
	if kind := activeKind(t, "/del/lock"); kind != "" && kind != "none" {
		t.Fatalf("重放后访问面链接应被撤掉，实际 %q", kind)
	}
}

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

	rec := deactivationReceiptOf(t, db, pageID)
	if rec.ReceiptState != pubmodel.ReceiptPending {
		t.Fatalf("业务未提交时回执必须保持 pending（不能把未开始的有效意图判死），实际 %q", rec.ReceiptState)
	}
	if kind := activeKind(t, "/deact/pending"); kind != "page" {
		t.Fatalf("业务未提交时不得动访问面，实际 %q", kind)
	}
}

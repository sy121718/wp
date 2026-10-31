package unit

import (
	"context"
	"strings"
	"testing"

	"go_wp/internal/pipeline"

	"gorm.io/gorm"

	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
)

func projectReceiptStateOf(t *testing.T, db *gorm.DB, projectID, path string) (state string, committed bool) {
	t.Helper()
	var row struct {
		ReceiptState string
		Committed    bool
	}
	if err := db.Raw(`SELECT receipt_state,
			COALESCE((receipt_data->>'businessCommitted')::boolean, false) AS committed
		FROM publication_receipts
		WHERE source_type = 'project' AND source_id = ? AND path = ? AND action = 'deactivate'
		ORDER BY id DESC LIMIT 1`, projectID, path).Scan(&row).Error; err != nil {
		t.Fatalf("读取项目级下线回执失败: %v", err)
	}
	return row.ReceiptState, row.Committed
}

func TestDeleteRedirectRegistersProjectReceipt(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	page := createPage(t, svc, projectID, "/landing", headingDocument)
	buildAndPublish(t, svc, page.ID)

	if _, err := svc.CreateRedirect(ctx, &pagedto.RedirectCreateReq{
		ProjectID: projectID, SourcePath: "/promo/autumn", TargetPath: "/landing",
	}); err != nil {
		t.Fatalf("新增重定向失败: %v", err)
	}
	if kind := activeKind(t, "/promo/autumn"); kind != "redirect" {
		t.Fatalf("新增后访问面应为 redirect: %s", kind)
	}

	if err := svc.DeleteRedirect(ctx, &pagedto.RedirectDeleteReq{ProjectID: projectID, Path: "/promo/autumn"}); err != nil {
		t.Fatalf("删除重定向失败: %v", err)
	}
	if kind := routeKind(t, db, projectID, "/promo/autumn"); kind != "" {
		t.Fatalf("删除后路由行应消失: %s", kind)
	}
	if kind := activeKind(t, "/promo/autumn"); kind != "none" {
		t.Fatalf("删除后访问面激活应解除: %s", kind)
	}
	state, committed := projectReceiptStateOf(t, db, projectID, "/promo/autumn")
	if state != "committed" {
		t.Fatalf("下线回执应收口为 committed: %s", state)
	}
	if !committed {
		t.Fatalf("businessCommitted 必须与业务事务同进同出")
	}
}

func TestDeleteRedirectRejectsTakenOverLink(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	page := createPage(t, svc, projectID, "/landing", headingDocument)
	buildAndPublish(t, svc, page.ID)

	if _, err := svc.CreateRedirect(ctx, &pagedto.RedirectCreateReq{
		ProjectID: projectID, SourcePath: "/promo/winter", TargetPath: "/landing",
	}); err != nil {
		t.Fatalf("新增重定向失败: %v", err)
	}

	if err := db.Exec("UPDATE page_routes SET route_kind = 'active' WHERE project_id = ? AND path = ?",
		projectID, "/promo/winter").Error; err != nil {
		t.Fatalf("改写路由行失败: %v", err)
	}

	err := svc.DeleteRedirect(ctx, &pagedto.RedirectDeleteReq{ProjectID: projectID, Path: "/promo/winter"})
	if err == nil || !strings.Contains(err.Error(), pageenums.ErrRedirectConflict) {
		t.Fatalf("归属已变应报 %q: %v", pageenums.ErrRedirectConflict, err)
	}
	if kind := routeKind(t, db, projectID, "/promo/winter"); kind != "active" {
		t.Fatalf("复验失败不得删行，路由行应保持 active: %s", kind)
	}
	if kind := activeKind(t, "/promo/winter"); kind != "redirect" {
		t.Fatalf("复验失败不得撤链，访问面应保持 redirect: %s", kind)
	}

	var receiptCount int64
	if qerr := db.Raw(`SELECT count(*) FROM publication_receipts
		WHERE source_type = 'project' AND source_id = ? AND path = ? AND action = 'deactivate'`,
		projectID, "/promo/winter").Scan(&receiptCount).Error; qerr != nil {
		t.Fatalf("统计项目级下线回执失败: %v", qerr)
	}
	if receiptCount != 0 {
		t.Fatalf("前置判定失败（登记之前就返回）不应留下回执，实际 %d 条", receiptCount)
	}
}

func TestRecoverProjectDeactivateKeepsPendingWithoutBusinessCommit(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	page := createPage(t, svc, projectID, "/landing", headingDocument)
	buildAndPublish(t, svc, page.ID)

	if _, err := svc.CreateRedirect(ctx, &pagedto.RedirectCreateReq{
		ProjectID: projectID, SourcePath: "/promo/spring", TargetPath: "/landing",
	}); err != nil {
		t.Fatalf("新增重定向失败: %v", err)
	}

	receiptData := `{"projectId":"` + projectID + `","businessCommitted":false,"oldPaths":["/promo/spring"],` +
		`"pathArtifacts":{"/promo/spring":{"locator":"file:/promo/spring"}}}`
	if err := db.Exec(`INSERT INTO publication_receipts
			(source_type, source_id, action, path, receipt_state, receipt_data, create_time)
		VALUES ('project', ?, 'deactivate', '/promo/spring', 'pending', ?::jsonb, now())`,
		projectID, receiptData).Error; err != nil {
		t.Fatalf("插入项目级回执失败: %v", err)
	}

	if _, _, err := recoverPending(t, svc, ctx); err != nil {
		t.Fatalf("收敛失败: %v", err)
	}
	if kind := activeKind(t, "/promo/spring"); kind != "redirect" {
		t.Fatalf("业务未提交时不得撤链: %s", kind)
	}
	state, _ := projectReceiptStateOf(t, db, projectID, "/promo/spring")
	if state != "pending" {
		t.Fatalf("业务未提交应保持 pending（不能判死一条有效意图）: %s", state)
	}
}

func redirectLocatorOf(t *testing.T, urlPath string) string {
	t.Helper()
	state, err := (&pipeline.LocalPublicationStore{ActiveRoot: pipeline.ActiveRoot()}).Inspect(urlPath)
	if err != nil {
		t.Fatalf("读取访问面状态失败: %v", err)
	}
	if state == nil || state.Locator == nil {
		return ""
	}
	return state.Locator.Provider + ":" + state.Locator.Key
}

func TestRecoverProjectDeactivateWithdrawsAfterCommit(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	page := createPage(t, svc, projectID, "/landing", headingDocument)
	buildAndPublish(t, svc, page.ID)

	if _, err := svc.CreateRedirect(ctx, &pagedto.RedirectCreateReq{
		ProjectID: projectID, SourcePath: "/promo/summer", TargetPath: "/landing",
	}); err != nil {
		t.Fatalf("新增重定向失败: %v", err)
	}

	locator := redirectLocatorOf(t, "/promo/summer")
	if locator == "" {
		t.Fatal("新增重定向后访问面应能读到定位器")
	}
	receiptData := `{"projectId":"` + projectID + `","businessCommitted":true,"oldPaths":["/promo/summer"],` +
		`"pathArtifacts":{"/promo/summer":{"locator":"` + locator + `"}}}`
	if err := db.Exec(`INSERT INTO publication_receipts
			(source_type, source_id, action, path, receipt_state, receipt_data, create_time)
		VALUES ('project', ?, 'deactivate', '/promo/summer', 'pending', ?::jsonb, now())`,
		projectID, receiptData).Error; err != nil {
		t.Fatalf("插入项目级回执失败: %v", err)
	}
	if err := db.Exec("DELETE FROM page_routes WHERE project_id = ? AND path = ?", projectID, "/promo/summer").Error; err != nil {
		t.Fatalf("删除路由行失败: %v", err)
	}
	if _, rolledBack, err := recoverPending(t, svc, ctx); err != nil {
		t.Fatalf("收敛失败: %v", err)
	} else if rolledBack != 0 {
		t.Fatalf("业务已提交的意图不该被判回滚: rolledBack=%d", rolledBack)
	}
	if kind := activeKind(t, "/promo/summer"); kind != "none" {
		t.Fatalf("业务已提交后应收敛掉访问面链接: %s", kind)
	}
	state, _ := projectReceiptStateOf(t, db, projectID, "/promo/summer")
	if state != "committed" {
		t.Fatalf("撤链成功应结案为 committed: %s", state)
	}
}

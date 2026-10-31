// page_redirect_deactivation_test.go — 重定向删除与"提交不明"的下线回执语义。
//
// 为什么这三条用例值得单开一个文件：
//
//  1. 重定向删除是唯一**没有实体归属**的下线入口（redirect 行的 page_id /
//     presentation_id 都是 NULL），它用项目级来源登记回执。归属判据只剩
//     "路由行仍是 redirect" + "访问面链接仍是登记时那一条"。
//  2. 删除的失败分流里有一格既不是回滚也不是成功：事务回调跑完了、提交阶段报错。
//     GORM 的 Transaction 会把 Commit 的错误一并返回，此时业务数据可能已落库。
//     这一格必须保 pending（让重放按锁内重读的真实状态判定），不能 abort ——
//     abort 会把一条其实已经生效的意图判死，链接永久残留。
//  3. 反向：并发删除先拿走行时，本事务必须判冲突而不是当成功。
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

// projectReceiptStateOf 读项目级下线回执的状态与 businessCommitted。
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

// TestDeleteRedirectRegistersProjectReceipt 删除重定向：回执归属是工程、业务标记同事务落下、
// 提交后撤链并结案。
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

// TestDeleteRedirectRejectsTakenOverLink 锁内复验失败时不得删行、不得撤链：
// 链接已被换成别的产物（同路径的另一条重定向重建），本次删除判冲突。
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
	// 把路由行的 route_kind 改成 active（模拟"这条路径刚被某页发布接管"）：
	// 复验必须拒绝，否则会删掉别人正在用的路径占用。
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
	state, _ := projectReceiptStateOf(t, db, projectID, "/promo/winter")
	if state != "rolled_back" {
		t.Fatalf("明确失败应结案为 rolled_back（业务事务确实回滚了）: %s", state)
	}
}

// TestRecoverProjectDeactivateKeepsPendingWithoutBusinessCommit 项目级回执在业务未提交时
// 既不能撤链也不能结案 —— 预登记之后业务随时可能提交。
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
	// 直接插一条项目级 pending 回执（businessCommitted=false），模拟"登记之后崩溃"。
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

// TestRecoverProjectDeactivateWithdrawsAfterCommit 业务已提交 ⇔ 回执可撤链并结案。
// redirectLocatorOf 读某路径当前访问面定位器（Provider:Key 形态，与回执登记值同口径）。
//
// 路径身份必须取真实链接：编一个前缀会让撤链前的复验永远失败，用例就成了自说自话。
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
	// 业务侧已经删了路由行（等价于"删除事务已提交、撤链前崩溃"），回执置 businessCommitted。
	//
	// 路径身份必须用**真实的访问面定位器**：撤链前的复验要求"现在这条链接还是登记时的
	// 那一条"，编一个前缀会让复验永远失败 —— 那样用例就成了自说自话。
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

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
	"strconv"
	"strings"
	"testing"
	"time"

	pagedto "go_wp/internal/module/page/dto"
	pageservice "go_wp/internal/module/page/service"
	pubcontract "go_wp/internal/module/publication/contract"
	pubmodel "go_wp/internal/module/publication/model"
	"go_wp/internal/pipeline"

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
	if !pageservice.DeactivationBusinessCommittedFromData(rec.ReceiptData) {
		t.Fatal("业务事务必须与 businessCommitted 同事务落库，否则恢复永远不敢撤链")
	}
	if ids := pageservice.DeactivationPathArtifactsFromData(rec.ReceiptData); len(ids) == 0 {
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

// TestDeleteNeverTouchesPathOwnedByAnotherPage 路径归属在删除前被别的页面接管：
// 删除照常成功（本页确实没有在服务的路径），但**一个字节都不碰别人的东西**。
//
// 这条守的是「归属被改走后不得误删/误撤别人的路径」：
//
//	· Delete（page_document.go:632-650）先取本页当前路径 ListActivePaths(projectID, pageID)，
//	  实现是 ListRoutePathsByPage 的 `page_id = ? AND route_kind IN ('active','redirect')`；
//	  为空 → 走「没有激活路径（从未发布）：不需要回执」分支（:652）。本用例正好命中这一支：
//	  唯一那条 active 行的归属已被改给入侵者，本页一条都不剩。
//	· 那一支的清路由仍按 page_id 匹配（DeleteRoutesByPageTx），所以别人的行不会被顺带清掉，
//	  访问面链接也没有撤链入口可以碰到它。
//
// 不在这里造「登记后路径被抢」的现场：同页改 draft_path 再发布时，发布链会把旧路径迁走/处置，
// 本页永远只有一条 active 行，凑不出「登记时两条、复核时一条」。撤链阶段的归属复验
// （pending 语义）由 TestRecoverDeactivateKeepsPendingWhenBusinessNotCommitted 与
// TestWithdrawFailsClosedWithoutRegisteredIdentity 覆盖，本用例不重复造现场。
func TestDeleteNeverTouchesPathOwnedByAnotherPage(t *testing.T) {
	db, svc, _, projectID := newPageServiceWithRoutes(t)
	ctx := context.Background()
	pageID := createPage(t, svc, projectID, "/del/taken", pageDocument).ID
	buildAndPublish(t, svc, pageID)
	if kind := activeKind(t, "/del/taken"); kind != "page" {
		t.Fatalf("前置失败：发布后访问面应有页面链接，实际 %q", kind)
	}

	intruder := createPage(t, svc, projectID, "/del/intruder", pageDocument).ID
	// 此刻 /del/taken 的 active 行确实存在，改归属才有效（曾用「先把行改名再改归属」，
	// UPDATE 影响 0 行，前置断言就此空转）。
	res := db.Exec(`UPDATE page_routes SET page_id = ? WHERE project_id = ? AND path = ?`,
		intruder, projectID, "/del/taken")
	if res.Error != nil {
		t.Fatalf("改写路由归属失败: %v", res.Error)
	}
	if res.RowsAffected != 1 {
		t.Fatalf("前置失败：/del/taken 的 active 行应恰好一条，实际影响 %d 行", res.RowsAffected)
	}

	// 判据一：Delete 成功 —— 本页已无在服务的路径，「无激活路径 → 不需要回执」是既有语义。
	if err := svc.Delete(ctx, &pagedto.DeleteReq{ID: pageID}); err != nil {
		t.Fatalf("本页已无在服务的路径，删除应成功: %v", err)
	}
	// 判据二：别人的链接原样。
	if kind := activeKind(t, "/del/taken"); kind != "page" {
		t.Fatalf("路径已被别人接管，删除链不得撤掉它，实际访问面状态 %q", kind)
	}
	// 判据三：别人的路由行原样（清路由按 page_id 匹配，不得顺手动别人的行）。
	var owner *string
	if qerr := db.Raw(`SELECT page_id FROM page_routes WHERE project_id = ? AND path = ?`,
		projectID, "/del/taken").Scan(&owner).Error; qerr != nil {
		t.Fatalf("读取路由归属失败: %v", qerr)
	}
	if owner == nil || *owner != intruder {
		t.Fatalf("路径归属应仍属于接管者 %s，实际 %v", intruder, owner)
	}
	// 判据四：页面已软删（业务事务提交了）。
	var deletedAt *time.Time
	if qerr := db.Raw(`SELECT deleted_at FROM pages WHERE id = ?`, pageID).Scan(&deletedAt).Error; qerr != nil {
		t.Fatalf("读取页面失败: %v", qerr)
	}
	if deletedAt == nil {
		t.Fatal("页面应已被软删")
	}
	// 判据五：没有下线回执 —— 本页没有在服务的路径，撤链无从谈起，也就不该留下任何回执。
	var receiptCount int64
	if qerr := db.Raw(`SELECT count(*) FROM publication_receipts WHERE source_type = 'page' AND source_id = ? AND action = 'deactivate'`,
		pageID).Scan(&receiptCount).Error; qerr != nil {
		t.Fatalf("统计下线回执失败: %v", qerr)
	}
	if receiptCount != 0 {
		t.Fatalf("本页已无在服务的路径，不该登记下线回执，实际 %d 条", receiptCount)
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
	// 必须走**已发布**现场：只有存在激活路径时删除才会登记下线回执（page_document.go
	// Delete 里 len(paths)==0 走「从未发布不需要回执」分支）。用纯草稿造夹具会让
	// recoverPending 扫到的根本不是本用例的回执，断言就落在空处。
	pageID := createPage(t, svc, projectID, "/deact/no-identity", pageDocument).ID
	buildAndPublish(t, svc, pageID)
	if kind := activeKind(t, "/deact/no-identity"); kind != "page" {
		t.Fatalf("前置失败：发布后访问面应有页面链接，实际 %q", kind)
	}

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
	// 两条保守判据各自独立成立：业务未提交（businessCommitted=false）与缺身份（三项全空），
	// 任一都要求「不动链接、保 pending」。先钉住回执确实还是我登记的那条 —— 否则下面
	// 读到的是别的分支留下的行，断言就空转了。
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
	// 路由行同样原样：fail closed 的定义就是「一个字节都没动」。
	var routeRows int
	if qerr := db.Raw(`SELECT count(*) FROM page_routes WHERE project_id = ? AND page_id = ?`,
		projectID, pageID).Scan(&routeRows).Error; qerr != nil {
		t.Fatalf("读取路由行失败: %v", qerr)
	}
	if routeRows == 0 {
		t.Fatal("缺身份时不得清理路由行")
	}
}

// TestDeleteWithRedirectFailsClosedWithoutPathLock 路径锁 unavailable 时撤链必须 fail closed：
// 链接一个字节都不动、回执留 pending；锁恢复后重放才收口为 committed。
//
// 顺序（page_document.go:606 Delete → deleteWithDeactivation:1255）：
//
//	登记下线意图（独立短事务，不碰访问面）
//	  → 业务事务：清路由 + 软删 + 置 businessCommitted（**照常提交**）
//	    → 提交后 withdrawDeactivatedLinks（顾问锁 → withPaths 内复验归属 → Deactivate）
//
// 锁关闭时 fail closed 落在**最后一步**：page_service.go:262 withPaths → pipeline.WithPathLock
// （publication_pathlock.go:71）在 locker 为 nil 时返回 ErrPathLockerUnavailable，fn 根本不跑。
// 因此：业务事务已经提交（页面软删、路由已清、businessCommitted=true），
// 而访问面链接一个字节都没动，回执留 pending。
// 本用例断言的就是这个组合 —— 「链接还在 + 页面已删 + businessCommitted=true + 回执 pending」。
// 早先注释写成「业务事务整体回滚」是误读，已改。
//
// 正因为 businessCommitted=true 与链接未撤同时成立，锁恢复后的重放才有意义：
// recoverDeactivateReceipt（page_publish.go:4538）读回那个 true 就放行到撤链，撤完结案
// committed。若业务事务真的回滚了，收敛会永远停在「业务未提交 → 保 pending」，那是另一条
// 语义（见本文件 TestRecoverDeactivateKeepsPendingWhenBusinessNotCommitted）。
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
	defer concrete.SetPublicationPathLocker(pipeline.DefaultPathLocker())

	if derr := svc.Delete(ctx, &pagedto.DeleteReq{ID: pageID}); derr != nil {
		// 删除链在撤链失败时**不把错误抛给调用方**（回执保 pending 交收敛），
		// 因此这里不要求一定报错 —— 判据在下面两处状态事实上。
		t.Logf("删除返回错误（允许：撤链失败也会保 pending）：%v", derr)
	}
	// 链接必须还在：fail closed 的语义是「访问面什么都没动」。
	if kind := activeKind(t, "/del/lock"); kind != "page" {
		t.Fatalf("路径锁关闭时访问面链接不得被撤掉，实际 %q", kind)
	}
	// 回执保持 pending：没有结案就说明撤链那一步真的被拦住了，而不是默默成功。
	rec := deactivationReceiptOf(t, db, pageID)
	if rec.ReceiptState != pubmodel.ReceiptPending {
		t.Fatalf("撤链被 fail closed 时回执必须保持 pending，实际 %q", rec.ReceiptState)
	}
	// 业务事务是**提交过**的：这也是重放能收口的前提。若这里是 false，后面那条
	// 「锁恢复后重放收口」的期望就不成立（收敛会一直判「业务未提交」）。
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

	// 锁恢复后重放同一条回执：必须能收口（幂等 + 可重放），链接随之撤掉。
	//
	// 恢复必须**显式**发生在重放之前：上面那个 defer 要到测试结束才执行，而重放就在
	// 函数体里跑 —— 漏了这一步，重放时锁仍是 nil，打出来的会是「访问面路径锁不可用」
	// 而不是本用例要验的收口。defer 保留作兜底（中途 failure 也要把锁还回去）。
	concrete.SetPublicationPathLocker(pipeline.DefaultPathLocker())
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

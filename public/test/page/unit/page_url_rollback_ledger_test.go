package unit

// page_url_rollback_ledger_test.go —— 改 URL / 回滚两种访问面切换的故障注入与恢复收敛
//（2026-09-19 全模块事务审计：高 2 条）。
//
// 这两条路径与 Publish 同形（先切访问面、再写数据库），但此前**没有 pending 回执**：
// 任一 DB 步失败都会留下「线上已按新 URL / 历史产物生效，而 pages.draft_path、
// page_publications、page_routes 还是旧值」，且启动恢复只看 switch_active，
// 看不到它们 —— 状态永久分裂。下面两条用例把「中途失败 → 回执停在 pending →
// 恢复收敛且幂等」钉住，并顺带验证 DB 各步真的是一个事务（半截写不落库）。

import (
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"

	pagedto "go_wp/internal/module/page/dto"
	"go_wp/internal/pipeline"
)

// TestUpdateURLCrashWindowConvergesOnRecovery 改 URL 崩在「FS 已切新路径、DB 未落定」窗口后收敛。
func TestUpdateURLCrashWindowConvergesOnRecovery(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	page := createPage(t, svc, projectID, "/url-old", headingDocument)
	buildAndPublish(t, svc, page.ID)
	beforePublication := publicationsOfDB(t, db, page.ID)

	// 故障注入：命中「FS 已切到新 URL、DB 事务尚未提交」窗口（真实崩溃会连进程一起终止）。
	injected := errors.New("故障注入：进程终止于切换访问面之后、DB 落定之前")
	setPublishWindowFault(t, svc, func() error { return injected })

	if _, uerr := svc.UpdateURL(ctx, &pagedto.UpdateURLReq{ID: page.ID, NewPath: "/url-new", WithRedirect: true}); uerr == nil {
		t.Fatal("窗口故障注入后 UpdateURL 应报错，实际返回成功")
	}

	// 窗口现场一：访问面已切（新路径是指向产物的普通链接）。
	if kind := activeKind(t, "/url-new"); kind != "page" {
		t.Fatalf("新路径应已激活为 page（切换已发生），实际 %q", kind)
	}
	// 窗口现场二：DB 段整体回滚 —— 草稿路径仍是旧值（半截写不落库）。
	if got := draftPathOf(t, db, page.ID); got != "/url-old" {
		t.Fatalf("崩溃窗口里 draft_path 不应被修改（事务应回滚），实际 %q", got)
	}
	if got := routeOccupiedAt(t, db, projectID, "/url-new"); got != "" {
		t.Fatalf("崩溃窗口里新路径不应有路由行，实际 %q", got)
	}
	if got := routeOccupiedAt(t, db, projectID, "/url-old"); got == "" {
		t.Fatal("崩溃窗口里旧路径的保留路由不应被迁移掉")
	}
	if got := publicationsOfDB(t, db, page.ID); !reflect.DeepEqual(got, beforePublication) {
		t.Fatalf("事务失败必须保留完整原发布指针与hash: before=%+v after=%+v", beforePublication, got)
	}
	// 窗口现场三：回执停在 pending（交启动恢复判定），且记下了旧路径与处置方式。
	if got := receiptStateForAction(t, db, "/url-new", "update_url"); got != "pending" {
		t.Fatalf("改 URL 回执应保持 pending，实际 %q", got)
	}

	// 进程重启：清注入点，跑启动恢复。
	setPublishWindowFault(t, svc, nil)
	recovered, rolledBack, rerr := recoverPending(t, svc, ctx)
	if rerr != nil {
		t.Fatalf("恢复失败: %v", rerr)
	}
	if recovered != 1 || rolledBack != 0 {
		t.Fatalf("应补完成 1 条、回滚 0 条，实际 %d / %d", recovered, rolledBack)
	}

	// 收敛一：草稿路径与该语言的激活路径都到新路径。
	if got := draftPathOf(t, db, page.ID); got != "/url-new" {
		t.Fatalf("恢复后 draft_path 应为 /url-new，实际 %q", got)
	}
	if got := publicationActivePath(t, db, page.ID); got != "/url-new" {
		t.Fatalf("恢复后激活路径应为 /url-new，实际 %q", got)
	}
	for _, pub := range publicationsOfDB(t, db, page.ID) {
		if pub.ActivePath == "/url-new" && pub.ArtifactHash != activeRouteArtifactHash(t, db, projectID, "/url-new") {
			t.Fatalf("恢复必须同时迁新产物hash: %+v", pub)
		}
	}
	// 收敛二：路由表与新路径、旧路径一致（旧路径按 WithRedirect 登记为 redirect）。
	if got := routeOccupiedAt(t, db, projectID, "/url-new"); got != "active" {
		t.Fatalf("恢复后 /url-new 应为 active 路由，实际 %q", got)
	}
	if got := routeOccupiedAt(t, db, projectID, "/url-old"); got != "redirect" {
		t.Fatalf("恢复后 /url-old 应为 redirect 路由（WithRedirect=true），实际 %q", got)
	}
	// 收敛三：新路径的路由指向一条真实产物行 —— 崩溃点若落在归档之前，恢复必须补归档。
	var joined int64
	if err := db.Raw(`SELECT COUNT(*) FROM page_routes r JOIN page_artifacts a ON a.id = r.artifact_id
		WHERE r.project_id = ? AND r.path = ?`, projectID, "/url-new").Scan(&joined).Error; err != nil {
		t.Fatalf("查询路由产物关联失败: %v", err)
	}
	if joined != 1 {
		t.Fatalf("新路径的路由应指向已归档的产物行，实际关联数 %d", joined)
	}
	// 收敛四：回执结案，下次启动不会重复判定。
	if got := receiptStateForAction(t, db, "/url-new", "update_url"); got != "committed" {
		t.Fatalf("恢复后回执应收敛为 committed（不许停在 pending），实际 %q", got)
	}

	// 幂等：重复恢复不改结果。
	againRecovered, againRolledBack, aerr := recoverPending(t, svc, ctx)
	if aerr != nil {
		t.Fatalf("重复恢复失败: %v", aerr)
	}
	if againRecovered != 0 || againRolledBack != 0 {
		t.Fatalf("重复恢复不应再判定任何回执，实际 %d / %d", againRecovered, againRolledBack)
	}
	if got := draftPathOf(t, db, page.ID); got != "/url-new" {
		t.Fatalf("重复恢复不得改动已收敛状态，draft_path 实际 %q", got)
	}
	if got := routeOccupiedAt(t, db, projectID, "/url-old"); got != "redirect" {
		t.Fatalf("重复恢复不得改动旧路径路由，实际 %q", got)
	}
}

// TestRollbackCrashWindowConvergesOnRecovery 回滚崩在切换**之前**（旧版窗口）后必须无副作用。
//
// 故障注入点在生产代码里的位置是 Rollback 的「实体锁已持、publisher.Rollback 尚未调用」
// （page_publish.go:862），所以窗口现场是：**访问面还没切**（仍是 v2 的链接）、DB 仍是
// v2、回执停在 pending。这条钉的是「失败不留下半截 FS 副作用」——FS 压根没动，所以下面
// 断言「链接仍是 h2」；收敛（收尾段）随后按链接现状把回执结案，不误判成回滚已生效。
//
// 窗口的**另一侧**（FS 已切、DB 未落定）由 TestRollbackCommitsSwitchBeforeDeathConvergesOnRecovery
// 用独立注入点 postSwitchFault 覆盖 —— 两条互补，不能互相替代。
func TestRollbackCrashWindowConvergesOnRecovery(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	page := createPage(t, svc, projectID, "/rb-window", pageDocument)
	h1 := buildAndPublish(t, svc, page.ID)

	// 第二版：改内容再发布，得到另一个产物与活跃指针。
	if _, serr := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: page.ID, ExpectedVersion: page.DraftVersion,
		DraftPath: "/rb-window", DraftDocument: json.RawMessage(headingDocument),
	}); serr != nil {
		t.Fatalf("保存 v2 失败: %v", serr)
	}
	h2 := buildAndPublish(t, svc, page.ID)
	if h1 == h2 {
		t.Fatalf("两版产物 hash 应不同，实际都是 %q", h1)
	}
	if got := activeHashOf(t, db, page.ID); got != h2 {
		t.Fatalf("第二版发布后活跃产物应为 %q，实际 %q", h2, got)
	}

	// 注入点 = publishWindowFault（命中在 publisher.Rollback **之前**）：实体锁已持、
	// 访问面尚未切换。记录发布链留下的账本成功身份，供「失败不得改写账本」断言用。
	beforeReceiptID := ledgerReceiptID(t, db, page.ID, "zh-CN")
	if beforeReceiptID == 0 {
		t.Fatal("前置失败：h2 发布后账本应已有成功身份，实际 0")
	}
	injected := errors.New("故障注入：实体锁已持、访问面尚未切换、DB 未落定")
	setPublishWindowFault(t, svc, func() error { return injected })

	if _, rerr := svc.Rollback(ctx, &pagedto.RollbackReq{ID: page.ID, TargetHash: h1}); rerr == nil {
		t.Fatal("窗口故障注入后 Rollback 应报错，实际返回成功")
	}

	// 现场一：注入点在 publisher.Rollback 之前就返回了，**一次访问面切换都没有发生**。
	// 链接必须仍是 v2 的产物 —— 断言 h1 就是另一份实现（那会意味着「失败也切了 FS」）。
	if got := linkedArtifactHash(t, "/rb-window"); got != h2 {
		t.Fatalf("故障注入在切换之前，访问面应保持第二版产物 %q，实际 %q", h2, got)
	}
	if got := activeHashOf(t, db, page.ID); got != h2 {
		t.Fatalf("崩溃窗口里活跃指针不应改写（事务应回滚），实际 %q", got)
	}
	// 现场二：回执被**显式结案为 rolled_back**，不是 pending。
	//
	// 为什么不是 pending：注入点在任何写入之前返回，生产走「访问面未切换」这一格
	// （page_publish.go 的 !rolledBack 分支）→ abortPublishReceipt。这里的状态是「可判定的
	// 无副作用失败」，结案成 rolled_back 就是正确语义；留 pending 反而会让收敛去猜一次
	// 从未发生的切换。收敛侧的正例由 TestRollbackCommitsSwitchBeforeDeathConvergesOnRecovery
	// 用postSwitchFault 覆盖。
	if got := receiptStateForAction(t, db, "/rb-window", "rollback"); got != "rolled_back" {
		t.Fatalf("未切换的回滚回执应显式结案为 rolled_back，实际 %q", got)
	}
	if got := ledgerReceiptID(t, db, page.ID, "zh-CN"); got != beforeReceiptID {
		t.Fatalf("失败的回滚不得改写账本成功身份：期望沿用发布留下的 %d，实际 %d", beforeReceiptID, got)
	}

	setPublishWindowFault(t, svc, nil)
	recovered, rolledBack, rerr := recoverPending(t, svc, ctx)
	if rerr != nil {
		t.Fatalf("恢复失败: %v", rerr)
	}
	if recovered != 0 || rolledBack != 0 {
		t.Fatalf("回执已显式结案，收敛不应再判定任何一条，实际 %d / %d", recovered, rolledBack)
	}

	// 收敛后状态必须原样：链接与活跃指针都还是 v2。
	// 这条同时钉住「已结案的回执不得被收敛重新当成待办」：否则一次失败的注入会变成
	// 线上静默回滚。
	if got := activeHashOf(t, db, page.ID); got != h2 {
		t.Fatalf("收敛不得改写活跃指针，期望 %q，实际 %q", h2, got)
	}
	if got := linkedArtifactHash(t, "/rb-window"); got != h2 {
		t.Fatalf("收敛不得切换访问面，期望 %q，实际 %q", h2, got)
	}
	if got := routeOccupiedAt(t, db, projectID, "/rb-window"); got != "active" {
		t.Fatalf("收敛后路径应为 active 路由，实际 %q", got)
	}
	if got := receiptStateForAction(t, db, "/rb-window", "rollback"); got != "rolled_back" {
		t.Fatalf("收敛不得改写已结案回执，期望 rolled_back，实际 %q", got)
	}
}

// TestRollbackCommitsSwitchBeforeDeathConvergesOnRecovery 回滚崩在「FS 已切、DB 未落定」
// 窗口的**另一侧**：进程在事务提交前死掉，重启后必须按链接现状补完。
//
// 与上一条的区别是注入点（见 Service.postSwitchFault 字段注释）：
//
//	· 上一条命中在 publisher.Rollback 之前 → FS 从未切换、事务回滚、回执被显式
//	  结案为 rolled_back。它证明的是「半截写不留 FS 副作用」。
//	· 这一条命中在 DB 三步写完、事务回调返回之前（rolledBack 已 true）→ 注入的错误
//	  等价于「此刻进程被 kill」：事务回滚、回执留 pending，但访问面**已经**指向 h1。
//	  它证明的是 TX-009 要消除的那个窗口：收敛能按链接实际指向补齐，而不是永远卡住。
//
// 断言刻意写成「收敛后链接 h1 / 活跃指针 h1 / 回执 committed / 二次重放零判定」，
// 一次放宽都不留：这条用例的全部价值就是「FS 已切的那一侧真的能被补完」。
func TestRollbackCommitsSwitchBeforeDeathConvergesOnRecovery(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	page := createPage(t, svc, projectID, "/rb-post-switch", pageDocument)
	h1 := buildAndPublish(t, svc, page.ID)

	if _, serr := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: page.ID, ExpectedVersion: page.DraftVersion,
		DraftPath: "/rb-post-switch", DraftDocument: json.RawMessage(headingDocument),
	}); serr != nil {
		t.Fatalf("保存 v2 失败: %v", serr)
	}
	h2 := buildAndPublish(t, svc, page.ID)
	if h1 == h2 {
		t.Fatalf("两版产物 hash 应不同，实际都是 %q", h1)
	}
	if got := linkedArtifactHash(t, "/rb-post-switch"); got != h2 {
		t.Fatalf("前置失败：访问面应指向第二版产物 %q，实际 %q", h2, got)
	}

	// 发布 h2 已经往账本写过成功身份；回滚事务失败后账本必须**保持这个旧值**，
	// 而不是 0 —— 断言 0 就是把「夹具本来就没写账本」当成通过。
	beforeReceiptID := ledgerReceiptID(t, db, page.ID, "zh-CN")
	if beforeReceiptID == 0 {
		t.Fatal("前置失败：h2 发布后账本应已有成功身份，实际 0")
	}

	setPostSwitchFault(t, svc, func() error { return errors.New("故障注入：进程在事务提交前被 kill") })
	defer setPostSwitchFault(t, svc, nil)

	if _, rerr := svc.Rollback(ctx, &pagedto.RollbackReq{ID: page.ID, TargetHash: h1}); rerr == nil {
		t.Fatal("提交前故障注入后 Rollback 应报错，实际返回成功")
	}

	// 死时的现场：访问面**已经**指向回滚目标（FS 切换不可回滚），
	// 而数据库仍停在 v2、回执 pending —— 这正是需要启动恢复的那个组合。
	if got := linkedArtifactHash(t, "/rb-post-switch"); got != h1 {
		t.Fatalf("注入点在切换之后：访问面应已指向回滚目标 %q，实际 %q", h1, got)
	}
	if got := activeHashOf(t, db, page.ID); got != h2 {
		t.Fatalf("事务应整体回滚，活跃指针仍应是 %q，实际 %q", h2, got)
	}
	if got := ledgerReceiptID(t, db, page.ID, "zh-CN"); got != beforeReceiptID {
		t.Fatalf("事务回滚后账本必须保持发布留下的成功身份 %d，实际 %d", beforeReceiptID, got)
	}
	if got := receiptStateForAction(t, db, "/rb-post-switch", "rollback"); got != "pending" {
		t.Fatalf("回滚回执应保持 pending 交收敛，实际 %q", got)
	}

	// 重启：收敛必须按链接现状把 DB 补齐到 h1（不是把链接打回 h2，也不是卡在 pending）。
	setPostSwitchFault(t, svc, nil)
	recovered, rolledBack, rerr := recoverPending(t, svc, ctx)
	if rerr != nil {
		t.Fatalf("恢复失败: %v", rerr)
	}
	if recovered != 1 || rolledBack != 0 {
		t.Fatalf("应补完成 1 条、回滚 0 条，实际 %d / %d", recovered, rolledBack)
	}
	if got := activeHashOf(t, db, page.ID); got != h1 {
		t.Fatalf("收敛后活跃指针应为回滚目标 %q，实际 %q", h1, got)
	}
	if got := linkedArtifactHash(t, "/rb-post-switch"); got != h1 {
		t.Fatalf("收敛后访问面应仍指向回滚目标 %q，实际 %q", h1, got)
	}
	if got := routeOccupiedAt(t, db, projectID, "/rb-post-switch"); got != "active" {
		t.Fatalf("收敛后路径应为 active 路由，实际 %q", got)
	}
	if got := receiptStateForAction(t, db, "/rb-post-switch", "rollback"); got != "committed" {
		t.Fatalf("收敛后回执应结案为 committed，实际 %q", got)
	}

	// 二次重放幂等：不再判定任何回执，状态不动。
	againRecovered, againRolledBack, aerr := recoverPending(t, svc, ctx)
	if aerr != nil {
		t.Fatalf("重复恢复失败: %v", aerr)
	}
	if againRecovered != 0 || againRolledBack != 0 {
		t.Fatalf("重复恢复不应再判定任何回执，实际 %d / %d", againRecovered, againRolledBack)
	}
	if got := activeHashOf(t, db, page.ID); got != h1 {
		t.Fatalf("重复恢复不得改动已收敛的活跃指针，实际 %q", got)
	}
}

// receiptStateForAction 读某路径某动作回执的最新状态（回执表是共用表，必须按 action 筛）。
func receiptStateForAction(t *testing.T, db *gorm.DB, path, action string) string {
	t.Helper()
	var state string
	if err := db.Raw(`SELECT receipt_state FROM publication_receipts WHERE path = ? AND action = ?
		ORDER BY id DESC LIMIT 1`, path, action).Scan(&state).Error; err != nil {
		t.Fatalf("读取回执状态失败: %v", err)
	}
	return state
}

// receiptStateByID 读**某一条**回执的状态（按 id，不按路径/动作）。
//
// 收敛领取的是**全表**待办（page_publish.go:3237 receiptClaimSourceTypes，不按页或
// 工程过滤），同一进程里别的用例留下的 pending 也会被这一轮领走 —— 所以「本轮结案了
// 几条」这类总数统计受同库其它 pending 影响，拿它写等式注定不稳。要钉住「这条回执按
// 它自己的现场被判成什么」，只有按 id 读才成立。
func receiptStateByID(t *testing.T, db *gorm.DB, id int64) string {
	t.Helper()
	var state string
	if err := db.Raw(`SELECT receipt_state FROM publication_receipts WHERE id = ?`, id).Scan(&state).Error; err != nil {
		t.Fatalf("读取回执 %d 状态失败: %v", id, err)
	}
	return state
}

// draftPathOf 读 pages.draft_path。
func draftPathOf(t *testing.T, db *gorm.DB, pageID string) string {
	t.Helper()
	var path string
	if err := db.Raw(`SELECT draft_path FROM pages WHERE id = ?`, pageID).Scan(&path).Error; err != nil {
		t.Fatalf("读取 draft_path 失败: %v", err)
	}
	return path
}

// publicationActivePath 读该页面激活记录里的路径（单语言用例只有一行）。
func publicationActivePath(t *testing.T, db *gorm.DB, pageID string) string {
	t.Helper()
	var path string
	if err := db.Raw(`SELECT active_path FROM page_publications WHERE page_id = ?`, pageID).Scan(&path).Error; err != nil {
		t.Fatalf("读取激活路径失败: %v", err)
	}
	return path
}

// routeOccupiedAt 读某路径的路由占用 kind；无占用返回空串。
func routeOccupiedAt(t *testing.T, db *gorm.DB, projectID, path string) string {
	t.Helper()
	var kind string
	if err := db.Raw(`SELECT route_kind FROM page_routes WHERE project_id = ? AND path = ?`,
		projectID, path).Scan(&kind).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("读取路由占用失败: %v", err)
	}
	return kind
}

// linkedArtifactHash 读访问面某路径激活产物的 hash（剥掉相对链接前缀）。
func linkedArtifactHash(t *testing.T, urlPath string) string {
	t.Helper()
	state, err := (&pipeline.LocalPublicationStore{ActiveRoot: pipeline.ActiveRoot()}).Inspect(urlPath)
	if err != nil {
		t.Fatalf("读取访问面状态失败: %v", err)
	}
	if state == nil || state.Locator == nil {
		t.Fatalf("路径 %s 未激活", urlPath)
	}
	if state.Kind != pipeline.PublicationPage {
		t.Fatalf("路径 %s 应是页面产物，实际 kind=%s", urlPath, state.Kind)
	}
	return strings.TrimPrefix(state.Locator.Key, "artifacts/")
}

// ---- 改 URL 的激活屏障（2026-10-30 定点整改）----
//
// 三件事此前没有任何用例守着，且都不是文档问题：
//
//	B1 同路径、同 artifact 的**另一条**回执成功后，老回执重放不得覆盖它的成功身份；
//	B2 旧路径处置失败时必须保持 pending 且可以重试（此前先结案再处置，失败即失去重放入口）；
//	B3 编译/切换没成功时不得执行旧路径处置（新路径没有产物，旧路径必须原样在线）。

// insertUpdateURLReceipt 直插一条 update_url 的 pending 回执（记下从哪个路径改到哪）。
//
// 只走「回执表 → 恢复例程」这一条真实入口：页面、产物行、访问面链接都用生产同形的夹具。
func insertUpdateURLReceipt(t *testing.T, db *gorm.DB, pageID, toPath, oldPath, lang, fromArtifactID string, redirect bool, capturedVersion int64) int64 {
	t.Helper()
	data := fmt.Sprintf(`{"projectId":%q,"lang":%q,"oldPath":%q,"redirect":%t,"capturedInputVersion":%d}`,
		projectIDOfPage(t, db, pageID), lang, oldPath, redirect, capturedVersion)
	// from_artifact_id 是 uuid NULL：调用方用空串表示「这次改址没有前置产物」（首次发布就改址），
	// 空串绑到 uuid 列会报 22P02（invalid input syntax for type uuid: ""），必须显式写 NULL。
	var fromArtifact any
	if strings.TrimSpace(fromArtifactID) != "" {
		fromArtifact = fromArtifactID
	}
	var id int64
	if err := db.Raw(`INSERT INTO publication_receipts (source_type, source_id, action, path, from_artifact_id, receipt_state, receipt_data, create_time)
		VALUES ('page', ?, 'update_url', ?, ?, 'pending', ?::jsonb, now()) RETURNING id`,
		pageID, toPath, fromArtifact, data).Scan(&id).Error; err != nil {
		t.Fatalf("插入改 URL 回执失败: %v", err)
	}
	return id
}

// projectIDOfPage 读页面所属工程（回执的 receipt_data 里要带它，恢复据此定位）。
func projectIDOfPage(t *testing.T, db *gorm.DB, pageID string) string {
	t.Helper()
	var projectID string
	if err := db.Raw(`SELECT project_id FROM pages WHERE id = ?`, pageID).Scan(&projectID).Error; err != nil {
		t.Fatalf("读取页面工程失败: %v", err)
	}
	return projectID
}

// TestUpdateURLOldReceiptDoesNotOverrideNewerSuccess 同路径、同 artifact 的另一条回执
// 成功后，老回执重放不得覆盖成功身份（B1）。
//
// 现场：页面从 /url-a 改到 /url-b 的两次尝试（同内容，产物 hash 相同）都留下了 pending
// 回执；后一条已经成功兑现（账本记着它的 id）。老回执被重放时，链接、产物、路径全部
// 与它自己记的一致 —— 只看「版本相等」或「artifact 相等」会判成功，把账本身份改成老的，
// 于是下一次收敛再也分不清谁是最新。
func TestUpdateURLOldReceiptDoesNotOverrideNewerSuccess(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	page := createPage(t, svc, projectID, "/url-a", headingDocument)

	// 真实产物（磁盘 manifest.json 带 canonicalPath=/url-b）：收敛判定「这条路径上跑的是它」
	// 读的就是磁盘那份 manifest（page_publish.go:2304 + pipeline/store.go:157-200）。
	hash, artifactID := seedUpdateURLArtifact(t, db, page.ID, "/url-b")
	linkActivePath(t, root, "/url-b", hash)

	// 老回执先落库（create_time 更早），新回执照常更靠后。
	//
	// 第三条是**历史残留**：同一页上一条与本次无关的 update_url pending（路径与产物都不同）。
	// 它存在的意义是钉住「收敛按回执自己的现场判决」，而不是只统计本用例关心的两条 ——
	// 没有它，第三条被标 rolled_back 只可能是越界误判，有了它则证明「判定范围是全表待办、
	// 判决依据是各自的事实」（见下面 recovered/rolledBack 的断言）。
	_, staleArtifactID := seedUpdateURLArtifact(t, db, page.ID, "/url-stale-new")
	staleID := insertUpdateURLReceipt(t, db, page.ID, "/url-stale-new", "/url-stale-old", "zh-CN", staleArtifactID, true, 0)
	oldID := insertUpdateURLReceipt(t, db, page.ID, "/url-b", "/url-a", "zh-CN", artifactID, true, 0)
	newID := insertUpdateURLReceipt(t, db, page.ID, "/url-b", "/url-a", "zh-CN", artifactID, true, 0)
	// 新回执已兑现：账本记着它。
	//
	// 必须先**造行**再改 receipt_id：本页从未走过发布链（夹具直接写产物与链接），
	// page_publications 里没有它的行 —— 裸 UPDATE 会静默影响 0 行，读回 0，
	// 于是「老回执不得覆盖新身份」的断言在空账本上恒真。列名按 062 + 205 之后的
	// 最终形态（published_at / update_time，没有 create_time）。
	if err := db.Exec(`INSERT INTO page_publications
		(page_id, lang, active_path, artifact_id, artifact_hash, published_at, update_time, receipt_id)
		VALUES (?, 'zh-CN', '/url-b', ?, ?, now(), now(), ?)`,
		page.ID, artifactID, hash, newID).Error; err != nil {
		t.Fatalf("预置账本行失败: %v", err)
	}
	if got := ledgerReceiptID(t, db, page.ID, "zh-CN"); got != newID {
		t.Fatalf("前置失败：账本身份应为新回执 %d，实际 %d（夹具没生效，后续断言会空转）", newID, got)
	}
	// 老回执的出发身份必须显式写成「不是我的」：receiptIdentityVerdict 的判据是
	// fromReceiptID → 自己 → 账本上出现的第三方身份。三条都为空时（from 缺省 = 0）
	// 判定会落到「仍是有效回执」这一格，与账本上写着新回执的事实无关 —— 那样测的
	// 是零值兜底，而不是「老回执不得覆盖新身份」。
	if err := db.Exec(`UPDATE publication_receipts SET receipt_data = receipt_data || ?::jsonb WHERE id = ?`,
		fmt.Sprintf(`{"fromReceiptId":%d}`, newID), oldID).Error; err != nil {
		t.Fatalf("预置老回执的出发身份失败: %v", err)
	}

	recovered, rolledBack, rerr := recoverPending(t, svc, ctx)
	if rerr != nil {
		t.Fatalf("恢复失败: %v", rerr)
	}
	if got := ledgerReceiptID(t, db, page.ID, "zh-CN"); got != newID {
		t.Fatalf("老回执不得覆盖新回执的成功身份：期望 %d，实际 %d", newID, got)
	}
	// 三条 pending 回执各自按**自己的现场**判决：
	//   · 新回执：新路径上的产物与账本身份都对得上 → 前向补齐，committed（recovered）；
	//   · 老回执：出发身份显式指向新回执 → receiptIdentityVerdict 判 superseded，结案 rolled_back；
	//   · 历史残留：/url-stale-new 上没有产物、账本也已前进到新回执 → 判「未生效」，rolled_back。
	//
	// 断言按**回执 id** 逐条做，不写总数等式：收敛领取的是全表待办（page_publish.go:3237），
	// 同一进程里别的用例留下的 pending 会被这一轮一起领走，总数只能当下界看。
	if recovered+rolledBack < 3 {
		t.Fatalf("本轮至少应判定这三条回执，实际 recovered=%d rolledBack=%d", recovered, rolledBack)
	}
	if got := receiptStateByID(t, db, newID); got != "committed" {
		t.Fatalf("新回执应补完成为 committed，实际 %q", got)
	}
	if got := receiptStateByID(t, db, oldID); got != "rolled_back" {
		t.Fatalf("老回执（出发身份指向新回执）应结案为 rolled_back，实际 %q", got)
	}
	if got := receiptStateByID(t, db, staleID); got != "rolled_back" {
		t.Fatalf("历史残留回执应按自己的现场判「未生效」并结案，实际 %q", got)
	}
	if got := draftPathOf(t, db, page.ID); got != "/url-b" {
		t.Fatalf("收敛后草稿路径应为 /url-b，实际 %q", got)
	}
}

// TestUpdateURLOldPathSettleFailureKeepsPending 旧路径处置失败时必须保持 pending（B2）。
//
// 现场：新路径已切到本次产物、账本仍是 from、版本一致 —— 判定这一步一定通过；
// 处置这一步失败在 301 落盘的祖先链检查上（见下）。
//
// **占位必须打在祖先上**。实测（internal/pipeline/publication.go:77-133）Activate 先
// ensureAncestorsAreDirs（:146-170，只查**祖先链**），再 os.Symlink(tmp) + os.Rename(tmp, link)
// —— unix 的 rename 是原子覆盖：旧路径**自身**是普通文件或符号链接时，激活照样成功。
// 真正会失败的分支只有两条：
//  1. ensureAncestorsAreDirs：某个**祖先**是符号链接（「父路径 … 已被符号链接占位」）
//     或普通文件（「父路径 … 已被非目录占位」）；
//  2. rename 的系统错误（跨挂载点等）。
//
// 所以占位物写成祖先 /dest 的普通文件，命中第 1 条 —— 那才是 301 落盘的真实失败分支。
//
// 失败发生在**提交之后**：settleOldPathLocked 先在自己的事务里提交 DB 条件写，事务返回
// 后才动文件系统。所以 DB 段（draft_path 迁到新路径、账本写成本条回执）已经生效且正确，
// 不回滚；失败只留在 FS 这一步 —— 回执保持 pending，下一轮按现场重放。
// 修复前的顺序是先 complete 再 settle：处置一失败回执已结案，那次失败再没有重放入口。
//
// 清理时必须先删掉这个文件再重放：删掉后 MkdirAll 才能建出 /dest 目录，
// 重放才有机会把 /dest/settle-old 建成 301 符号链接（下面第三段的 redirect 断言靠它）。
func TestUpdateURLOldPathSettleFailureKeepsPending(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	page := createPage(t, svc, projectID, "/dest/settle-old", headingDocument)

	hash, artifactID := seedUpdateURLArtifact(t, db, page.ID, "/settle-new")
	linkActivePath(t, root, "/settle-new", hash)
	// 占位物打在**祖先** /dest 上（普通文件，不是目录）：Activate 的原子 rename 会覆盖
	// 「旧路径自身是普通文件」的现场（internal/pipeline/publication.go:124-133），只有
	// ensureAncestorsAreDirs（:146-170）会在祖先非目录时失败 —— 那才是 301 落盘的真实失败分支。
	blockedOldPath := "/dest/settle-old"
	blocker := filepath.Join(activeDir(t), "dest")
	if err := os.MkdirAll(filepath.Dir(blocker), 0o755); err != nil {
		t.Fatalf("创建祖先父目录失败: %v", err)
	}
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("预置祖先占位文件失败: %v", err)
	}
	// 占位物必须是普通文件：符号链接会走「父路径已被符号链接占位」那条同源失败，
	// 但语义不同（这里要证的是祖先**非目录**）。
	if info, serr := os.Lstat(blocker); serr != nil || !info.Mode().IsRegular() {
		t.Fatalf("前置失败：祖先占位物应为普通文件，err=%v", serr)
	}
	// 账本起始身份 = 本回执的出发身份，且出发身份非零：这样 L3 的
	// receiptIdentityVerdict(from, self, cur) 落在「仍有效」而不是「被替代」——
	// 本用例要证明的是「**处置**失败保 pending」，不是「判定为被替代」。
	//
	// 必须**先造账本行**再写 receipt_id：本页的发布链没走过（夹具直接写产物与链接），
	// page_publications 里没有它的行，裸 UPDATE 静默影响 0 行 → 账本身份读回 0 →
	// L3 的「账本为空」判据成立，回执会被前向补齐到 committed，根本走不到处置那一步。
	// 列名按 062 + 205 之后的最终形态（published_at / update_time，没有 create_time）。
	seedFromReceiptID := int64(1)
	receiptID := insertUpdateURLReceipt(t, db, page.ID, "/settle-new", blockedOldPath, "zh-CN", artifactID, true, 0)
	if err := db.Exec(`INSERT INTO page_publications
		(page_id, lang, active_path, artifact_id, artifact_hash, published_at, update_time, receipt_id)
		VALUES (?, 'zh-CN', ?, ?, ?, now(), now(), ?)
		ON CONFLICT (page_id, lang) DO UPDATE SET receipt_id = EXCLUDED.receipt_id`,
		page.ID, blockedOldPath, artifactID, hash, seedFromReceiptID).Error; err != nil {
		t.Fatalf("预置账本起始身份失败: %v", err)
	}
	if got := ledgerReceiptID(t, db, page.ID, "zh-CN"); got != seedFromReceiptID {
		t.Fatalf("前置失败：账本身份应为 %d，实际 %d（夹具没生效，后续断言会空转）", seedFromReceiptID, got)
	}
	if err := db.Exec(`UPDATE publication_receipts SET receipt_data = receipt_data || ?::jsonb WHERE id = ?`,
		fmt.Sprintf(`{"fromReceiptId":%d}`, seedFromReceiptID), receiptID).Error; err != nil {
		t.Fatalf("预置出发身份失败: %v", err)
	}

	if _, _, rerr := recoverPending(t, svc, ctx); rerr != nil {
		t.Fatalf("恢复应把失败交回调用方（保留 pending），实际 %v", rerr)
	}
	if got := receiptStateForAction(t, db, "/settle-new", "update_url"); got != "pending" {
		t.Fatalf("旧路径处置失败后回执必须保持 pending（否则没有重放入口），实际 %q", got)
	}
	// 处置在 DB 提交**之后**：失败只留在 FS 这一步，DB 段不回滚 ——
	//
	// 收敛重放走 recoverUpdateURLReceipt：L2/L3/L4 判定通过后 applyUpdateURL 在事务内
	// 提交（draft_path 迁到 /settle-new、账本写成本条回执），事务返回后才发现旧路径那条
	// 路径被普通文件占位、301 建不出来。这正是「DB 已一致、FS 差一脚」的形态：回执留
	// pending 交下一轮重放，而不是把已经正确的 DB 段回滚掉。
	if got := draftPathOf(t, db, page.ID); got != "/settle-new" {
		t.Fatalf("处置失败发生在提交之后：draft_path 应已迁到新路径 /settle-new，实际 %q", got)
	}
	if got := ledgerReceiptID(t, db, page.ID, "zh-CN"); got != receiptID {
		t.Fatalf("账本应已是本条回执身份（等于 DB 段已提交）：期望 %d，实际 %d", receiptID, got)
	}
	// FS 确实没处置：占位文件还在，旧路径没有被改成 301。
	//
	// 这里刻意**不用** activeKind：祖先 /dest 此刻被普通文件占着，os.Lstat 走这条路径
	// 会回 ENOTDIR（not a directory），activeKind 把它当致命错误直接 Fatal —— 断言本身
	// 先崩，就不是在证实现。改用服务自己的读法（InspectPublicationPath 内部走
	// publication.Inspect，读不到即返回 false）表达同一件事：这条旧路径上没有激活产物。
	type pathInspector interface {
		InspectPublicationPath(path string) (string, bool)
	}
	insp, ok := svc.(pathInspector)
	if !ok {
		t.Fatal("服务未暴露 InspectPublicationPath：无法在祖先被占位的现场读回处置结果")
	}
	if loc, live := insp.InspectPublicationPath(blockedOldPath); live {
		t.Fatalf("处置失败时旧路径不应有激活产物，实际 %q", loc)
	}
	// 失败只留在 FS 这一步：回执必须仍是 pending，下一轮照样能领到它重放。
	if got := receiptStateForAction(t, db, "/settle-new", "update_url"); got != "pending" {
		t.Fatalf("旧路径处置失败后回执必须保持 pending（否则没有重放入口），实际 %q", got)
	}
	// 修好现场后重放同一段：必须能收敛（幂等 + 可重放）。
	if err := os.Remove(blocker); err != nil {
		t.Fatalf("清理占位文件失败: %v", err)
	}
	// 二次重放按**本条回执 id** 判定，不写总数等式（收敛领取全表待办，见 receiptStateByID 注释）。
	if _, _, rerr := recoverPending(t, svc, ctx); rerr != nil {
		t.Fatalf("修好现场后重放失败: %v", rerr)
	}
	if got := receiptStateForAction(t, db, "/settle-new", "update_url"); got != "committed" {
		t.Fatalf("重放成功后回执应结案为 committed，实际 %q", got)
	}
	if got := ledgerReceiptID(t, db, page.ID, "zh-CN"); got != receiptID {
		t.Fatalf("重放成功应把本条回执身份写进账本：期望 %d，实际 %d", receiptID, got)
	}
	if kind := activeKind(t, blockedOldPath); kind != "redirect" {
		t.Fatalf("重放成功后旧路径应已按 WithRedirect 处置为 301，实际 %q", kind)
	}
}

// TestUpdateURLWithoutSwitchSkipsOldPathSettle 编译/切换没成功时不得处置旧路径（B3）。
//
// 现场：新路径上没有任何产物（内核切换没发生），回执是 pending。旧路径必须原样在线 ——
// 「意图已落库」不等于「激活成功」，在新路径还没有字节的时候把旧路径改成 301，
// 访客会从一条能看的页面被送去一个 404。
func TestUpdateURLWithoutSwitchSkipsOldPathSettle(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	page := createPage(t, svc, projectID, "/no-switch-old", headingDocument)

	// 旧路径仍在线，指向一份真实产物（磁盘 manifest 的 canonicalPath=/no-switch-old）。
	oldHash, oldArtifactID := seedUpdateURLArtifact(t, db, page.ID, "/no-switch-old")
	linkActivePath(t, root, "/no-switch-old", oldHash)
	if err := db.Exec(`INSERT INTO page_publications (page_id, lang, active_path, artifact_id, artifact_hash, published_at, update_time)
		VALUES (?, 'zh-CN', '/no-switch-old', ?, ?, now(), now())`, page.ID, oldArtifactID, oldHash).Error; err != nil {
		t.Fatalf("预置账本失败: %v", err)
	}
	// 新路径上**刻意什么都不放**（不 seed 产物）= 切换没发生：这是本用例的现场本身，
	// 不是夹具缺陷 —— updateURLArtifactAt 会如实判「这条路径上没有产物」。
	insertUpdateURLReceipt(t, db, page.ID, "/no-switch-new", "/no-switch-old", "zh-CN", oldArtifactID, true, 0)

	if _, _, rerr := recoverPending(t, svc, ctx); rerr != nil {
		t.Fatalf("恢复失败: %v", rerr)
	}
	if kind := activeKind(t, "/no-switch-old"); kind != "page" {
		t.Fatalf("切换未成功时旧路径必须仍是在线页面（不得被处置成 %q）", kind)
	}
	if got := receiptStateForAction(t, db, "/no-switch-new", "update_url"); got != "pending" {
		t.Fatalf("账本未前进时老回执应保留 pending 等自己的重放，实际 %q", got)
	}
	if got := ledgerReceiptID(t, db, page.ID, "zh-CN"); got != 0 {
		t.Fatalf("不得写入任何成功身份，实际 %d", got)
	}
}

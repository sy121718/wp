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
	"encoding/json"
	"errors"
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

// TestRollbackCrashWindowConvergesOnRecovery 回滚崩在「FS 已切回历史产物、DB 未落定」窗口后收敛。
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

	injected := errors.New("故障注入：进程终止于切换访问面之后、DB 落定之前")
	setPublishWindowFault(t, svc, func() error { return injected })

	if _, rerr := svc.Rollback(ctx, &pagedto.RollbackReq{ID: page.ID, TargetHash: h1}); rerr == nil {
		t.Fatal("窗口故障注入后 Rollback 应报错，实际返回成功")
	}

	// 窗口现场一：访问面已切回 v1 产物，DB 仍说 v2。
	if got := linkedArtifactHash(t, "/rb-window"); got != h1 {
		t.Fatalf("访问面应已指向回滚目标 %q，实际 %q", h1, got)
	}
	if got := activeHashOf(t, db, page.ID); got != h2 {
		t.Fatalf("崩溃窗口里活跃指针不应改写（事务应回滚），实际 %q", got)
	}
	// 窗口现场二：回执停在 pending。
	if got := receiptStateForAction(t, db, "/rb-window", "rollback"); got != "pending" {
		t.Fatalf("回滚回执应保持 pending，实际 %q", got)
	}

	setPublishWindowFault(t, svc, nil)
	recovered, rolledBack, rerr := recoverPending(t, svc, ctx)
	if rerr != nil {
		t.Fatalf("恢复失败: %v", rerr)
	}
	if recovered != 1 || rolledBack != 0 {
		t.Fatalf("应补完成 1 条、回滚 0 条，实际 %d / %d", recovered, rolledBack)
	}

	// 收敛：活跃指针与路由都回到 v1。
	if got := activeHashOf(t, db, page.ID); got != h1 {
		t.Fatalf("恢复后活跃产物应为回滚目标 %q，实际 %q", h1, got)
	}
	if got := routeOccupiedAt(t, db, projectID, "/rb-window"); got != "active" {
		t.Fatalf("恢复后路径应为 active 路由，实际 %q", got)
	}
	if got := receiptStateForAction(t, db, "/rb-window", "rollback"); got != "committed" {
		t.Fatalf("恢复后回执应收敛为 committed，实际 %q", got)
	}

	// 幂等：重复恢复不改结果。
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

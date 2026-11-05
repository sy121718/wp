package unit

import (
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	publish "go_wp/pkg/publish"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"

	pagedto "go_wp/internal/module/page/dto"
)

func TestUpdateURLCrashWindowConvergesOnRecovery(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	page := createPage(t, svc, projectID, "/url-old", headingDocument)
	buildAndPublish(t, svc, page.ID)
	beforePublication := publicationsOfDB(t, db, page.ID)

	injected := errors.New("故障注入：进程终止于切换访问面之后、DB 落定之前")
	setPublishWindowFault(t, svc, func() error { return injected })

	if _, uerr := svc.UpdateURL(ctx, &pagedto.UpdateURLReq{ID: page.ID, NewPath: "/url-new", WithRedirect: true}); uerr == nil {
		t.Fatal("窗口故障注入后 UpdateURL 应报错，实际返回成功")
	}

	if kind := activeKind(t, "/url-new"); kind != "page" {
		t.Fatalf("新路径应已激活为 page（切换已发生），实际 %q", kind)
	}

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

	if got := receiptStateForAction(t, db, "/url-new", "update_url"); got != "pending" {
		t.Fatalf("改 URL 回执应保持 pending，实际 %q", got)
	}

	setPublishWindowFault(t, svc, nil)
	recovered, rolledBack, rerr := recoverPending(t, svc, ctx)
	if rerr != nil {
		t.Fatalf("恢复失败: %v", rerr)
	}
	if recovered != 1 || rolledBack != 0 {
		t.Fatalf("应补完成 1 条、回滚 0 条，实际 %d / %d", recovered, rolledBack)
	}

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

	if got := routeOccupiedAt(t, db, projectID, "/url-new"); got != "active" {
		t.Fatalf("恢复后 /url-new 应为 active 路由，实际 %q", got)
	}
	if got := routeOccupiedAt(t, db, projectID, "/url-old"); got != "redirect" {
		t.Fatalf("恢复后 /url-old 应为 redirect 路由（WithRedirect=true），实际 %q", got)
	}

	var joined int64
	if err := db.Raw(`SELECT COUNT(*) FROM page_routes r JOIN page_artifacts a ON a.id = r.artifact_id
		WHERE r.project_id = ? AND r.path = ?`, projectID, "/url-new").Scan(&joined).Error; err != nil {
		t.Fatalf("查询路由产物关联失败: %v", err)
	}
	if joined != 1 {
		t.Fatalf("新路径的路由应指向已归档的产物行，实际关联数 %d", joined)
	}

	if got := receiptStateForAction(t, db, "/url-new", "update_url"); got != "committed" {
		t.Fatalf("恢复后回执应收敛为 committed（不许停在 pending），实际 %q", got)
	}

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

func TestRollbackCrashWindowConvergesOnRecovery(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	page := createPage(t, svc, projectID, "/rb-window", pageDocument)
	h1 := buildAndPublish(t, svc, page.ID)

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

	beforeReceiptID := ledgerReceiptID(t, db, page.ID, "zh-CN")
	if beforeReceiptID == 0 {
		t.Fatal("前置失败：h2 发布后账本应已有成功身份，实际 0")
	}
	injected := errors.New("故障注入：实体锁已持、访问面尚未切换、DB 未落定")
	setPublishWindowFault(t, svc, func() error { return injected })

	if _, rerr := svc.Rollback(ctx, &pagedto.RollbackReq{ID: page.ID, TargetHash: h1}); rerr == nil {
		t.Fatal("窗口故障注入后 Rollback 应报错，实际返回成功")
	}

	if got := linkedArtifactHash(t, "/rb-window"); got != h2 {
		t.Fatalf("故障注入在切换之前，访问面应保持第二版产物 %q，实际 %q", h2, got)
	}
	if got := activeHashOf(t, db, page.ID); got != h2 {
		t.Fatalf("崩溃窗口里活跃指针不应改写（事务应回滚），实际 %q", got)
	}

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

	beforeReceiptID := ledgerReceiptID(t, db, page.ID, "zh-CN")
	if beforeReceiptID == 0 {
		t.Fatal("前置失败：h2 发布后账本应已有成功身份，实际 0")
	}

	setPostSwitchFault(t, svc, func() error { return errors.New("故障注入：进程在事务提交前被 kill") })
	defer setPostSwitchFault(t, svc, nil)

	if _, rerr := svc.Rollback(ctx, &pagedto.RollbackReq{ID: page.ID, TargetHash: h1}); rerr == nil {
		t.Fatal("提交前故障注入后 Rollback 应报错，实际返回成功")
	}

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

func receiptStateForAction(t *testing.T, db *gorm.DB, path, action string) string {
	t.Helper()
	var state string
	if err := db.Raw(`SELECT receipt_state FROM publication_receipts WHERE path = ? AND action = ?
		ORDER BY id DESC LIMIT 1`, path, action).Scan(&state).Error; err != nil {
		t.Fatalf("读取回执状态失败: %v", err)
	}
	return state
}

func receiptStateByID(t *testing.T, db *gorm.DB, id int64) string {
	t.Helper()
	var state string
	if err := db.Raw(`SELECT receipt_state FROM publication_receipts WHERE id = ?`, id).Scan(&state).Error; err != nil {
		t.Fatalf("读取回执 %d 状态失败: %v", id, err)
	}
	return state
}

func draftPathOf(t *testing.T, db *gorm.DB, pageID string) string {
	t.Helper()
	var path string
	if err := db.Raw(`SELECT draft_path FROM pages WHERE id = ?`, pageID).Scan(&path).Error; err != nil {
		t.Fatalf("读取 draft_path 失败: %v", err)
	}
	return path
}

func publicationActivePath(t *testing.T, db *gorm.DB, pageID string) string {
	t.Helper()
	var path string
	if err := db.Raw(`SELECT active_path FROM page_publications WHERE page_id = ?`, pageID).Scan(&path).Error; err != nil {
		t.Fatalf("读取激活路径失败: %v", err)
	}
	return path
}

func routeOccupiedAt(t *testing.T, db *gorm.DB, projectID, path string) string {
	t.Helper()
	var kind string
	if err := db.Raw(`SELECT route_kind FROM page_routes WHERE project_id = ? AND path = ?`,
		projectID, path).Scan(&kind).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("读取路由占用失败: %v", err)
	}
	return kind
}

func linkedArtifactHash(t *testing.T, urlPath string) string {
	t.Helper()
	state, err := (&publish.LocalPublicationStore{ActiveRoot: publish.ActiveRoot()}).Inspect(urlPath)
	if err != nil {
		t.Fatalf("读取访问面状态失败: %v", err)
	}
	if state == nil || state.Locator == nil {
		t.Fatalf("路径 %s 未激活", urlPath)
	}
	if state.Kind != publish.PublicationPage {
		t.Fatalf("路径 %s 应是页面产物，实际 kind=%s", urlPath, state.Kind)
	}
	return strings.TrimPrefix(state.Locator.Key, "artifacts/")
}

func insertUpdateURLReceipt(t *testing.T, db *gorm.DB, pageID, toPath, oldPath, lang, fromArtifactID string, redirect bool, capturedVersion int64) int64 {
	t.Helper()
	data := fmt.Sprintf(`{"projectId":%q,"lang":%q,"oldPath":%q,"redirect":%t,"capturedInputVersion":%d}`,
		projectIDOfPage(t, db, pageID), lang, oldPath, redirect, capturedVersion)

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

func projectIDOfPage(t *testing.T, db *gorm.DB, pageID string) string {
	t.Helper()
	var projectID string
	if err := db.Raw(`SELECT project_id FROM pages WHERE id = ?`, pageID).Scan(&projectID).Error; err != nil {
		t.Fatalf("读取页面工程失败: %v", err)
	}
	return projectID
}

func TestUpdateURLOldReceiptDoesNotOverrideNewerSuccess(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	page := createPage(t, svc, projectID, "/url-a", headingDocument)

	hash, artifactID := seedUpdateURLArtifact(t, db, page.ID, "/url-b")
	linkActivePath(t, root, "/url-b", hash)

	_, staleArtifactID := seedUpdateURLArtifact(t, db, page.ID, "/url-stale-new")
	staleID := insertUpdateURLReceipt(t, db, page.ID, "/url-stale-new", "/url-stale-old", "zh-CN", staleArtifactID, true, 0)
	oldID := insertUpdateURLReceipt(t, db, page.ID, "/url-b", "/url-a", "zh-CN", artifactID, true, 0)
	newID := insertUpdateURLReceipt(t, db, page.ID, "/url-b", "/url-a", "zh-CN", artifactID, true, 0)

	if err := db.Exec(`INSERT INTO page_publications
		(page_id, lang, active_path, artifact_id, artifact_hash, published_at, update_time, receipt_id)
		VALUES (?, 'zh-CN', '/url-b', ?, ?, now(), now(), ?)`,
		page.ID, artifactID, hash, newID).Error; err != nil {
		t.Fatalf("预置账本行失败: %v", err)
	}
	if got := ledgerReceiptID(t, db, page.ID, "zh-CN"); got != newID {
		t.Fatalf("前置失败：账本身份应为新回执 %d，实际 %d（夹具没生效，后续断言会空转）", newID, got)
	}

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

func TestUpdateURLOldPathSettleFailureKeepsPending(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	page := createPage(t, svc, projectID, "/dest/settle-old", headingDocument)

	hash, artifactID := seedUpdateURLArtifact(t, db, page.ID, "/settle-new")
	linkActivePath(t, root, "/settle-new", hash)

	blockedOldPath := "/dest/settle-old"
	blocker := filepath.Join(activeDir(t), "dest")
	if err := os.MkdirAll(filepath.Dir(blocker), 0o755); err != nil {
		t.Fatalf("创建祖先父目录失败: %v", err)
	}
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("预置祖先占位文件失败: %v", err)
	}

	if info, serr := os.Lstat(blocker); serr != nil || !info.Mode().IsRegular() {
		t.Fatalf("前置失败：祖先占位物应为普通文件，err=%v", serr)
	}

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

	if got := draftPathOf(t, db, page.ID); got != "/settle-new" {
		t.Fatalf("处置失败发生在提交之后：draft_path 应已迁到新路径 /settle-new，实际 %q", got)
	}
	if got := ledgerReceiptID(t, db, page.ID, "zh-CN"); got != receiptID {
		t.Fatalf("账本应已是本条回执身份（等于 DB 段已提交）：期望 %d，实际 %d", receiptID, got)
	}

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

	if got := receiptStateForAction(t, db, "/settle-new", "update_url"); got != "pending" {
		t.Fatalf("旧路径处置失败后回执必须保持 pending（否则没有重放入口），实际 %q", got)
	}

	if err := os.Remove(blocker); err != nil {
		t.Fatalf("清理占位文件失败: %v", err)
	}

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

func TestUpdateURLWithoutSwitchSkipsOldPathSettle(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	page := createPage(t, svc, projectID, "/no-switch-old", headingDocument)

	oldHash, oldArtifactID := seedUpdateURLArtifact(t, db, page.ID, "/no-switch-old")
	linkActivePath(t, root, "/no-switch-old", oldHash)
	if err := db.Exec(`INSERT INTO page_publications (page_id, lang, active_path, artifact_id, artifact_hash, published_at, update_time)
		VALUES (?, 'zh-CN', '/no-switch-old', ?, ?, now(), now())`, page.ID, oldArtifactID, oldHash).Error; err != nil {
		t.Fatalf("预置账本失败: %v", err)
	}

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

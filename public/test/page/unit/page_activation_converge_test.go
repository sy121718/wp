package unit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	pageservice "go_wp/internal/module/page/service"

	json "go_wp/pkg/jsonx"

	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
)

func readInputVersionOf(t *testing.T, db *gorm.DB, pageID string) int64 {
	t.Helper()
	var v int64
	if err := db.Raw("SELECT input_version FROM pages WHERE id = ?", pageID).Scan(&v).Error; err != nil {
		t.Fatalf("读取代次失败: %v", err)
	}
	return v
}

func readActiveHash(t *testing.T, db *gorm.DB, pageID, lang string) string {
	t.Helper()
	var h string
	if err := db.Raw("SELECT COALESCE(artifact_hash, '') FROM page_publications WHERE page_id = ? AND lang = ?", pageID, lang).
		Scan(&h).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("读取账本失败: %v", err)
	}
	return h
}

func activeLinkHash(t *testing.T, urlPath string) string {
	t.Helper()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	link := filepath.Join(root, "public", "active", strings.TrimPrefix(urlPath, "/"))
	target, err := os.Readlink(link)
	if err != nil {
		return ""
	}
	return filepath.Base(target)
}

func TestSameStagedArtifactCanBeActivatedTwice(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	page, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/twice", DraftDocument: json.RawMessage(emptyRevDoc),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}
	hash := "hash-twice-1"
	writeArtifactDir(t, root, hash)
	artifactID := insertArtifactRow(t, db, page.ID, hash)
	if err := db.Exec("UPDATE pages SET staged_artifact_id = ? WHERE id = ?", artifactID, page.ID).Error; err != nil {
		t.Fatalf("挂暂存产物失败: %v", err)
	}
	linkActivePath(t, root, "/twice", hash)

	insertPendingReceipt(t, db, page.ID, "/twice", "zh-CN", artifactID)
	insertPendingReceipt(t, db, page.ID, "/twice", "zh-CN", artifactID)

	recovered, _, rerr := recoverPending(t, svc, ctx)
	if rerr != nil {
		t.Fatalf("恢复失败: %v", rerr)
	}
	if recovered < 1 {
		t.Fatalf("至少一条应被补齐，实际 recovered=%d", recovered)
	}
	if got := readActiveHash(t, db, page.ID, "zh-CN"); got != hash {
		t.Fatalf("账本应补齐为本次产物 %s，实际 %q", hash, got)
	}
}

func TestRecoveryDoesNotOverwriteNewerSuccess(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	page, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/superseded", DraftDocument: json.RawMessage(emptyRevDoc),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}
	oldHash, newerHash := "hash-sup-old", "hash-sup-new"
	writeArtifactDir(t, root, oldHash)
	writeArtifactDir(t, root, newerHash)
	oldID := insertArtifactRow(t, db, page.ID, oldHash)
	newerID := insertArtifactRow(t, db, page.ID, newerHash)

	if err := db.Exec(`
		INSERT INTO page_publications (page_id, lang, active_path, artifact_id, artifact_hash, published_at, update_time)
		VALUES (?, 'zh-CN', '/superseded', ?, ?, NOW(), NOW())`,
		page.ID, newerID, newerHash).Error; err != nil {
		t.Fatalf("准备账本失败: %v", err)
	}
	linkActivePath(t, root, "/superseded", newerHash)

	insertPendingReceipt(t, db, page.ID, "/superseded", "zh-CN", oldID)

	if _, _, rerr := recoverPending(t, svc, ctx); rerr != nil {
		t.Fatalf("恢复不应报错: %v", rerr)
	}
	if got := readActiveHash(t, db, page.ID, "zh-CN"); got != newerHash {
		t.Fatalf("账本必须保持更新的成功产物 %s，实际 %q", newerHash, got)
	}
	if got := activeLinkHash(t, "/superseded"); got != newerHash {
		t.Fatalf("访问面必须仍是更新产物，实际 %q", got)
	}
}

func TestRecoveryAbortsWhenLinkIsNotOurs(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	page, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/notours", DraftDocument: json.RawMessage(emptyRevDoc),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}
	hash := "hash-notours"
	writeArtifactDir(t, root, hash)
	artifactID := insertArtifactRow(t, db, page.ID, hash)

	insertPendingReceipt(t, db, page.ID, "/notours", "zh-CN", artifactID)

	if _, _, rerr := recoverPending(t, svc, ctx); rerr != nil {
		t.Fatalf("恢复不应报错: %v", rerr)
	}
	if got := readActiveHash(t, db, page.ID, "zh-CN"); got != "" {
		t.Fatalf("切换没发生时不得写账本，实际 %q", got)
	}
	if got := receiptState(t, db, "/notours"); got != "rolled_back" {
		t.Fatalf("回执应结案为 rolled_back，实际 %q", got)
	}
}

func TestConvergenceDoesNotAdvanceInputVersion(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	page, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/no-advance", DraftDocument: json.RawMessage(emptyRevDoc),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}
	hash := "hash-no-adv"
	writeArtifactDir(t, root, hash)
	artifactID := insertArtifactRow(t, db, page.ID, hash)
	if err := db.Exec("UPDATE pages SET staged_artifact_id = ? WHERE id = ?", artifactID, page.ID).Error; err != nil {
		t.Fatalf("挂暂存产物失败: %v", err)
	}
	linkActivePath(t, root, "/no-advance", hash)
	insertPendingReceipt(t, db, page.ID, "/no-advance", "zh-CN", artifactID)

	before := readInputVersionOf(t, db, page.ID)
	if _, _, rerr := recoverPending(t, svc, ctx); rerr != nil {
		t.Fatalf("恢复失败: %v", rerr)
	}
	if after := readInputVersionOf(t, db, page.ID); after != before {
		t.Fatalf("激活与收敛不得推进输入代次（%d → %d）", before, after)
	}
}

func TestConcurrentRecoveriesSerialiseOnPageRow(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	page, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/concurrent", DraftDocument: json.RawMessage(emptyRevDoc),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}
	hash := "hash-conc-1"
	writeArtifactDir(t, root, hash)
	artifactID := insertArtifactRow(t, db, page.ID, hash)
	if err := db.Exec("UPDATE pages SET staged_artifact_id = ? WHERE id = ?", artifactID, page.ID).Error; err != nil {
		t.Fatalf("挂暂存产物失败: %v", err)
	}
	linkActivePath(t, root, "/concurrent", hash)
	insertPendingReceipt(t, db, page.ID, "/concurrent", "zh-CN", artifactID)

	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, _, e := recoverPending(t, svc, ctx)
			done <- e
		}()
	}
	for i := 0; i < 2; i++ {
		if e := <-done; e != nil {
			t.Fatalf("并发收敛不应报错: %v", e)
		}
	}
	if got := readActiveHash(t, db, page.ID, "zh-CN"); got != hash {
		t.Fatalf("并发收敛后账本应为 %s，实际 %q", hash, got)
	}
	if got := activeLinkHash(t, "/concurrent"); got != hash {
		t.Fatalf("并发收敛后访问面应为 %s，实际 %q", hash, got)
	}
}

func TestPageMenuStalePortIsExposed(t *testing.T) {
	_, svc, _, _ := newPageService(t)

	port, ok := svc.(pagecontract.PageMenuStalePort)
	if !ok {
		t.Fatal("页面 Service 必须实现 pagecontract.PageMenuStalePort（navigation 同事务标记的入口）")
	}

	n, err := port.MarkMenuStaleTx(context.Background(), nil, "any-project", "", time.Time{})
	if err != nil {
		t.Fatalf("空菜单位置不应报错，实际 %v", err)
	}
	if n != 0 {
		t.Fatalf("空菜单位置命中数应为 0，实际 %d", n)
	}

	if _, perr := port.MarkMenuStaleTx(context.Background(), nil, "", "header", time.Time{}); !errors.Is(perr, pageservice.ErrProjectRequired) {
		t.Fatalf("空工程 id 应返回 ErrProjectRequired，实际 %v", perr)
	}
}

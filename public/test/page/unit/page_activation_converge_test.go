package unit

// page_activation_converge_test.go — 激活闸门与收敛的端到端语义（本 owner 的改动）。
//
// 覆盖需要真实文件系统 + 真实数据库的那几类场景：
//   · 同版本 attempt：同一份暂存产物被两次激活，第二次仍能收敛（内核不按 attempt 去重），
//     而收敛侧的「账本已被替代」判定把后来者挡在 DB 之外；
//   · 恢复撞上新成功 attempt：账本已换人 → 不覆盖；
//   · 激活不推进输入代次：只有输入变更才推进；
//   · 并发不同入口同实体：两路收敛并发时账本与访问面最终一致；
//   · 菜单依赖标记端口：契约形状固定（navigation 侧按它编译）。
//
// 断言对象是线上事实（符号链接指向）+ 数据库账本，而不是某个函数的返回值。

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

// readInputVersionOf 读当前输入代次。
func readInputVersionOf(t *testing.T, db *gorm.DB, pageID string) int64 {
	t.Helper()
	var v int64
	if err := db.Raw("SELECT input_version FROM pages WHERE id = ?", pageID).Scan(&v).Error; err != nil {
		t.Fatalf("读取代次失败: %v", err)
	}
	return v
}

// readActiveHash 读该语言账本上的产物 hash（无记录返回空串）。
func readActiveHash(t *testing.T, db *gorm.DB, pageID, lang string) string {
	t.Helper()
	var h string
	if err := db.Raw("SELECT COALESCE(artifact_hash, '') FROM page_publications WHERE page_id = ? AND lang = ?", pageID, lang).
		Scan(&h).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("读取账本失败: %v", err)
	}
	return h
}

// activeLinkHash 读访问面上某路径实际指向的产物 hash（读链接，不读库）。
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

// TestSameStagedArtifactCanBeActivatedTwice 同版本 attempt：同一份暂存产物被两次激活，
// 第二次仍然能补齐（而不是因为「已经有人试过」被拒）。
//
// 内核不认识「第几次尝试」，attempt 身份只用于收敛判定（账本归属），不用于阻止激活 ——
// 否则一次 FS 成功 / DB 失败的发布再也没法收敛。
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
	// 两条同路径回执：第一次 attempt 的 FS 已成功、DB 未写；第二次是重跑的同版本激活。
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

// TestRecoveryDoesNotOverwriteNewerSuccess 恢复撞上新成功 attempt：不覆盖更新的成功结果。
//
// 账本已被另一次成功激活写走，此时收敛必须放弃写入 —— 覆盖会把更新的正确状态
// 改成陈旧字节，这是最坏的一类回归。
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
		INSERT INTO page_publications (page_id, lang, active_path, artifact_id, artifact_hash, published_at, create_time, update_time)
		VALUES (?, 'zh-CN', '/superseded', ?, ?, NOW(), NOW(), NOW())`,
		page.ID, newerID, newerHash).Error; err != nil {
		t.Fatalf("准备账本失败: %v", err)
	}
	linkActivePath(t, root, "/superseded", newerHash)
	// 回执声称自己切了旧产物（伪造「晚到的旧 attempt」）。
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

// TestRecoveryAbortsWhenLinkIsNotOurs 链接不是本次产物：本次激活从未生效，只结案不改状态。
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
	// 访问面完全没链接（切换没发生）。
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

// TestConvergenceDoesNotAdvanceInputVersion 收敛/激活不推进输入代次。
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

// TestConcurrentRecoveriesSerialiseOnPageRow 并发不同入口同实体：两路收敛同时跑，
// 最终的账本与访问面一致（行锁把判定串起来）。
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

// TestPageMenuStalePortIsExposed 菜单依赖标记端口的契约形状（navigation 按它编译）。
//
// 行为由 model 层用例覆盖（page_input_version_model_test.go 的 MarkMenuStaleTx）；
// 这里只钉住「这一条签名存在且唯一入口」，签名一变就是跨模块契约破坏。
func TestPageMenuStalePortIsExposed(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	// 装配层（routers/assembly_publish.go）对 pageService 做同一断言，不通过就 panic：
	// 这里用真实 Service 钉住「实现了」，否则那条 panic 只会在部署时炸。
	port, ok := svc.(pagecontract.PageMenuStalePort)
	if !ok {
		t.Fatal("页面 Service 必须实现 pagecontract.PageMenuStalePort（navigation 同事务标记的入口）")
	}
	// 行为：空 kind 不触库、返回 0 且无错误（与 model 层语义一致，见 MarkMenuStaleTx）。
	n, err := port.MarkMenuStaleTx(context.Background(), nil, "any-project", "", time.Time{})
	if err != nil {
		t.Fatalf("空菜单位置不应报错，实际 %v", err)
	}
	if n != 0 {
		t.Fatalf("空菜单位置命中数应为 0，实际 %d", n)
	}
	// 空工程 id 是调用方缺陷（pages 带 FORCE 策略，不设作用域会静默 0 行），必须报错。
	if _, perr := port.MarkMenuStaleTx(context.Background(), nil, "", "header", time.Time{}); !errors.Is(perr, pageservice.ErrProjectRequired) {
		t.Fatalf("空工程 id 应返回 ErrProjectRequired，实际 %v", perr)
	}
}

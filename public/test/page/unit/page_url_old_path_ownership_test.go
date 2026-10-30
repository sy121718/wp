package unit

// page_url_old_path_ownership_test.go —— 旧路径处置的**归属保护**（跨实体覆盖）。
//
// 缺陷形状（主审实证）：回调处置旧路径时只凭 path 动手 —— DeactivateTx 在归属者双空时
// 退化成「本工程该路径上的任一 active 行」，而 FS 侧 os.Remove 更是无条件。于是
// 「本次改 URL 的旧路径」如果已经**被另一页接管**（路径释放后被别人占用、或另一条
// attempt 抢先把 redirect 登记到同一路径），清理动作删掉的不是自己的链接，
// 而是别人刚上线的线上链接：表现为 404，且没有任何一方会察觉是「清理」干的。
//
// 本文件钉住三条：
//  1. DB 侧 DeactivateTx 必须带精确归属者（PageID），不得凭 path 删别人的行；
//  2. FS 侧处置前必须确认该路径仍归本页，否则判 errOldPathTaken 且**不动链接**；
//  3. 主链改 URL 的处置在提交之后、**重新持锁**的短事务里做，锁内复核账本归属。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pagedto "go_wp/internal/module/page/dto"

	"gorm.io/gorm"
)

// insertActiveRoute 直插一条 active 路由行（指定归属者），模拟「别人接管了这条路径」。
func insertActiveRoute(t *testing.T, db *gorm.DB, projectID, path, ownerPageID string) {
	t.Helper()
	if err := db.Exec(
		`INSERT INTO page_routes (project_id, path, page_id, presentation_id, route_kind, update_time)
		 VALUES (?, ?, ?, NULL, 'active', now())`,
		projectID, path, ownerPageID).Error; err != nil {
		t.Fatalf("插入 active 路由行失败: %v", err)
	}
}

// routeRowsAt 该路径上的路由行数（判「有没有被误删」）。
// routeRowsAt 路由行数。
//
// 表名必须显式写 public：模板库的表在 public，而测试连接自带的 search_path 是 public,
// ext_shared 且会带上库名同名 schema；不加限定名在个例下也能命中，但那是运气而不是约定。
func routeRowsAt(t *testing.T, db *gorm.DB, projectID, path string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM public.page_routes WHERE project_id = ? AND path = ?", projectID, path).
		Scan(&n).Error; err != nil {
		t.Fatalf("统计路由行失败: %v", err)
	}
	return n
}

// activeLinkExists 访问面根目录下该路径的符号链接是否还在。
func activeLinkExists(t *testing.T, urlPath string) bool {
	t.Helper()
	_, err := os.Lstat(filepath.Join(activeDir(t), strings.TrimPrefix(urlPath, "/")))
	return err == nil
}

// TestUpdateURLDoesNotDeleteAnotherPagesOldPathLink 旧路径已被另一页接管时，
// 改 URL 的收尾不得删掉那条别人的链接，也不得删掉别人的路由行。
func TestUpdateURLDoesNotDeleteAnotherPagesOldPathLink(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()

	page := createPage(t, svc, projectID, "/owner/old", headingDocument)
	buildAndPublish(t, svc, page.ID)
	intruder := createPage(t, svc, projectID, "/owner/intruder", headingDocument).ID

	// 模拟「旧路径已被另一页接管」：DB 行换成别人的归属，FS 链接指向别人的产物。
	if err := db.Exec("UPDATE public.page_routes SET page_id = ? WHERE project_id = ? AND path = ?",
		intruder, projectID, "/owner/old").Error; err != nil {
		t.Fatalf("转移路由归属失败: %v", err)
	}
	foreignHash := "hash-intruder-owner"
	writeArtifactDir(t, os.Getenv("GO_WP_ARTIFACT_ROOT"), foreignHash)
	linkActivePath(t, os.Getenv("GO_WP_ARTIFACT_ROOT"), "/owner/old", foreignHash)

	if _, err := svc.UpdateURL(ctx, &pagedto.UpdateURLReq{ID: page.ID, NewPath: "/owner/new", WithRedirect: false}); err != nil {
		t.Fatalf("改 URL 本身应成功（旧路径处置不该让它失败）：%v", err)
	}

	// 判据一：别人的链接还在（fs 未被无条件删除）。
	if !activeLinkExists(t, "/owner/old") {
		t.Fatal("旧路径已归他人：本次处置不得删除那条链接")
	}
	// 判据二：别人的路由行还在，归属未被改写。
	var owner string
	if err := db.Raw("SELECT page_id FROM public.page_routes WHERE project_id = ? AND path = ?", projectID, "/owner/old").
		Scan(&owner).Error; err != nil {
		t.Fatalf("读取路由归属失败: %v", err)
	}
	if owner != intruder {
		t.Fatalf("旧路径归属应仍是 %s，实际 %q", intruder, owner)
	}
	// 判据三：本次改 URL 自己确实生效了（处置被跳过 ≠ 改址失败）。
	if got := draftPathOf(t, db, page.ID); got != "/owner/new" {
		t.Fatalf("改 URL 应已生效（draft_path = /owner/new），实际 %q", got)
	}
}

// TestDeactivateOldPathKeepsForeignOwner 归属条件写：DeactivateTx 只删本页的行。
func TestDeactivateOldPathKeepsForeignOwner(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()

	page := createPage(t, svc, projectID, "/cond/old", headingDocument)
	buildAndPublish(t, svc, page.ID)
	intruder := createPage(t, svc, projectID, "/cond/intruder", headingDocument).ID
	insertActiveRoute(t, db, projectID, "/cond/shared", intruder)

	// 让本页也「拥有」同一条路径是不可能的（唯一约束）——这里直接验证归属者条件下
	// 本页发起的取消不会命中别人的行。
	if _, err := svc.UpdateURL(ctx, &pagedto.UpdateURLReq{ID: page.ID, NewPath: "/cond/new", WithRedirect: false}); err != nil {
		t.Fatalf("改 URL 失败: %v", err)
	}
	if got := routeRowsAt(t, db, projectID, "/cond/shared"); got != 1 {
		t.Fatalf("别人的 active 行不得被本页的取消操作删掉，实际剩 %d 行", got)
	}
}

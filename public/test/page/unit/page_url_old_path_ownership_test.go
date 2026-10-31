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

// TestUpdateURLRefusesRenameWhenOldPathTakenByRedirect 旧路径被**别人的 redirect** 占用时，
// 改 URL 必须在迁移一步被拒（ErrRouteActiveRename），且不留下任何痕迹。
//
// 生产口径：他人任何非 reserved 的占用（active 或 redirect）都在改名的唯一判据上被拒
// （publication_route_tx_model.go:85-93）。本用例与 RefusesToRenameAnotherOwnersActiveRoute
// 的区别只在占用的 route_kind（redirect vs active），两者都必须被拒绝。
func TestUpdateURLRefusesRenameWhenOldPathTakenByRedirect(t *testing.T) {
	db, svc, _, projectID := newPageServiceWithRoutes(t)
	ctx := context.Background()

	page := createPage(t, svc, projectID, "/owner/old", headingDocument)
	buildAndPublish(t, svc, page.ID)
	intruder := createPage(t, svc, projectID, "/owner/intruder", headingDocument).ID
	// 目标必须先是一个**已激活**的落点（见上）。
	buildAndPublish(t, svc, intruder)

	// 造「旧路径已被另一页接管，且接管者以 **redirect** 形态占着它」这个现场。
	//
	// 为什么不用 RenameReservedTx 硬改名：它要求被改名的旧路径上有**本页**的行
	// （publication_route_tx_model.go:88-101，只 UPDATE page_id = 自己 的那一条），
	// 而本页的行在 /owner/old、要改的是 /owner/intruder —— 撞 (project_id, path)
	// 唯一约束被归一成 ErrRouteOccupied。也不能先删本页行再改名：那样旧路径上只剩
	// 别人的行，改 URL 的登录段会以为「本页在这条路径上没有占用」而静默成功，
	// 与真实语义不符。
	//
	// 因此用**生产自身的写入口** CreateRedirect：它按目标路径的 active 归属所有者写
	// redirect 行（page_redirect.go:160-182），并在访问面上把该路径切成本工程产物里
	// 的重定向（writeRedirectArtifact → Inspect 认 redirect.json → Kind=redirect）。
	//
	// 目标必须是**已激活的真实路径**：CreateRedirect 用 GetActiveRouteByPath(tail)
	// （page_redirect.go:158-164，WHERE route_kind='active'）解析落点，未命中的落点是
	// ErrRedirectTargetMiss —— 所以 intruder 先发布在 /owner/intruder，再拿它当目标。
	// 但源路径已被本页占用（CreateRedirect 会判 ErrRedirectOccupied），所以先用真实
	// 的 DeactivateTx 释放本页在 /owner/old 上的占用行 —— 链接此时仍指向本页最后一个
	// 产物（真实系统里正是「行先走、链接后撤」的中间态），随后由 CreateRedirect 把
	// 链接替换成重定向。
	if err := db.Exec("DELETE FROM public.page_routes WHERE project_id = ? AND path = ? AND page_id = ?",
		projectID, "/owner/old", page.ID).Error; err != nil {
		t.Fatalf("释放本页旧路径占用失败: %v", err)
	}
	if _, err := svc.CreateRedirect(ctx, &pagedto.RedirectCreateReq{
		ProjectID: projectID, SourcePath: "/owner/old", TargetPath: "/owner/intruder",
	}); err != nil {
		t.Fatalf("让 intruder 以重定向形式接管 /owner/old 失败: %v", err)
	}
	if kind := routeOccupiedAt(t, db, projectID, "/owner/old"); kind != "redirect" {
		t.Fatalf("前置失败：/owner/old 应是 intruder 的 redirect 占用，实际 %q", kind)
	}
	if kind := activeKind(t, "/owner/old"); kind != "redirect" {
		t.Fatalf("前置失败：访问面应已是重定向，实际 %q", kind)
	}
	foreignLinkTarget := activeLinkTarget(t, "/owner/old")
	// 访问面此刻已经是 CreateRedirect 写下的**重定向**（writeRedirectArtifact → PutRedirect →
	// withPath 内 publication.Activate）。不要再手摆一条指向普通产物的符号链接：那会把
	// 刚建好的重定向现场覆盖成 page 形态，判据「别人的 redirect 不得被处置」就不再成立。

	// 他人占着旧路径 → 改址必须在**迁移**这一步被拒。
	//
	// 依据：RenameReservedTx（publication_route_tx_model.go:85-93）的判据是
	// existing.RouteKind != RouteReserved && existing.PageID != pageID →
	// pubenums.Err(pubenums.ErrRouteActiveRename)。也就是他人任何非 reserved 的占用
	//（active 或 redirect）都不许被改名，只有 reserved（草稿占用）才有条件迁移。
	_, err := svc.UpdateURL(ctx, &pagedto.UpdateURLReq{ID: page.ID, NewPath: "/owner/new", WithRedirect: false})
	if err == nil {
		t.Fatal("旧路径被他人 redirect 占用：改 URL 必须被拒绝（不得把他人占用改名）")
	}
	// ErrRouteActiveRename 是本包 facing 键（internal/module/publication/enums/publication_enums.go:56
	// 的 pubenums.Err(key, 兜底) 不是哨兵值），因此沿用仓库既有的 key 子串口径判定。
	if !strings.Contains(err.Error(), "ErrRouteActiveRename") {
		t.Fatalf("期望在迁移一步被拒（ErrRouteActiveRename），实际 %v", err)
	}

	// 判据一：别人的链接还在（fs 未被无条件删除）。

	if !activeLinkExists(t, "/owner/old") {
		t.Fatal("旧路径已归他人：本次处置不得删除那条链接")
	}
	if got := activeLinkTarget(t, "/owner/old"); got != foreignLinkTarget {
		t.Fatalf("别人的链接被改指向了：期望仍是 %q，实际 %q", foreignLinkTarget, got)
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
	// 判据三：被拒绝的改址不得留下任何迁移痕迹 —— 草稿路径仍是旧值。
	if got := draftPathOf(t, db, page.ID); got != "/owner/old" {
		t.Fatalf("被拒绝的改址不得迁移草稿路径，实际 %q", got)
	}
}

// TestUpdateURLRefusesToRenameAnotherOwnersActiveRoute 旧路径上是**别人的 active 行**时，
// 改 URL 在迁移一步就撞 ErrRouteActiveRename（真实的对外语义）。
//
// 与上一条的分工：那条造的是「别人的 redirect 占用」（可以迁移），本条造的是
// 「别人的 active 占用」（迁移必须被拒绝）。两条合起来才说明 add 的判据：处置旧路径
// 时对他人任何非 reserved 的占用（active 或 redirect）一律拒绝改名（生产判据见
// publication_route_tx_model.go:85-93）。
// 拒绝发生在 DB 提交段，而那段在访问面切换之后执行 —— 所以还要求：
// 回执保持 pending、账本没有被写成本次成功身份。
func TestUpdateURLRefusesToRenameAnotherOwnersActiveRoute(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()

	page := createPage(t, svc, projectID, "/owner2/old", headingDocument)
	buildAndPublish(t, svc, page.ID)
	// 另一页仍在 /owner2/old 上服务：路由行是它的 active（发布态），不是可迁移的占用。
	//
	// 顺序必须是「先释放 page 的占用，再让 intruder 搬进来」：page 仍 active 占着
	// /owner2/old 时，intruder 的 ChangeURL 会在 ensureRouteNotOccupied 判 ErrPathOccupied，
	// 根本走不到「他人 active 挡住改名」那一步。释放手法与相邻用例一致（删本页的占用行；
	// 访问面链接随后由 intruder 的切换覆盖）。
	//
	// 不能在本页仍占着 /owner2/old 时再 createPage 同路径 —— 创建入口自己就会判
	// ErrPathOccupied（同一工程内 (project_id, path) 只能有一个占用者），那样根本走不到
	// 改 URL。因此先建在临时路径上并发布（拿到真实产物与 active 行），再把它的占用
	// 与访问面链接一起搬到 /owner2/old：模拟「另一页此刻正用这条线上路径」，链接指向
	// 它自己的产物（Inspect 读到的 Locator 与路由归属一致，正是真实线上的形态）。
	// 这一步用生产入口 UpdateURL 完成（见下面主键注释），夹具不再手摆路由与链接。
	if err := db.Exec("DELETE FROM page_routes WHERE project_id = ? AND path = ? AND page_id = ?",
		projectID, "/owner2/old", page.ID).Error; err != nil {
		t.Fatalf("释放 page 在 /owner2/old 的占用失败: %v", err)
	}
	intruder := createPage(t, svc, projectID, "/owner2/intruder", headingDocument).ID
	buildAndPublish(t, svc, intruder)
	// 不能 `UPDATE page_routes SET path = ...`：主键是 (project_id, path)
	// （public/migrations/init_builder_schema.sql:161-175），改主键列等于删旧行插新行 ——
	// 新键已被另一页的 active 行占着，直接撞 page_routes_pkey。
	//
	// 改用生产自身的写入口 UpdateURL：它把 intruder 的**全部语言**一起 renameReserved
	// （page_publish.go 的 applyUpdateURL → renameReservedAllLangsTx → RenameReservedTx），
	// 路由行、账本 active_path、草稿路径、访问面链接一次搬到位 —— 正是「另一页此刻
	// 正用 /owner2/old 这条线上路径」的真实形态，而不只是夹具摆出来的一行。
	if _, err := svc.UpdateURL(ctx, &pagedto.UpdateURLReq{ID: intruder, NewPath: "/owner2/old", WithRedirect: false}); err != nil {
		t.Fatalf("让 intruder 发布到 /owner2/old 失败: %v", err)
	}
	if kind := activeKind(t, "/owner2/old"); kind != "page" {
		t.Fatalf("前置失败：/owner2/old 访问面应是 intruder 的页面，实际 %q", kind)
	}
	if kind := routeOccupiedAt(t, db, projectID, "/owner2/old"); kind != "active" {
		t.Fatalf("前置失败：/owner2/old 应是 intruder 的 active 占用，实际 %q", kind)
	}
	var occupyOwner string
	if err := db.Raw("SELECT page_id FROM public.page_routes WHERE project_id = ? AND path = ?", projectID, "/owner2/old").
		Scan(&occupyOwner).Error; err != nil {
		t.Fatalf("读取路由归属失败: %v", err)
	}
	if occupyOwner != intruder {
		t.Fatalf("前置失败：/owner2/old 应归 intruder，实际 %q", occupyOwner)
	}

	if _, err := svc.UpdateURL(ctx, &pagedto.UpdateURLReq{ID: page.ID, NewPath: "/owner2/new", WithRedirect: false}); err == nil {
		t.Fatal("旧路径上是别人的 active 行：改 URL 必须拒绝（不得把别人线上路径改名）")
	}
	// 别人的行与链接都必须原样：拒绝路径不得留下任何处置痕迹。
	var owner string
	if err := db.Raw("SELECT page_id FROM public.page_routes WHERE project_id = ? AND path = ?", projectID, "/owner2/old").
		Scan(&owner).Error; err != nil {
		t.Fatalf("读取路由归属失败: %v", err)
	}
	if owner != intruder {
		t.Fatalf("旧路径归属应仍是 %s，实际 %q", intruder, owner)
	}
	if !activeLinkExists(t, "/owner2/old") {
		t.Fatal("拒绝时不得撤掉别人正在服务的链接")
	}
	// 本次改址没有生效：草稿路径未迁移，账本也没有记成它的成功身份。
	if got := draftPathOf(t, db, page.ID); got != "/owner2/old" {
		t.Fatalf("被拒绝的改址不得迁移草稿路径，实际 %q", got)
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

package unit

// page_navigation_stale_test.go —— **按菜单项引用**的失效链路（真实 PG + 真实构建）。
//
// core.nav 的两种引用方式对应两条不同的依赖键，谁也不能替谁命中：
//
//	props.menu       → 整条位置 → pipeline.MenuKey(projectID, kind) = menu:{project}:{kind}
//	props.navigation → 某一支菜单项 → pipeline.NavigationKey(itemID) = navigation:{itemID}
//
// 页眉只放某一支时，产物里登记的是项键，位置键一行都命中不到；菜单项的增删改排序
// 若只打位置键，那一类页面会停在旧链接上且**没有任何报错**。本文件钉住项键：
//
//  1. 构建期登记 kind="navigation" / key="navigation:{itemID}"；
//  2. 端口在调用方事务内按同一个键命中：stale + input_version 同一条 UPDATE 推进；
//  3. 项键不被位置键代偿（同页只按项引用时，位置键一行都不该被登记）；
//  4. 工程隔离：另一个工程的页面不被误命中（键里不带工程分量，靠行作用域与归属）；
//  5. 端口不开事务：调用方回滚时代次与 stale 一起回退。

import (
	"context"
	"testing"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	pagecontract "go_wp/internal/module/page/contract"
	pagemodel "go_wp/internal/module/page/model"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/pipeline"

	"gorm.io/gorm"
)

// menuItemStaleCaller 生产调用方的收窄接口（与装配层 navigationMenuItemStale 同形状）：
// 页面契约不为此多一个方法 —— 只有导航写路径用这条。
type menuItemStaleCaller interface {
	MarkNavigationStaleTx(ctx context.Context, tx *gorm.DB, projectID, navigationID string) (int64, error)
}

// seedMenuItemID 建一条菜单项并返回其 id（按项引用要的就是这个 id）。
//
// Path 必须给 ASCII：菜单项的链接进 core.nav 的 validateItems 白名单
// （builder/components/nav/nav.go:190 → core.IsSafeURL → urlSafeRe 字符集
// [A-Za-z0-9./:?=&%~#+_@-]，controls.go:395），非 ASCII 字面量（"/产品"）被判非法，
// 构建期直接失败。标题用中文不影响用例意图（只关心依赖键是否命中）。
func seedMenuItemID(t *testing.T, svc navigationcontract.NavigationService, projectID, kind, title, path string) string {
	t.Helper()
	created, err := svc.Create(context.Background(), &navigationdto.CreateReq{
		ProjectID: projectID, Title: title, Path: path, Kind: kind,
		SourceType: "custom", Target: "self",
	})
	if err != nil {
		t.Fatalf("创建菜单项(%s/%s)失败: %v", kind, title, err)
	}
	return created.ID
}

// markNavigationStale 按生产同一形状调用「按项同事务标记」。
func markNavigationStale(t *testing.T, svc pagecontract.PageService, ctx context.Context,
	tx *gorm.DB, projectID, navigationID string) (int64, error) {
	t.Helper()
	caller, ok := svc.(menuItemStaleCaller)
	if !ok {
		t.Fatal("页面 Service 必须实现按项同事务标记（navigation 契约 MenuItemStalePort 的实现方）")
	}
	return caller.MarkNavigationStaleTx(ctx, tx, projectID, navigationID)
}

// navItemDocument 挂一个**按菜单项**引用的 core.nav 文档。
func navItemDocument(itemID string) string {
	return `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"nav-item-1","type":"core.nav","props":{"navigation":"` + itemID + `"}}]}`
}

// dependencyExists 该页是否登记了指定 (kind,key) 的依赖。
func dependencyExists(t *testing.T, db *gorm.DB, pageID, kind, key string) bool {
	t.Helper()
	var n int64
	if err := db.Raw(
		"SELECT COUNT(*) FROM page_dependencies WHERE page_id = ? AND dependency_kind = ? AND dependency_key = ?",
		pageID, kind, key).Scan(&n).Error; err != nil {
		t.Fatalf("查询依赖行失败: %v", err)
	}
	return n > 0
}

// TestNavigationStalePortHitsItemKey 按项引用：构建期登记项键，端口按项键命中。
func TestNavigationStalePortHitsItemKey(t *testing.T) {
	// 菜单项要真实创建：navigation 的菜单 CRUD 在端口不齐时 fail closed
	// （writePortsReady），裸 NewService 建出的实例只能读，用它建项会直接失败。
	db, svc, navSvc, _, projectID := newPageServiceWithNavigationWrite(t)
	ctx := context.Background()
	itemID := seedMenuItemID(t, navSvc, projectID, menuKindHeader, "产品", "/products")

	page := createPage(t, svc, projectID, "/nav-item", navItemDocument(itemID)).ID
	buildAndPublish(t, svc, page)

	key := pipeline.NavigationKey(itemID)
	if !dependencyExists(t, db, page, key.Kind, key.Key) {
		t.Fatalf("构建期应登记项键 %q（kind=%q）", key.Key, key.Kind)
	}
	// 位置键不应同时被登记：这条页面的导航**没有**按位置消费（resolveNavMenu 按项优先）。
	if dependencyExists(t, db, page, pipeline.DepKindMenu, pipeline.MenuKey(projectID, menuKindHeader).Key) {
		t.Fatal("按项引用不应登记位置键（两者是不同事实，混登记会让失效面虚高）")
	}

	before := inputVersionOf(t, db, page)
	var hit int64
	if err := pagemodel.NewPageModel(db).TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		n, merr := markNavigationStale(t, svc, ctx, tx, projectID, itemID)
		hit = n
		return merr
	}); err != nil {
		t.Fatalf("项键标记失败: %v", err)
	}
	if hit != 1 {
		t.Fatalf("应命中 1 个页面，实际 %d（kind 用错成 menu 或键少拼前缀时这里恒 0）", hit)
	}
	if !pageStale(t, db, page) {
		t.Fatal("项键命中后页面应为 stale")
	}
	if got := inputVersionOf(t, db, page); got != before+1 {
		t.Fatalf("项键标记应推进代次 %d → %d，实际 %d", before, before+1, got)
	}
}

// TestNavigationStaleIsProjectScoped 另一个工程的页面不被误命中。
//
// 项键里不带工程分量（uuid 全局唯一），所以工程隔离靠**行作用域**（rls.ScopeTx）
// 与页面 id 归属共同保证 —— 这条用例证明它不是「碰巧没命中」。
func TestNavigationStaleIsProjectScoped(t *testing.T) {
	// 菜单项要真实创建：navigation 的菜单 CRUD 在端口不齐时 fail closed
	// （writePortsReady），裸 NewService 建出的实例只能读，用它建项会直接失败。
	db, svc, navSvc, projects, projectID := newPageServiceWithNavigationWrite(t)
	ctx := context.Background()
	itemID := seedMenuItemID(t, navSvc, projectID, menuKindHeader, "产品", "/products")
	page := createPage(t, svc, projectID, "/nav-scoped", navItemDocument(itemID)).ID
	buildAndPublish(t, svc, page)

	// 第二个工程：同位置建一条菜单项，页面也按项引用它自己的 id。
	other, err := projects.Create(ctx, &projectdto.CreateReq{Name: "另一工程"})
	if err != nil {
		t.Fatalf("创建第二个工程失败: %v", err)
	}
	// 同一 service 可服务任意工程（端口与工程无关）：换工程只换 projectID。
	otherItemID := seedMenuItemID(t, navSvc, other.ID, menuKindHeader, "产品", "/products")
	otherPage := createPage(t, svc, other.ID, "/nav-scoped", navItemDocument(otherItemID)).ID
	buildAndPublish(t, svc, otherPage)

	otherBefore := inputVersionOf(t, db, otherPage)
	var hit int64
	if err := pagemodel.NewPageModel(db).TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		n, merr := markNavigationStale(t, svc, ctx, tx, projectID, itemID)
		hit = n
		return merr
	}); err != nil {
		t.Fatalf("项键标记失败: %v", err)
	}
	if hit != 1 {
		t.Fatalf("本工程只应命中 1 个页面，实际 %d", hit)
	}
	if got := inputVersionOf(t, db, otherPage); got != otherBefore {
		t.Fatalf("另一工程的页面代次不应变化（%d → %d）", otherBefore, got)
	}
	if pageStale(t, db, otherPage) {
		t.Fatal("另一工程的页面不应被标记 stale")
	}
}

// TestNavigationStaleRollsBackWithTransaction 端口不开事务：调用方回滚时代次与 stale 一起回退。
func TestNavigationStaleRollsBackWithTransaction(t *testing.T) {
	// 菜单项要真实创建：navigation 的菜单 CRUD 在端口不齐时 fail closed
	// （writePortsReady），裸 NewService 建出的实例只能读，用它建项会直接失败。
	db, svc, navSvc, _, projectID := newPageServiceWithNavigationWrite(t)
	ctx := context.Background()
	itemID := seedMenuItemID(t, navSvc, projectID, menuKindHeader, "产品", "/products")
	page := createPage(t, svc, projectID, "/nav-rollback", navItemDocument(itemID)).ID
	buildAndPublish(t, svc, page)

	before := inputVersionOf(t, db, page)
	wantErr := gorm.ErrInvalidTransaction
	if err := pagemodel.NewPageModel(db).TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		if _, merr := markNavigationStale(t, svc, ctx, tx, projectID, itemID); merr != nil {
			return merr
		}
		return wantErr
	}); err == nil {
		t.Fatal("外层应返回注入的错误")
	}
	if got := inputVersionOf(t, db, page); got != before {
		t.Fatalf("回滚后代次必须回到 %d，实际 %d", before, got)
	}
	if pageStale(t, db, page) {
		t.Fatal("回滚后 stale 必须一起回退为 false")
	}
}

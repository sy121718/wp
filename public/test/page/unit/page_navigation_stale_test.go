package unit

import (
	"context"
	publish "go_wp/pkg/publish"
	"testing"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	pagecontract "go_wp/internal/module/page/contract"
	pagemodel "go_wp/internal/module/page/model"
	projectdto "go_wp/internal/module/project/dto"

	"gorm.io/gorm"
)

type menuItemStaleCaller interface {
	MarkNavigationStaleTx(ctx context.Context, tx *gorm.DB, projectID, navigationID string) (int64, error)
}

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

func markNavigationStale(t *testing.T, svc pagecontract.PageService, ctx context.Context,
	tx *gorm.DB, projectID, navigationID string) (int64, error) {
	t.Helper()
	caller, ok := svc.(menuItemStaleCaller)
	if !ok {
		t.Fatal("页面 Service 必须实现按项同事务标记（navigation 契约 MenuItemStalePort 的实现方）")
	}
	return caller.MarkNavigationStaleTx(ctx, tx, projectID, navigationID)
}

func navItemDocument(itemID string) string {
	return `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"nav-item-1","type":"core.nav","props":{"navigation":"` + itemID + `"}}]}`
}

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

func TestNavigationStalePortHitsItemKey(t *testing.T) {

	db, svc, navSvc, _, projectID := newPageServiceWithNavigationWrite(t)
	ctx := context.Background()
	itemID := seedMenuItemID(t, navSvc, projectID, menuKindHeader, "产品", "/products")

	page := createPage(t, svc, projectID, "/nav-item", navItemDocument(itemID)).ID
	buildAndPublish(t, svc, page)

	key := publish.NavigationKey(itemID)
	if !dependencyExists(t, db, page, key.Kind, key.Key) {
		t.Fatalf("构建期应登记项键 %q（kind=%q）", key.Key, key.Kind)
	}

	if dependencyExists(t, db, page, publish.DepKindMenu, publish.MenuKey(projectID, menuKindHeader).Key) {
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

func TestNavigationStaleIsProjectScoped(t *testing.T) {

	db, svc, navSvc, projects, projectID := newPageServiceWithNavigationWrite(t)
	ctx := context.Background()
	itemID := seedMenuItemID(t, navSvc, projectID, menuKindHeader, "产品", "/products")
	page := createPage(t, svc, projectID, "/nav-scoped", navItemDocument(itemID)).ID
	buildAndPublish(t, svc, page)

	other, err := projects.Create(ctx, &projectdto.CreateReq{Name: "另一工程"})
	if err != nil {
		t.Fatalf("创建第二个工程失败: %v", err)
	}

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

func TestNavigationStaleRollsBackWithTransaction(t *testing.T) {

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

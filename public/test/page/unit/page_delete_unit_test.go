package unit

import (
	"context"
	"testing"
	"time"

	pageenums "go_wp/internal/module/page/enums"
	pagedto "go_wp/internal/module/page/dto"
	pubmodel "go_wp/internal/module/publication/model"
)

// ---- 删除 ----

// TestPageDeleteNotFound nil / 空 ID 删除请求属于参数错误；
// 不存在的页面（合法 ID）才返回页面不存在。
func TestPageDeleteNotFound(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	ctx := context.Background()
	if err := svc.Delete(ctx, nil); err == nil || err.Error() != pageenums.ErrInvalidParam {
		t.Errorf("nil 请求应返回 %q: %v", pageenums.ErrInvalidParam, err)
	}
	if err := svc.Delete(ctx, &pagedto.DeleteReq{}); err == nil || err.Error() != pageenums.ErrInvalidParam {
		t.Errorf("空 ID 应返回 %q: %v", pageenums.ErrInvalidParam, err)
	}
	if err := svc.Delete(ctx, &pagedto.DeleteReq{ID: "6f2c9d0e-1a2b-3c4d-8e9f-0a1b2c3d4e5f"}); err == nil || err.Error() != pageenums.ErrPageNotFound {
		t.Errorf("不存在页面应返回 %q: %v", pageenums.ErrPageNotFound, err)
	}
}

// TestPageDeleteReleasedReservedRoute 软删后：列表/详情不可见、reserved 路由占用
// 释放、行保留审计留痕、同路径可被新页面重新创建。
func TestPageDeleteReleasedReservedRoute(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/del-a", pageDocument)
	if kind := routeKind(t, db, projectID, "/del-a"); kind != pubmodel.RouteReserved {
		t.Fatalf("创建后应为 reserved: %s", kind)
	}

	if err := svc.Delete(ctx, &pagedto.DeleteReq{ID: created.ID}); err != nil {
		t.Fatalf("删除失败: %v", err)
	}

	// 1) 列表不可见。
	list, err := svc.List(ctx, "")
	if err != nil {
		t.Fatalf("列表查询失败: %v", err)
	}
	for _, p := range list {
		if p.ID == created.ID {
			t.Errorf("软删页面不应出现在列表: %+v", p)
		}
	}
	// 2) 详情不可见。
	if _, err := svc.Detail(ctx, &pagedto.DetailReq{ID: created.ID}); err == nil || err.Error() != pageenums.ErrPageNotFound {
		t.Errorf("软删后详情应不可见: %v", err)
	}
	// 3) 路由占用释放（修复前软删后 routeCount==1 残留、路径永久占用）。
	var routeCount int64
	if err := db.Table("page_routes").Where("project_id = ? AND path = ?", projectID, "/del-a").Count(&routeCount).Error; err != nil {
		t.Fatalf("查询路由失败: %v", err)
	}
	if routeCount != 0 {
		t.Errorf("软删后路由占用应释放: %d 行残留", routeCount)
	}
	// 4) 行保留（软删审计留痕）：行总数仍为 1，但无未删除行。
	var total, alive int64
	if err := db.Table("pages").Where("id = ?", created.ID).Count(&total).Error; err != nil {
		t.Fatalf("查询页面失败: %v", err)
	}
	if err := db.Table("pages").Where("id = ? AND deleted_at IS NULL", created.ID).Count(&alive).Error; err != nil {
		t.Fatalf("查询页面失败: %v", err)
	}
	if total != 1 || alive != 0 {
		t.Errorf("软删应保留行（total=%d）且无未删除行（alive=%d）", total, alive)
	}
	// 5) 同路径可重新创建。
	recreated := createPage(t, svc, projectID, "/del-a", pageDocument)
	if recreated.ID == created.ID {
		t.Errorf("新页面应为新 ID")
	}
}

// TestPageDeleteReleasedActiveAndRedirectRoutes 已发布（active）并注册过 301
// （redirect）的页面删除后，active 与 redirect 路由占用全部释放，同路径可被
// 新页面占用。
//
// 说明：路由状态用 SQL 直接构造（reserved→active 模拟发布、插入 redirect 行模拟
// 改 URL 注册的 301），不依赖 UpdateURL 链路，保持 Delete 用例与 publication 模块
// 的演进解耦。
func TestPageDeleteReleasedActiveAndRedirectRoutes(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/del-old", pageDocument)

	// 模拟发布：reserved → active。
	if err := db.Table("page_routes").
		Where("project_id = ? AND path = ?", projectID, "/del-old").
		Update("route_kind", pubmodel.RouteActive).Error; err != nil {
		t.Fatalf("模拟发布失败: %v", err)
	}
	// 模拟改 URL 注册的 301：旧路径 redirect 行。
	redirectPath := "/del-old-301"
	if err := db.Table("page_routes").Create(map[string]any{
		"project_id": projectID, "path": redirectPath,
		"page_id": created.ID, "route_kind": pubmodel.RouteRedirect, "updated_at": time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("模拟 301 注册失败: %v", err)
	}
	if kind := routeKind(t, db, projectID, "/del-old"); kind != pubmodel.RouteActive {
		t.Fatalf("模拟发布失败，应为 active: %s", kind)
	}
	if kind := routeKind(t, db, projectID, redirectPath); kind != pubmodel.RouteRedirect {
		t.Fatalf("模拟 301 失败，应为 redirect: %s", kind)
	}

	if err := svc.Delete(ctx, &pagedto.DeleteReq{ID: created.ID}); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	var routeCount int64
	if err := db.Table("page_routes").Where("project_id = ? AND page_id = ?", projectID, created.ID).Count(&routeCount).Error; err != nil {
		t.Fatalf("查询路由失败: %v", err)
	}
	if routeCount != 0 {
		t.Errorf("软删后该页面全部路由（active/redirect）应清理: %d 行残留", routeCount)
	}
	// 释放后的路径可被新页面创建。
	recreated := createPage(t, svc, projectID, "/del-old", pageDocument)
	if recreated.ID == "" {
		t.Fatal("同路径新建失败")
	}
}

// TestPageDeleteIdempotent 已软删页面再次删除应返回页面不存在（幂等语义，
// 且不影响其他页面及其路由）。
func TestPageDeleteIdempotent(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/del-dup", pageDocument)
	other := createPage(t, svc, projectID, "/del-keep", pageDocument)
	if err := svc.Delete(ctx, &pagedto.DeleteReq{ID: created.ID}); err != nil {
		t.Fatalf("首次删除失败: %v", err)
	}
	if err := svc.Delete(ctx, &pagedto.DeleteReq{ID: created.ID}); err == nil || err.Error() != pageenums.ErrPageNotFound {
		t.Errorf("重复删除应返回 %q: %v", pageenums.ErrPageNotFound, err)
	}
	// 其他页面与其路由不受影响。
	if _, err := svc.Detail(ctx, &pagedto.DetailReq{ID: other.ID}); err != nil {
		t.Errorf("其他页面应不受影响: %v", err)
	}
	if kind := routeKind(t, db, projectID, "/del-keep"); kind != pubmodel.RouteReserved {
		t.Errorf("其他页面路由应保留: %s", kind)
	}
}

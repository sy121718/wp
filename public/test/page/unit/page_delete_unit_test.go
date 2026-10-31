package unit

import (
	"context"
	"testing"
	"time"

	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pubmodel "go_wp/internal/module/publication/model"
)

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

	list, err := svc.List(ctx, &pagedto.ListReq{ProjectID: projectID})
	if err != nil {
		t.Fatalf("列表查询失败: %v", err)
	}
	for _, p := range list {
		if p.ID == created.ID {
			t.Errorf("软删页面不应出现在列表: %+v", p)
		}
	}

	if _, err := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: created.ID}); err == nil || err.Error() != pageenums.ErrPageNotFound {
		t.Errorf("软删后详情应不可见: %v", err)
	}

	var routeCount int64
	if err := db.Table("page_routes").Where("project_id = ? AND path = ?", projectID, "/del-a").Count(&routeCount).Error; err != nil {
		t.Fatalf("查询路由失败: %v", err)
	}
	if routeCount != 0 {
		t.Errorf("软删后路由占用应释放: %d 行残留", routeCount)
	}

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

	recreated := createPage(t, svc, projectID, "/del-a", pageDocument)
	if recreated.ID == created.ID {
		t.Errorf("新页面应为新 ID")
	}
}

func TestPageDeleteReleasedActiveAndRedirectRoutes(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/del-old", pageDocument)

	if err := db.Table("page_routes").
		Where("project_id = ? AND path = ?", projectID, "/del-old").
		Update("route_kind", pubmodel.RouteActive).Error; err != nil {
		t.Fatalf("模拟发布失败: %v", err)
	}

	redirectPath := "/del-old-301"
	if err := db.Table("page_routes").Create(map[string]any{
		"project_id": projectID, "path": redirectPath,
		"page_id": created.ID, "route_kind": pubmodel.RouteRedirect, "update_time": time.Now().UTC(),
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

	recreated := createPage(t, svc, projectID, "/del-old", pageDocument)
	if recreated.ID == "" {
		t.Fatal("同路径新建失败")
	}
}

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

	if _, err := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: other.ID}); err != nil {
		t.Errorf("其他页面应不受影响: %v", err)
	}
	if kind := routeKind(t, db, projectID, "/del-keep"); kind != pubmodel.RouteReserved {
		t.Errorf("其他页面路由应保留: %s", kind)
	}
}

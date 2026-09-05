package unit

import (
	"context"
	"testing"

	pubdto "go_wp/internal/module/publication/dto"
	pubenums "go_wp/internal/module/publication/enums"
	pubmodel "go_wp/internal/module/publication/model"
)

// TestPublicationRenameReservedSuccess 正常改名：reserved 行路径迁移，
// 旧路径消失、新路径出现且归属不变。
func TestPublicationRenameReservedSuccess(t *testing.T) {
	svc := newUnitService(t)
	seedRoute(t, svc, "/old", pubmodel.RouteReserved, strPtr(pageID), nil)

	if err := svc.RenameReserved(context.Background(), &pubdto.RenameReservedReq{
		ProjectID: projectID, PageID: pageID, OldPath: "/old", NewPath: "/new",
	}); err != nil {
		t.Fatalf("改名失败: %v", err)
	}
	if !routeMissing(t, svc, "/old") {
		t.Fatal("旧路径应消失")
	}
	route := mustRoute(t, svc, "/new")
	if route.RouteKind != pubmodel.RouteReserved {
		t.Fatalf("新路径应为 reserved: %+v", route)
	}
	if route.PageID == nil || *route.PageID != pageID {
		t.Fatalf("归属应保持不变: %+v", route)
	}
}

// TestPublicationRenameReservedSamePath 新旧路径相同：幂等返回 nil，无副作用。
func TestPublicationRenameReservedSamePath(t *testing.T) {
	svc := newUnitService(t)
	seedRoute(t, svc, "/same", pubmodel.RouteReserved, strPtr(pageID), nil)

	if err := svc.RenameReserved(context.Background(), &pubdto.RenameReservedReq{
		ProjectID: projectID, PageID: pageID, OldPath: "/same", NewPath: "/same",
	}); err != nil {
		t.Fatalf("同名改名应幂等成功: %v", err)
	}
	if n := countRoutes(t, svc, "path = ?", "/same"); n != 1 {
		t.Fatalf("不应产生变化: %d", n)
	}
}

// TestPublicationRenameReservedMissing 不存在的旧路径：幂等返回 nil
// （注释：草稿路由可能尚未建立）。
func TestPublicationRenameReservedMissing(t *testing.T) {
	svc := newUnitService(t)
	if err := svc.RenameReserved(context.Background(), &pubdto.RenameReservedReq{
		ProjectID: projectID, PageID: pageID, OldPath: "/ghost", NewPath: "/new",
	}); err != nil {
		t.Fatalf("不存在路径改名应幂等成功: %v", err)
	}
	if n := countRoutes(t, svc, "1 = 1"); n != 0 {
		t.Fatalf("不应产生任何行: %d", n)
	}
}

// TestPublicationRenameReservedConflict 新路径已被其他页面占用：
// 改名必须失败且原占用保持不变。当前实现返回原始 DB 主键冲突
// （23505）而非 ErrRouteOccupied（错误映射缺陷，见报告）。
func TestPublicationRenameReservedConflict(t *testing.T) {
	svc := newUnitService(t)
	seedRoute(t, svc, "/mine", pubmodel.RouteReserved, strPtr(pageID), nil)
	seedRoute(t, svc, "/occupied", pubmodel.RouteReserved, strPtr(otherPageID), nil)

	err := svc.RenameReserved(context.Background(), &pubdto.RenameReservedReq{
		ProjectID: projectID, PageID: pageID, OldPath: "/mine", NewPath: "/occupied",
	})
	if err == nil {
		t.Fatal("新路径被他人占用时改名应失败")
	}
	t.Logf("改名冲突错误类型: %v", err)
	// 原占用均保持不变。
	if routeMissing(t, svc, "/mine") {
		t.Fatal("失败后旧路径不应消失")
	}
	route := mustRoute(t, svc, "/occupied")
	if route.PageID == nil || *route.PageID != otherPageID {
		t.Fatalf("他人占用不应被改写: %+v", route)
	}
}

// TestPublicationRenameReservedActive 对 active 行改名（非法转移）：
// 当前实现 UPDATE 因 route_kind=reserved 条件不命中，RowsAffected=0
// 被当作幂等成功返回 nil，静默无操作。固化当前行为；状态机判定见报告。
func TestPublicationRenameReservedActive(t *testing.T) {
	svc := newUnitService(t)
	seedRoute(t, svc, "/live", pubmodel.RouteActive, strPtr(pageID), strPtr(artifactUUID))

	if err := svc.RenameReserved(context.Background(), &pubdto.RenameReservedReq{
		ProjectID: projectID, PageID: pageID, OldPath: "/live", NewPath: "/renamed",
	}); err != nil {
		t.Fatalf("当前实现对 active 行改名返回 nil: %v", err)
	}
	// active 行未被动过，也没有新行。
	if routeMissing(t, svc, "/live") {
		t.Fatal("active 行不应被移动")
	}
	if n := countRoutes(t, svc, "path = ?", "/renamed"); n != 0 {
		t.Fatalf("不应建立新行: %d", n)
	}
}

// TestPublicationRenameReservedNilRequest 空请求返回 ErrRouteNotFound。
func TestPublicationRenameReservedNilRequest(t *testing.T) {
	svc := newUnitService(t)
	err := svc.RenameReserved(context.Background(), nil)
	containsErr(t, err, pubenums.ErrRouteNotFound)
}

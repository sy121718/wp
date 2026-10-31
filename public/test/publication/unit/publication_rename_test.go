package unit

import (
	"context"
	"testing"

	pubdto "go_wp/internal/module/publication/dto"
	pubenums "go_wp/internal/module/publication/enums"
	pubmodel "go_wp/internal/module/publication/model"
)

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

func TestPublicationRenameReservedConflict(t *testing.T) {
	svc := newUnitService(t)
	seedRoute(t, svc, "/mine", pubmodel.RouteReserved, strPtr(pageID), nil)
	seedRoute(t, svc, "/occupied", pubmodel.RouteReserved, strPtr(otherPageID), nil)

	err := svc.RenameReserved(context.Background(), &pubdto.RenameReservedReq{
		ProjectID: projectID, PageID: pageID, OldPath: "/mine", NewPath: "/occupied",
	})
	containsErr(t, err, pubenums.ErrRouteOccupied)

	if routeMissing(t, svc, "/mine") {
		t.Fatal("失败后旧路径不应消失")
	}
	route := mustRoute(t, svc, "/occupied")
	if route.PageID == nil || *route.PageID != otherPageID {
		t.Fatalf("他人占用不应被改写: %+v", route)
	}
}

func TestPublicationRenameReservedOwnActive(t *testing.T) {
	svc := newUnitService(t)
	seedRoute(t, svc, "/live", pubmodel.RouteActive, strPtr(pageID), strPtr(artifactUUID))

	err := svc.RenameReserved(context.Background(), &pubdto.RenameReservedReq{
		ProjectID: projectID, PageID: pageID, OldPath: "/live", NewPath: "/renamed",
	})
	if err != nil {
		t.Fatalf("本页 active 行改名应成功（UpdateURL 流程 DB 同步）: %v", err)
	}
	if routeMissing(t, svc, "/renamed") {
		t.Fatal("active 行应迁移到新路径")
	}
	if n := countRoutes(t, svc, "path = ?", "/live"); n != 0 {
		t.Fatalf("旧路径行应被迁移: %d", n)
	}
}

func TestPublicationRenameReservedForeignActive(t *testing.T) {
	svc := newUnitService(t)
	other := "eeeeeeee-0000-0000-0000-000000000099"
	seedRoute(t, svc, "/live", pubmodel.RouteActive, strPtr(other), strPtr(artifactUUID))

	err := svc.RenameReserved(context.Background(), &pubdto.RenameReservedReq{
		ProjectID: projectID, PageID: pageID, OldPath: "/live", NewPath: "/renamed",
	})
	containsErr(t, err, pubenums.ErrRouteActiveRename)
	if routeMissing(t, svc, "/live") {
		t.Fatal("他人 active 行不应被移动")
	}
	if n := countRoutes(t, svc, "path = ?", "/renamed"); n != 0 {
		t.Fatalf("不应建立新行: %d", n)
	}
}

func TestPublicationRenameReservedNilRequest(t *testing.T) {
	svc := newUnitService(t)
	err := svc.RenameReserved(context.Background(), nil)
	containsErr(t, err, pubenums.ErrInvalidParam)
}

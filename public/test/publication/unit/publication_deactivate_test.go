package unit

import (
	"context"
	"testing"

	pubdto "go_wp/internal/module/publication/dto"
	pubenums "go_wp/internal/module/publication/enums"
	pubmodel "go_wp/internal/module/publication/model"
)

func TestPublicationDeactivateActive(t *testing.T) {
	svc := newUnitService(t)
	seedRoute(t, svc, "/live", pubmodel.RouteActive, strPtr(pageID), strPtr(artifactUUID))

	if err := svc.Deactivate(context.Background(), &pubdto.DeactivateReq{
		ProjectID: projectID, Path: "/live",
	}); err != nil {
		t.Fatalf("停用失败: %v", err)
	}
	if n := countRoutes(t, svc, "path = ?", "/live"); n != 0 {
		t.Fatalf("停用后路由应删除: %d", n)
	}
}

func TestPublicationDeactivateMissing(t *testing.T) {
	svc := newUnitService(t)
	if err := svc.Deactivate(context.Background(), &pubdto.DeactivateReq{
		ProjectID: projectID, Path: "/never-exists",
	}); err != nil {
		t.Fatalf("停用不存在路径应幂等成功: %v", err)
	}
	if n := countRoutes(t, svc, "1 = 1"); n != 0 {
		t.Fatalf("不应产生任何行: %d", n)
	}
}

func TestPublicationDeactivateRepeat(t *testing.T) {
	svc := newUnitService(t)
	seedRoute(t, svc, "/again", pubmodel.RouteActive, strPtr(pageID), strPtr(artifactUUID))
	for i := 0; i < 2; i++ {
		if err := svc.Deactivate(context.Background(), &pubdto.DeactivateReq{
			ProjectID: projectID, Path: "/again",
		}); err != nil {
			t.Fatalf("第 %d 次停用失败: %v", i+1, err)
		}
	}
}

func TestPublicationDeactivateReserved(t *testing.T) {
	svc := newUnitService(t)
	seedRoute(t, svc, "/draft", pubmodel.RouteReserved, strPtr(pageID), nil)
	if err := svc.Deactivate(context.Background(), &pubdto.DeactivateReq{
		ProjectID: projectID, Path: "/draft",
	}); err != nil {
		t.Fatalf("停用 reserved 路径: %v", err)
	}

	if n := countRoutes(t, svc, "path = ?", "/draft"); n != 1 {
		t.Fatalf("reserved 行应保留: %d", n)
	}
}

func TestPublicationDeactivatePresentationRow(t *testing.T) {
	svc := newUnitService(t)
	seedRoute(t, svc, "/instance", pubmodel.RouteActive, nil, strPtr(artifactUUID))
	if err := svc.Deactivate(context.Background(), &pubdto.DeactivateReq{
		ProjectID: projectID, Path: "/instance",
	}); err != nil {
		t.Fatalf("停用应无错误: %v", err)
	}
	if n := countRoutes(t, svc, "path = ?", "/instance"); n != 1 {
		t.Fatalf("展示实例占用不应被页面停用删除: %d", n)
	}
}

func TestPublicationDeactivateNilRequest(t *testing.T) {
	svc := newUnitService(t)
	err := svc.Deactivate(context.Background(), nil)
	containsErr(t, err, pubenums.ErrInvalidParam)
}

package unit

import (
	"context"
	"testing"

	pubdto "go_wp/internal/module/publication/dto"
	pubenums "go_wp/internal/module/publication/enums"
	pubmodel "go_wp/internal/module/publication/model"
)

// TestPublicationDeactivateActive 正常停用：active 行被删除，删除幂等。
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

// TestPublicationDeactivateMissing 不存在的路径：幂等返回 nil，无副作用。
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

// TestPublicationDeactivateRepeat 重复停用同一路径：幂等成功。
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

// TestPublicationDeactivateReserved 停用未激活（reserved）路径：
// 当前实现删除条件仅限 page_id IS NOT NULL，不区分 route_kind，
// 因此 reserved 行也会被删除。固化当前行为；状态机语义判定见报告。
func TestPublicationDeactivateReserved(t *testing.T) {
	svc := newUnitService(t)
	seedRoute(t, svc, "/draft", pubmodel.RouteReserved, strPtr(pageID), nil)
	if err := svc.Deactivate(context.Background(), &pubdto.DeactivateReq{
		ProjectID: projectID, Path: "/draft",
	}); err != nil {
		t.Fatalf("停用 reserved 路径: %v", err)
	}
	// 修复语义：Deactivate 只取消 active 占用，不得误删 reserved（草稿占用）行。
	if n := countRoutes(t, svc, "path = ?", "/draft"); n != 1 {
		t.Fatalf("reserved 行应保留: %d", n)
	}
}

// TestPublicationDeactivatePresentationRow 展示实例占用（page_id 为空）不会被停用：
// 删除条件要求 page_id IS NOT NULL。
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

// TestPublicationDeactivateNilRequest 空请求返回 ErrInvalidParam（参数错误，
// 与路径占用不存在语义无关）。
func TestPublicationDeactivateNilRequest(t *testing.T) {
	svc := newUnitService(t)
	err := svc.Deactivate(context.Background(), nil)
	containsErr(t, err, pubenums.ErrInvalidParam)
}

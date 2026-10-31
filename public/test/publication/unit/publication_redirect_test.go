package unit

import (
	"context"
	"testing"

	pubdto "go_wp/internal/module/publication/dto"
	pubenums "go_wp/internal/module/publication/enums"
	pubmodel "go_wp/internal/module/publication/model"
)

func TestPublicationRedirectNormal(t *testing.T) {
	svc := newUnitService(t)
	seedRoute(t, svc, "/old", pubmodel.RouteActive, strPtr(pageID), strPtr(artifactUUID))

	route, err := svc.Redirect(context.Background(), &pubdto.RedirectReq{
		ProjectID: projectID, OldPath: "/old", PageID: pageID, ArtifactID: artifactUUID,
	})
	if err != nil {
		t.Fatalf("重定向失败: %v", err)
	}
	if route.RouteKind != pubmodel.RouteRedirect {
		t.Fatalf("应为 redirect: %+v", route)
	}
	if route.ArtifactID == nil || *route.ArtifactID != artifactUUID {
		t.Fatalf("应绑定重定向产物: %+v", route)
	}
	if n := countReceipts(t, svc, "action = ? AND receipt_state = ?", "redirect", pubmodel.ReceiptCommitted); n != 1 {
		t.Fatalf("应有 1 条 redirect committed 回执: %d", n)
	}
}

func TestPublicationRedirectMissing(t *testing.T) {
	svc := newUnitService(t)
	route, err := svc.Redirect(context.Background(), &pubdto.RedirectReq{
		ProjectID: projectID, OldPath: "/vanished", PageID: pageID, ArtifactID: artifactUUID,
	})
	if err != nil {
		t.Fatalf("不存在路径重定向应幂等建立: %v", err)
	}
	if route.RouteKind != pubmodel.RouteRedirect {
		t.Fatalf("应为 redirect: %+v", route)
	}
	if n := countRoutes(t, svc, "path = ?", "/vanished"); n != 1 {
		t.Fatalf("应建立 1 行: %d", n)
	}
}

func TestPublicationRedirectConflict(t *testing.T) {
	svc := newUnitService(t)
	seedRoute(t, svc, "/other", pubmodel.RouteActive, strPtr(otherPageID), strPtr(artifactUUID))

	_, err := svc.Redirect(context.Background(), &pubdto.RedirectReq{
		ProjectID: projectID, OldPath: "/other", PageID: pageID, ArtifactID: artifactUUID,
	})
	containsErr(t, err, pubenums.ErrRouteOccupied)
	route := mustRoute(t, svc, "/other")
	if route.PageID == nil || *route.PageID != otherPageID || route.RouteKind != pubmodel.RouteActive {
		t.Fatalf("原占用应保持不变: %+v", route)
	}
}

func TestPublicationRedirectNoArtifact(t *testing.T) {
	svc := newUnitService(t)
	seedRoute(t, svc, "/noart", pubmodel.RouteActive, strPtr(pageID), strPtr(artifactUUID))

	res, err := svc.Redirect(context.Background(), &pubdto.RedirectReq{
		ProjectID: projectID, OldPath: "/noart", PageID: pageID,
	})
	if err != nil {
		t.Fatalf("空 ArtifactID 重定向应成功（产物不入库），实际: %v", err)
	}

	if res.RouteKind != pubmodel.RouteRedirect {
		t.Fatalf("路由应转 redirect: %+v", res)
	}

	var receipt pubmodel.ReceiptEntity
	if err := svc.Model().ReceiptDB(context.Background()).
		Where("action = ?", "redirect").Order("create_time DESC").Limit(1).
		First(&receipt).Error; err != nil {
		t.Fatalf("查询回执失败: %v", err)
	}
	if receipt.ToArtifact != nil {
		t.Fatalf("空 ArtifactID 回执 to_artifact_id 应为 NULL，实际: %v", *receipt.ToArtifact)
	}
}

func TestPublicationRedirectMalformedArtifact(t *testing.T) {
	svc := newUnitService(t)
	seedRoute(t, svc, "/bad", pubmodel.RouteActive, strPtr(pageID), strPtr(artifactUUID))

	_, err := svc.Redirect(context.Background(), &pubdto.RedirectReq{
		ProjectID: projectID, OldPath: "/bad", PageID: pageID, ArtifactID: `abc"def`,
	})
	if err == nil {
		t.Fatal("含引号 ArtifactID 应导致失败（jsonb 非法 JSON）")
	}
	t.Logf("畸形 ArtifactID 错误类型: %v", err)

	route := mustRoute(t, svc, "/bad")
	if route.RouteKind != pubmodel.RouteActive {
		t.Fatalf("事务应整体回滚，路由保持 active: %+v", route)
	}
	if n := countReceipts(t, svc, "1 = 1"); n != 0 {
		t.Fatalf("失败事务不应残留回执: %d", n)
	}
}

func TestPublicationRedirectNilRequest(t *testing.T) {
	svc := newUnitService(t)
	_, err := svc.Redirect(context.Background(), nil)
	containsErr(t, err, pubenums.ErrInvalidParam)
}

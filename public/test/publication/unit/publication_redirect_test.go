package unit

import (
	"context"
	"testing"

	pubdto "go_wp/internal/module/publication/dto"
	pubenums "go_wp/internal/module/publication/enums"
	pubmodel "go_wp/internal/module/publication/model"
)

// TestPublicationRedirectNormal 正常重定向：active 行转为 redirect，
// 绑定 artifact 并生成 committed 回执。
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

// TestPublicationRedirectMissing 不存在路径：幂等建立 redirect 行（非 404）。
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

// TestPublicationRedirectConflict 目标路径被其他页面占用时重定向失败且原占用不变。
// 当前实现返回原始 DB 主键冲突而非 ErrRouteOccupied（错误映射缺陷，见报告）。
func TestPublicationRedirectConflict(t *testing.T) {
	svc := newUnitService(t)
	seedRoute(t, svc, "/other", pubmodel.RouteActive, strPtr(otherPageID), strPtr(artifactUUID))

	_, err := svc.Redirect(context.Background(), &pubdto.RedirectReq{
		ProjectID: projectID, OldPath: "/other", PageID: pageID, ArtifactID: artifactUUID,
	})
	if err == nil {
		t.Fatal("他人占用时重定向应失败")
	}
	t.Logf("他人占用重定向错误类型: %v", err)
	route := mustRoute(t, svc, "/other")
	if route.PageID == nil || *route.PageID != otherPageID || route.RouteKind != pubmodel.RouteActive {
		t.Fatalf("原占用应保持不变: %+v", route)
	}
}

// TestPublicationRedirectNoArtifact 空 ArtifactID 是 DTO 文档化的合法输入
// （「ArtifactID 允许为空（重定向产物不入库）」），但当前实现无条件
// 将 ToArtifact 绑定为空串写入 uuid 列，导致 SQLSTATE 22P02 解析失败、
// 整个事务回滚（高严重度 bug，见报告）。本测试固化该失败行为。
func TestPublicationRedirectNoArtifact(t *testing.T) {
	svc := newUnitService(t)
	seedRoute(t, svc, "/noart", pubmodel.RouteActive, strPtr(pageID), strPtr(artifactUUID))

	_, err := svc.Redirect(context.Background(), &pubdto.RedirectReq{
		ProjectID: projectID, OldPath: "/noart", PageID: pageID,
	})
	if err == nil {
		t.Fatal("空 ArtifactID 当前实现必然失败（to_artifact_id='' 写入 uuid 列）")
	}
	t.Logf("空 ArtifactID 错误类型: %v", err)
	// 事务必须整体回滚：路由保持 active，无回执残留。
	route := mustRoute(t, svc, "/noart")
	if route.RouteKind != pubmodel.RouteActive {
		t.Fatalf("失败事务应整体回滚，路由保持 active: %+v", route)
	}
	if n := countReceipts(t, svc, "1 = 1"); n != 0 {
		t.Fatalf("失败事务不应残留回执: %d", n)
	}
}

// TestPublicationRedirectMalformedArtifact 含非法字符（引号）的 ArtifactID：
// 回执 ReceiptData 由字符串拼接生成非法 JSON，jsonb 插入失败导致整个事务回滚
// （激活路径已改用 json.Marshal 安全序列化，此处为遗留不一致，见报告）。
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
	// 事务必须整体回滚：路由保持 active，无回执残留。
	route := mustRoute(t, svc, "/bad")
	if route.RouteKind != pubmodel.RouteActive {
		t.Fatalf("事务应整体回滚，路由保持 active: %+v", route)
	}
	if n := countReceipts(t, svc, "1 = 1"); n != 0 {
		t.Fatalf("失败事务不应残留回执: %d", n)
	}
}

// TestPublicationRedirectNilRequest 空请求返回 ErrRouteNotFound。
func TestPublicationRedirectNilRequest(t *testing.T) {
	svc := newUnitService(t)
	_, err := svc.Redirect(context.Background(), nil)
	containsErr(t, err, pubenums.ErrRouteNotFound)
}

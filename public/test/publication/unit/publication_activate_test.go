package unit

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	pubdto "go_wp/internal/module/publication/dto"
	pubenums "go_wp/internal/module/publication/enums"
	pubmodel "go_wp/internal/module/publication/model"
)

func TestPublicationActivateFirstTime(t *testing.T) {
	t.Run("无预留直接激活建立占用", func(t *testing.T) {
		svc := newUnitService(t)
		route, err := svc.Activate(context.Background(), &pubdto.ActivateReq{
			ProjectID: projectID, Path: "/home", PageID: pageID, ArtifactID: artifactUUID,
		})
		if err != nil {
			t.Fatalf("首次激活失败: %v", err)
		}
		if route.RouteKind != pubmodel.RouteActive || route.ArtifactID == nil || *route.ArtifactID != artifactUUID {
			t.Fatalf("激活后状态错误: %+v", route)
		}
		if route.PageID == nil || *route.PageID != pageID {
			t.Fatalf("激活行应归属 pageID: %+v", route)
		}
		if n := countReceipts(t, svc, "receipt_state = ?", pubmodel.ReceiptCommitted); n != 1 {
			t.Fatalf("应有 1 条 committed 回执: %d", n)
		}
		if n := countReceipts(t, svc, "receipt_state = ?", pubmodel.ReceiptPending); n != 0 {
			t.Fatalf("不应残留 pending 回执: %d", n)
		}
	})
	t.Run("reserved 升级为 active", func(t *testing.T) {
		svc := newUnitService(t)
		seedRoute(t, svc, "/draft", pubmodel.RouteReserved, strPtr(pageID), nil)
		route, err := svc.Activate(context.Background(), &pubdto.ActivateReq{
			ProjectID: projectID, Path: "/draft", PageID: pageID, ArtifactID: artifactUUID,
		})
		if err != nil {
			t.Fatalf("reserved 升级失败: %v", err)
		}
		if route.RouteKind != pubmodel.RouteActive {
			t.Fatalf("reserved 应升级为 active: %+v", route)
		}

		if n := countRoutes(t, svc, "path = ?", "/draft"); n != 1 {
			t.Fatalf("升级后应只有 1 行: %d", n)
		}
	})
}

func TestPublicationActivateOccupied(t *testing.T) {
	t.Run("其他页面占用", func(t *testing.T) {
		svc := newUnitService(t)
		seedRoute(t, svc, "/taken", pubmodel.RouteActive, strPtr(otherPageID), strPtr(artifactUUID))
		_, err := svc.Activate(context.Background(), &pubdto.ActivateReq{
			ProjectID: projectID, Path: "/taken", PageID: pageID, ArtifactID: artifactUUID,
		})
		containsErr(t, err, pubenums.ErrRouteOccupied)

		route := mustRoute(t, svc, "/taken")
		if route.PageID == nil || *route.PageID != otherPageID {
			t.Fatalf("原占用被破坏: %+v", route)
		}
	})
	t.Run("展示实例占用", func(t *testing.T) {
		svc := newUnitService(t)

		seedRoute(t, svc, "/instance", pubmodel.RouteActive, nil, strPtr(artifactUUID))
		_, err := svc.Activate(context.Background(), &pubdto.ActivateReq{
			ProjectID: projectID, Path: "/instance", PageID: pageID, ArtifactID: artifactUUID,
		})
		containsErr(t, err, pubenums.ErrRouteOccupied)
	})
	t.Run("拒绝后回执补偿为 rolled_back", func(t *testing.T) {
		svc := newUnitService(t)
		seedRoute(t, svc, "/taken2", pubmodel.RouteActive, strPtr(otherPageID), strPtr(artifactUUID))
		_, _ = svc.Activate(context.Background(), &pubdto.ActivateReq{
			ProjectID: projectID, Path: "/taken2", PageID: pageID, ArtifactID: artifactUUID,
		})
		if n := countReceipts(t, svc, "receipt_state = ?", pubmodel.ReceiptRolledBack); n != 1 {
			t.Fatalf("失败回执应补偿为 rolled_back: %d", n)
		}
		if n := countReceipts(t, svc, "receipt_state = ?", pubmodel.ReceiptPending); n != 0 {
			t.Fatalf("不应残留 pending 回执: %d", n)
		}
	})
}

func TestPublicationActivateRepeat(t *testing.T) {
	svc := newUnitService(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		route, err := svc.Activate(ctx, &pubdto.ActivateReq{
			ProjectID: projectID, Path: "/repeat", PageID: pageID, ArtifactID: artifactUUID,
		})
		if err != nil {
			t.Fatalf("第 %d 次重复激活失败: %v", i+1, err)
		}
		if route.RouteKind != pubmodel.RouteActive {
			t.Fatalf("应为 active: %+v", route)
		}
	}
	if n := countRoutes(t, svc, "path = ?", "/repeat"); n != 1 {
		t.Fatalf("重复激活不应产生多行: %d", n)
	}
	if n := countReceipts(t, svc, "receipt_state = ?", pubmodel.ReceiptCommitted); n != 3 {
		t.Fatalf("应有 3 条 committed 回执: %d", n)
	}
}

func TestPublicationActivateMissingArtifact(t *testing.T) {
	svc := newUnitService(t)
	missing := "99999999-9999-9999-9999-999999999999"
	route, err := svc.Activate(context.Background(), &pubdto.ActivateReq{
		ProjectID: projectID, Path: "/ghost", PageID: pageID, ArtifactID: missing,
	})
	if err != nil {
		t.Fatalf("当前实现应接受不存在 artifact: %v", err)
	}
	if route.ArtifactID == nil || *route.ArtifactID != missing {
		t.Fatalf("artifact 应原样绑定: %+v", route)
	}
}

func TestPublicationActivateNilRequest(t *testing.T) {
	svc := newUnitService(t)
	_, err := svc.Activate(context.Background(), nil)
	containsErr(t, err, pubenums.ErrInvalidParam)
}

func TestPublicationActivateConcurrentSamePage(t *testing.T) {
	svc := newUnitService(t)
	ctx := context.Background()
	const goroutines = 8
	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = svc.Activate(ctx, &pubdto.ActivateReq{
				ProjectID: projectID, Path: "/race-self", PageID: pageID, ArtifactID: artifactUUID,
			})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d 同一页面重复激活应幂等成功: %v", i, err)
		}
	}
	if n := countRoutes(t, svc, "path = ?", "/race-self"); n != 1 {
		t.Fatalf("并发同页激活应仅 1 行路由: %d", n)
	}
	if n := countReceipts(t, svc, "receipt_state = ?", pubmodel.ReceiptCommitted); n != goroutines {
		t.Fatalf("应有 %d 条 committed 回执: %d", goroutines, n)
	}
}

func TestPublicationActivateConcurrentSamePath(t *testing.T) {
	svc := newUnitService(t)
	ctx := context.Background()
	const goroutines = 8
	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	ok := make([]bool, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			pID := fmt.Sprintf("dddddddd-0000-0000-0000-0000000000%02d", idx+1)
			_, err := svc.Activate(ctx, &pubdto.ActivateReq{
				ProjectID: projectID, Path: "/race", PageID: pID, ArtifactID: artifactUUID,
			})
			errs[idx] = err
			ok[idx] = err == nil
		}(i)
	}
	wg.Wait()

	successCount := 0
	for i := 0; i < goroutines; i++ {
		if ok[i] {
			successCount++
			continue
		}
		t.Logf("goroutine %d 失败，错误类型: %v", i, errs[i])
	}
	if successCount != 1 {
		t.Fatalf("并发激活应恰好 1 个成功，实际 %d 个", successCount)
	}

	if n := countRoutes(t, svc, "path = ?", "/race"); n != 1 {
		t.Fatalf("应恰好 1 行路由: %d", n)
	}

	bizErr := 0
	rawErr := 0
	for i := 0; i < goroutines; i++ {
		if ok[i] {
			continue
		}
		if strings.Contains(errs[i].Error(), pubenums.ErrRouteOccupied) {
			bizErr++
		} else {
			rawErr++
		}
	}
	t.Logf("并发失败者: %d 个业务错误 ErrRouteOccupied，%d 个非业务错误（原始 DB 错误）", bizErr, rawErr)
	if rawErr > 0 {
		t.Logf("并发唯一约束竞争已复现：部分失败者拿到原始 DB 错误而非 ErrRouteOccupied")
	}
}

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

// TestPublicationActivateFirstTime 首次激活：无既有占用时幂等建立 active 行，
// 回执流转为 committed。
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
		// 原行原地升级（主键 project_id+path 不变），不产生重复行。
		if n := countRoutes(t, svc, "path = ?", "/draft"); n != 1 {
			t.Fatalf("升级后应只有 1 行: %d", n)
		}
	})
}

// TestPublicationActivateOccupied 目标路径已被其他实体占用时必须拒绝：
// 他人页面（page_id 不同）与展示实例（page_id 为空）两种情况。
func TestPublicationActivateOccupied(t *testing.T) {
	t.Run("其他页面占用", func(t *testing.T) {
		svc := newUnitService(t)
		seedRoute(t, svc, "/taken", pubmodel.RouteActive, strPtr(otherPageID), strPtr(artifactUUID))
		_, err := svc.Activate(context.Background(), &pubdto.ActivateReq{
			ProjectID: projectID, Path: "/taken", PageID: pageID, ArtifactID: artifactUUID,
		})
		containsErr(t, err, pubenums.ErrRouteOccupied)
		// 原占用保持不变。
		route := mustRoute(t, svc, "/taken")
		if route.PageID == nil || *route.PageID != otherPageID {
			t.Fatalf("原占用被破坏: %+v", route)
		}
	})
	t.Run("展示实例占用", func(t *testing.T) {
		svc := newUnitService(t)
		// presentation 占用：page_id 为空。
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

// TestPublicationActivateRepeat 重复激活同一路径（同页面）幂等成功，每次生成 committed 回执。
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

// TestPublicationActivateMissingArtifact 激活不存在的 artifact：
// 当前实现不校验 ArtifactID 是否存在（无 FK、无存在性检查），绑定任意值成功。
// 固化当前行为；缺陷判定见报告（激活点缺少 artifact 存在性校验）。
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

// TestPublicationActivateNilRequest 空请求返回 ErrRouteNotFound（错误映射待议，见报告）。
func TestPublicationActivateNilRequest(t *testing.T) {
	svc := newUnitService(t)
	_, err := svc.Activate(context.Background(), nil)
	containsErr(t, err, pubenums.ErrRouteNotFound)
}

// TestPublicationActivateConcurrentSamePath 两个页面并发激活同一路径：
// 必须恰好一个成功；失败者若在唯一约束上竞争，错误是原始 DB 主键冲突
// 而非 ErrRouteOccupied（并发错误映射缺陷，见报告）。
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
			// 每个 goroutine 用不同 page（并发抢占同一路径的归属者不同）。
			pID := pageID
			if idx > 0 {
				pID = otherPageID[:len(otherPageID)-2] + fmt.Sprintf("%02d", idx)
			}
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
	// 只有一个 active 行，且归属为成功者。
	if n := countRoutes(t, svc, "path = ?", "/race"); n != 1 {
		t.Fatalf("应恰好 1 行路由: %d", n)
	}
	// 记录失败错误是否为业务错误（报告素材）。
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

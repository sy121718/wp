package unit

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pageservice "go_wp/internal/module/page/service"

	"gorm.io/gorm"
)

type convergeCapable interface {
	ConvergePendingReceipts(context.Context) (int, error)
	PendingReceiptStatus(context.Context) (int64, time.Time, error)
	NotifyPendingReceipt()
}

func convergerOf(t *testing.T, svc pagecontract.PageService) convergeCapable {
	t.Helper()
	c, ok := svc.(convergeCapable)
	if !ok {
		t.Fatal("页面服务未提供发布回执收敛入口")
	}
	return c
}

func TestConvergePendingReceiptsCompletesAndIsIdempotent(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	page, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/converge", DraftDocument: []byte(emptyRevDoc),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}
	hash := "hash-converge-1"
	writeArtifactDir(t, root, hash)
	artifactID := insertArtifactRow(t, db, page.ID, hash)

	linkActivePath(t, root, "/converge", hash)
	insertPendingReceipt(t, db, page.ID, "/converge", "zh-CN", artifactID)

	converger := convergerOf(t, svc)
	converged, cerr := converger.ConvergePendingReceipts(ctx)
	if cerr != nil {
		t.Fatalf("收敛失败: %v", cerr)
	}
	if converged != 1 {
		t.Fatalf("应收敛 1 条回执，实际 %d", converged)
	}
	if got := receiptState(t, db, "/converge"); got != "committed" {
		t.Fatalf("收敛后回执应结案为 committed，实际 %q", got)
	}
	if got := activeHashOf(t, db, page.ID); got != hash {
		t.Fatalf("收敛应补齐该语言的激活记录到 %s，实际 %q", hash, got)
	}

	pending, lastConvergeAt, serr := converger.PendingReceiptStatus(ctx)
	if serr != nil {
		t.Fatalf("读取回执收敛状态失败: %v", serr)
	}
	if pending != 0 {
		t.Fatalf("收敛后不应残留 pending，实际 %d", pending)
	}
	if lastConvergeAt.IsZero() {
		t.Fatal("收敛后应记录最近一次收敛时刻")
	}

	again, aerr := converger.ConvergePendingReceipts(ctx)
	if aerr != nil {
		t.Fatalf("重复收敛失败: %v", aerr)
	}
	if again != 0 {
		t.Fatalf("重复收敛不应再判定任何回执，实际 %d", again)
	}
	if got := receiptState(t, db, "/converge"); got != "committed" {
		t.Fatalf("重复收敛不得改变已结案回执，实际 %q", got)
	}
	if got := activeHashOf(t, db, page.ID); got != hash {
		t.Fatalf("重复收敛不得改变已补齐的激活记录，实际 %q", got)
	}
}

func TestConvergePendingReceiptsAbortsSupersededReceipt(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	page, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/converge-fail", DraftDocument: []byte(emptyRevDoc),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}

	hash, artifactID := seedUpdateURLArtifact(t, db, page.ID, "/converge-fail")
	linkActivePath(t, root, "/converge-fail", hash)

	fromID := insertUpdateURLReceipt(t, db, page.ID, "/converge-from-new", "/converge-from-old", "zh-CN", artifactID, true, 0)
	if err := db.Exec(`UPDATE publication_receipts SET receipt_state = 'committed' WHERE id = ?`, fromID).Error; err != nil {
		t.Fatalf("预置出发回执状态失败: %v", err)
	}
	itemID := insertPendingUpdateURLReceipt(t, db, page.ID, "/converge-fail", "/converge-fail-old", "zh-CN", artifactID)

	thirdPartyID := insertUpdateURLReceipt(t, db, page.ID, "/converge-third-new", "/converge-third-old", "zh-CN", artifactID, true, 0)
	if err := db.Exec(`UPDATE publication_receipts SET receipt_state = 'committed' WHERE id = ?`, thirdPartyID).Error; err != nil {
		t.Fatalf("预置第三方回执状态失败: %v", err)
	}
	if err := db.Exec(`INSERT INTO page_publications (page_id, lang, active_path, artifact_id, artifact_hash, published_at, update_time, receipt_id) VALUES (?, 'zh-CN', ?, ?, ?, now(), now(), ?)`,
		page.ID, "/converge-fail", artifactID, hash, thirdPartyID).Error; err != nil {
		t.Fatalf("预置第三方成功身份失败: %v", err)
	}

	if ledger := ledgerReceiptID(t, db, page.ID, "zh-CN"); ledger != thirdPartyID {
		t.Fatalf("夹具未生效：账本 receipt_id 应为第三方 %d，实际 %d", thirdPartyID, ledger)
	}
	if err := db.Exec(`UPDATE publication_receipts SET receipt_data = receipt_data || ?::jsonb WHERE id = ?`,
		fmt.Sprintf(`{"fromReceiptId":%d}`, fromID), itemID).Error; err != nil {
		t.Fatalf("预置出发身份失败: %v", err)
	}

	converger := convergerOf(t, svc)
	converged, cerr := converger.ConvergePendingReceipts(ctx)
	if cerr != nil {
		t.Fatalf("这条回执走的是 abort 分支（已判定为被替代），收敛不该回传错误: %v", cerr)
	}

	_ = converged

	if got := receiptStateByID(t, db, itemID); got != "rolled_back" {
		t.Fatalf("账本上的成功身份属另一条回执 → 应结案为 rolled_back，实际 %q", got)
	}

	if n := publicationCount(t, db, page.ID); n != 1 {
		t.Fatalf("判定为被替代时不得新增激活记录（夹具预置 1 条），实际 %d 条", n)
	}
	if ledger := ledgerReceiptID(t, db, page.ID, "zh-CN"); ledger != thirdPartyID {
		t.Fatalf("被替代的回执不得覆盖账本身份：应为第三方 %d，实际 %d", thirdPartyID, ledger)
	}

	if pending, _, serr := converger.PendingReceiptStatus(ctx); serr != nil {
		t.Fatalf("读取回执收敛状态失败: %v", serr)
	} else if pending != 0 {
		t.Fatalf("已结案的回执不应再计入待办数，实际 %d", pending)
	}

	if _, rerr := converger.ConvergePendingReceipts(ctx); rerr != nil {
		t.Fatalf("二次收敛不该报错: %v", rerr)
	}
	if got := receiptStateByID(t, db, itemID); got != "rolled_back" {
		t.Fatalf("二次收敛不得改写已结案状态，实际 %q", got)
	}
}

func TestConvergePendingReceiptsFastPathWithoutTicker(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	impl, ok := svc.(*pageservice.Service)
	if !ok {
		t.Fatalf("页面服务类型不符，无法启动收敛调度: %T", svc)
	}
	pageservice.StartPendingReceiptConvergenceSchedulerWithInterval(impl, time.Hour)

	waitForCondition(t, 10*time.Second, "收敛调度没有在预期时间内完成启动首跑", func() bool {
		_, last, err := impl.PendingReceiptStatus(ctx)
		return err == nil && !last.IsZero()
	})

	page, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/converge-wake", DraftDocument: []byte(emptyRevDoc),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}
	hash := "hash-converge-wake"
	writeArtifactDir(t, root, hash)
	artifactID := insertArtifactRow(t, db, page.ID, hash)
	linkActivePath(t, root, "/converge-wake", hash)
	insertPendingReceipt(t, db, page.ID, "/converge-wake", "zh-CN", artifactID)

	time.Sleep(300 * time.Millisecond)
	if got := receiptState(t, db, "/converge-wake"); got != "pending" {
		t.Fatalf("定时器间隔 1 小时，未推快通道前不应被收敛，实际 %q", got)
	}

	impl.NotifyPendingReceipt()
	waitForCondition(t, 10*time.Second, "推快通道后回执没有被收敛（信号未驱动收敛）", func() bool {
		return receiptStateQuiet(db, "/converge-wake") == "committed"
	})

	if got := activeHashOf(t, db, page.ID); got != hash {
		t.Fatalf("快通道收敛后应补齐激活记录到 %s，实际 %q", hash, got)
	}
}

func waitForCondition(t *testing.T, timeout time.Duration, msg string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(msg)
}

func receiptStateQuiet(db *gorm.DB, path string) string {
	var state string
	if err := db.Raw("SELECT receipt_state FROM publication_receipts WHERE path = ? AND action = 'switch_active'", path).Scan(&state).Error; err != nil {
		return ""
	}
	return state
}

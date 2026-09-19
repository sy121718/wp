package unit

// page_receipt_converge_test.go —— 未结案发布回执的定时收敛（不再依赖重启）。
//
// 背景：UpdateURL / Rollback / switch_active 失败后会在 publication_receipts 留下 pending
// 回执，而恢复此前**只在进程启动时**跑一次 —— 长时间不重启就一直 pending，
// 线上与库长期不一致，且没有任何可见性。下面三条用例把新入口钉住：
//
//	① pending 回执被收敛（补完成 + 收敛后回执结案），且重复收敛零变化（幂等）；
//	② 重放仍失败时**保留 pending**、错误可见、不 panic（失败的行下一轮还能重放）；
//	③ 写路径提交后的快通道能收敛 —— 定时器间隔取到用例时长之外，收敛只可能来自信号。

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pageservice "go_wp/internal/module/page/service"

	"gorm.io/gorm"
)

// convergeCapable 收敛入口（运维能力不进服务契约，走类型断言 —— 与 recoverPending 同一手法）。
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

// TestConvergePendingReceiptsCompletesAndIsIdempotent ① pending 回执被收敛，且重复收敛零变化。
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
	// 访问面已切换（符号链接指向本次产物）：收敛应判定为「切换已生效、DB 没跟上」并补完成。
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

	// 可观测：pending 归零 + 最近一次收敛时刻已记录（健康检查靠这两个值判断收敛是否还在跑）。
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

	// 幂等：再跑一次不该有任何变化。
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

// TestConvergePendingReceiptsKeepsPendingWhenReplayFails ② 重放仍失败：保留 pending、错误可见、不 panic。
func TestConvergePendingReceiptsKeepsPendingWhenReplayFails(t *testing.T) {
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
	hash := "hash-converge-fail"
	writeArtifactDir(t, root, hash)
	artifactID := insertArtifactRow(t, db, page.ID, hash)
	linkActivePath(t, root, "/converge-fail", hash)
	insertPendingReceipt(t, db, page.ID, "/converge-fail", "zh-CN", artifactID)

	// 故障注入：命中「访问面已切换、数据库尚未落定」窗口，重放同样会失败。
	injected := errors.New("故障注入：重放仍失败")
	setPublishWindowFault(t, svc, func() error { return injected })

	converger := convergerOf(t, svc)
	converged, cerr := converger.ConvergePendingReceipts(ctx)
	if cerr == nil {
		t.Fatal("重放失败必须回传错误（否则失败会被当成收敛完成）")
	}
	if converged != 0 {
		t.Fatalf("重放失败不应计入已收敛，实际 %d", converged)
	}
	// 关键：失败的 pending 绝不能被标死 —— 标成 rolled_back 等于把「状态未知」
	// 换成了「确定没生效」，比不收敛更糟。
	if got := receiptState(t, db, "/converge-fail"); got != "pending" {
		t.Fatalf("重放失败的回执必须保留 pending，实际 %q", got)
	}
	if n := publicationCount(t, db, page.ID); n != 0 {
		t.Fatalf("重放失败时不得写入激活记录，实际 %d 条", n)
	}
	// 失败的行还在待办里：可观测口径必须看得见它。
	pending, _, serr := converger.PendingReceiptStatus(ctx)
	if serr != nil {
		t.Fatalf("读取回执收敛状态失败: %v", serr)
	}
	if pending != 1 {
		t.Fatalf("失败的 pending 应计入待办数，实际 %d", pending)
	}

	// 清掉注入点（等价于重启后的干净状态）后能收敛 —— 证明上面保留的 pending 是可重放的。
	setPublishWindowFault(t, svc, nil)
	recovered, rerr := converger.ConvergePendingReceipts(ctx)
	if rerr != nil {
		t.Fatalf("清除注入点后应收敛成功: %v", rerr)
	}
	if recovered != 1 {
		t.Fatalf("清除注入点后应收敛 1 条，实际 %d", recovered)
	}
	if got := receiptState(t, db, "/converge-fail"); got != "committed" {
		t.Fatalf("重放成功后回执应结案为 committed，实际 %q", got)
	}
	if got := activeHashOf(t, db, page.ID); got != hash {
		t.Fatalf("重放成功后应补齐激活记录到 %s，实际 %q", hash, got)
	}
}

// TestConvergePendingReceiptsFastPathWithoutTicker ③ 写路径提交后的快通道能在定时器之外收敛。
//
// 判定方式：调度器间隔取 1 小时（用例时长内定时器不可能触发），并在**启动首跑结束之后**
// 才插入 pending 回执 —— 此刻能被收敛就只可能来自快通道（NotifyPendingReceipt）。
func TestConvergePendingReceiptsFastPathWithoutTicker(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	impl, ok := svc.(*pageservice.Service)
	if !ok {
		t.Fatalf("页面服务类型不符，无法启动收敛调度: %T", svc)
	}
	pageservice.StartPendingReceiptConvergenceSchedulerWithInterval(impl, time.Hour)

	// 等启动首跑结束：首跑无论有没有待办都会写「最近一次收敛时刻」。
	waitForCondition(t, 10*time.Second, "收敛调度没有在预期时间内完成启动首跑", func() bool {
		_, last, err := impl.PendingReceiptStatus(ctx)
		return err == nil && !last.IsZero()
	})

	// 首跑之后才造 pending：这一条只可能被快通道收掉。
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

	// 先确认「没推信号就不会收敛」：否则这条用例证明不了快通道真的被走到。
	time.Sleep(300 * time.Millisecond)
	if got := receiptState(t, db, "/converge-wake"); got != "pending" {
		t.Fatalf("定时器间隔 1 小时，未推快通道前不应被收敛，实际 %q", got)
	}

	impl.NotifyPendingReceipt()
	waitForCondition(t, 10*time.Second, "推快通道后回执没有被收敛（信号未驱动收敛）", func() bool {
		return receiptStateQuiet(db, "/converge-wake") == "committed"
	})
	// 收敛结果与定时路径一致：激活记录一并补齐。
	if got := activeHashOf(t, db, page.ID); got != hash {
		t.Fatalf("快通道收敛后应补齐激活记录到 %s，实际 %q", hash, got)
	}
}

// waitForCondition 轮询等待条件成立（在测试 goroutine 内轮询，超时即 Fatal）。
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

// receiptStateQuiet 读回执状态（不 Fatal，供轮询使用）。
func receiptStateQuiet(db *gorm.DB, path string) string {
	var state string
	if err := db.Raw("SELECT receipt_state FROM publication_receipts WHERE path = ? AND action = 'switch_active'", path).Scan(&state).Error; err != nil {
		return ""
	}
	return state
}

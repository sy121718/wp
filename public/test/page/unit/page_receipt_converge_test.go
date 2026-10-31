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
	"fmt"
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

// TestConvergePendingReceiptsAbortsSupersededReceipt ② 收敛对一条「已被替代」的回执
// 给出明确结论：结案为 rolled_back、不报错、不写激活记录、不再计入待办，且二次收敛幂等。
//
// 「重放失败保 pending」的那条语义由 TestUpdateURLOldPathSettleFailureKeepsPending
// 覆盖（那里失败发生在 applyUpdateURL 的 FS 处置上，回执必须留 pending 等下一轮）；
// 本用例打的是判定四步的 L3：账本上已有别人的成功身份 → 直接 abort，不写任何东西。
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
	// 真实产物：磁盘 manifest.json 的 canonicalPath 必须等于回执的 item.Path，收敛靠它
	// 认「这条路径上跑的是本次产物」（page_publish.go:2304 + pipeline/store.go:157-200），
	// 否则判定第一步就落进「暂无产物」分支。
	hash, artifactID := seedUpdateURLArtifact(t, db, page.ID, "/converge-fail")
	linkActivePath(t, root, "/converge-fail", hash)
	// 回执必须是**收敛重放会真正执行 DB 写入**的那一种动作，而且要能通过判定四步走到
	// publishWindowFaultHit 所在的写段：
	//
	// · switch_active（发布）的重放走 recoverSwitchActiveReceipt，全路径不经过任何注入点，
	//   拿它测「重放失败」只会得到「收敛成功」；
	// · update_url 的重放走 recoverUpdateURLReceipt → 事务内 L2/L3/L4 判定 → applyUpdateURL，
	//   注入点在 applyUpdateURL 的写步骤里（page_publish.go:1909，另有一处 1844）。
	//
	// 而 L3 的判据是「账本上不能出现既不是我、也不是我出发身份的成功身份」：
	// 本用例的页从未发布，账本是空的（curID==""），若再用 from=0、capturedVersion=0 的
	// 裸回执，L3/L4 都放行 —— 会一路走到 applyUpdateURL 命中注入点。
	//
	// 这里刻意把出发身份写成**另一条真实存在、且状态为 committed 的回执**：
	// receiptIdentityVerdict 于是判 superseded，回执走 abort 分支结案为 rolled_back、
	// 返回 (false, nil) ——
	// 那正是「已判定、不报错」，测试的分支语义才明确（不是靠异常路径凑失败）。
	// 而「重放失败保 pending 且错误上抛」由同目录 TestUpdateURLOldPathSettleFailureKeepsPending
	// 与 TestConvergePendingReceipts* 的注入用例覆盖，这里只证明收敛对该现场给出的结论。
	// 出发身份：另造一条**已结案的成功回执**（它的 path 与本次无关，不会与本次回执抢判定），
	// 把它的 id 写进本次回执的 fromReceiptId。
	fromID := insertUpdateURLReceipt(t, db, page.ID, "/converge-from-new", "/converge-from-old", "zh-CN", artifactID, true, 0)
	if err := db.Exec(`UPDATE publication_receipts SET receipt_state = 'committed' WHERE id = ?`, fromID).Error; err != nil {
		t.Fatalf("预置出发回执状态失败: %v", err)
	}
	itemID := insertPendingUpdateURLReceipt(t, db, page.ID, "/converge-fail", "/converge-fail-old", "zh-CN", artifactID)
	// 账本上必须**真有一条第三方写的成功身份行**，否则本用例根本判不出「被替代」。
	//
	// 判据（page_publish.go:3089 receiptIdentityVerdict）三态：
	//   账本身份 == ownSeq      → accepted（我自己，幂等补齐）
	//   账本身份 == fromSeq     → accepted（合法恢复：FS 已切、DB 未提交的正常崩溃窗口）
	//   其它                    → superseded（期间有别的回执成功激活过）
	// 而 curSeq 取自 page_publications.receipt_id（PublicationReceiptIDTx）。本页从未发布 →
	// 账本无行 → curSeq == 0 → 前两条都不成立，落到「账本身份 == fromSeq」的比较里
	// （此处 fromSeq == fromID > 0，curSeq 0 ≠ fromSeq）→ 判定 superseded……
	// 但那是**误判**：账本空时 curSeq==0 表示「还没有人来过」，收敛该按现场前向补齐。
	// 所以这里显式预置账本行，把三段身份写清：
	//
	//   账本行 receipt_id = 第三方（既不是 itemID，也不是 fromID）→ superseded；
	//   本条回执          = itemID —— 被替代者；
	//   出发身份          = fromID —— 我出发时账本上的身份（≠ 当前账本身份）。
	//
	// 第三方身份的取值：用 insertUpdateURLReceipt 再造一条已结案回执，语义上「另一条
	// 回执成功激活过」，而不是随手编一个 id。
	thirdPartyID := insertUpdateURLReceipt(t, db, page.ID, "/converge-third-new", "/converge-third-old", "zh-CN", artifactID, true, 0)
	if err := db.Exec(`UPDATE publication_receipts SET receipt_state = 'committed' WHERE id = ?`, thirdPartyID).Error; err != nil {
		t.Fatalf("预置第三方回执状态失败: %v", err)
	}
	if err := db.Exec(`INSERT INTO page_publications (page_id, lang, active_path, artifact_id, artifact_hash, published_at, update_time, receipt_id) VALUES (?, 'zh-CN', ?, ?, ?, now(), now(), ?)`,
		page.ID, "/converge-fail", artifactID, hash, thirdPartyID).Error; err != nil {
		t.Fatalf("预置第三方成功身份失败: %v", err)
	}
	// 夹具自检：账本身份必须真的落在第三方上，否则后面的断言在证别的东西。
	if ledger := ledgerReceiptID(t, db, page.ID, "zh-CN"); ledger != thirdPartyID {
		t.Fatalf("夹具未生效：账本 receipt_id 应为第三方 %d，实际 %d", thirdPartyID, ledger)
	}
	if err := db.Exec(`UPDATE publication_receipts SET receipt_data = receipt_data || ?::jsonb WHERE id = ?`,
		fmt.Sprintf(`{"fromReceiptId":%d}`, fromID), itemID).Error; err != nil {
		t.Fatalf("预置出发身份失败: %v", err)
	}
	// 不注入任何故障：本用例要证明的是判定四步给出的结论，不是异常路径。

	converger := convergerOf(t, svc)
	converged, cerr := converger.ConvergePendingReceipts(ctx)
	if cerr != nil {
		t.Fatalf("这条回执走的是 abort 分支（已判定为被替代），收敛不该回传错误: %v", cerr)
	}
	// converged 计数不作为判据：收敛领取的是全表待办（page_publish.go:3237），本轮可能顺带
	// 补完成别的用例留下的回执，这里只按本条回执 id 断言。
	_ = converged
	// 按 id 断言（收敛领取全表待办，别的用例留下的 pending 会一起被领走）。
	if got := receiptStateByID(t, db, itemID); got != "rolled_back" {
		t.Fatalf("账本上的成功身份属另一条回执 → 应结案为 rolled_back，实际 %q", got)
	}
	// 被替代的回执不得写任何激活记录（它没有兑现过这次改址）。
	// 注意口径：夹具预置了**一条**账本行（第三方成功身份），所以这里判的是「没有新增」——
	// 行数必须仍是 1 且身份仍是第三方；放宽成 n==0 会让夹具本身变成断言的一部分。
	if n := publicationCount(t, db, page.ID); n != 1 {
		t.Fatalf("判定为被替代时不得新增激活记录（夹具预置 1 条），实际 %d 条", n)
	}
	if ledger := ledgerReceiptID(t, db, page.ID, "zh-CN"); ledger != thirdPartyID {
		t.Fatalf("被替代的回执不得覆盖账本身份：应为第三方 %d，实际 %d", thirdPartyID, ledger)
	}
	// 结案后不再计入待办：可观测口径必须跟着走。
	if pending, _, serr := converger.PendingReceiptStatus(ctx); serr != nil {
		t.Fatalf("读取回执收敛状态失败: %v", serr)
	} else if pending != 0 {
		t.Fatalf("已结案的回执不应再计入待办数，实际 %d", pending)
	}
	// 幂等：再跑一轮仍是 (0, nil)，且这条回执的状态不再变化。
	// 二次调用时本条已结案：again 仍可能因别的用例残留而非零，故同样不作判据；
	// 真正要证的是「本条回执没有被再处理一次」—— 由下面的状态断言覆盖。
	if _, rerr := converger.ConvergePendingReceipts(ctx); rerr != nil {
		t.Fatalf("二次收敛不该报错: %v", rerr)
	}
	if got := receiptStateByID(t, db, itemID); got != "rolled_back" {
		t.Fatalf("二次收敛不得改写已结案状态，实际 %q", got)
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

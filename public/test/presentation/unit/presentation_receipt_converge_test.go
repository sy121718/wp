package unit

// presentation_receipt_converge_test.go —— 多语言发布回执的定时收敛（与 page 侧对称）。
//
// 背景：page 侧已交付 ConvergePendingReceipts（分批领取 + SKIP LOCKED + 写路径快通道 +
// 可观测），而 presentation 实例的访问面切换回执**只有进程启动时恢复**——
// 长时间不重启就一直 pending，线上与库长期不一致，且没有任何可见性。
// 本文件把新入口钉住，四条用例对应四条要求：
//
//	① pending 回执被收敛（补完成 + 结案），重复收敛零变化（幂等）；
//	② 重放仍失败时**保留 pending**、错误可见、不 panic（失败的行下一轮还能重放）；
//	③ 快通道能在定时器之外收敛（间隔取到用例时长之外，未推信号就不动）；
//	④ 领取口径不串台：presentation 的收敛领不走 page 口径的切换回执，反之亦然。

import (
	"context"
	"testing"
	"time"

	contentdto "go_wp/internal/module/content/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationservice "go_wp/internal/module/presentation/service"
	pubcontract "go_wp/internal/module/publication/contract"
)

// seedPartialPublish 造出一条 presentation 的 pending 回执：
// 第二个语言（en-US）的路由登记失败 —— 该语言的访问面已切换、回执保持 pending。
//
// 用真实的发布链而不是手插回执行：回执里的 from/to 产物、path、lang 都是主链写下的，
// 收敛判定（符号链接指向哪个产物、路径是否仍是该语言的当前路径）才有真实依据。
func seedPartialPublish(t *testing.T, f *presFixture, injector *routeFaultInjector, slug, logicalPath string) string {
	t.Helper()
	ctx := context.Background()
	entity, err := f.content.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: slug,
		Data: map[string]any{"title": "收敛用例 " + slug, "excerpt": "摘要"},
	})
	if err != nil {
		t.Fatalf("创建实体失败: %v", err)
	}
	injector.setFailPath("/en" + logicalPath)
	_, err = f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID, URLPath: logicalPath,
	})
	injector.setFailPath("")
	if err == nil {
		t.Fatal("注入路由登记故障后发布必须返回错误（访问面已切换、回执待收敛）")
	}
	var instID string
	if err = f.db.Raw("SELECT id FROM presentation_instances WHERE project_id = ? AND entity_id = ?",
		f.projectID, entity.ID).Scan(&instID).Error; err != nil || instID == "" {
		t.Fatalf("实例应已建行（发布失败不等于实例不存在）: instID=%q err=%v", instID, err)
	}
	if pending, _ := receiptStates(t, f, instID); pending != 1 {
		t.Fatalf("故障注入后应恰好 1 条 pending 回执，实际 %d", pending)
	}
	return instID
}

// newConvergeFixture 建一个带路由故障注入的 fixture，并启用两种语言 + 模板。
func newConvergeFixture(t *testing.T, injector *routeFaultInjector) *presFixture {
	t.Helper()
	f := newPresFixtureWithRoutes(t, func(routes pubcontract.PublicationService) pubcontract.PublicationService {
		injector.PublicationService = routes
		return injector
	})
	if f == nil {
		return nil
	}
	enableTwoLangs(t, f)
	f.createTemplate(t)
	return f
}

// TestPresentationConvergePendingReceiptsCompletesAndIsIdempotent
// ① pending 回执被收敛（补完成 + 结案），且重复收敛零变化。
func TestPresentationConvergePendingReceiptsCompletesAndIsIdempotent(t *testing.T) {
	injector := &routeFaultInjector{}
	f := newConvergeFixture(t, injector)
	if f == nil {
		return
	}
	ctx := context.Background()
	instID := seedPartialPublish(t, f, injector, "converge-shirt", "/products/converge-shirt")

	// 故障已消除：收敛按访问面证据补齐路由登记与语言账本，回执结案。
	converged, cerr := f.pres.ConvergePendingReceipts(ctx)
	if cerr != nil {
		t.Fatalf("收敛失败: %v", cerr)
	}
	if converged != 1 {
		t.Fatalf("应收敛 1 条回执，实际 %d", converged)
	}
	if pending, committed := receiptStates(t, f, instID); pending != 0 || committed < 1 {
		t.Fatalf("收敛后回执不应再有 pending，实际 pending=%d committed=%d", pending, committed)
	}
	if n := countActiveRoutes(t, f, "/en/products/converge-shirt"); n != 1 {
		t.Fatalf("收敛应补齐 en-US 的路由登记（恰好 1 条 active），实际 %d", n)
	}

	// 可观测：pending 归零 + 最近一次收敛时刻已记录。
	pending, lastConvergeAt, serr := f.pres.PendingReceiptStatus(ctx)
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
	again, aerr := f.pres.ConvergePendingReceipts(ctx)
	if aerr != nil {
		t.Fatalf("重复收敛失败: %v", aerr)
	}
	if again != 0 {
		t.Fatalf("重复收敛不应再判定任何回执，实际 %d", again)
	}
	if pending, _ := receiptStates(t, f, instID); pending != 0 {
		t.Fatalf("重复收敛不得改变已结案回执，实际 pending=%d", pending)
	}
	if n := countActiveRoutes(t, f, "/en/products/converge-shirt"); n != 1 {
		t.Fatalf("重复收敛不应产生重复路由行，实际 %d", n)
	}
}

// TestPresentationConvergeKeepsPendingWhenReplayFails ② 重放仍失败：保留 pending、错误可见、不 panic。
func TestPresentationConvergeKeepsPendingWhenReplayFails(t *testing.T) {
	injector := &routeFaultInjector{}
	f := newConvergeFixture(t, injector)
	if f == nil {
		return
	}
	ctx := context.Background()
	instID := seedPartialPublish(t, f, injector, "converge-fail-shirt", "/products/converge-fail-shirt")

	// 故障注入：重放同样会失败（路由登记这一步就是重放要补的那一步）。
	injector.setFailing(true)

	converged, cerr := f.pres.ConvergePendingReceipts(ctx)
	if cerr == nil {
		t.Fatal("重放失败必须回传错误（否则失败会被当成收敛完成）")
	}
	if converged != 0 {
		t.Fatalf("重放失败不应计入已收敛，实际 %d", converged)
	}
	// 关键：失败的 pending 绝不能被标死 —— 标成 rolled_back 等于把「状态未知」
	// 换成「确定没生效」，比不收敛更糟。
	if pending, _ := receiptStates(t, f, instID); pending != 1 {
		t.Fatalf("重放失败的回执必须保留 pending，实际 %d", pending)
	}
	// 失败的 pending 仍在待办里可见（可观测口径与领取口径一致）。
	pending, _, serr := f.pres.PendingReceiptStatus(ctx)
	if serr != nil {
		t.Fatalf("读取回执收敛状态失败: %v", serr)
	}
	if pending != 1 {
		t.Fatalf("失败的 pending 应计入待办数，实际 %d", pending)
	}

	// 清掉注入点（等价于重启后的干净状态）后能收敛 —— 证明上面保留的 pending 是可重放的。
	injector.setFailing(false)
	recovered, rerr := f.pres.ConvergePendingReceipts(ctx)
	if rerr != nil {
		t.Fatalf("清除注入点后应收敛成功: %v", rerr)
	}
	if recovered != 1 {
		t.Fatalf("清除注入点后应收敛 1 条，实际 %d", recovered)
	}
	if pending, committed := receiptStates(t, f, instID); pending != 0 || committed < 1 {
		t.Fatalf("重放成功后回执应结案，实际 pending=%d committed=%d", pending, committed)
	}
}

// TestPresentationConvergeFastPathWithoutTicker ③ 写路径的快通道能在定时器之外收敛。
//
// 判定方式：调度器间隔取 1 小时（用例时长内定时器不可能触发），并在**启动首跑结束之后**
// 才插入一条 pending 回执 —— 此刻能被收敛就只可能来自快通道（NotifyPendingReceipt）。
//
// 为什么手插回执而不是走「故障注入的生产失败」：生产失败路径自己会推快通道
// （presentation_i18n.go 的主链收口），那样就分不清收敛是信号驱动的还是定时器趁机做的。
// 手插的行指向一个真实实例的既有活跃产物，重放要补的正是「路由登记 + 语言账本」这两步。
func TestPresentationConvergeFastPathWithoutTicker(t *testing.T) {
	injector := &routeFaultInjector{}
	f := newConvergeFixture(t, injector)
	if f == nil {
		return
	}
	ctx := context.Background()

	presentationServiceStarting(t, f)

	// 等启动首跑结束：首跑无论有没有待办都会写「最近一次收敛时刻」。
	waitForStatus(t, f, 10*time.Second, func(last time.Time) bool { return !last.IsZero() },
		"收敛调度没有在预期时间内完成启动首跑")

	// 一次**正常**发布（不注入故障）：它的回执全部当场结案，不产生 pending。
	entity, err := f.content.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "converge-wake-shirt",
		Data: map[string]any{"title": "快通道衬衫", "excerpt": "摘要"},
	})
	if err != nil {
		t.Fatalf("创建实体失败: %v", err)
	}
	const logicalPath = "/products/converge-wake-shirt"
	inst, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID, URLPath: logicalPath,
	})
	if err != nil {
		t.Fatalf("CreateInstance 失败: %v", err)
	}
	var artifactID string
	if err = f.db.Raw("SELECT artifact_id FROM presentation_publications WHERE presentation_id = ? AND lang = ?",
		inst.ID, "zh-CN").Scan(&artifactID).Error; err != nil || artifactID == "" {
		t.Fatalf("读取默认语言账本失败: artifactID=%q err=%v", artifactID, err)
	}

	// 首跑之后才造 pending：这一条只可能被快通道收掉。
	insertSyntheticPresentationReceipt(t, f, inst.ID, f.projectID, logicalPath, "zh-CN", artifactID)

	// 先确认「没推信号就不会收敛」：否则这条用例证明不了快通道真的被走到。
	time.Sleep(300 * time.Millisecond)
	if pending, _ := receiptStates(t, f, inst.ID); pending != 1 {
		t.Fatalf("定时器间隔 1 小时，未推快通道前不应被收敛，实际 pending=%d", pending)
	}

	f.pres.NotifyPendingReceipt()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if pending, _ := receiptStatesQuiet(t, f, inst.ID); pending == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("推快通道后回执没有被收敛（信号未驱动收敛）")
}

// TestPresentationConvergeDoesNotClaimForeignReceipts ④ 领取口径不串台。
//
// 同一张 publication_receipts 上叠着多套恢复职责：page 的三种访问面切换动作与
// presentation 的（唯一）switch_active。领取口径一旦分叉或放宽，要么领到不属于自己的行
// 反复重放、要么自己的残留永远没人收。这里同时从「领取端」与「收敛端」两侧断言。
func TestPresentationConvergeDoesNotClaimForeignReceipts(t *testing.T) {
	injector := &routeFaultInjector{}
	f := newConvergeFixture(t, injector)
	if f == nil {
		return
	}
	ctx := context.Background()
	instID := seedPartialPublish(t, f, injector, "converge-scope-shirt", "/products/converge-scope-shirt")

	// 合成一条**手工页面口径**的 pending 回执（source_type = page）：有自己的收敛例程，
	// 不该被 presentation 的收敛领走。path 用只在本用例出现的串，便于断言它没被动过。
	const pagePath = "/converge-scope-page"
	insertRawPendingReceipt(t, f, "page", "00000000-0000-0000-0000-0000000000aa", pagePath)

	// 领取端：presentation 口径只领到 presentation 的那一行。
	items, err := f.routes.ClaimPendingReceipts(ctx, &pubcontract.ReceiptsQueryReq{
		SourceType: pubcontract.ReceiptSourcePresentation,
		Actions:    []string{pubcontract.ReceiptActionSwitchActive},
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("按 presentation 口径领取失败: %v", err)
	}
	if len(items) != 1 || items[0].SourceType != pubcontract.ReceiptSourcePresentation || items[0].SourceID != instID {
		t.Fatalf("presentation 口径应只领到实例 %s 的那 1 条回执，实际 %+v", instID, items)
	}
	// 反之亦然：page 口径（本模块的唯一动作词表 + page 归属）只领到 page 的那一行。
	pageItems, perr := f.routes.ClaimPendingReceipts(ctx, &pubcontract.ReceiptsQueryReq{
		SourceType: pubcontract.ReceiptSourcePage,
		Actions:    []string{pubcontract.ReceiptActionSwitchActive},
		Limit:      10,
	})
	if perr != nil {
		t.Fatalf("按 page 口径领取失败: %v", perr)
	}
	if len(pageItems) != 1 || pageItems[0].SourceType != pubcontract.ReceiptSourcePage || pageItems[0].Path != pagePath {
		t.Fatalf("page 口径应只领到 %s 的那 1 条回执，实际 %+v", pagePath, pageItems)
	}

	// 收敛端：presentation 的收敛只收自己那一批，page 的切换回执原地不动。
	if _, cerr := f.pres.ConvergePendingReceipts(ctx); cerr != nil {
		t.Fatalf("收敛失败: %v", cerr)
	}
	if pending, _ := receiptStates(t, f, instID); pending != 0 {
		t.Fatalf("实例的回执应已结案，实际 pending=%d", pending)
	}
	if got := receiptStateByPath(t, f, pagePath); got != "pending" {
		t.Fatalf("手工页面口径的回执不该被 presentation 的收敛动过，实际 %q", got)
	}
}

// presentationServiceStarting 以 1 小时的定时器间隔启动收敛调度（定时器在用例时长内不会触发）。
func presentationServiceStarting(t *testing.T, f *presFixture) {
	t.Helper()
	presentationservice.StartPendingReceiptConvergenceSchedulerWithInterval(f.pres, time.Hour)
}

// waitForStatus 轮询等待「最近一次收敛时刻」满足条件（在测试 goroutine 内轮询，超时即 Fatal）。
func waitForStatus(t *testing.T, f *presFixture, timeout time.Duration,
	cond func(time.Time) bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, last, err := f.pres.PendingReceiptStatus(context.Background()); err == nil && cond(last) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(msg)
}

// receiptStatesQuiet 与 receiptStates 同口径，但查询失败不 Fatal（轮询等待用）。
func receiptStatesQuiet(t *testing.T, f *presFixture, instanceID string) (pending, committed int) {
	t.Helper()
	var rows []string
	if err := f.db.Raw(
		"SELECT receipt_state FROM publication_receipts WHERE source_type = 'presentation' AND source_id = ? AND action = 'switch_active' ORDER BY id",
		instanceID).Scan(&rows).Error; err != nil {
		return -1, -1
	}
	for _, s := range rows {
		switch s {
		case "pending":
			pending++
		case "committed":
			committed++
		}
	}
	return pending, committed
}

// insertRawPendingReceipt 直插一条 pending 回执（source_id 用固定 uuid，本表无外键）。
func insertRawPendingReceipt(t *testing.T, f *presFixture, sourceType, sourceID, path string) {
	t.Helper()
	if err := f.db.Exec(
		"INSERT INTO publication_receipts (source_type, source_id, action, path, receipt_state, receipt_data, create_time) VALUES (?, ?, 'switch_active', ?, 'pending', '{}'::jsonb, now())",
		sourceType, sourceID, path).Error; err != nil {
		t.Fatalf("插入 pending 回执失败: %v", err)
	}
}

// insertSyntheticPresentationReceipt 直插一条**指向真实实例与真实活跃产物**的 pending 回执。
//
// receipt_data 必须带 projectId 与 lang：恢复例程用它们定位实例（RLS 作用域）与判定
// 「路径是否仍是该语言的当前访问路径」，缺了就只能走保守分支。
func insertSyntheticPresentationReceipt(t *testing.T, f *presFixture, instanceID, projectID, path, lang, artifactID string) {
	t.Helper()
	data := `{"projectId":"` + projectID + `","lang":"` + lang + `"}`
	if err := f.db.Exec(
		"INSERT INTO publication_receipts (source_type, source_id, action, path, to_artifact_id, receipt_state, receipt_data, create_time) VALUES ('presentation', ?, 'switch_active', ?, ?, 'pending', ?::jsonb, now())",
		instanceID, path, artifactID, data).Error; err != nil {
		t.Fatalf("插入 pending 回执失败: %v", err)
	}
}

// receiptStateByPath 按路径取回执状态（口径不串台用例用）。
func receiptStateByPath(t *testing.T, f *presFixture, path string) string {
	t.Helper()
	var state string
	if err := f.db.Raw(
		"SELECT receipt_state FROM publication_receipts WHERE path = ? ORDER BY id DESC LIMIT 1",
		path).Scan(&state).Error; err != nil {
		t.Fatalf("查询回执状态失败: %v", err)
	}
	return state
}

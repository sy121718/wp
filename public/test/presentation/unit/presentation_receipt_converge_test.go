package unit

import (
	"context"
	"testing"
	"time"

	contentdto "go_wp/internal/module/content/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationservice "go_wp/internal/module/presentation/service"
	pubcontract "go_wp/internal/module/publication/contract"
)

func seedPartialPublish(t *testing.T, f *presFixture, _ *routeFaultInjector, slug, logicalPath string) string {
	t.Helper()
	ctx := context.Background()
	entity, err := f.content.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: slug,
		Data: map[string]any{"title": "收敛用例 " + slug, "excerpt": "摘要"},
	})
	if err != nil {
		t.Fatalf("创建实体失败: %v", err)
	}
	inst, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID, URLPath: logicalPath,
	})
	if err != nil {
		t.Fatalf("正常发布失败（收敛用例的前置是「发布成功过」）: %v", err)
	}
	enPath := "/en" + logicalPath
	var artifactID string
	if err = f.db.Raw("SELECT artifact_id FROM presentation_publications WHERE presentation_id = ? AND lang = ?",
		inst.ID, "en-US").Scan(&artifactID).Error; err != nil || artifactID == "" {
		t.Fatalf("读取 en-US 账本产物失败: id=%q err=%v", artifactID, err)
	}

	if err = f.db.Exec("DELETE FROM page_routes WHERE presentation_id = ? AND path = ?", inst.ID, enPath).Error; err != nil {
		t.Fatalf("删除 en-US 路由行失败: %v", err)
	}
	if err = f.db.Exec("DELETE FROM presentation_publications WHERE presentation_id = ? AND lang = ?", inst.ID, "en-US").Error; err != nil {
		t.Fatalf("删除 en-US 账本行失败: %v", err)
	}
	insertSyntheticPresentationReceipt(t, f, inst.ID, f.projectID, enPath, "en-US", artifactID)
	if pending, _ := receiptStates(t, f, inst.ID); pending != 1 {
		t.Fatalf("现场应恰好一条 pending 回执，实际 %d", pending)
	}
	return inst.ID
}

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

func TestPresentationConvergePendingReceiptsCompletesAndIsIdempotent(t *testing.T) {
	injector := &routeFaultInjector{}
	f := newConvergeFixture(t, injector)
	if f == nil {
		return
	}
	ctx := context.Background()
	instID := seedPartialPublish(t, f, injector, "converge-shirt", "/products/converge-shirt")

	converged, cerr := f.pres.ConvergePendingReceipts(ctx)
	if cerr != nil {
		t.Fatalf("收敛失败: %v", cerr)
	}
	if converged != 1 {
		t.Fatalf("应收敛 en-US 的 pending 回执，实际 %d", converged)
	}
	if pending, committed := receiptStates(t, f, instID); pending != 0 || committed < 1 {
		t.Fatalf("收敛后回执不应再有 pending，实际 pending=%d committed=%d", pending, committed)
	}
	if n := countActiveRoutes(t, f, "/en/products/converge-shirt"); n != 1 {
		t.Fatalf("收敛应补齐 en-US 的路由登记（恰好 1 条 active），实际 %d", n)
	}

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

func TestPresentationConvergeKeepsPendingWhenReplayFails(t *testing.T) {
	injector := &routeFaultInjector{}
	f := newConvergeFixture(t, injector)
	if f == nil {
		return
	}
	ctx := context.Background()
	instID := seedPartialPublish(t, f, injector, "converge-fail-shirt", "/products/converge-fail-shirt")

	injector.setFailing(true)

	converged, cerr := f.pres.ConvergePendingReceipts(ctx)
	if cerr == nil {
		t.Fatal("重放失败必须回传错误（否则失败会被当成收敛完成）")
	}
	if converged != 0 {
		t.Fatalf("重放失败不应计入已收敛，实际 %d", converged)
	}

	if pending, _ := receiptStates(t, f, instID); pending != 1 {
		t.Fatalf("重放失败的回执必须保留 pending，实际 %d", pending)
	}

	pending, _, serr := f.pres.PendingReceiptStatus(ctx)
	if serr != nil {
		t.Fatalf("读取回执收敛状态失败: %v", serr)
	}
	if pending != 1 {
		t.Fatalf("失败的 pending 应计入待办数，实际 %d", pending)
	}

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

func TestPresentationConvergeFastPathWithoutTicker(t *testing.T) {
	injector := &routeFaultInjector{}
	f := newConvergeFixture(t, injector)
	if f == nil {
		return
	}
	ctx := context.Background()

	presentationServiceStarting(t, f)

	waitForStatus(t, f, 10*time.Second, func(last time.Time) bool { return !last.IsZero() },
		"收敛调度没有在预期时间内完成启动首跑")

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

	insertSyntheticPresentationReceipt(t, f, inst.ID, f.projectID, logicalPath, "zh-CN", artifactID)

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

func TestPresentationConvergeDoesNotClaimForeignReceipts(t *testing.T) {
	injector := &routeFaultInjector{}
	f := newConvergeFixture(t, injector)
	if f == nil {
		return
	}
	ctx := context.Background()
	instID := seedPartialPublish(t, f, injector, "converge-scope-shirt", "/products/converge-scope-shirt")

	const pagePath = "/converge-scope-page"
	insertRawPendingReceipt(t, f, "page", "00000000-0000-0000-0000-0000000000aa", pagePath)

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

func presentationServiceStarting(t *testing.T, f *presFixture) {
	t.Helper()
	presentationservice.StartPendingReceiptConvergenceSchedulerWithInterval(f.pres, time.Hour)
}

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

func insertRawPendingReceipt(t *testing.T, f *presFixture, sourceType, sourceID, path string) {
	t.Helper()
	if err := f.db.Exec(
		"INSERT INTO publication_receipts (source_type, source_id, action, path, receipt_state, receipt_data, create_time) VALUES (?, ?, 'switch_active', ?, 'pending', '{}'::jsonb, now())",
		sourceType, sourceID, path).Error; err != nil {
		t.Fatalf("插入 pending 回执失败: %v", err)
	}
}

func insertSyntheticPresentationReceipt(t *testing.T, f *presFixture, instanceID, projectID, path, lang, artifactID string) {
	t.Helper()
	data := `{"projectId":"` + projectID + `","lang":"` + lang + `"}`
	if err := f.db.Exec(
		"INSERT INTO publication_receipts (source_type, source_id, action, path, to_artifact_id, receipt_state, receipt_data, create_time) VALUES ('presentation', ?, 'switch_active', ?, ?, 'pending', ?::jsonb, now())",
		instanceID, path, artifactID, data).Error; err != nil {
		t.Fatalf("插入 pending 回执失败: %v", err)
	}
}

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

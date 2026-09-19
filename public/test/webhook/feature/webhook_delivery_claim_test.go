package feature

// webhook_delivery_claim_test.go — 投递「认领 + 租约」状态机的回归。
//
// 背景：认领态是为修掉「同一 pending 行被两个 worker 同时投递」的窄窗口而加的。
// 那条窄窗口在旧代码里几乎不可见（两个 worker 都读到 pending、都发出请求，只在事后留一条
// 「并发重复投递」的警告日志），所以它的回归必须落在**状态机本身**上：
//
//   1. 认领是互斥的 —— 第二次认领必须拿到 0 行（这就是「第二个 worker 不会发请求」的证明）；
//   2. 终态只写一次 —— 落定走 WHERE status=delivering，第二次落定拿到 0 行；
//   3. 未经认领的行**写不进终态** —— 「先认领再投递」不再只是调用方的自觉；
//   4. 租约过期后可抢占、未过期不可抢占；
//   5. 退认领（认领后读不到行）能回到 pending，且立刻可再次认领；
//   6. 重放名单同时覆盖「陈旧 pending」与「租约过期的 delivering」，且两类分开计数；
//   7. 人工重投只认 failed —— delivering 的行不能被改回 pending。
//
// 用真实 PG（support.NewMigratedPGTestDB）：条件更新、update_time 的比较、gorm 的 map 更新与
// 自动时间列的相互作用只有真库能验。尤其第 4 条里那条断言 —— 「写进 update_time 的值确实是
// 传进去的那个」（gorm 对 UpdatedAt 有自动写入的约定，若它盖掉我们的值，租约判定会静默失效，
// 这里必须因此变红）。

import (
	"context"
	"testing"
	"time"

	webhookenums "go_wp/internal/module/webhook/enums"
	webhookmodel "go_wp/internal/module/webhook/model"
	webhookservice "go_wp/internal/module/webhook/service"
	"go_wp/public/test/support"
)

// newClaimFixture 建一个端点 + 一条 pending 投递，返回模型与投递。
func newClaimFixture(t *testing.T) (*webhookmodel.WebhookModel, *webhookmodel.WebhookDeliveryEntity) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	m := webhookmodel.NewWebhookModel(db)
	ctx := context.Background()
	now := time.Now()

	ep := &webhookmodel.WebhookEndpointEntity{
		EventType: "order.paid", TargetURL: "https://example.com/hook",
		// 仅占位：本用例不投递、不解密。
		SecretCipher: "not-a-real-cipher", Status: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := m.CreateEndpoint(ctx, ep); err != nil {
		t.Fatalf("建端点失败: %v", err)
	}
	d := &webhookmodel.WebhookDeliveryEntity{
		EndpointID: ep.ID, EventType: "order.paid", Payload: "{}",
		Status: webhookenums.DeliveryStatusPending, CreatedAt: now, UpdatedAt: now,
	}
	if err := m.CreateDelivery(ctx, d); err != nil {
		t.Fatalf("建投递日志失败: %v", err)
	}
	return m, d
}

// reload 重新读一行：断言落库后的真实状态，而不是内存里的对象。
func reload(t *testing.T, m *webhookmodel.WebhookModel, id uint64) *webhookmodel.WebhookDeliveryEntity {
	t.Helper()
	e, err := m.GetDelivery(context.Background(), id)
	if err != nil {
		t.Fatalf("读投递失败: %v", err)
	}
	return e
}

// TestWebhookDeliveryClaimExclusive 认领互斥 + 终态只写一次 + 未认领不可落定。
func TestWebhookDeliveryClaimExclusive(t *testing.T) {
	ctx := context.Background()
	m, d := newClaimFixture(t)

	// 未经认领就落定：必须被拒（WHERE status=delivering 的效果）。
	if ok, err := m.MarkDeliveryResult(ctx, d.ID, webhookenums.DeliveryStatusDelivered, 200, "", time.Now()); err != nil || ok {
		t.Fatalf("未认领的行不应能落定终态：affected=%v err=%v", ok, err)
	}

	at := time.Now()
	claimed, err := m.ClaimDelivery(ctx, d.ID, 5*time.Minute, at)
	if err != nil || !claimed {
		t.Fatalf("首次认领应成功：claimed=%v err=%v", claimed, err)
	}
	if got := reload(t, m, d.ID).Status; got != webhookenums.DeliveryStatusDelivering {
		t.Fatalf("认领后状态应为 delivering，实际 %q", got)
	}

	// 第二个 worker（租约未过期）必须拿到 0 行 —— 它因此不会发出请求。
	again, err := m.ClaimDelivery(ctx, d.ID, 5*time.Minute, at.Add(time.Second))
	if err != nil {
		t.Fatalf("第二次认领报错: %v", err)
	}
	if again {
		t.Fatal("租约未过期时第二次认领必须失败（否则就是老的重复投递窄窗口）")
	}

	settled, err := m.MarkDeliveryResult(ctx, d.ID, webhookenums.DeliveryStatusDelivered, 200, "", time.Now())
	if err != nil || !settled {
		t.Fatalf("已认领的行应能落定：affected=%v err=%v", settled, err)
	}
	if got := reload(t, m, d.ID); got.Status != webhookenums.DeliveryStatusDelivered || got.Attempts != 1 {
		t.Fatalf("落定结果不对：status=%q attempts=%d（应 delivered/1）", got.Status, got.Attempts)
	}

	// 终态只写一次：第二次落定必须拿到 0 行，且不改写已经有结果。
	double, err := m.MarkDeliveryResult(ctx, d.ID, webhookenums.DeliveryStatusFailed, 0, "boom", time.Now())
	if err != nil {
		t.Fatalf("第二次落定报错: %v", err)
	}
	if double {
		t.Fatal("终态只能写一次：第二次落定必须拿到 0 行")
	}
	if got := reload(t, m, d.ID); got.Status != webhookenums.DeliveryStatusDelivered || got.Attempts != 1 {
		t.Fatalf("第二次落定改写了结果：status=%q attempts=%d", got.Status, got.Attempts)
	}
}

// TestWebhookDeliveryLeaseReclaim 租约未过期不可抢占、过期后可抢占。
func TestWebhookDeliveryLeaseReclaim(t *testing.T) {
	ctx := context.Background()
	m, d := newClaimFixture(t)

	claimAt := time.Now()
	if ok, err := m.ClaimDelivery(ctx, d.ID, 5*time.Minute, claimAt); err != nil || !ok {
		t.Fatalf("首次认领应成功：%v/%v", ok, err)
	}
	// 认领时刻必须真的写进 update_time（租约起点）。gorm 对 UpdatedAt 有自动写入约定，
	// 若它盖掉我们的值，租约起点会变成 now，下面「过期可抢占」就会失败 —— 这条断言是
	// 把那个静默失效钉住的唯一手段。
	if got := reload(t, m, d.ID).UpdatedAt; !got.Equal(claimAt.Truncate(time.Microsecond)) {
		t.Fatalf("认领应把 update_time 写成租约起点：期望 %s，实际 %s",
			claimAt.Truncate(time.Microsecond), got)
	}

	// 认领后 4 分钟：仍在租约内。
	if ok, err := m.ClaimDelivery(ctx, d.ID, 5*time.Minute, claimAt.Add(4*time.Minute)); err != nil || ok {
		t.Fatalf("租约内不应可抢占：%v/%v", ok, err)
	}
	// 认领后 6 分钟：租约已过期 → 可抢占（worker 崩溃 / 卡死的唯一补救路径）。
	if ok, err := m.ClaimDelivery(ctx, d.ID, 5*time.Minute, claimAt.Add(6*time.Minute)); err != nil || !ok {
		t.Fatalf("租约过期后应可抢占：%v/%v", ok, err)
	}
	// 抢占者落定一次；被抢占者若随后才回来，会拿到 0 行（终态依旧只写一次）。
	if ok, err := m.MarkDeliveryResult(ctx, d.ID, webhookenums.DeliveryStatusFailed, 500, "late", time.Now()); err != nil || !ok {
		t.Fatalf("抢占后的落定应成功一次：%v/%v", ok, err)
	}
}

// TestWebhookDeliveryReleaseClaim 退认领回到 pending 且立刻可再认领。
func TestWebhookDeliveryReleaseClaim(t *testing.T) {
	ctx := context.Background()
	m, d := newClaimFixture(t)

	if ok, err := m.ClaimDelivery(ctx, d.ID, 5*time.Minute, time.Now()); err != nil || !ok {
		t.Fatalf("首次认领应成功：%v/%v", ok, err)
	}
	if err := m.ReleaseDeliveryClaim(ctx, d.ID, time.Now()); err != nil {
		t.Fatalf("退认领失败: %v", err)
	}
	if got := reload(t, m, d.ID); got.Status != webhookenums.DeliveryStatusPending || got.Attempts != 0 {
		t.Fatalf("退认领后应为 pending 且 attempts 不变：status=%q attempts=%d", got.Status, got.Attempts)
	}
	// 退认领立刻生效：不必等一整个租约。
	if ok, err := m.ClaimDelivery(ctx, d.ID, 5*time.Minute, time.Now()); err != nil || !ok {
		t.Fatalf("退认领后应立刻可再认领：%v/%v", ok, err)
	}
}

// TestWebhookStaleDeliveryListing 重放名单同时覆盖陈旧 pending 与租约过期的 delivering。
func TestWebhookStaleDeliveryListing(t *testing.T) {
	ctx := context.Background()
	m, fresh := newClaimFixture(t)

	// ① 陈旧 pending：create_time 一小时前（入队丢了 / 队列没启用）。
	old := &webhookmodel.WebhookDeliveryEntity{
		EndpointID: fresh.EndpointID, EventType: "order.paid", Payload: "{}",
		Status:    webhookenums.DeliveryStatusPending,
		CreatedAt: time.Now().Add(-time.Hour), UpdatedAt: time.Now().Add(-time.Hour),
	}
	if err := m.CreateDelivery(ctx, old); err != nil {
		t.Fatalf("建陈旧 pending 失败: %v", err)
	}

	// ② 租约过期的 delivering：一小时前认领（worker 崩溃 / 卡死）。
	//
	// 刻意让 create_time 是**刚才**（"刚创建就认领然后卡死"），update_time 才是一小时前 ——
	// 这样这一条只可能被「delivering 看 update_time」捞到：若实现把两类行都按 create_time 判，
	// 它会被判成「刚创建的新鲜行」而漏掉，卡死的投递就永远救不回来。
	stuck := &webhookmodel.WebhookDeliveryEntity{
		EndpointID: fresh.EndpointID, EventType: "order.paid", Payload: "{}",
		Status:    webhookenums.DeliveryStatusPending,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := m.CreateDelivery(ctx, stuck); err != nil {
		t.Fatalf("建卡死投递失败: %v", err)
	}
	if ok, err := m.ClaimDelivery(ctx, stuck.ID, 5*time.Minute, time.Now().Add(-time.Hour)); err != nil || !ok {
		t.Fatalf("按一小时前认领应成功：%v/%v", ok, err)
	}

	rows, err := m.ListStaleDeliveries(ctx, time.Now().Add(-5*time.Minute), time.Now().Add(-5*time.Minute), 50)
	if err != nil {
		t.Fatalf("列陈旧投递失败: %v", err)
	}
	seen := map[uint64]string{}
	for _, r := range rows {
		seen[r.ID] = r.Status
	}
	if got, ok := seen[old.ID]; !ok || got != webhookenums.DeliveryStatusPending {
		t.Fatalf("陈旧 pending 应进入重放名单：%v", seen)
	}
	if got, ok := seen[stuck.ID]; !ok || got != webhookenums.DeliveryStatusDelivering {
		t.Fatalf("租约过期的 delivering 应进入重放名单：%v", seen)
	}
	if _, ok := seen[fresh.ID]; ok {
		t.Fatalf("刚创建的 pending 不该进重放名单（会变成重复投递）：%v", seen)
	}

	// ③ 重放报告把两类分开计数（病因完全不同：一个是队列，一个是 worker）。
	svc := webhookservice.NewService(m)
	report, err := svc.ReplayPendingDeliveries(ctx, &webhookservice.ReplayPendingDeliveriesReq{Limit: 50})
	if err != nil {
		t.Fatalf("重放失败: %v", err)
	}
	if report.PendingTotal == 0 || report.DeliveringTotal == 0 {
		t.Fatalf("报告应分别给出两类积压：pending=%d delivering=%d",
			report.PendingTotal, report.DeliveringTotal)
	}
	if report.Scanned < 2 {
		t.Fatalf("应至少扫到两条陈旧行，实际 %d", report.Scanned)
	}
	// 队列在测试环境里通常未启用：入队会失败并落进 Failed（那一行状态不变，可再重放）。
	// 这里断言**守恒**而不是队列行为 —— 队列启没启用都不该让条目凭空消失。
	if report.Requeued+report.Failed != report.Scanned {
		t.Fatalf("扫描/入队/失败应守恒：scanned=%d requeued=%d failed=%d",
			report.Scanned, report.Requeued, report.Failed)
	}
	// 无论入队成没成，重放都不改状态（真源是行，队列只是加速器）。
	if got := reload(t, m, old.ID).Status; got != webhookenums.DeliveryStatusPending {
		t.Fatalf("重放不该改 pending 行的状态，实际 %q", got)
	}
}

// TestWebhookRetryOnlyFromFailed 人工重投只认 failed：delivering 不能重投。
func TestWebhookRetryOnlyFromFailed(t *testing.T) {
	ctx := context.Background()
	m, d := newClaimFixture(t)

	// 正在投（delivering）的行不能被改回 pending —— 那会让队列再派一个 worker，
	// 正是认领态要消除的重复投递。
	if ok, err := m.ClaimDelivery(ctx, d.ID, 5*time.Minute, time.Now()); err != nil || !ok {
		t.Fatalf("认领应成功：%v/%v", ok, err)
	}
	if ok, err := m.MarkDeliveryRetryable(ctx, d.ID, time.Now()); err != nil || ok {
		t.Fatalf("delivering 的行不该可重投：affected=%v err=%v", ok, err)
	}
	if got := reload(t, m, d.ID).Status; got != webhookenums.DeliveryStatusDelivering {
		t.Fatalf("状态应保持 delivering，实际 %q", got)
	}

	// failed 的行可以重投（既有语义不变），且 attempts 保留、last_error 清空。
	if _, err := m.MarkDeliveryResult(ctx, d.ID, webhookenums.DeliveryStatusFailed, 500, "boom", time.Now()); err != nil {
		t.Fatalf("落定 failed 失败: %v", err)
	}
	if ok, err := m.MarkDeliveryRetryable(ctx, d.ID, time.Now()); err != nil || !ok {
		t.Fatalf("failed 的行应可重投：%v/%v", ok, err)
	}
	got := reload(t, m, d.ID)
	if got.Status != webhookenums.DeliveryStatusPending || got.Attempts != 1 || got.LastError != "" {
		t.Fatalf("重投后应为 pending / attempts=1 / last_error 清空：status=%q attempts=%d err=%q",
			got.Status, got.Attempts, got.LastError)
	}
}

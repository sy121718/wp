package feature

import (
	"context"
	"testing"
	"time"

	webhookenums "go_wp/internal/module/webhook/enums"
	webhookmodel "go_wp/internal/module/webhook/model"
	webhookservice "go_wp/internal/module/webhook/service"
	"go_wp/public/test/support"
)

func newClaimFixture(t *testing.T) (*webhookmodel.WebhookModel, *webhookmodel.WebhookDeliveryEntity) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	m := webhookmodel.NewWebhookModel(db)
	ctx := context.Background()
	now := time.Now()

	ep := &webhookmodel.WebhookEndpointEntity{
		EventType: "order.paid", TargetURL: "https://example.com/hook",

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

func reload(t *testing.T, m *webhookmodel.WebhookModel, id uint64) *webhookmodel.WebhookDeliveryEntity {
	t.Helper()
	e, err := m.GetDelivery(context.Background(), id)
	if err != nil {
		t.Fatalf("读投递失败: %v", err)
	}
	return e
}

func TestWebhookDeliveryClaimExclusive(t *testing.T) {
	ctx := context.Background()
	m, d := newClaimFixture(t)

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

func TestWebhookDeliveryLeaseReclaim(t *testing.T) {
	ctx := context.Background()
	m, d := newClaimFixture(t)

	claimAt := time.Now()
	if ok, err := m.ClaimDelivery(ctx, d.ID, 5*time.Minute, claimAt); err != nil || !ok {
		t.Fatalf("首次认领应成功：%v/%v", ok, err)
	}

	if got := reload(t, m, d.ID).UpdatedAt; !got.Equal(claimAt.Truncate(time.Microsecond)) {
		t.Fatalf("认领应把 update_time 写成租约起点：期望 %s，实际 %s",
			claimAt.Truncate(time.Microsecond), got)
	}

	if ok, err := m.ClaimDelivery(ctx, d.ID, 5*time.Minute, claimAt.Add(4*time.Minute)); err != nil || ok {
		t.Fatalf("租约内不应可抢占：%v/%v", ok, err)
	}

	if ok, err := m.ClaimDelivery(ctx, d.ID, 5*time.Minute, claimAt.Add(6*time.Minute)); err != nil || !ok {
		t.Fatalf("租约过期后应可抢占：%v/%v", ok, err)
	}

	if ok, err := m.MarkDeliveryResult(ctx, d.ID, webhookenums.DeliveryStatusFailed, 500, "late", time.Now()); err != nil || !ok {
		t.Fatalf("抢占后的落定应成功一次：%v/%v", ok, err)
	}
}

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

	if ok, err := m.ClaimDelivery(ctx, d.ID, 5*time.Minute, time.Now()); err != nil || !ok {
		t.Fatalf("退认领后应立刻可再认领：%v/%v", ok, err)
	}
}

func TestWebhookStaleDeliveryListing(t *testing.T) {
	ctx := context.Background()
	m, fresh := newClaimFixture(t)

	old := &webhookmodel.WebhookDeliveryEntity{
		EndpointID: fresh.EndpointID, EventType: "order.paid", Payload: "{}",
		Status:    webhookenums.DeliveryStatusPending,
		CreatedAt: time.Now().Add(-time.Hour), UpdatedAt: time.Now().Add(-time.Hour),
	}
	if err := m.CreateDelivery(ctx, old); err != nil {
		t.Fatalf("建陈旧 pending 失败: %v", err)
	}

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

	if report.Requeued+report.Failed != report.Scanned {
		t.Fatalf("扫描/入队/失败应守恒：scanned=%d requeued=%d failed=%d",
			report.Scanned, report.Requeued, report.Failed)
	}

	if got := reload(t, m, old.ID).Status; got != webhookenums.DeliveryStatusPending {
		t.Fatalf("重放不该改 pending 行的状态，实际 %q", got)
	}
}

func TestWebhookRetryOnlyFromFailed(t *testing.T) {
	ctx := context.Background()
	m, d := newClaimFixture(t)

	if ok, err := m.ClaimDelivery(ctx, d.ID, 5*time.Minute, time.Now()); err != nil || !ok {
		t.Fatalf("认领应成功：%v/%v", ok, err)
	}
	if ok, err := m.MarkDeliveryRetryable(ctx, d.ID, time.Now()); err != nil || ok {
		t.Fatalf("delivering 的行不该可重投：affected=%v err=%v", ok, err)
	}
	if got := reload(t, m, d.ID).Status; got != webhookenums.DeliveryStatusDelivering {
		t.Fatalf("状态应保持 delivering，实际 %q", got)
	}

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

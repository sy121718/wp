package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	"testing"
	"time"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	orderservice "go_wp/internal/module/order/service"

	"go_wp/public/test/support"
)

const (
	summaryProjectA = "11111111-1111-1111-1111-111111111111"
	summaryProjectB = "22222222-2222-2222-2222-222222222222"
)

func newSummaryFixture(t *testing.T) (*ordermodel.OrderModel, *orderservice.Service) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil, nil
	}
	m := ordermodel.NewOrderModel(db)
	svc := orderservice.NewService(
		m,
		ordermodel.NewOrderItemModel(db),
		ordermodel.NewOrderStatusLogModel(db),
		ordermodel.NewCouponModel(db),
		ordermodel.NewReturnModel(db),
		nil, nil, nil, nil,
	)
	return m, svc
}

func mkSummaryOrder(t *testing.T, m *ordermodel.OrderModel,
	projectID, orderNo, status string, userID uint64, total int64, createdAt time.Time) {
	t.Helper()
	uid := userID
	e := &ordermodel.OrderEntity{
		ProjectID:  projectID,
		OrderNo:    orderNo,
		Status:     status,
		UserID:     &uid,
		Total:      total,
		Currency:   "CNY",
		CreateTime: createdAt,
		UpdateTime: createdAt,

		Attribution: json.RawMessage("{}"),
	}
	if err := m.Create(context.Background(), e); err != nil {
		t.Fatalf("落单 %s 失败: %v", orderNo, err)
	}
}

func summaryTime(day int) time.Time {
	return time.Date(2026, 9, day, 12, 0, 0, 0, time.Local)
}

func TestCustomerOrderSummaryAggregatesPaidOnly(t *testing.T) {
	m, svc := newSummaryFixture(t)
	if m == nil {
		return
	}
	const userA, userB = uint64(1001), uint64(1002)
	mkSummaryOrder(t, m, summaryProjectA, "A-PAID", ordermodel.OrderStatusPaid, userA, 10000, summaryTime(1))
	mkSummaryOrder(t, m, summaryProjectA, "A-CANCELLED", ordermodel.OrderStatusCancelled, userA, 20000, summaryTime(5))
	mkSummaryOrder(t, m, summaryProjectA, "A-REFUNDED", ordermodel.OrderStatusRefunded, userA, 30000, summaryTime(10))

	mkSummaryOrder(t, m, summaryProjectA, "B-PAID", ordermodel.OrderStatusPaid, userB, 5000, summaryTime(3))
	mkSummaryOrder(t, m, summaryProjectB, "A-OTHER-PROJECT", ordermodel.OrderStatusPaid, userA, 70000, summaryTime(8))

	res, err := svc.CustomerOrderSummaryOf(context.Background(), &orderdto.CustomerOrderSummaryReq{
		ProjectID: summaryProjectA, UserID: userA,
	})
	if err != nil {
		t.Fatalf("取订单摘要失败: %v", err)
	}
	if res.OrderCount != 3 {
		t.Errorf("该客户在这个工程下共 3 单，实得 %d", res.OrderCount)
	}
	if res.PaidOrderCount != 1 {
		t.Errorf("计入消费的应只有已付款那 1 单，实得 %d", res.PaidOrderCount)
	}
	if res.TotalAmount != 10000 {
		t.Errorf("累计消费应为 10000 分（取消与退款不计），实得 %d", res.TotalAmount)
	}
	if res.TotalAmountLabel != "100.00" {
		t.Errorf("金额展示文本应为 100.00，实得 %q", res.TotalAmountLabel)
	}
	if !res.HasOrders {
		t.Error("有订单时 HasOrders 应为 true")
	}
	if res.LastOrderNo != "A-REFUNDED" {
		t.Errorf("最近一单应是最后下单的那单（含取消/退款单），实得 %q", res.LastOrderNo)
	}
	if res.LastOrderTimeText == "" || res.LastOrderID == 0 {
		t.Errorf("最近一单的时间文本与 id 都应有值：%+v", res)
	}
}

func TestCustomerOrderSummaryCountsShippedAndCompleted(t *testing.T) {
	m, svc := newSummaryFixture(t)
	if m == nil {
		return
	}
	const user = uint64(2001)
	mkSummaryOrder(t, m, summaryProjectA, "S1", ordermodel.OrderStatusPending, user, 111, summaryTime(1))
	mkSummaryOrder(t, m, summaryProjectA, "S2", ordermodel.OrderStatusShipped, user, 2000, summaryTime(2))
	mkSummaryOrder(t, m, summaryProjectA, "S3", ordermodel.OrderStatusCompleted, user, 3000, summaryTime(3))

	res, err := svc.CustomerOrderSummaryOf(context.Background(), &orderdto.CustomerOrderSummaryReq{
		ProjectID: summaryProjectA, UserID: user,
	})
	if err != nil {
		t.Fatalf("取订单摘要失败: %v", err)
	}
	if res.OrderCount != 3 || res.PaidOrderCount != 2 {
		t.Errorf("应为 3 单其中 2 单计入消费，实得 %d / %d", res.OrderCount, res.PaidOrderCount)
	}
	if res.TotalAmount != 5000 {
		t.Errorf("累计消费应为 5000 分（待付款不计），实得 %d", res.TotalAmount)
	}
}

func TestCustomerOrderSummaryEmptyIsNotError(t *testing.T) {
	m, svc := newSummaryFixture(t)
	if m == nil {
		return
	}
	res, err := svc.CustomerOrderSummaryOf(context.Background(), &orderdto.CustomerOrderSummaryReq{
		ProjectID: summaryProjectA, UserID: 424242,
	})
	if err != nil {
		t.Fatalf("没有订单不应报错: %v", err)
	}
	if res.HasOrders || res.OrderCount != 0 || res.TotalAmount != 0 {
		t.Errorf("零订单的摘要应全是零值：%+v", res)
	}
	if res.LastOrderNo != "" || res.LastOrderTimeText != "" {
		t.Errorf("零订单不应有最近一单：%+v", res)
	}
}

func TestCustomerOrderSummaryRequiresProjectAndUser(t *testing.T) {
	m, svc := newSummaryFixture(t)
	if m == nil {
		return
	}
	cases := []struct {
		name string
		req  *orderdto.CustomerOrderSummaryReq
	}{
		{"缺工程", &orderdto.CustomerOrderSummaryReq{UserID: 1001}},
		{"缺客户", &orderdto.CustomerOrderSummaryReq{ProjectID: summaryProjectA}},
		{"两者都缺", &orderdto.CustomerOrderSummaryReq{}},
		{"nil 请求", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.CustomerOrderSummaryOf(context.Background(), tc.req); err == nil {
				t.Fatal("应拒绝并报参数错误")
			} else if err.Error() != orderenums.ErrInvalidParam {
				t.Errorf("错误文案应为 %q，实得 %q", orderenums.ErrInvalidParam, err.Error())
			}
		})
	}
}

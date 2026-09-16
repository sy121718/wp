package feature

// order_customer_summary_window_test.go — 客户订单摘要的单条查询（审计 DB-010）。
//
// 审计原文：报表类查询未用窗口函数，客户订单摘要靠多次查询。核对现状**属实**：
// CustomerOrderSummaryOf 一次调用发三条 SQL —— AggregateByUser 的聚合一条、
// List 里的分页计数一条（摘要页根本不要 total，白扫一遍全量行）、List 取最近一单一条。
//
// 改动把它收敛成 model.SummaryByUser 的一条窗口函数查询。本文件钉住两件事：
//
//	1. 改完之后这个摘要只发 1 条取 orders 的 SQL（计数 logger，先例见
//	   public/test/pkg/i18n/content_translation_test.go）；
//	2. 结果与旧口径逐字段一致 —— 对照值用**独立 SQL 现算**，不复用被测代码，
//	   否则「实现改了、断言跟着改」会把错的一起放过。

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	orderdto "go_wp/internal/module/order/dto"
	ordermodel "go_wp/internal/module/order/model"
	orderservice "go_wp/internal/module/order/service"

	"go_wp/public/test/support"
)

// summaryQueryCounter 只数「从 orders 取数」的语句（摘要该发的库交互）。
type summaryQueryCounter struct {
	mu    sync.Mutex
	query int
}

func (l *summaryQueryCounter) LogMode(gormlogger.LogLevel) gormlogger.Interface { return l }
func (l *summaryQueryCounter) Info(context.Context, string, ...interface{})     {}
func (l *summaryQueryCounter) Warn(context.Context, string, ...interface{})     {}
func (l *summaryQueryCounter) Error(context.Context, string, ...interface{})    {}

func (l *summaryQueryCounter) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	low := strings.ToLower(sql)
	// gorm 生成的语句表名带双引号（FROM "orders"），Raw 手写的没有 —— 两种都要认。
	if strings.Contains(low, "from orders") || strings.Contains(low, "from \"orders\"") {
		l.mu.Lock()
		l.query++
		l.mu.Unlock()
	}
}

func (l *summaryQueryCounter) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.query
}

func (l *summaryQueryCounter) reset() {
	l.mu.Lock()
	l.query = 0
	l.mu.Unlock()
}

// newCountedSummaryFixture 与 newSummaryFixture 同一装配，只把 model 换成挂了计数
// logger 的会话（计数 logger 只观察不改语义）。
func newCountedSummaryFixture(t *testing.T) (*gorm.DB, *ordermodel.OrderModel, *orderservice.Service, *summaryQueryCounter) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil, nil, nil, nil
	}
	cl := &summaryQueryCounter{}
	counted := db.Session(&gorm.Session{Logger: cl})
	m := ordermodel.NewOrderModel(counted)
	svc := orderservice.NewService(
		m,
		ordermodel.NewOrderItemModel(counted),
		ordermodel.NewOrderStatusLogModel(counted),
		ordermodel.NewCouponModel(counted),
		ordermodel.NewReturnModel(counted),
		nil, nil, nil, nil,
	)
	return db, m, svc, cl
}

// TestCustomerOrderSummarySingleQuery 摘要只发 1 条 SQL，且与旧口径结果逐字段一致。
func TestCustomerOrderSummarySingleQuery(t *testing.T) {
	db, m, svc, cl := newCountedSummaryFixture(t)
	if db == nil {
		return
	}
	const userA, userB = uint64(2001), uint64(2002)
	mkSummaryOrder(t, m, summaryProjectA, "W-PAID", ordermodel.OrderStatusPaid, userA, 10000, summaryTime(1))
	mkSummaryOrder(t, m, summaryProjectA, "W-CANCELLED", ordermodel.OrderStatusCancelled, userA, 20000, summaryTime(5))
	mkSummaryOrder(t, m, summaryProjectA, "W-REFUNDED", ordermodel.OrderStatusRefunded, userA, 30000, summaryTime(10))
	// 干扰项：同工程别的客户、别的工程里的同一个客户。
	mkSummaryOrder(t, m, summaryProjectA, "W-B-PAID", ordermodel.OrderStatusPaid, userB, 5000, summaryTime(3))
	mkSummaryOrder(t, m, summaryProjectB, "W-A-OTHER", ordermodel.OrderStatusPaid, userA, 70000, summaryTime(8))

	cl.reset()
	res, err := svc.CustomerOrderSummaryOf(context.Background(), &orderdto.CustomerOrderSummaryReq{
		ProjectID: summaryProjectA, UserID: userA,
	})
	if err != nil {
		t.Fatalf("取订单摘要失败: %v", err)
	}
	if got := cl.count(); got != 1 {
		t.Errorf("摘要应只发 1 条 orders 查询，实际 %d 条（多条 = 摘要各值之间可能落在不同快照上）", got)
	}

	// 对照：旧口径的三条 SQL 各自现算（聚合 + 计数 + 最近一单），逐字段比对。
	ctx := context.Background()
	var want struct {
		OrderCount      int64     `gorm:"column:order_count"`
		PaidOrderCount  int64     `gorm:"column:paid_order_count"`
		TotalAmount     int64     `gorm:"column:total_amount"`
		LastOrderID     uint64    `gorm:"column:last_order_id"`
		LastOrderNo     string    `gorm:"column:last_order_no"`
		LastOrderStatus string    `gorm:"column:last_order_status"`
		LastOrderTime   time.Time `gorm:"column:last_order_time"`
	}
	if err := db.WithContext(ctx).Raw(
		"SELECT COUNT(*) AS order_count, "+
			"COALESCE(SUM(CASE WHEN status IN ('paid','shipped','completed') THEN 1 ELSE 0 END), 0) AS paid_order_count, "+
			"COALESCE(SUM(CASE WHEN status IN ('paid','shipped','completed') THEN total ELSE 0 END), 0) AS total_amount "+
			"FROM orders WHERE project_id = ? AND user_id = ?",
		summaryProjectA, userA).Scan(&want).Error; err != nil {
		t.Fatalf("对照聚合失败: %v", err)
	}
	// 最近一单的对照要单独一个结构体：Scan 会把这次没返回的列按零值落回目标，
	// 复用同一个结构体会把上面的聚合结果抹掉（对照值全 0 会伪装成「新实现不一致」）。
	var wantLast struct {
		LastOrderID     uint64    `gorm:"column:last_order_id"`
		LastOrderNo     string    `gorm:"column:last_order_no"`
		LastOrderStatus string    `gorm:"column:last_order_status"`
		LastOrderTime   time.Time `gorm:"column:last_order_time"`
	}
	if err := db.WithContext(ctx).Raw(
		"SELECT id AS last_order_id, order_no AS last_order_no, status AS last_order_status, create_time AS last_order_time "+
			"FROM orders WHERE project_id = ? AND user_id = ? ORDER BY id DESC LIMIT 1",
		summaryProjectA, userA).Scan(&wantLast).Error; err != nil {
		t.Fatalf("对照最近一单失败: %v", err)
	}
	want.LastOrderID = wantLast.LastOrderID
	want.LastOrderNo = wantLast.LastOrderNo
	want.LastOrderStatus = wantLast.LastOrderStatus
	want.LastOrderTime = wantLast.LastOrderTime

	if res.OrderCount != want.OrderCount {
		t.Errorf("订单数不一致：新 %d / 旧 %d", res.OrderCount, want.OrderCount)
	}
	if res.PaidOrderCount != want.PaidOrderCount {
		t.Errorf("计入消费的订单数不一致：新 %d / 旧 %d", res.PaidOrderCount, want.PaidOrderCount)
	}
	if res.TotalAmount != want.TotalAmount {
		t.Errorf("累计消费不一致：新 %d / 旧 %d", res.TotalAmount, want.TotalAmount)
	}
	if res.LastOrderID != want.LastOrderID || res.LastOrderNo != want.LastOrderNo ||
		res.LastOrderStatus != want.LastOrderStatus {
		t.Errorf("最近一单不一致：新 (%d,%s,%s) / 旧 (%d,%s,%s)",
			res.LastOrderID, res.LastOrderNo, res.LastOrderStatus,
			want.LastOrderID, want.LastOrderNo, want.LastOrderStatus)
	}
	if res.LastOrderTime == nil || !res.LastOrderTime.Equal(want.LastOrderTime) {
		t.Errorf("最近一单时间不一致：新 %v / 旧 %v", res.LastOrderTime, want.LastOrderTime)
	}
	if !res.HasOrders {
		t.Errorf("有订单的客户 HasOrders 应为 true")
	}
	if res.TotalAmountLabel != "100.00" {
		t.Errorf("累计消费展示值应为 100.00（只算已付款那 1 单），实得 %s", res.TotalAmountLabel)
	}
}

// TestCustomerOrderSummaryNoOrdersKeepsOneRow 零订单客户：窗口聚合不产生行，
// 靠单行哨兵仍返回一行零值 —— 「一单没下」与「查不到这个客户」必须分得开，
// 而页面正是靠 HasOrders 分支的。
func TestCustomerOrderSummaryNoOrdersKeepsOneRow(t *testing.T) {
	db, _, svc, cl := newCountedSummaryFixture(t)
	if db == nil {
		return
	}
	cl.reset()
	res, err := svc.CustomerOrderSummaryOf(context.Background(), &orderdto.CustomerOrderSummaryReq{
		ProjectID: summaryProjectA, UserID: 424242,
	})
	if err != nil {
		t.Fatalf("取零订单客户摘要失败: %v", err)
	}
	if cl.count() != 1 {
		t.Errorf("零订单客户也应只发 1 条 orders 查询，实际 %d 条", cl.count())
	}
	if res.HasOrders || res.OrderCount != 0 || res.PaidOrderCount != 0 || res.TotalAmount != 0 {
		t.Errorf("零订单客户应得全零且 HasOrders=false，实得 %+v", res)
	}
	if res.LastOrderID != 0 || res.LastOrderNo != "" || res.LastOrderTime != nil {
		t.Errorf("零订单客户不应有最近一单，实得 id=%d no=%q time=%v", res.LastOrderID, res.LastOrderNo, res.LastOrderTime)
	}
}

// TestOrderListIssuesCountAndFind 量出旧摘要路径里 List 那一半的开销：
// List 固定发 2 条（分页计数 + 取数），加上聚合那条，旧摘要一次调用共 3 条。
// 这条测试是 DB-010「多次查询」的事实依据，也钉住 List 的行为不被顺手改掉。
func TestOrderListIssuesCountAndFind(t *testing.T) {
	db, m, _, cl := newCountedSummaryFixture(t)
	if db == nil {
		return
	}
	const userA = uint64(3001)
	mkSummaryOrder(t, m, summaryProjectA, "L-1", ordermodel.OrderStatusPaid, userA, 1000, summaryTime(1))
	mkSummaryOrder(t, m, summaryProjectA, "L-2", ordermodel.OrderStatusPaid, userA, 2000, summaryTime(2))

	uid := userA
	cl.reset()
	if _, _, err := m.List(context.Background(), ordermodel.OrderFilter{
		ProjectID: summaryProjectA, UserID: &uid, Limit: 1,
	}); err != nil {
		t.Fatalf("列表查询失败: %v", err)
	}
	if got := cl.count(); got != 2 {
		t.Errorf("List 应发 2 条 orders 查询（分页计数 + 取数），实际 %d 条", got)
	}
}

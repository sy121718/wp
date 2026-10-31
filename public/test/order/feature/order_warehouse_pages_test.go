package feature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	orderstock "go_wp/internal/module/inventory/outbound/orderstock"
	orderdto "go_wp/internal/module/order/dto"
	orderhttp "go_wp/internal/module/order/inbound/http"
	ordermodel "go_wp/internal/module/order/model"
	"go_wp/internal/templates"
)

func newWarehousePagesEngine(t *testing.T, f *orderFixture) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	ship := orderhttp.NewOrderShipmentPageHandle(f.orders, f.projects)
	returns := orderhttp.NewReturnPageHandle(f.orders, f.projects,
		orderstock.NewWarehouseSource(f.inventory), nil)
	engine.GET("/admin/orders/pending-ship",
		adminui.PageSlot(orderhttp.PendingShipPageTemplate), ship.PendingShipPage)
	engine.GET("/admin/returns",
		adminui.PageSlot(orderhttp.ReturnListPageTemplate), returns.ReturnsPage)
	return engine
}

func pendingShipBody(t *testing.T, engine *gin.Engine, query string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/orders/pending-ship?"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("待发货页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func TestPendingShipPagePaginatesAndFilters(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "分页商品", 20, 50)
	engine := newWarehousePagesEngine(t, f)

	names := []string{"张三", "李四", "王五"}
	orderNos := make([]string, 0, len(names))
	for _, name := range names {
		req := f.createBaseReq(vid, 1)
		req.CustomerName = name
		created, err := f.orders.CreateOrder(ctx, req)
		if err != nil {
			t.Fatalf("建单失败: %v", err)
		}
		f.payOrder(t, created.ID)
		orderNos = append(orderNos, created.OrderNo)
	}

	body := pendingShipBody(t, engine, "project="+f.projectID+"&limit=2")
	if !strings.Contains(body, "共 3 条") {
		t.Fatalf("分页条的总数应是库里真实的 3 条（不是本页条数），实际 HTML：%s", clipHTML(body))
	}
	if !strings.Contains(body, `class="pagination"`) {
		t.Fatalf("一页装不下时应渲染分页条")
	}

	filtered := pendingShipBody(t, engine, "project="+f.projectID+"&keyword="+"李四")
	if !strings.Contains(filtered, "李四") {
		t.Fatalf("关键词筛选后应留下客户名匹配的单")
	}
	for i, name := range names {
		if name == "李四" {
			continue
		}
		if strings.Contains(filtered, orderNos[i]) {
			t.Fatalf("关键词=%q 不该出现 %s 的单 %s", "李四", name, orderNos[i])
		}
	}

	if !strings.Contains(filtered, "keyword=") {
		t.Fatalf("分页基址应保留关键词筛选")
	}
}

func TestPendingShipPageBlocksUnshippableOrder(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, shortVid := f.addProduct(t, "缺货商品", 30, 5)
	_, goodVid := f.addProduct(t, "有货商品", 30, 5)

	shortReq := f.createBaseReq(shortVid, 2)
	shortReq.CustomerName = "缺货客户"
	shortOrder, err := f.orders.CreateOrder(ctx, shortReq)
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	f.payOrder(t, shortOrder.ID)

	goodReq := f.createBaseReq(goodVid, 2)
	goodReq.CustomerName = "有货客户"
	goodOrder, err := f.orders.CreateOrder(ctx, goodReq)
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	f.payOrder(t, goodOrder.ID)

	if err := f.db.Exec("UPDATE inventory_stocks SET quantity = 0, reserved_quantity = 0 "+
		"WHERE variant_id = ? AND warehouse_id = ?", shortVid, f.warehouse).Error; err != nil {
		t.Fatalf("制造缺货失败: %v", err)
	}

	engine := newWarehousePagesEngine(t, f)
	body := pendingShipBody(t, engine, "project="+f.projectID)
	if !strings.Contains(body, "发货仓的库存不够本次要发的件数") {
		t.Fatalf("缺货的单应给出「库存不够」的说明，实际 HTML：%s", clipHTML(body))
	}

	if !strings.Contains(body, "ship-form?orderId="+strconvU(goodOrder.ID)) {
		t.Fatalf("有货的单应仍能打开发货表单")
	}
	if strings.Contains(body, "ship-form?orderId="+strconvU(shortOrder.ID)) {
		t.Fatalf("缺货的单不该给出可点的发货按钮")
	}
}

func TestReturnPageShowsWarehouseFacts(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "退货仓商品", 60, 20)
	orderID, userID, itemID := f.paidOrder(t, "warehouse-return@example.com", vid, 2)

	if _, serr := f.orders.ShipOrder(ctx, &orderdto.ShipOrderReq{
		OrderID: orderID, ProjectID: f.projectID, RequestID: "return-wh-ship",
		Lines: []orderdto.ShipLineReq{{OrderItemID: itemID, Quantity: 2}},
	}); serr != nil {
		t.Fatalf("发货失败: %v", serr)
	}
	retReq := f.returnReq(orderID, []orderdto.ReturnItemReq{{OrderItemID: itemID, Quantity: 2}})

	retReq.UserID = userID
	created, rerr := f.orders.RequestReturn(ctx, retReq)
	if rerr != nil {
		t.Fatalf("申请退货失败: %v", rerr)
	}
	if created.ID == 0 {
		t.Fatalf("退货申请应返回单号")
	}

	engine := newWarehousePagesEngine(t, f)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/returns?project="+f.projectID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("退货页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	if !strings.Contains(body, "深圳仓") {
		t.Fatalf("退货列表应显示原发货仓名称，实际 HTML：%s", clipHTML(body))
	}
	if !strings.Contains(body, "待入库行数") {
		t.Fatalf("退货列表应有「待入库行数」列")
	}

	if !strings.Contains(body, `<strong>1</strong>`) {
		t.Fatalf("待入库行数应为 1（有一行还没收货）")
	}
	_ = userID
}

func TestFulfillmentAxisFollowsShipment(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "履约商品", 70, 20)
	created, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 4))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	f.payOrder(t, created.ID)
	itemIDs := f.orderItemIDs(t, created.ID)

	fulfillmentOf := func() (status, fulfillment string) {
		t.Helper()
		detail, derr := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: created.ID})
		if derr != nil || detail.Head == nil {
			t.Fatalf("读订单详情失败: %v", derr)
		}
		return detail.Head.Status, detail.Head.FulfillmentStatus
	}

	if _, fulfillment := fulfillmentOf(); fulfillment != ordermodel.FulfillmentPaid {
		t.Fatalf("已付款未发货时履约应是 paid，实际 %q", fulfillment)
	}

	if _, serr := f.orders.ShipOrder(ctx, &orderdto.ShipOrderReq{
		OrderID: created.ID, ProjectID: f.projectID, RequestID: "ff-partial",
		Lines: []orderdto.ShipLineReq{{OrderItemID: itemIDs[0], Quantity: 2}},
	}); serr != nil {
		t.Fatalf("部分发货失败: %v", serr)
	}
	status, fulfillment := fulfillmentOf()
	if status != ordermodel.OrderStatusPaid {
		t.Fatalf("部分发货后订单状态应仍是 paid，实际 %s", status)
	}
	if fulfillment != ordermodel.FulfillmentPartial {
		t.Fatalf("部分发货后履约应是 partial，实际 %q", fulfillment)
	}

	list, lerr := f.orders.ListOrders(ctx, &orderdto.ListOrderReq{
		ProjectID: f.projectID, FulfillmentStatus: ordermodel.FulfillmentPartial,
	})
	if lerr != nil {
		t.Fatalf("按履约筛选失败: %v", lerr)
	}
	found := false
	for _, o := range list.List {
		if o.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("按 partial 筛选应回这张部分发货的单")
	}
	if list.FulfillmentCounts[ordermodel.FulfillmentPartial] != 1 {
		t.Fatalf("履约计数应记 1 张 partial，实际 %d", list.FulfillmentCounts[ordermodel.FulfillmentPartial])
	}

	if _, serr := f.orders.ShipOrder(ctx, &orderdto.ShipOrderReq{
		OrderID: created.ID, ProjectID: f.projectID, RequestID: "ff-rest",
		Lines: []orderdto.ShipLineReq{{OrderItemID: itemIDs[0], Quantity: 2}},
	}); serr != nil {
		t.Fatalf("剩余发货失败: %v", serr)
	}
	status, fulfillment = fulfillmentOf()
	if status != ordermodel.OrderStatusShipped {
		t.Fatalf("全部发完后订单状态应是 shipped，实际 %s", status)
	}
	if fulfillment != ordermodel.FulfillmentShipped {
		t.Fatalf("全部发完后履约应是 shipped，实际 %q", fulfillment)
	}
}

func strconvU(id uint64) string {
	if id == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for id > 0 {
		i--
		buf[i] = byte('0' + id%10)
		id /= 10
	}
	return string(buf[i:])
}

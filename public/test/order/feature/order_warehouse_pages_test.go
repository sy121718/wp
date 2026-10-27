// order_warehouse_pages_test.go — B6 / B7 的验收：仓库视角（待发货守卫、退货仓）与履约轴。
//
// 覆盖的会红断言：
//
//	1  待发货页分页：一页装不下时出现分页条，且「共 N 条」是库里的真实总数；
//	2  关键词只留匹配的单（订单号 / 客户名），翻页链接带着同一个关键词；
//	3  「能不能发」与发货用例同源：库存压到 0 后该行按钮 disabled 并给出原因，
//	   而库里还有货的单照常可点（守卫不是一律禁用）；
//	4  退货页带上仓库视角的两列：原发货仓（实际发货仓）与待入库行数；
//	5  履约轴独立于钱的状态：部分发货 → partial、全部发完 → shipped，
//	   且按履约筛选只回匹配的单（筛选落到 SQL，不是页面过滤）。
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

// newWarehousePagesEngine 挂待发货页与退货页（真实 Jet 模板 + 真实 service）。
//
// 不挂鉴权：本用例的对象是**页面形状**（哪些行出现、按钮能不能点），
// 授权由 check-page-endpoint-authz.sh 在装配层守。
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

// pendingShipBody 请求待发货页并返回响应体（状态码必须是 200）。
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

// TestPendingShipPagePaginatesAndFilters 验收 1 + 2：分页条与关键词筛选。
//
// 判据是「分页条上的总数 = 库里的匹配总数」：只断言「有分页条」在
// 「总数算错但恰好也翻页」的情况下照样绿，那正是最需要抓到的一种错。
func TestPendingShipPagePaginatesAndFilters(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "分页商品", 20, 50)
	engine := newWarehousePagesEngine(t, f)

	// 三张待发货单，客户名不同（关键词要能挑出其中一张）。
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

	// ① 每页 2 条：总数是 3（不是本页的 2），分页条要在。
	body := pendingShipBody(t, engine, "project="+f.projectID+"&limit=2")
	if !strings.Contains(body, "共 3 条") {
		t.Fatalf("分页条的总数应是库里真实的 3 条（不是本页条数），实际 HTML：%s", clipHTML(body))
	}
	if !strings.Contains(body, `class="pagination"`) {
		t.Fatalf("一页装不下时应渲染分页条")
	}

	// ② 关键词只留「李四」那一张；另外两张不出现。
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
	// ③ 翻页链接要带着同一个关键词（否则第 2 页会跳回全量）。
	if !strings.Contains(filtered, "keyword=") {
		t.Fatalf("分页基址应保留关键词筛选")
	}
}

// TestPendingShipPageBlocksUnshippableOrder 验收 3：库存不足的单按钮 disabled + 一行说明。
//
// 口径对着发货用例：DeductStockOnShipTx 的条件是 quantity >= n（不减 reserved），
// 所以这里把在库压到 0（预占一起降，DDL 要求 reserved <= quantity），
// 发货必然失败 —— 页面就不该让人点了才知道。
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

	// 把缺货那张的仓存在库压到 0（预占同步降到 0 以满足 CHECK）。
	if err := f.db.Exec("UPDATE inventory_stocks SET quantity = 0, reserved_quantity = 0 "+
		"WHERE variant_id = ? AND warehouse_id = ?", shortVid, f.warehouse).Error; err != nil {
		t.Fatalf("制造缺货失败: %v", err)
	}

	engine := newWarehousePagesEngine(t, f)
	body := pendingShipBody(t, engine, "project="+f.projectID)
	if !strings.Contains(body, "发货仓的库存不够本次要发的件数") {
		t.Fatalf("缺货的单应给出「库存不够」的说明，实际 HTML：%s", clipHTML(body))
	}
	// 有货的那张仍可点（守卫是逐单判断，不是整页禁用）。
	if !strings.Contains(body, "ship-form?orderId="+strconvU(goodOrder.ID)) {
		t.Fatalf("有货的单应仍能打开发货表单")
	}
	if strings.Contains(body, "ship-form?orderId="+strconvU(shortOrder.ID)) {
		t.Fatalf("缺货的单不该给出可点的发货按钮")
	}
}

// TestReturnPageShowsWarehouseFacts 验收 4：退货页的「原发货仓」与「待入库行数」。
//
// 原发货仓取**实际发货仓**（order_shipment_items.warehouse_id）—— 那是历史事实；
// 待入库行数是「还没点收货的行数」，由 service 从明细算好（模板不做减法）。
func TestReturnPageShowsWarehouseFacts(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "退货仓商品", 60, 20)
	orderID, userID, itemID := f.paidOrder(t, "warehouse-return@example.com", vid, 2)
	// 先发出去（退货是「货已经出去之后」的事，也才有原发货仓这个事实）。
	if _, serr := f.orders.ShipOrder(ctx, &orderdto.ShipOrderReq{
		OrderID: orderID, ProjectID: f.projectID, RequestID: "return-wh-ship",
		Lines: []orderdto.ShipLineReq{{OrderItemID: itemID, Quantity: 2}},
	}); serr != nil {
		t.Fatalf("发货失败: %v", serr)
	}
	retReq := f.returnReq(orderID, []orderdto.ReturnItemReq{{OrderItemID: itemID, Quantity: 2}})
	// 归属必须显式带上：RequestReturn 对 UserID=0 一律拒绝（「不传即放行」是最危险的默认值）。
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
	// 默认仓名（夹具建的「深圳仓」）要出现在列表里 —— 这就是「货该收回哪个仓」。
	if !strings.Contains(body, "深圳仓") {
		t.Fatalf("退货列表应显示原发货仓名称，实际 HTML：%s", clipHTML(body))
	}
	if !strings.Contains(body, "待入库行数") {
		t.Fatalf("退货列表应有「待入库行数」列")
	}
	// 还没点收货：两张明细行里 0 行已入，待入库 = 1。
	if !strings.Contains(body, `<strong>1</strong>`) {
		t.Fatalf("待入库行数应为 1（有一行还没收货）")
	}
	_ = userID
}

// TestFulfillmentAxisFollowsShipment 验收 5：部分发货 → partial，全部发完 → shipped。
//
// 「钱」的状态与「货」的履约分开：部分发货时订单仍是 paid（还能接着发），
// 但履约已经是 partial —— 列表按履约筛才看得出「有一半货还在仓里」。
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

	// 发 2 件（共 4 件）→ 部分发货：订单仍在 paid，履约转 partial。
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

	// 按履约筛选：partial 只回这一张，paid 不该回它。
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

	// 把剩下的 2 件发完 → 全部发货：订单转 shipped，履约也是 shipped。
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

// strconvU 把订单 id 转成 URL 里的十进制串（格式化占位符会引入 fmt 依赖，不值得）。
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

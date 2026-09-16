package feature

// order_flow_test.go — 订单模块的用例链路测试（BIZ-1 销售侧）。
//
// 装配与生产一致（routers.SetupRoutes）：inventory 的实现作为库存端口注入 product，
// product 的实现作为「变体快照端口」传进 order —— 所以这里走的是真实跨模块链路，
// 不是各自的替身。
//
// 覆盖的是**不变量**而不是「方法能跑通」：
//   · 价格只信服务端；订单项是快照，改商品不该改历史订单；
//   · 扣减不足必须整单失败（不能扣一半），且失败的订单要留痕；
//   · 幂等键命中不得第二次扣库存；
//   · 状态机只走合法边；
//   · 取消归还库存，退款不归还（钱与货分开）。

import (
	"context"
	"testing"

	"gorm.io/gorm"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	orderservice "go_wp/internal/module/order/service"
	productdto "go_wp/internal/module/product/dto"
	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventorymodel "go_wp/internal/module/product/inventory/model"
	orderstock "go_wp/internal/module/product/inventory/outbound/orderstock"
	inventoryservice "go_wp/internal/module/product/inventory/service"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	usermodel "go_wp/internal/module/user/model"
	userservice "go_wp/internal/module/user/service"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// orderFixture 隔离 PG schema + 生产迁移与种子 + 真实跨模块 service。
type orderFixture struct {
	orders    *orderservice.Service
	products  *productservice.Service
	inventory *inventoryservice.Service
	users     *userservice.Service
	mail      *fakeMail
	db        *gorm.DB
	projectID string
	warehouse string
}

func newOrderFixture(t *testing.T) *orderFixture {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行生产数据种子失败: %v", err)
	}
	if db == nil {
		return nil
	}
	ctx := context.Background()
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(ctx, &projectdto.CreateReq{Name: "订单测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	inv := inventoryservice.NewService(inventorymodel.NewModel(db), projects)
	products := productservice.NewService(productmodel.NewModel(db), projects)
	products.SetInventoryService(inv)
	inv.SetVariantCost(products)

	// 建单要扣库存，必须有默认仓 —— 商品建变体时会往归属仓生成一条 0 库存记录。
	wh, err := inv.CreateWarehouse(ctx, &inventorydto.CreateWarehouseReq{
		ProjectID: project.ID, Code: "SZ", Name: "深圳仓", IsDefault: true,
	})
	if err != nil {
		t.Fatalf("建默认仓失败: %v", err)
	}
	mail := &fakeMail{}
	users := userservice.NewService(
		usermodel.NewUserModel(db),
		usermodel.NewUserProfileModel(db),
		usermodel.NewUserPreferenceModel(db),
		mail, "测试站",
	)
	orders := orderservice.NewService(
		ordermodel.NewOrderModel(db),
		ordermodel.NewOrderItemModel(db),
		ordermodel.NewOrderStatusLogModel(db),
		ordermodel.NewCouponModel(db),
		ordermodel.NewReturnModel(db),
		products,
		orderstock.New(inv),
		users,
	)
	return &orderFixture{
		orders: orders, products: products, inventory: inv, users: users, mail: mail,
		db: db, projectID: project.ID, warehouse: wh.ID,
	}
}

// addProduct 建商品 + 一个带价的变体，并把指定数量的货补进默认仓。
func (f *orderFixture) addProduct(t *testing.T, name string, price float64, stock int) (productID, variantID string) {
	t.Helper()
	ctx := context.Background()
	cost := price / 2
	p, err := f.products.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: name})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}
	v, err := f.products.CreateVariant(ctx, &productdto.CreateVariantReq{
		ProductID: p.ID, Price: &price, CostPrice: &cost,
		SKUCode: "SKU-" + name, WarehouseID: f.warehouse,
	})
	if err != nil {
		t.Fatalf("建变体失败: %v", err)
	}
	if stock > 0 {
		if _, err := f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
			ProjectID:  f.projectID,
			Direction:  "in",
			ReasonCode: "purchase_in",
			SourceType: "manual",
			Lines:      []inventorydto.StockChangeLineReq{{VariantID: v.ID, WarehouseID: f.warehouse, Quantity: stock}},
		}); err != nil {
			t.Fatalf("补库存失败: %v", err)
		}
	}
	return p.ID, v.ID
}

// stockOf 读某变体在默认仓的可用量（经 inventory service，不直读表）。
func (f *orderFixture) stockOf(t *testing.T, variantID string) int {
	t.Helper()
	st, err := f.inventory.GetStock(context.Background(), &inventorydto.GetStockReq{
		VariantID: variantID, WarehouseID: f.warehouse,
	})
	if err != nil {
		t.Fatalf("读库存失败: %v", err)
	}
	return st.Quantity
}

// createBaseReq 造一个最小可用建单请求。
func (f *orderFixture) createBaseReq(variantID string, qty int) *orderdto.CreateOrderReq {
	return &orderdto.CreateOrderReq{
		ProjectID:     f.projectID,
		CustomerEmail: "buyer@example.com",
		CustomerName:  "买家",
		Items:         []orderdto.OrderItemReq{{VariantID: variantID, Quantity: qty}},
		Shipping:      orderdto.OrderAddress{Name: "买家", Phone: "13800000000", City: "深圳", Address: "某某路 1 号"},
	}
}

// TestOrderCreateDeductsStockAndSnapshotsPrice：建单成功 → 状态待付款、金额按服务端价格算、库存扣减。
func TestOrderCreateDeductsStockAndSnapshotsPrice(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "T恤", 99.50, 10)

	res, err := f.orders.CreateOrder(context.Background(), f.createBaseReq(vid, 2))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	if res.Status != ordermodel.OrderStatusPending {
		t.Fatalf("新单应为 pending，实际 %s", res.Status)
	}
	// 99.50 元 × 2 = 19900 分（元→分的换算发生在商品域的边界上）
	if res.Total != 19900 {
		t.Fatalf("总额应为 19900 分，实际 %d", res.Total)
	}
	if got := f.stockOf(t, vid); got != 8 {
		t.Fatalf("库存应从 10 扣到 8，实际 %d", got)
	}

	detail, err := f.orders.GetOrder(context.Background(), &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.ID})
	if err != nil {
		t.Fatalf("读订单详情失败: %v", err)
	}
	if len(detail.Items) != 1 {
		t.Fatalf("应有 1 个订单项，实际 %d", len(detail.Items))
	}
	it := detail.Items[0]
	if it.ProductName != "T恤" || it.UnitPrice != 9950 || it.LineSubtotal != 19900 {
		t.Fatalf("订单项快照不对: %+v", it)
	}
	if it.CostPrice != 4975 {
		t.Fatalf("成本快照应为 4975 分，实际 %d", it.CostPrice)
	}
}

// TestOrderItemSnapshotSurvivesProductRename：改商品名之后，历史订单仍显示下单时的名字。
func TestOrderItemSnapshotSurvivesProductRename(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	pid, vid := f.addProduct(t, "旧名字", 10, 5)
	ctx := context.Background()
	res, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 1))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	newName := "新名字"
	if _, err := f.products.Update(ctx, &productdto.UpdateReq{ID: pid, Name: &newName}); err != nil {
		t.Fatalf("改商品名失败: %v", err)
	}
	detail, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.ID})
	if err != nil {
		t.Fatalf("读订单详情失败: %v", err)
	}
	if detail.Items[0].ProductName != "旧名字" {
		t.Fatalf("订单项是快照，改名后应仍是「旧名字」，实际 %q", detail.Items[0].ProductName)
	}
}

// TestOrderCreateIsIdempotentByRequestID：同一幂等键重复提交只落一单、只扣一次库存。
func TestOrderCreateIsIdempotentByRequestID(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "卫衣", 100, 5)
	ctx := context.Background()

	req := f.createBaseReq(vid, 2)
	req.RequestID = "req-abc-1"
	first, err := f.orders.CreateOrder(ctx, req)
	if err != nil {
		t.Fatalf("首次建单失败: %v", err)
	}
	if first.Duplicated {
		t.Fatalf("首次建单不该标记为重复")
	}
	again := f.createBaseReq(vid, 2)
	again.RequestID = "req-abc-1"
	second, err := f.orders.CreateOrder(ctx, again)
	if err != nil {
		t.Fatalf("重复提交应原样返回而不是报错: %v", err)
	}
	if !second.Duplicated || second.ID != first.ID || second.OrderNo != first.OrderNo {
		t.Fatalf("重复提交应命中同一单，实际 %+v vs %+v", second, first)
	}
	if got := f.stockOf(t, vid); got != 3 {
		t.Fatalf("幂等生效时库存只该扣一次（5→3），实际 %d", got)
	}
}

// TestOrderCreateRejectsInsufficientStockAndCancels：库存不足 → 整单失败，且订单留痕为已取消。
func TestOrderCreateRejectsInsufficientStockAndCancels(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "限量款", 50, 1)
	ctx := context.Background()

	_, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 3))
	if err == nil {
		t.Fatalf("库存不足应建单失败")
	}
	if err.Error() != orderenums.ErrStockInsufficient {
		t.Fatalf("应报库存不足，实际 %q", err.Error())
	}
	if got := f.stockOf(t, vid); got != 1 {
		t.Fatalf("失败的单不该扣走库存，实际 %d", got)
	}
	// 失败的订单要留在库里并标记取消 —— 静默丢弃会让「为什么没下单」无从查起。
	var count int64
	if err := f.db.Model(&ordermodel.OrderEntity{}).
		Where("project_id = ? AND status = ?", f.projectID, ordermodel.OrderStatusCancelled).
		Count(&count).Error; err != nil {
		t.Fatalf("统计已取消订单失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("应有 1 条因库存不足而取消的订单，实际 %d", count)
	}
}

// TestOrderCreateRejectsUnknownVariant：离谱的规格 id 必须报错，而不是静默少一行。
func TestOrderCreateRejectsUnknownVariant(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, err := f.orders.CreateOrder(context.Background(), f.createBaseReq("00000000-0000-0000-0000-000000000000", 1))
	if err == nil {
		t.Fatalf("不存在的规格应建单失败")
	}
	if got := err.Error(); len(got) < len(orderenums.ErrVariantNotFound) ||
		got[:len(orderenums.ErrVariantNotFound)] != orderenums.ErrVariantNotFound {
		t.Fatalf("应报规格不存在，实际 %q", got)
	}
}

// TestOrderStatusFlowFollowsAllowedEdges：状态机只放行合法边。
func TestOrderStatusFlowFollowsAllowedEdges(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "状态测试", 20, 10)
	ctx := context.Background()
	res, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 1))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	// pending 直接跳到 shipped 是非法边（没付款就不能发货）。
	if err := f.orders.ChangeStatus(ctx, &orderdto.ChangeStatusReq{
		OrderID: res.ID, ToStatus: ordermodel.OrderStatusShipped,
	}); err == nil {
		t.Fatalf("pending → shipped 应被拒绝")
	}
	// 合法链：pending → paid → shipped → completed
	for _, to := range []string{ordermodel.OrderStatusPaid, ordermodel.OrderStatusShipped, ordermodel.OrderStatusCompleted} {
		if err := f.orders.ChangeStatus(ctx, &orderdto.ChangeStatusReq{
			OrderID: res.ID, ToStatus: to, OperatorName: "tester",
		}); err != nil {
			t.Fatalf("%s 流转应成功: %v", to, err)
		}
	}
	// 终态不再流转。
	if err := f.orders.ChangeStatus(ctx, &orderdto.ChangeStatusReq{
		OrderID: res.ID, ToStatus: ordermodel.OrderStatusPaid,
	}); err == nil {
		t.Fatalf("completed 之后不该还能回到 paid")
	}
	// 流转链要留痕（4 条：建单 + 三次流转）。
	detail, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.ID})
	if err != nil {
		t.Fatalf("读详情失败: %v", err)
	}
	if len(detail.Logs) != 4 {
		t.Fatalf("应有 4 条流转记录，实际 %d", len(detail.Logs))
	}
}

// TestOrderCancelReturnsStock：取消订单归还库存。
func TestOrderCancelReturnsStock(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "可取消", 30, 6)
	ctx := context.Background()
	res, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 4))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	if got := f.stockOf(t, vid); got != 2 {
		t.Fatalf("建单后应剩 2，实际 %d", got)
	}
	if _, err := f.orders.CancelOrder(ctx, &orderdto.CancelOrderReq{
		OrderID: res.ID, Reason: "买家改主意", OperatorName: "tester",
	}); err != nil {
		t.Fatalf("取消失败: %v", err)
	}
	if got := f.stockOf(t, vid); got != 6 {
		t.Fatalf("取消后库存应回到 6，实际 %d", got)
	}
	// 已取消的单不能再取消。
	if _, err := f.orders.CancelOrder(ctx, &orderdto.CancelOrderReq{
		OrderID: res.ID, Reason: "再来一次",
	}); err == nil {
		t.Fatalf("重复取消应被拒绝")
	}
}

// TestOrderRefundDoesNotReturnStock：退款是钱的事，不动物流。
func TestOrderRefundDoesNotReturnStock(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "可退款", 80, 5)
	ctx := context.Background()
	res, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 2))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	if err := f.orders.ChangeStatus(ctx, &orderdto.ChangeStatusReq{
		OrderID: res.ID, ToStatus: ordermodel.OrderStatusPaid,
	}); err != nil {
		t.Fatalf("付款流转失败: %v", err)
	}
	if err := f.orders.RefundOrder(ctx, &orderdto.RefundOrderReq{
		OrderID: res.ID, Reason: "只退运费", TransactionID: "TX-1", OperatorName: "tester",
	}); err != nil {
		t.Fatalf("退款失败: %v", err)
	}
	if got := f.stockOf(t, vid); got != 3 {
		t.Fatalf("退款不该动库存（应仍是 3），实际 %d", got)
	}
	detail, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.ID})
	if err != nil {
		t.Fatalf("读详情失败: %v", err)
	}
	if detail.Head.Status != ordermodel.OrderStatusRefunded || detail.Head.TransactionID != "TX-1" {
		t.Fatalf("退款状态与流水号应落库: %+v", detail.Head)
	}
}

// TestOrderPersistsAttributionAndAdminNote：归因与轨迹、后台备注都要能落库并读回。
func TestOrderPersistsAttributionAndAdminNote(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "归因测试", 10, 5)
	ctx := context.Background()

	req := f.createBaseReq(vid, 1)
	req.AdminNote = "代发：由 XX 供应商发货"
	req.CreatedVia = ordermodel.CreatedViaAdmin
	req.Attribution = &orderdto.Attribution{
		SourceType: "utm",
		Referrer:   "https://ad.example.com/landing",
		UTM: orderdto.UTMInfo{
			Source: "google", Medium: "cpc", Campaign: "spring_sale",
		},
		Ad: orderdto.AdInfo{GCLID: "gclid-123", FBCLID: "fbclid-456"},
		Session: orderdto.SessionInfo{
			Entry: "/landing", Pages: 4, Count: 2, StartTime: "2026-09-12T10:00:00Z", DurationSeconds: 320,
		},
		Device: orderdto.DeviceInfo{Type: "mobile", UserAgent: "UA/1.0", Screen: "390x844"},
		// 首次触点与本次会话来源**故意不同**：先点广告来、隔几天搜品牌词才下单，
		// 两个口径必须各存各的，否则 first-touch 会被 last-touch 覆盖掉。
		First: orderdto.FirstTouch{
			SourceType: "referral",
			Referrer:   "https://blog.example.com/post",
			UTM:        orderdto.UTMInfo{Source: "weibo", Medium: "social", Campaign: "brand_awareness"},
			Landing:    "/ad-landing",
			At:         "2026-09-01T08:00:00Z",
		},
		Landing: "/",
		Trail: []orderdto.TrailPage{
			{URL: "/products/a", Title: "商品 A", At: "2026-09-12T10:02:11Z", Seconds: 42},
			{URL: "/cart", Title: "购物车", At: "2026-09-12T10:05:03Z", Seconds: 8},
		},
	}
	res, err := f.orders.CreateOrder(ctx, req)
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	detail, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.ID})
	if err != nil {
		t.Fatalf("读详情失败: %v", err)
	}
	h := detail.Head
	if h.AdminNote != "代发：由 XX 供应商发货" {
		t.Fatalf("后台备注应落库，实际 %q", h.AdminNote)
	}
	if h.Attribution == nil {
		t.Fatalf("归因应落库并可读回")
	}
	a := h.Attribution
	if a.SourceType != "utm" || a.UTM.Campaign != "spring_sale" {
		t.Fatalf("UTM 应落库: %+v", a.UTM)
	}
	if a.Ad.GCLID != "gclid-123" || a.Ad.FBCLID != "fbclid-456" {
		t.Fatalf("广告点击 id 应落库: %+v", a.Ad)
	}
	if a.Session.Pages != 4 || a.Session.Entry != "/landing" {
		t.Fatalf("会话事实应落库: %+v", a.Session)
	}
	if len(a.Trail) != 2 || a.Trail[0].URL != "/products/a" || a.Trail[1].Seconds != 8 {
		t.Fatalf("下单前浏览轨迹应落库: %+v", a.Trail)
	}
	if a.First.SourceType != "referral" || a.First.UTM.Campaign != "brand_awareness" ||
		a.First.Landing != "/ad-landing" || a.First.At != "2026-09-01T08:00:00Z" {
		t.Fatalf("首次触点应独立落库（last-touch 与 first-touch 是两个口径）: %+v", a.First)
	}
	if a.First.UTM.Campaign == a.UTM.Campaign {
		t.Fatalf("本用例里两个口径故意不同，若相等说明有一边被覆盖了")
	}
	if h.CreatedVia != ordermodel.CreatedViaAdmin {
		t.Fatalf("下单入口应落库，实际 %q", h.CreatedVia)
	}
}

// TestOrderListFiltersByStatusAndKeyword：列表按状态与关键词过滤，计数是全局的。
func TestOrderListFiltersByStatusAndKeyword(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "列表测试", 10, 20)
	ctx := context.Background()
	first, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 1))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	if _, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 1)); err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	if err := f.orders.ChangeStatus(ctx, &orderdto.ChangeStatusReq{
		OrderID: first.ID, ToStatus: ordermodel.OrderStatusPaid,
	}); err != nil {
		t.Fatalf("流转失败: %v", err)
	}
	all, err := f.orders.ListOrders(ctx, &orderdto.ListOrderReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if all.Total != 2 {
		t.Fatalf("应有 2 单，实际 %d", all.Total)
	}
	if all.Counts[ordermodel.OrderStatusPending] != 1 || all.Counts[ordermodel.OrderStatusPaid] != 1 {
		t.Fatalf("计数应按状态分组: %+v", all.Counts)
	}
	paid, err := f.orders.ListOrders(ctx, &orderdto.ListOrderReq{
		ProjectID: f.projectID, Status: ordermodel.OrderStatusPaid,
	})
	if err != nil {
		t.Fatalf("按状态过滤失败: %v", err)
	}
	if paid.Total != 1 || paid.List[0].ID != first.ID {
		t.Fatalf("按状态过滤结果不对: %+v", paid.List)
	}
	byKeyword, err := f.orders.ListOrders(ctx, &orderdto.ListOrderReq{
		ProjectID: f.projectID, Keyword: "buyer@example.com",
	})
	if err != nil {
		t.Fatalf("按关键词过滤失败: %v", err)
	}
	if byKeyword.Total != 2 {
		t.Fatalf("按邮箱应命中 2 单，实际 %d", byKeyword.Total)
	}
}

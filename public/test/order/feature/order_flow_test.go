package feature

import (
	"context"
	"go_wp/pkg/i18n"
	json "go_wp/pkg/jsonx"
	"testing"

	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventorymodel "go_wp/internal/module/inventory/model"
	orderstock "go_wp/internal/module/inventory/outbound/orderstock"
	inventoryservice "go_wp/internal/module/inventory/service"
	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	orderservice "go_wp/internal/module/order/service"
	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	usermodel "go_wp/internal/module/user/model"
	userservice "go_wp/internal/module/user/service"

	masterdatamodel "go_wp/internal/module/masterdata/model"
	masterdataservice "go_wp/internal/module/masterdata/service"
	"go_wp/public/schema"
	"go_wp/public/test/support"
)

type orderFixture struct {
	orders    *orderservice.Service
	products  *productservice.Service
	inventory *inventoryservice.Service
	users     *userservice.Service
	mail      *fakeMail
	db        *gorm.DB
	projectID string
	warehouse string

	projects *projectservice.Service

	orderModel  *ordermodel.OrderModel
	itemModel   *ordermodel.OrderItemModel
	logModel    *ordermodel.OrderStatusLogModel
	couponModel *ordermodel.CouponModel
	returnModel *ordermodel.ReturnModel
}

func newOrderFixture(t *testing.T) *orderFixture {
	return newOrderFixtureWithStock(t, nil)
}

// injectOrderFeatureWords 把生产种子写进库的词条刷新进 i18n 进程缓存。
// schema.Apply 只写库、不刷缓存；不注入的话取词一律落兜底，HTML 断言会误判成「模板没渲染」。
func injectOrderFeatureWords(t *testing.T, db *gorm.DB) {
	t.Helper()
	var rows []struct {
		ItemKey string `gorm:"column:item_key"`
		Lang    string
		Value   string `gorm:"column:value"`
	}
	if err := db.Table("sys_i18n").Select("item_key, lang, item_value AS value").
		Where("status = ?", 1).Scan(&rows).Error; err != nil {
		t.Fatalf("读取词条失败：%v", err)
	}
	words := make(map[string]map[string]string, len(rows))
	for _, r := range rows {
		if words[r.ItemKey] == nil {
			words[r.ItemKey] = map[string]string{}
		}
		words[r.ItemKey][r.Lang] = r.Value
	}
	i18n.InjectForTest(words, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
}

func newOrderFixtureWithStock(t *testing.T, stock ordercontract.StockOperator) *orderFixture {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if err := schema.Apply(db); err != nil {
		t.Fatalf("载入 schema 失败: %v", err)
	}
	if db == nil {
		return nil
	}
	// schema.Apply 只写库、不刷 i18n 进程缓存；HTML 断言的取词必须先注入。
	injectOrderFeatureWords(t, db)
	ctx := context.Background()
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(ctx, &projectdto.CreateReq{Name: "订单测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	inv := inventoryservice.NewService(inventorymodel.NewModel(db), projects)
	products := productservice.NewService(productmodel.NewModel(db), projects, masterdataservice.NewService(masterdatamodel.NewModel(db), projects))
	products.SetInventoryService(inv)
	inv.SetVariantWarehouseStocks(products)

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
	if stock == nil {
		stock = orderstock.New(inv)
	}
	orderModel := ordermodel.NewOrderModel(db)
	itemModel := ordermodel.NewOrderItemModel(db)
	logModel := ordermodel.NewOrderStatusLogModel(db)
	couponModel := ordermodel.NewCouponModel(db)
	returnModel := ordermodel.NewReturnModel(db)
	orders := orderservice.NewService(
		orderModel, itemModel, logModel, couponModel, returnModel,
		products,
		stock,
		users,
		nil,
	)
	orders.SetProjects(projects)

	orders.SetShipmentModels(ordermodel.NewOrderShipmentModel(db), ordermodel.NewOrderCarrierModel(db))
	orders.SetWarehouseResolver(orderstock.NewWarehouseResolver(inv))

	orders.SetStockRowLocator(orderstock.New(inv))

	orders.SetShipStockProbe(orderstock.NewShipProbe(inv, products))

	orders.SetWarehouseSource(orderstock.NewWarehouseSource(inv))
	return &orderFixture{
		orders: orders, products: products, inventory: inv, users: users, mail: mail,
		db: db, projectID: project.ID, warehouse: wh.ID, projects: projects,
		orderModel: orderModel, itemModel: itemModel, logModel: logModel,
		couponModel: couponModel, returnModel: returnModel,
	}
}

func (f *orderFixture) serviceWithStock(stock ordercontract.StockOperator) *orderservice.Service {
	svc := orderservice.NewService(
		f.orderModel, f.itemModel, f.logModel, f.couponModel, f.returnModel,
		f.products, stock, f.users, nil,
	)

	svc.SetShipmentModels(ordermodel.NewOrderShipmentModel(f.db), ordermodel.NewOrderCarrierModel(f.db))
	svc.SetWarehouseResolver(orderstock.NewWarehouseResolver(f.inventory))
	svc.SetStockRowLocator(orderstock.New(f.inventory))
	svc.SetShipStockProbe(orderstock.NewShipProbe(f.inventory, f.products))
	svc.SetWarehouseSource(orderstock.NewWarehouseSource(f.inventory))
	return svc
}

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

func (f *orderFixture) reservedOf(t *testing.T, variantID string) int {
	t.Helper()
	st, err := f.inventory.GetStock(context.Background(), &inventorydto.GetStockReq{
		VariantID: variantID, WarehouseID: f.warehouse,
	})
	if err != nil {
		t.Fatalf("读库存失败: %v", err)
	}
	return st.ReservedQuantity
}

func (f *orderFixture) createBaseReq(variantID string, qty int) *orderdto.CreateOrderReq {
	return &orderdto.CreateOrderReq{
		ProjectID:     f.projectID,
		CustomerEmail: "buyer@example.com",
		CustomerName:  "买家",
		Items:         []orderdto.OrderItemReq{{VariantID: variantID, Quantity: qty}},
		Shipping:      orderdto.OrderAddress{Name: "买家", Phone: "13800000000", City: "深圳", Address: "某某路 1 号"},
	}
}

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

	if res.Total != 19900 {
		t.Fatalf("总额应为 19900 分，实际 %d", res.Total)
	}

	if got := f.stockOf(t, vid); got != 10 {
		t.Fatalf("下单不得扣减在库数量（两段式：下单只预占），期望 10，实际 %d", got)
	}
	if got := f.reservedOf(t, vid); got != 2 {
		t.Fatalf("下单应预占 2 件，实际 %d", got)
	}

	detail, err := f.orders.GetOrder(context.Background(), &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.ID})
	if err != nil {
		t.Fatalf("读订单详情失败: %v", err)
	}
	if detail.Head.CreatedVia != ordermodel.CreatedViaCheckout {
		t.Fatalf("结算建单来源应为 checkout，实际 %q", detail.Head.CreatedVia)
	}
	if len(detail.Items) != 1 {
		t.Fatalf("应有 1 个订单项，实际 %d", len(detail.Items))
	}
	it := detail.Items[0]
	if it.ProductName != "T恤" || it.UnitPrice != 9950 || it.LineSubtotal != 19900 {
		t.Fatalf("订单项快照不对: %+v", it)
	}

	if it.CostPrice != nil {
		t.Fatalf("库存行未核算时订单行成本应为 null，实际 %d", *it.CostPrice)
	}
}

func TestOrderAPIEntryPersistsSource(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "API 来源测试", 10, 5)
	req := f.createBaseReq(vid, 1)
	if err := json.Unmarshal([]byte(`{"createdVia":"admin"}`), req); err != nil {
		t.Fatal(err)
	}
	res, err := f.orders.CreateAPIOrder(context.Background(), req)
	if err != nil {
		t.Fatalf("API 建单失败: %v", err)
	}
	detail, err := f.orders.GetOrder(context.Background(), &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.ID})
	if err != nil {
		t.Fatalf("读取 API 订单失败: %v", err)
	}
	if detail.Head.CreatedVia != ordermodel.CreatedViaAPI {
		t.Fatalf("API 建单来源应为 api，实际 %q", detail.Head.CreatedVia)
	}
}

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

	if got := f.reservedOf(t, vid); got != 2 {
		t.Fatalf("幂等生效时预占只该加一次（2），实际 %d", got)
	}
	if got := f.stockOf(t, vid); got != 5 {
		t.Fatalf("预占不得改在库数量，期望 5，实际 %d", got)
	}
}

func TestOrderCreateRejectsInsufficientStockAndRollsBack(t *testing.T) {
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
		t.Fatalf("失败的单不该动在库数量，实际 %d", got)
	}
	if got := f.reservedOf(t, vid); got != 0 {
		t.Fatalf("预占失败不该留下任何预占，实际 %d", got)
	}

	for _, table := range []string{"orders", "order_items", "order_status_logs"} {
		if n := f.tableCount(t, table); n != 0 {
			t.Fatalf("库存不足必须整单回滚，%s 里应 0 行，实际 %d", table, n)
		}
	}
}

func (f *orderFixture) tableCount(t *testing.T, table string) int64 {
	t.Helper()
	var n int64
	if err := f.db.Raw("SELECT COUNT(*) FROM " + table).Scan(&n).Error; err != nil {
		t.Fatalf("统计 %s 失败: %v", table, err)
	}
	return n
}

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

	if err := f.orders.ChangeStatus(ctx, &orderdto.ChangeStatusReq{
		OrderID: res.ID, ToStatus: ordermodel.OrderStatusShipped,
	}); err == nil {
		t.Fatalf("pending → shipped 应被拒绝")
	}

	for _, to := range []string{ordermodel.OrderStatusPaid, ordermodel.OrderStatusShipped, ordermodel.OrderStatusCompleted} {
		if err := f.orders.ChangeStatus(ctx, &orderdto.ChangeStatusReq{
			OrderID: res.ID, ToStatus: to, OperatorName: "tester",
		}); err != nil {
			t.Fatalf("%s 流转应成功: %v", to, err)
		}
	}

	if err := f.orders.ChangeStatus(ctx, &orderdto.ChangeStatusReq{
		OrderID: res.ID, ToStatus: ordermodel.OrderStatusPaid,
	}); err == nil {
		t.Fatalf("completed 之后不该还能回到 paid")
	}

	detail, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.ID})
	if err != nil {
		t.Fatalf("读详情失败: %v", err)
	}
	if len(detail.Logs) != 4 {
		t.Fatalf("应有 4 条流转记录，实际 %d", len(detail.Logs))
	}
}

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

	if got := f.reservedOf(t, vid); got != 4 {
		t.Fatalf("建单后预占应为 4，实际 %d", got)
	}
	if got := f.stockOf(t, vid); got != 6 {
		t.Fatalf("建单不得扣减在库数量，期望 6，实际 %d", got)
	}
	if _, err := f.orders.CancelOrder(ctx, &orderdto.CancelOrderReq{
		OrderID: res.ID, Reason: "买家改主意", OperatorName: "tester",
	}); err != nil {
		t.Fatalf("取消失败: %v", err)
	}
	if got := f.reservedOf(t, vid); got != 0 {
		t.Fatalf("取消后预占应释放回 0，实际 %d", got)
	}
	if got := f.stockOf(t, vid); got != 6 {
		t.Fatalf("取消不改变在库数量（货从来没出库），期望 6，实际 %d", got)
	}

	if _, err := f.orders.CancelOrder(ctx, &orderdto.CancelOrderReq{
		OrderID: res.ID, Reason: "再来一次",
	}); err == nil {
		t.Fatalf("重复取消应被拒绝")
	}
}

func TestOrderRefundUnshippedReturnsStock(t *testing.T) {
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
	if got := f.stockOf(t, vid); got != 5 {
		t.Fatalf("未发货退款应把货归还（应回到 5），实际 %d", got)
	}
	detail, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.ID})
	if err != nil {
		t.Fatalf("读详情失败: %v", err)
	}
	if detail.Head.Status != ordermodel.OrderStatusRefunded || detail.Head.TransactionID != "TX-1" {
		t.Fatalf("退款状态与流水号应落库: %+v", detail.Head)
	}
}

func TestOrderPersistsAttributionAndAdminNote(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "归因测试", 10, 5)
	ctx := context.Background()

	req := f.createBaseReq(vid, 1)
	req.AdminNote = "代发：由 XX 供应商发货"
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
	res, err := f.orders.CreateAdminOrder(ctx, req)
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

package feature

// cart_fixture_test.go — 购物车与访客结算链路的测试装配。
//
// 装配方式与生产一致（internal/routers/routes.go 的那一段）：商品与库存真装配、
// 订单域拿到可变体快照端口、购物车拿到订单服务 + 商品快照端口 + 可用量端口 + 支付通道。
// 所以这些用例走的是真实跨模块链路，不是替身拼出来的假链路。
//
// 购物车 cookie 由**被测代码自己签发**（Add 返回的 Cookie 字段），测试不手工构造 ——
// 手工造 cookie 等于绕开签名边界，那样测出来的「能下单」在生产里可能根本进不来。

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"gorm.io/gorm"

	cartcontract "go_wp/internal/module/cart/contract"
	cartdto "go_wp/internal/module/cart/dto"
	mockpaypal "go_wp/internal/module/cart/outbound/mockpaypal"
	cartservice "go_wp/internal/module/cart/service"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventorymodel "go_wp/internal/module/inventory/model"
	orderstock "go_wp/internal/module/inventory/outbound/orderstock"
	inventoryservice "go_wp/internal/module/inventory/service"
	mailcontract "go_wp/internal/module/mail/contract"
	orderdto "go_wp/internal/module/order/dto"
	ordermodel "go_wp/internal/module/order/model"
	orderservice "go_wp/internal/module/order/service"
	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	usermodel "go_wp/internal/module/user/model"
	userservice "go_wp/internal/module/user/service"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// cartTestSecret 测试用签名密钥（生产取 auth.SessionSecret()）。
const cartTestSecret = "cart-feature-test-secret"

// fakeMail 记录被调用的邮件请求（只为断言「发了哪封信」）。
type fakeMail struct {
	calls []*mailcontract.SendInput
}

func (f *fakeMail) SendTransactional(_ context.Context, in *mailcontract.SendInput) (*mailcontract.SendOutcome, error) {
	f.calls = append(f.calls, in)
	return &mailcontract.SendOutcome{Queued: true}, nil
}

func (f *fakeMail) findTemplate(key string) *mailcontract.SendInput {
	for _, c := range f.calls {
		if c.TemplateKey == key {
			return c
		}
	}
	return nil
}

// failingGateway 永远扣款失败的通道（验证「订单保留在待付款」而不是被丢弃）。
type failingGateway struct{}

func (failingGateway) Method() string { return "paypal" }
func (failingGateway) Title() string  { return "PayPal（模拟失败）" }

func (failingGateway) Charge(_ context.Context, _ *cartcontract.PaymentChargeReq) (*cartcontract.PaymentChargeResult, error) {
	return nil, errors.New("模拟通道不可用")
}

// VerifyCallback 失败通道同样不提供回调能力：它连扣款都失败，不可能有成功的通知。
func (failingGateway) VerifyCallback(map[string]string, []byte) (*cartcontract.PaymentCallback, error) {
	return nil, errors.New("模拟通道不可用")
}

// cartFixture 隔离 PG + 生产迁移与种子 + 真实跨模块 service。
type cartFixture struct {
	cart      *cartservice.Service
	orders    *orderservice.Service
	products  *productservice.Service
	inventory *inventoryservice.Service
	users     *userservice.Service
	projects  *projectservice.Service
	mail      *fakeMail
	db        *gorm.DB
	projectID string
	warehouse string
}

// newCartFixture 装配（支付通道 = 模拟 PayPal）。
func newCartFixture(t *testing.T) *cartFixture {
	t.Helper()
	return newCartFixtureWithGateway(t, mockpaypal.New(cartTestSecret))
}

// newCartFixtureWithGateway 装配（指定支付通道，用于验证失败路径）。
func newCartFixtureWithGateway(t *testing.T, gateway cartcontract.PaymentGateway) *cartFixture {
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
	project, err := projects.Create(ctx, &projectdto.CreateReq{Name: "购物车测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	inv := inventoryservice.NewService(inventorymodel.NewModel(db), projects)
	products := productservice.NewService(productmodel.NewModel(db), projects)
	products.SetInventoryService(inv)
	// 可用量端口（inventory 实现、product 消费）。漏接这一步**不会报错**：
	// product 的 VariantAvailabilities 只是返回空表，于是购物车的库存预检静默失效 ——
	// 表现是「库存只有 3 件，却能加 5 件进购物车，到结算才被拒」。
	// 生产装配（routers.SetupRoutes）里有这一段，测试必须与它一致，否则测出来的是假链路。
	products.SetAvailabilityPort(inv)
	inv.SetVariantCost(products)

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
		nil, // webhooks：本用例不接线外部集成通道
	)
	cart := cartservice.NewService(orders, products, products, gateway, cartTestSecret)
	// 站点运费规则端口（本项目新增）：与生产装配（routers.assembly 的 wireRuntimeAccessFace）
	// 逐字一致 —— 结算的基准运费来自 projects.settings，不再由调用方传入。
	// 漏接这一行不会报错：运费恒 0，于是「站点配了运费却不收」这类回归在测试里看不出来。
	if reader, ok := projectcontract.ProjectService(projects).(projectcontract.ShippingPolicyReader); ok {
		cart.SetShippingPolicyReader(reader)
	} else {
		t.Fatal("project service 未实现 ShippingPolicyReader（装配契约变了）")
	}
	return &cartFixture{
		cart: cart, orders: orders, products: products, inventory: inv, users: users,
		projects: projects, mail: mail, db: db, projectID: project.ID, warehouse: wh.ID,
	}
}

// setShippingPolicy 配置站点级运费规则（单位**分**，与库内同口径）。
//
// 为什么经 project 契约而不是直接改库：站点设置的真源是 projects.settings 这一列，
// 而「它长什么样」由 project 的 dto 定义 —— 手拼 JSON 的测试会在键名变化时静默失效
// （读出来恒为 0，运费变成「没配」），那正是本文件要防的那类静默回归。
func (f *cartFixture) setShippingPolicy(t *testing.T, baseCents, thresholdCents int64) {
	t.Helper()
	ctx := context.Background()
	current, err := f.projects.Detail(ctx, &projectdto.DetailReq{ID: f.projectID})
	if err != nil || current == nil {
		t.Fatalf("读工程失败: %v", err)
	}
	settings, err := json.Marshal(projectdto.SiteSettings{
		ShippingBaseFee:       baseCents,
		ShippingFreeThreshold: thresholdCents,
	})
	if err != nil {
		t.Fatalf("序列化站点设置失败: %v", err)
	}
	if _, err := f.projects.Update(ctx, &projectdto.UpdateReq{
		ID: f.projectID, Name: current.Name, Settings: settings,
	}); err != nil {
		t.Fatalf("保存站点设置失败: %v", err)
	}
}

// addProduct 建商品 + 一个带价的变体，并把指定数量的货补进默认仓。
func (f *cartFixture) addProduct(t *testing.T, name string, price float64, stock int) (productID, variantID string) {
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

// stockOf 读某变体在默认仓的可用量。
func (f *cartFixture) stockOf(t *testing.T, variantID string) int {
	t.Helper()
	st, err := f.inventory.GetStock(context.Background(), &inventorydto.GetStockReq{
		VariantID: variantID, WarehouseID: f.warehouse,
	})
	if err != nil {
		t.Fatalf("读库存失败: %v", err)
	}
	return st.Quantity
}

// addToCart 加购（cookie 由被测代码签发）。
func (f *cartFixture) addToCart(t *testing.T, variantID string, qty int, cookie string) *cartdto.CartSnapshot {
	t.Helper()
	snap, err := f.cart.Add(context.Background(), &cartdto.CartAddReq{
		ProjectID: f.projectID, VariantID: variantID, Quantity: qty, Cookie: cookie,
	})
	if err != nil {
		t.Fatalf("加购失败: %v", err)
	}
	return snap
}

// checkoutReq 结算请求（字段齐备的最小集合）。
func (f *cartFixture) checkoutReq(cookie string) *cartdto.CartCheckoutReq {
	return &cartdto.CartCheckoutReq{
		ProjectID: f.projectID,
		Cookie:    cookie,
		Email:     "buyer@example.com",
		Name:      "张三",
		Phone:     "13800000000",
		Shipping: orderdto.OrderAddress{
			Name: "张三", Phone: "13800000000",
			Province: "广东省", City: "深圳市", District: "南山区",
			Address: "科技园一号", Zip: "518000",
		},
		RequestID: "req-1",
		UserAgent: "Mozilla/5.0 (Test)",
		IPAddress: "203.0.113.9",
	}
}

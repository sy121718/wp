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
	"errors"
	"testing"

	"gorm.io/gorm"

	cartcontract "go_wp/internal/module/cart/contract"
	cartdto "go_wp/internal/module/cart/dto"
	mockpaypal "go_wp/internal/module/cart/outbound/mockpaypal"
	cartservice "go_wp/internal/module/cart/service"
	maildto "go_wp/internal/module/mail/dto"
	orderdto "go_wp/internal/module/order/dto"
	ordermodel "go_wp/internal/module/order/model"
	orderservice "go_wp/internal/module/order/service"
	productdto "go_wp/internal/module/product/dto"
	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventorymodel "go_wp/internal/module/product/inventory/model"
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

// cartTestSecret 测试用签名密钥（生产取 auth.SessionSecret()）。
const cartTestSecret = "cart-feature-test-secret"

// fakeMail 记录被调用的邮件请求（只为断言「发了哪封信」）。
type fakeMail struct {
	calls []*maildto.SendTemplateReq
}

func (f *fakeMail) SendTemplate(_ context.Context, req *maildto.SendTemplateReq) (*maildto.SendResult, error) {
	f.calls = append(f.calls, req)
	return &maildto.SendResult{Queued: true, To: req.To}, nil
}

func (f *fakeMail) findTemplate(key string) *maildto.SendTemplateReq {
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

// cartFixture 隔离 PG + 生产迁移与种子 + 真实跨模块 service。
type cartFixture struct {
	cart      *cartservice.Service
	orders    *orderservice.Service
	products  *productservice.Service
	inventory *inventoryservice.Service
	users     *userservice.Service
	mail      *fakeMail
	db        *gorm.DB
	projectID string
	warehouse string
}

// newCartFixture 装配（支付通道 = 模拟 PayPal）。
func newCartFixture(t *testing.T) *cartFixture {
	t.Helper()
	return newCartFixtureWithGateway(t, mockpaypal.New())
}

// newCartFixtureWithGateway 装配（指定支付通道，用于验证失败路径）。
func newCartFixtureWithGateway(t *testing.T, gateway cartcontract.PaymentGateway) *cartFixture {
	t.Helper()
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil
	}
	if err := migrations.Run(db); err != nil {
		t.Fatalf("执行生产迁移建表失败: %v", err)
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行生产数据种子失败: %v", err)
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
		usermodel.NewUserSessionModel(db),
		usermodel.NewUserProfileModel(db),
		usermodel.NewUserPreferenceModel(db),
		mail, "测试站",
	)
	orders := orderservice.NewService(
		ordermodel.NewOrderModel(db),
		ordermodel.NewOrderItemModel(db),
		ordermodel.NewOrderStatusLogModel(db),
		products,
		inv,
		users,
	)
	cart := cartservice.NewService(orders, products, products, gateway, cartTestSecret)
	return &cartFixture{
		cart: cart, orders: orders, products: products, inventory: inv, users: users,
		mail: mail, db: db, projectID: project.ID, warehouse: wh.ID,
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

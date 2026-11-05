package feature

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
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

	masterdatamodel "go_wp/internal/module/masterdata/model"
	masterdataservice "go_wp/internal/module/masterdata/service"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

const cartTestSecret = "cart-feature-test-secret"

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

type failingGateway struct{}

func (failingGateway) Method() string { return "paypal" }
func (failingGateway) Title() string  { return "PayPal（模拟失败）" }

func (failingGateway) Charge(_ context.Context, _ *cartcontract.PaymentChargeReq) (*cartcontract.PaymentChargeResult, error) {
	return nil, errors.New("模拟通道不可用")
}

func (failingGateway) VerifyCallback(map[string]string, []byte) (*cartcontract.PaymentCallback, error) {
	return nil, errors.New("模拟通道不可用")
}

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

func newCartFixture(t *testing.T) *cartFixture {
	t.Helper()
	return newCartFixtureWithGateway(t, mockpaypal.New(cartTestSecret))
}

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
	products := productservice.NewService(productmodel.NewModel(db), projects, masterdataservice.NewService(masterdatamodel.NewModel(db), projects))
	products.SetInventoryService(inv)
	inv.SetVariantWarehouseStocks(products)

	products.SetAvailabilityPort(inv)

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
		nil,
	)
	orders.SetProjects(projects)

	orders.SetShipmentModels(ordermodel.NewOrderShipmentModel(db), ordermodel.NewOrderCarrierModel(db))
	orders.SetWarehouseResolver(orderstock.NewWarehouseResolver(inv))
	cart := cartservice.NewService(orders, products, products, gateway, cartTestSecret)

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

func (f *cartFixture) reservedOf(t *testing.T, variantID string) int {
	t.Helper()
	st, err := f.inventory.GetStock(context.Background(), &inventorydto.GetStockReq{
		VariantID: variantID, WarehouseID: f.warehouse,
	})
	if err != nil {
		t.Fatalf("读库存失败: %v", err)
	}
	return st.ReservedQuantity
}

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

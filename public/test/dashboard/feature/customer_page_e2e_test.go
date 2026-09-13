package feature

// customer_page_e2e_test.go — 后台客户管理页的端到端链路。
//
// 真实 PostgreSQL（隔离 schema + 生产迁移 + 生产 seed）→ 真实 user / order / project service
// → 真实 dashboard handler → 真实 Jet 模板渲染。这一条测试覆盖的是页面背后**整条数据链**：
//
//	GET /admin/customers        客户列表（含筛选与计数）
//	GET /admin/customers/detail 客户资料 + **跨模块**的订单摘要（订单模块的只读聚合）
//	POST /admin/customers/status 停用账号 → 库里真的变了
//	POST /admin/customers/unlock 解除锁定 → 锁定字段真的被清空
//
// 只测「handler 返回 200」是不够的：客户页最容易出的错是「页面渲染出来了，
// 但数字是错的」（金额口径、锁定判定、筛选没落到 SQL 上），所以每条断言都回库核对。
//
// PG 不可用时 t.Skip（与其他功能测试一致）。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	dashboardhttp "go_wp/internal/module/dashboard/inbound/http"
	orderdto "go_wp/internal/module/order/dto"
	ordermodel "go_wp/internal/module/order/model"
	orderservice "go_wp/internal/module/order/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	usermodel "go_wp/internal/module/user/model"
	userservice "go_wp/internal/module/user/service"
	"go_wp/internal/templates"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

type customerE2EEnv struct {
	engine  *gin.Engine
	db      *gorm.DB
	users   *usermodel.UserModel
	orders  *ordermodel.OrderModel
	project string
}

func newCustomerE2EEnv(t *testing.T) *customerE2EEnv {
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
	project, err := projects.Create(ctx, &projectdto.CreateReq{Name: "客户页测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}

	// 访客账号这条链路不碰会话与资料表，构造时传 nil（与服务装配的「可选依赖」口径一致）。
	users := userservice.NewService(usermodel.NewUserModel(db), nil, nil, nil, nil, "测试站")
	// 订单侧只需要订单模块自己的 model：客户页的订单摘要是纯聚合，不碰商品与库存。
	orders := orderservice.NewService(
		ordermodel.NewOrderModel(db),
		ordermodel.NewOrderItemModel(db),
		ordermodel.NewOrderStatusLogModel(db),
		ordermodel.NewCouponModel(db),
		ordermodel.NewReturnModel(db),
		nil, nil, nil,
	)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "internal/templates"), true)
	page := dashboardhttp.NewCustomerPageHandle(users, orders, projects)
	engine.GET("/admin/customers", page.CustomersPage)
	engine.GET("/admin/customers/detail", page.CustomerDetailPage)
	engine.POST("/admin/customers/status", page.CustomerStatusSave)
	engine.POST("/admin/customers/unlock", page.CustomerUnlock)

	return &customerE2EEnv{
		engine: engine, db: db,
		users: usermodel.NewUserModel(db), orders: ordermodel.NewOrderModel(db),
		project: project.ID,
	}
}

// mkE2ECustomer 直接落一个客户（走注册链路要发邮件，这里要的是页面链路）。
func (e *customerE2EEnv) mkE2ECustomer(t *testing.T, username, email, display string, status int) *usermodel.UserEntity {
	t.Helper()
	registered := time.Date(2026, 9, 12, 9, 0, 0, 0, time.Local)
	u := &usermodel.UserEntity{
		Username: username, Email: email, DisplayName: &display,
		Status: status, RegisteredAt: &registered,
	}
	if err := e.users.Create(context.Background(), u); err != nil {
		t.Fatalf("建客户 %s 失败: %v", username, err)
	}
	if err := e.users.UpdateFields(context.Background(), u.ID, map[string]any{"email_verified_at": registered}); err != nil {
		t.Fatalf("标记邮箱已验证失败: %v", err)
	}
	return u
}

func (e *customerE2EEnv) mkE2EOrder(t *testing.T, no, status string, userID uint64, total int64, at time.Time) {
	t.Helper()
	uid := userID
	row := &ordermodel.OrderEntity{
		ProjectID: e.project, OrderNo: no, Status: status, UserID: &uid,
		Total: total, Currency: "CNY", CreateTime: at, UpdateTime: at,
		Attribution: json.RawMessage("{}"),
	}
	if err := e.orders.Create(context.Background(), row); err != nil {
		t.Fatalf("落单 %s 失败: %v", no, err)
	}
}

func (e *customerE2EEnv) get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func (e *customerE2EEnv) post(t *testing.T, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	return rec
}

// TestCustomerPageE2EListDetailAndWrites 一条链路走完：列表 → 详情（含跨模块订单摘要）→ 停用 → 解锁。
func TestCustomerPageE2EListDetailAndWrites(t *testing.T) {
	env := newCustomerE2EEnv(t)
	if env == nil {
		return
	}

	customer := env.mkE2ECustomer(t, "zoe", "zoe@example.com", "佐伊", usermodel.UserStatusActive)
	env.mkE2ECustomer(t, "other", "other@example.com", "别人", usermodel.UserStatusActive)
	env.mkE2EOrder(t, "E-1", ordermodel.OrderStatusPaid, customer.ID, 12000,
		time.Date(2026, 9, 10, 10, 0, 0, 0, time.Local))
	env.mkE2EOrder(t, "E-2", ordermodel.OrderStatusCancelled, customer.ID, 99999,
		time.Date(2026, 9, 11, 10, 0, 0, 0, time.Local))

	t.Run("列表页渲染真实客户并支持关键词筛选", func(t *testing.T) {
		rec := env.get(t, "/admin/customers?keyword=zoe")
		if rec.Code != http.StatusOK {
			t.Fatalf("列表页返回 %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "zoe@example.com") || !strings.Contains(body, "佐伊") {
			t.Fatalf("列表页没有渲染出客户")
		}
		if strings.Contains(body, "other@example.com") {
			t.Errorf("关键词筛选取不到的效果：另一个客户也出现在结果里")
		}
		// 计数是全局口径（不受筛选影响）。
		if !strings.Contains(body, "全部 2") {
			t.Errorf("计数条应显示全部 2 个账号")
		}
	})

	t.Run("详情页的订单摘要走订单模块的只读聚合", func(t *testing.T) {
		rec := env.get(t, "/admin/customers/detail?id="+strconv.FormatUint(customer.ID, 10))
		if rec.Code != http.StatusOK {
			t.Fatalf("详情页返回 %d", rec.Code)
		}
		body := rec.Body.String()
		for _, want := range []string{
			"zoe@example.com", "订单数 2", "¥120.00", "E-2",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("详情页缺少 %q", want)
			}
		}
		// 取消单的金额不能混进累计消费（口径由订单模块决定，页面只展示）。
		if strings.Contains(body, "¥1,119.99") || strings.Contains(body, "1119.99") {
			t.Errorf("累计消费把取消单也算进去了")
		}
	})

	t.Run("停用账号会真的落库", func(t *testing.T) {
		rec := env.post(t, "/admin/customers/status", url.Values{
			"customerId": {strconv.FormatUint(customer.ID, 10)},
			"toStatus":   {strconv.Itoa(usermodel.UserStatusDisabled)},
		})
		if rec.Code != http.StatusFound {
			t.Fatalf("停用应 302 回列表页，实际 %d", rec.Code)
		}
		stored, err := env.users.GetByID(context.Background(), customer.ID)
		if err != nil {
			t.Fatalf("回读客户失败: %v", err)
		}
		if stored.Status != usermodel.UserStatusDisabled {
			t.Fatalf("停用没有落库，实际状态 %d", stored.Status)
		}
	})

	t.Run("解除锁定会清掉锁定时间与失败计数", func(t *testing.T) {
		ctx := context.Background()
		if err := env.users.UpdateFields(ctx, customer.ID, map[string]any{
			"locked_until_time":   time.Now().Add(30 * time.Minute),
			"login_failure_count": 5,
		}); err != nil {
			t.Fatalf("设置锁定失败: %v", err)
		}
		rec := env.post(t, "/admin/customers/unlock", url.Values{
			"customerId": {strconv.FormatUint(customer.ID, 10)},
		})
		if rec.Code != http.StatusFound {
			t.Fatalf("解锁应 302 回列表页，实际 %d", rec.Code)
		}
		loc := rec.Header().Get("Location")
		if !strings.Contains(loc, "ok=") {
			t.Errorf("解锁后应带成功回执：%s", loc)
		}
		stored, err := env.users.GetByID(ctx, customer.ID)
		if err != nil {
			t.Fatalf("回读客户失败: %v", err)
		}
		if stored.LockedUntilTime != nil || stored.LoginFailureCount != 0 {
			t.Fatalf("解锁没有清干净：lockedUntil=%v count=%d", stored.LockedUntilTime, stored.LoginFailureCount)
		}
	})

	t.Run("解析出的订单摘要与订单模块的契约一致", func(t *testing.T) {
		// 页面上的数字必须来自契约，而不是页面自己算的 —— 这里直接调契约核对一次。
		orders := orderservice.NewService(
			env.orders, ordermodel.NewOrderItemModel(env.db), ordermodel.NewOrderStatusLogModel(env.db),
			ordermodel.NewCouponModel(env.db), ordermodel.NewReturnModel(env.db), nil, nil, nil)
		res, err := orders.CustomerOrderSummaryOf(context.Background(), &orderdto.CustomerOrderSummaryReq{
			ProjectID: env.project, UserID: customer.ID,
		})
		if err != nil {
			t.Fatalf("取订单摘要失败: %v", err)
		}
		if res.OrderCount != 2 || res.PaidOrderCount != 1 || res.TotalAmount != 12000 {
			t.Fatalf("订单摘要口径不对：%+v", res)
		}
	})
}

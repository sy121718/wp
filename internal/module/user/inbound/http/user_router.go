package userhttp

// 为什么页面不在 SetupUserRoutes 里注册（是装配顺序，不是分层洁癖）：
// 客户详情页要读订单摘要（ordercontract.CustomerOrderSummaryReader），而订单模块装配在
// user **之后** —— 订单依赖 user 的访客开号端口。所以页面注册只能发生在订单契约就绪之后，
// 由装配层在订单模块装配完成后调用一次（此时 adminPages / userAdminSvc / orderSvc /
// projectService 都已就绪）。
//
// 页面挂在装配层传入的 /admin 组上：该组已有 Session + CSRF + 权限上下文中间件
// （见 internal/routers/assembly.go 的 adminPages）。写动作额外按**对应 API 的路径**
// 走 Casbin 权限点，与 /api/customer/* 完全同源，一个字符都不改。
// pages 为 nil 时跳过注册 —— 与 rg == nil 早退同构，装配不因缺少页面组而失败。

// 访客JSON路由由 setupSiteUserRoutes 注册在独立的 /api/site/user 组。
// 该组使用访客 cookie、CSRF 与逐路由公开权限声明，不进入后台会话链。

import (
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	usercontract "go_wp/internal/module/user/contract"
	usermodel "go_wp/internal/module/user/model"
	userservice "go_wp/internal/module/user/service"
	"go_wp/internal/permission"
)

const (
	userSensitiveRateLimit  = 10
	userSensitiveRateWindow = time.Minute
)

// SetupUserRoutes 装配用户模块并注册访客路由，返回对外契约。
//
// mail 允许为 nil（邮件模块未装配时注册仍可用，只是发不出验证邮件）。
func SetupUserRoutes(
	router *gin.Engine,
	db *gorm.DB,
	mail usercontract.MailSender,
	siteName string,
) usercontract.UserService {
	// cookie 存储必须在注册路由之前就绪：装配失败时直接 panic 而不是让每个请求
	// 在运行时各自失败一次 —— 会话密钥读不到属于配置错误，早失败早发现。
	if err := setupUserCookieStore(); err != nil {
		panic("用户模块：初始化访客会话存储失败: " + err.Error())
	}

	svc := userservice.NewService(
		usermodel.NewUserModel(db),
		usermodel.NewUserProfileModel(db),
		usermodel.NewUserPreferenceModel(db),
		mail,
		siteName,
	)
	h := NewHandle(svc)
	setupSiteUserRoutes(router, h)

	// 会话没有保留期任务：状态与设备台账都在 Redis，随 TTL 自然消失
	// （原先的 user_sessions 台账清理任务随那张表一起删除）。
	return svc
}

// SetupCustomerAdminRoutes 挂载后台客户管理路由（挂 authorizedAPI 组）。
//
// svc 为 nil 时直接不注册：装配缺陷应该由调用方（routes.go 的断言）炸掉，
// 而不是在这里注册一批「一调就 500」的接口。
func SetupCustomerAdminRoutes(rg *permission.RouteGroup, svc usercontract.CustomerAdminPort) {
	if rg == nil || svc == nil {
		return
	}
	h := NewCustomerHandle(svc)
	g := rg.Group("/customer")
	g.GET("/list", permission.UserCustomerList, h.ListCustomers)
	g.GET("/get", permission.UserCustomerDetail, h.GetCustomer)
	g.POST("/status", permission.UserCustomerStatus, h.SetCustomerStatus)
	g.POST("/unlock", permission.UserCustomerUnlock, h.UnlockCustomer)
	g.POST("/bulk-status", permission.UserCustomerBulkStatus, h.BulkCustomerStatus)
	g.POST("/bulk-unlock", permission.UserCustomerBulkUnlock, h.BulkCustomerUnlock)
}

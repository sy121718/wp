package userhttp

// user_router.go — 用户模块（访客账号）的装配与路由注册。
//
// 挂载位置：**直接挂在 engine 上**（公开路由），不进 /api 那组。
// 原因见包注释：/api 挂着管理后台的三件套（Session + CSRF + Casbin），
// 而访客账号没有权限点、也不该进 Casbin 的策略表。
// 这里的链路是「自己的 cookie 会话 + 自己的 CSRF token」，没有 Casbin —— 少一层不是省事，
// 而是把「谁有权做什么」收敛到业务代码：访客能做的只有「操作自己的账号」。
//
// 公开路由的先例：mailhttp.SetupTrackingRoutes（营销追踪端点）。

import (
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/internal/middleware/builtin"
	usercontract "go_wp/internal/module/user/contract"
	usermodel "go_wp/internal/module/user/model"
	userservice "go_wp/internal/module/user/service"
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
		usermodel.NewUserSessionModel(db),
		usermodel.NewUserProfileModel(db),
		usermodel.NewUserPreferenceModel(db),
		mail,
		siteName,
	)
	h := NewHandle(svc)

	// CSRF：用访客自己的 token 存取器（存在访客会话里）。
	// 直接用 builtin.CSRFMiddleware() 会去读后台的 gowp_session，
	// 结果就是「访客表单永远 403」或「把后台的 token 顶掉」。
	csrf := builtin.CSRFMiddlewareWith(userCSRFStore{})

	// 公开组：注册 / 验证 / 登录 / 登出 / 密码重置。未登录也要能访问。
	public := router.Group("/user", attachUserSession(svc), csrf)
	{
		public.GET("/register", h.ShowRegister)
		sensitive := builtin.RequestRateLimitMiddleware(userSensitiveRateLimit, userSensitiveRateWindow)
		public.POST("/register", sensitive, h.DoRegister)
		public.GET("/activate", h.Activate)
		public.POST("/resend", sensitive, h.DoResendActivation)
		public.GET("/login", h.ShowLogin)
		public.POST("/login", sensitive, h.DoLogin)
		public.POST("/logout", h.Logout)
		public.GET("/forgot", h.ShowForgot)
		public.POST("/forgot", sensitive, h.DoForgot)
		public.GET("/reset", h.ShowReset)
		public.POST("/reset", sensitive, h.DoReset)
	}

	// 账号中心：必须已登录。
	private := router.Group("/user", attachUserSession(svc), csrf, requireUser())
	{
		private.GET("/account", h.ShowAccount)
		private.POST("/account/profile", h.DoUpdateProfile)
		private.POST("/account/preference", h.DoUpdatePreference)
		private.POST("/account/password", h.DoChangePassword)
		private.POST("/account/sessions/revoke", h.DoRevokeSession)
		private.POST("/account/sessions/revoke-others", h.DoRevokeOtherSessions)
	}

	return svc
}

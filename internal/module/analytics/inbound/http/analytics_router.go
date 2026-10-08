package analyticshttp

// 页面挂在装配层传入的 /admin 组上（Session + CSRF + 权限上下文已由装配层挂好）。
// 这里没有写操作，因此不挂 CasbinMiddlewareForPath —— 权限点 analytics:view 用在
// 菜单过滤与只读 API 的 Casbin 策略上。
// 注意：i18n 文案词条页仍归 dashboard，不随本次搬迁移动。

// 公开打点直挂引擎（与 mail 的追踪端点、cart 的支付回调同一位置与同一理由）：
// 访客浏览器不会带后台会话与 CSRF token，把它挂进 /api 三层链只会得到 401/403。
// 后台只读聚合挂 authorizedAPI（Session + CSRF + Casbin，权限点 analytics:view）。

import (
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/config"
	"go_wp/internal/middleware/builtin"
	"go_wp/internal/module/analytics/contract"
	"go_wp/internal/module/analytics/model"
	"go_wp/internal/module/analytics/service"
	"go_wp/internal/module/project/contract"
	"go_wp/internal/permission"
	"go_wp/pkg/logger"
)

const (
	analyticsCollectRateLimit  = 60
	analyticsCollectRateWindow = time.Minute
)

// SetupAnalyticsRoutes 装配访问统计模块并注册路由，返回模块契约。
//
// sessionSecret 为会话密钥，**只作兜底输入**：匿名 hash 的盐优先取配置项
// analytics.pepper（独立盐），未配置时由会话密钥经 HKDF 派生（SEC-013）——
// 不再把会话密钥直接当盐用。IP 与访客标识只以带盐哈希落库。
// rg 为已挂 Session + CSRF + Casbin 的业务 API 组；router 为引擎（公开路由挂它）；
// pages 为装配层传入的后台页面组（/admin，已挂 Session + CSRF + 权限上下文），
// projects 是统计页选工程要用的契约（页面只依赖 contract，不碰 project 的 model/service）。
// pages 为 nil 时只跳过页面注册。
func SetupAnalyticsRoutes(rg *permission.RouteGroup, router *gin.Engine, db *gorm.DB,
	sessionSecret string, pages *gin.RouterGroup,
	projects projectcontract.ProjectService) analyticscontract.AnalyticsService {
	svc := analyticsservice.NewService(analyticsmodel.NewModel(db), resolveAnonSalt(sessionSecret))
	// 注入必须排在两个调度器**之前**：StartAnalyticsRetentionScheduler 与
	// StartAnalyticsRollupScheduler 都会先跑一次再进定时循环，晚一步注入的话首跑就落在
	// 「契约缺失」上 —— 保留期清理与汇总各少一轮，而日志里只会有一条错误，很容易被当成
	// 一次性抖动。同一个实例既提供「列出全部工程」（汇总扇出用），也提供「各工程的访问明细
	// 保留几天」（保留期清理用）—— 后者由 SetProjects 内部按窄接口取。
	svc.SetProjects(projects)
	analyticsservice.StartAnalyticsRetentionScheduler(svc)
	// 按天预聚合（审计 DB-005 / IDX-010）：历史窗口的统计查询读汇总表，
	// 成本与明细行数脱钩。与保留期任务同形：先跑一次再每小时一次。
	analyticsservice.StartAnalyticsRollupScheduler(svc)
	handle := NewHandle(svc)

	// 公开打点（访问面）：只写一条浏览记录，没有查询与删除能力。
	if router != nil {
		router.POST("/analytics/collect",
			builtin.RequestRateLimitMiddleware(analyticsCollectRateLimit, analyticsCollectRateWindow),
			handle.Collect)
	}
	// 后台只读聚合（后台统计页与只读 API 共用同一份实现）。
	if rg != nil {
		g := rg.Group("/analytics")
		g.GET("/summary", permission.AnalyticsView, handle.Summary)
	}

	// 后台统计页（/admin/analytics）：与上面只读 API 共用同一份 Summary 实现。
	setupAnalyticsPageRoutes(pages, svc, projects)

	return svc
}

// resolveAnonSalt 解析打点匿名 hash 的盐（SEC-013）。
//
// 盐的来源、轮换方式与历史哈希的去向见 analyticsservice.ResolveAnonSalt。
// 这里只做装配层该做的事：从配置取独立盐（analytics.pepper）、交给解析函数、
// 把降级形态记成日志 —— 装配期是唯一能发现降级的时机，
// 运行起来之后「IP 哈希为什么对不上历史」不会自己暴露成错误。
func resolveAnonSalt(sessionSecret string) string {
	configured := ""
	if v, err := config.GetViper(); err == nil && v != nil {
		configured = v.GetString("analytics.pepper")
	}
	salt, independent := analyticsservice.ResolveAnonSalt(configured, sessionSecret)
	if independent {
		if analyticsservice.WeakAnonSalt(salt) {
			logger.Scene("analytics").Warn("analytics.pepper 过短（建议用 openssl rand -hex 32 生成 64 位十六进制）：短盐可被枚举反查，带盐哈希会退化成裸哈希")
		}
		return salt
	}
	if salt == "" {
		logger.Scene("analytics").Warn("打点匿名哈希的盐为空（未配置 analytics.pepper 且会话密钥为空）：裸哈希可被枚举反查，请配置独立盐（openssl rand -hex 32）")
		return salt
	}
	logger.Scene("analytics").Warn("未配置 analytics.pepper：打点盐由会话密钥经 HKDF 派生，不泄露会话密钥，但轮换会话密钥会使历史 IP 哈希断档；建议配置独立盐（openssl rand -hex 32）")
	return salt
}

package analyticshttp

import (
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/config"
	"go_wp/internal/middleware/builtin"
	analyticscontract "go_wp/internal/module/analytics/contract"
	analyticsmodel "go_wp/internal/module/analytics/model"
	analyticsservice "go_wp/internal/module/analytics/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/permission"
	"go_wp/pkg/logger"
)

const (
	analyticsCollectRateLimit  = 60
	analyticsCollectRateWindow = time.Minute
)

func SetupAnalyticsRoutes(rg *permission.RouteGroup, router *gin.Engine, db *gorm.DB,
	sessionSecret string, pages *gin.RouterGroup,
	projects projectcontract.ProjectService) analyticscontract.AnalyticsService {
	svc := analyticsservice.NewService(analyticsmodel.NewModel(db), resolveAnonSalt(sessionSecret))

	svc.SetProjects(projects)
	analyticsservice.StartAnalyticsRetentionScheduler(svc)

	analyticsservice.StartAnalyticsRollupScheduler(svc)
	handle := NewHandle(svc)

	if router != nil {
		router.POST("/analytics/collect",
			builtin.RequestRateLimitMiddleware(analyticsCollectRateLimit, analyticsCollectRateWindow),
			handle.Collect)
	}

	if rg != nil {
		g := rg.Group("/analytics")
		g.GET("/summary", permission.AnalyticsView, handle.Summary)
	}

	setupAnalyticsPageRoutes(pages, svc, projects)

	return svc
}

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

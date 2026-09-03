// Package middleware 是 Gin HTTP 中间件的入口与装配层。
//
// 职责：
//   - Setup() — 从 config 读取配置，按固定顺序挂载全局中间件链
//   - 具体的中间件实现全部委托给 builtin 子包（internal/middleware/builtin）
//
// 中间件链顺序（由上到下依次执行）：
//  1. Recovery          — 兜底 panic 恢复，返回 500
//  2. CORS              — 跨域资源共享，预检请求直接返回 204
//  3. SecurityHeaders   — 安全响应头（X-Content-Type-Options, X-Frame-Options, CSP）
//  4. RequestBodyLimit  — 限制请求体大小，按 /upload 路径区分普通/上传限制
//  5. RequestRateLimit  — 固定窗口限流（按 IP+ 路径），由配置开关
//  6. RequestLogCapture — 结构化 HTTP 请求日志（按配置开关）
//
// 配置读取方式：
//
//	通过 config.GetViper() 获取全局 viper 实例，自行读取对应的配置键值。
//	不依赖 main 函数传参，降低调用方耦合。
package middleware

import (
	"strings"
	"time"

	"go_wp/config"
	"go_wp/internal/middleware/builtin"
	"go_wp/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// Setup 读取 viper 配置并按固定顺序挂载默认中间件链。
//
// 挂载方式：
//
//	engine.Use() — 全局生效，影响所有已注册和未来注册的路由
//
// 安全说明：
//   - bodyLimit / rateLimit / logCapture 的配置值通过 viper 读取，读取失败时使用安全默认值
//   - rateLimit 和 logCapture 由配置开关控制是否启用，默认关闭
//
// 参数 engine 不能为 nil，否则直接返回。
func Setup(engine *gin.Engine) {
	if engine == nil {
		return
	}

	cfg, err := config.GetViper()
	if err != nil {
		logger.Scene("init").With("err", err).Warn("中间件配置读取失败，使用默认值")
		cfg = nil
	}

	engine.Use(builtin.Recovery())
	engine.Use(builtin.CORS())
	engine.Use(builtin.SecurityHeadersMiddleware())
	engine.Use(buildBodyLimit(cfg))
	if isRateLimitEnabled(cfg) {
		engine.Use(builtin.RequestRateLimitMiddleware(getRateLimit(cfg)))
	}
	engine.Use(builtin.RequestLogCaptureMiddleware(isLogCaptureEnabled(cfg)))
}

// buildBodyLimit 从配置中解析请求体大小限制，构造 BodyLimit 中间件。
//
// 配置键：
//   - server.request_body_limit — 普通请求体上限（默认 2MB）
//   - server.upload_body_limit  — 上传请求体上限（默认 32MB）
//
// 当配置值解析失败或为 0 时使用默认值。
func buildBodyLimit(cfg *viper.Viper) gin.HandlerFunc {
	requestLimit := int64(2 * 1024 * 1024) // 2MB 默认
	uploadLimit := int64(32 * 1024 * 1024) // 32MB 默认

	if cfg != nil {
		if v := parseSize(cfg.GetString("server.request_body_limit")); v > 0 {
			requestLimit = v
		}
		if v := parseSize(cfg.GetString("server.upload_body_limit")); v > 0 {
			uploadLimit = v
		}
	}

	return builtin.RequestBodyLimitMiddleware(requestLimit, uploadLimit)
}

// isRateLimitEnabled 检查配置是否启用了限流。
// 配置键：server.rate_limit_enabled
func isRateLimitEnabled(cfg *viper.Viper) bool {
	return cfg != nil && cfg.GetBool("server.rate_limit_enabled")
}

// getRateLimit 从配置中读取限流参数。
//
// 配置键：
//   - server.rate_limit_limit  — 窗口内允许的最大请求数（默认 120）
//   - server.rate_limit_window — 时间窗口（默认 1m）
//
// 当配置值未设置或非法时使用默认值。
func getRateLimit(cfg *viper.Viper) (int, time.Duration) {
	limit := 120
	window := time.Minute

	if cfg != nil {
		if v := cfg.GetInt("server.rate_limit_limit"); v > 0 {
			limit = v
		}
		if v := cfg.GetDuration("server.rate_limit_window"); v > 0 {
			window = v
		}
	}

	return limit, window
}

// isLogCaptureEnabled 检查配置是否开启了 HTTP 请求日志捕获。
// 配置键：log.capture.http
func isLogCaptureEnabled(cfg *viper.Viper) bool {
	return cfg != nil && cfg.GetBool("log.capture.http")
}

// parseSize 将可读的大小字符串（如 "2MB"、"100KB"）解析为字节数。
//
// 支持的格式：GB、MB、KB、B（不区分大小写）。
// 解析失败或输入空字符串时返回 0。
func parseSize(raw string) int64 {
	raw = strings.ToUpper(strings.TrimSpace(raw))
	if raw == "" {
		return 0
	}

	units := []struct {
		Suffix string
		Scale  int64
	}{
		{"GB", 1024 * 1024 * 1024},
		{"MB", 1024 * 1024},
		{"KB", 1024},
		{"B", 1},
	}

	for _, unit := range units {
		if strings.HasSuffix(raw, unit.Suffix) {
			number := strings.TrimSuffix(raw, unit.Suffix)
			val := parseInt64(strings.TrimSpace(number))
			if val > 0 {
				return val * unit.Scale
			}
		}
	}

	return parseInt64(raw)
}

// parseInt64 将纯数字字符串解析为 int64。
// 遇到非数字字符立即返回 0，不做容错处理。
func parseInt64(s string) int64 {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int64(c-'0')
	}
	return n
}

// CloseComponents 关闭所有运行时组件。
// 代理到 config.CloseComponents()，供外部统一调用入口。
func CloseComponents() error {
	return config.CloseComponents()
}

// ValidateReady 检查所有组件是否就绪。
// 代理到 config.ValidateReady()，用于 /readyz 健康检查。
func ValidateReady() error {
	return config.ValidateReady()
}

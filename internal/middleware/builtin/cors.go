package builtin

import (
	"net/http"
	"strings"

	appconfig "go_wp/config"
	"go_wp/pkg/logger"

	"github.com/gin-gonic/gin"
)

// corsAllowedOriginsKey CORS 白名单的配置键（server 段）。
// config.yaml 示例：
//
//	server:
//	  cors_allowed_origins: ["https://admin.example.com"]
const corsAllowedOriginsKey = "server.cors_allowed_origins"

// CORSConfig CORS 中间件的可选配置参数。
//
// 字段说明：
//   - AllowedOrigins：允许的来源域名列表。nil/空表示未配置白名单，
//     行为取决于运行模式（见 CORS 注释）；非空则仅匹配白名单来源。
//   - AllowedMethods：允许的 HTTP 方法
//   - AllowedHeaders：允许的请求头
type CORSConfig struct {
	AllowedOrigins []string
	AllowedMethods []string
	AllowedHeaders []string
}

// defaultCORSConfig 返回默认 CORS 配置。
//
// 默认行为（AllowedOrigins 为空时由 CORS 按运行模式决定，见 CORS 注释）：
//   - 只允许 GET、POST、OPTIONS
//   - 允许常见的认证和签名相关请求头
func defaultCORSConfig() CORSConfig {
	return CORSConfig{
		AllowedOrigins: nil,
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{
			"Origin",
			"Content-Type",
			"Accept",
			"Authorization",
			"X-Requested-With",
			"X-Timestamp",
			"X-Signature",
			"X-Nonce",
			"X-CSRF-Token",
		},
	}
}

// CORS 返回一个 CORS 跨域中间件。
//
// 白名单来源（优先级从高到低）：
//  1. 显式传入的 cfg[0].AllowedOrigins（测试 / 局部挂载时使用）
//  2. 配置 server.cors_allowed_origins（生产使用，config.yaml 维护）
//  3. 两者皆空 → 无白名单，按运行模式回退：
//     - release：拒绝非同源请求，不反射 Origin（fail-closed，浏览器收不到
//       Access-Control-Allow-Origin 即拒绝跨域读取），启动时打一条 Warn；
//     - debug / test：反射请求 Origin 并允许携带 Cookie（本地开发便利），
//       启动时打一条 Warn 提醒生产必须配置白名单。
//
// 白名单非空时：仅命中白名单的 Origin 下发
// Access-Control-Allow-Origin（精确回显该 Origin）并保留
// Access-Control-Allow-Credentials: true；未命中不设置任何 CORS 头，
// 与 Allow-Credentials 永不冲突（反射任意 Origin + credentials 的组合已移除）。
//
// 处理逻辑：
//   - 无 Origin 的请求（同源 / curl）直接放行，不附加 CORS 头
//   - OPTIONS 预检请求 → 命中则附加 CORS 头后返回 204；未命中直接 204（不附加）
//   - 普通请求 → 命中则附加 CORS 头后继续
func CORS(cfg ...CORSConfig) gin.HandlerFunc {
	config := defaultCORSConfig()
	if len(cfg) > 0 {
		if origins := cfg[0].AllowedOrigins; origins != nil {
			config.AllowedOrigins = normalizeOrigins(origins)
		}
		if methods := cfg[0].AllowedMethods; len(methods) > 0 {
			config.AllowedMethods = methods
		}
		if headers := cfg[0].AllowedHeaders; len(headers) > 0 {
			config.AllowedHeaders = headers
		}
	}

	// 未显式传白名单时，从配置读取 server.cors_allowed_origins。
	if len(config.AllowedOrigins) == 0 {
		if v, err := appconfig.GetViper(); err == nil {
			if origins := v.GetStringSlice(corsAllowedOriginsKey); len(origins) > 0 {
				config.AllowedOrigins = normalizeOrigins(origins)
			}
		}
	}

	// 无白名单时的运行模式回退策略（在工厂内计算一次，避免每请求判断）。
	allowAny := len(config.AllowedOrigins) == 0
	reflectInDev := allowAny && gin.Mode() != gin.ReleaseMode
	if allowAny {
		if reflectInDev {
			logger.Scene("middleware").Warn("CORS 未配置白名单（server.cors_allowed_origins 为空），debug/test 模式回退为反射任意 Origin + Allow-Credentials，生产环境必须配置白名单")
		} else {
			logger.Scene("middleware").Warn("CORS 未配置白名单（server.cors_allowed_origins 为空）且处于 release 模式：拒绝所有跨域请求（不反射 Origin）")
		}
	}

	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")

		// OPTIONS 预检：统一 204；命中（白名单或开发回退）才附加 CORS 头，
		// 无 Origin 的 OPTIONS 也返回 204（不附加头，浏览器预检必带 Origin）。
		if c.Request.Method == http.MethodOptions {
			if origin != "" && originAllowedByPolicy(origin, config.AllowedOrigins, allowAny, reflectInDev) {
				setCORSHeaders(c, origin, config)
			}
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		if origin == "" {
			// 无 Origin：同源或非浏览器请求，不受 CORS 约束，直接放行。
			c.Next()
			return
		}

		if originAllowedByPolicy(origin, config.AllowedOrigins, allowAny, reflectInDev) {
			setCORSHeaders(c, origin, config)
		}
		c.Next()
	}
}

// originAllowedByPolicy 综合判定 Origin 是否允许：
//   - allowAny（无白名单）：仅开发回退（reflectInDev）允许，release 一律拒绝；
//   - 有白名单：精确匹配白名单（含 "*" 通配）。
func originAllowedByPolicy(origin string, allowedOrigins []string, allowAny, reflectInDev bool) bool {
	if allowAny {
		return reflectInDev
	}
	return originAllowed(origin, allowedOrigins)
}

// setCORSHeaders 向响应头写入 CORS 字段。
// 仅应在 origin 已通过白名单/回退策略判定为允许时调用。
func setCORSHeaders(c *gin.Context, origin string, config CORSConfig) {
	c.Header("Access-Control-Allow-Origin", origin)
	c.Header("Access-Control-Allow-Methods", strings.Join(config.AllowedMethods, ", "))
	c.Header("Access-Control-Allow-Headers", strings.Join(config.AllowedHeaders, ", "))
	c.Header("Access-Control-Allow-Credentials", "true")
	c.Header("Access-Control-Max-Age", "86400")
}

// originAllowed 判断请求 Origin 是否命中白名单（精确匹配，保留 "*" 通配语义）。
func originAllowed(origin string, allowedOrigins []string) bool {
	for _, allowed := range allowedOrigins {
		allowed = strings.TrimSpace(allowed)
		if allowed == "*" || strings.EqualFold(allowed, origin) {
			return true
		}
	}
	return false
}

// normalizeOrigins 清理白名单配置：去空白、去空项、按序去重。
func normalizeOrigins(origins []string) []string {
	seen := make(map[string]struct{}, len(origins))
	result := make([]string, 0, len(origins))
	for _, raw := range origins {
		origin := strings.TrimSpace(raw)
		if origin == "" {
			continue
		}
		key := strings.ToLower(origin)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, origin)
	}
	return result
}

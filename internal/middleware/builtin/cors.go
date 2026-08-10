package builtin

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// CORSConfig CORS 中间件的可选配置参数。
//
// 字段说明：
//   - AllowedOrigins：允许的来源域名列表，nil 或空切片表示允许所有来源
//   - AllowedMethods：允许的 HTTP 方法
//   - AllowedHeaders：允许的请求头
//
// 开发阶段直接使用 CORS() 即可（允许全部来源）。
// 生产环境可传入自定义配置限制来源。
type CORSConfig struct {
	AllowedOrigins []string
	AllowedMethods []string
	AllowedHeaders []string
}

// defaultCORSConfig 返回宽松的默认 CORS 配置。
//
// 默认行为：
//   - 允许任意来源（nil = 回显请求 Origin 或返回 *）
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
		},
	}
}

// CORS 返回一个 CORS 跨域中间件。
//
// 未传参时使用宽松默认值（允许所有来源），适合开发环境。
// 传参时可覆盖 AllowedOrigins / AllowedMethods / AllowedHeaders。
//
// 处理逻辑：
//   - OPTIONS 预检请求 → 直接返回 204 No Content
//   - 普通请求 → 追加跨域响应头后继续
//
// 响应头：
//
//	Access-Control-Allow-Origin      — 回显请求 Origin 或 *
//	Access-Control-Allow-Methods     — 允许的方法列表
//	Access-Control-Allow-Headers     — 允许的请求头列表
//	Access-Control-Allow-Credentials — true，允许携带 Cookie
//	Access-Control-Max-Age           — 86400 秒（24 小时），预检结果缓存时间
//
// 适用位置：全局 engine.Use()，必须在所有业务路由之前。
func CORS(cfg ...CORSConfig) gin.HandlerFunc {
	config := defaultCORSConfig()
	if len(cfg) > 0 {
		if origins := cfg[0].AllowedOrigins; origins != nil {
			config.AllowedOrigins = origins
		}
		if methods := cfg[0].AllowedMethods; len(methods) > 0 {
			config.AllowedMethods = methods
		}
		if headers := cfg[0].AllowedHeaders; len(headers) > 0 {
			config.AllowedHeaders = headers
		}
	}

	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")

		// 预检请求：不执行业务处理器，直接返回 204
		if c.Request.Method == http.MethodOptions {
			setCORSHeaders(c, origin, config)
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		setCORSHeaders(c, origin, config)
		c.Next()
	}
}

// setCORSHeaders 根据配置向响应头写入 CORS 字段。
func setCORSHeaders(c *gin.Context, origin string, config CORSConfig) {
	if len(config.AllowedOrigins) == 0 {
		if origin != "" {
			c.Header("Access-Control-Allow-Origin", origin)
		} else {
			c.Header("Access-Control-Allow-Origin", "*")
		}
	} else {
		for _, allowed := range config.AllowedOrigins {
			if strings.EqualFold(allowed, origin) || allowed == "*" {
				c.Header("Access-Control-Allow-Origin", origin)
				break
			}
		}
	}

	c.Header("Access-Control-Allow-Methods", strings.Join(config.AllowedMethods, ", "))
	c.Header("Access-Control-Allow-Headers", strings.Join(config.AllowedHeaders, ", "))
	c.Header("Access-Control-Expose-Headers", "X-New-Token")
	c.Header("Access-Control-Allow-Credentials", "true")
	c.Header("Access-Control-Max-Age", "86400")
}

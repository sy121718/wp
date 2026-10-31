package builtin

import (
	"net/http"
	"strings"

	appconfig "go_wp/config"
	"go_wp/pkg/logger"

	"github.com/gin-gonic/gin"
)

const corsAllowedOriginsKey = "server.cors_allowed_origins"

type CORSConfig struct {
	AllowedOrigins []string
	AllowedMethods []string
	AllowedHeaders []string
}

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
			"X-CSRF-Token",
		},
	}
}

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

	if len(config.AllowedOrigins) == 0 {
		if v, err := appconfig.GetViper(); err == nil {
			if origins := v.GetStringSlice(corsAllowedOriginsKey); len(origins) > 0 {
				config.AllowedOrigins = normalizeOrigins(origins)
			}
		}
	}

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

		if c.Request.Method == http.MethodOptions {
			if origin != "" && originAllowedByPolicy(origin, config.AllowedOrigins, allowAny, reflectInDev) {
				setCORSHeaders(c, origin, config)
			}
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		if origin == "" {

			c.Next()
			return
		}

		if originAllowedByPolicy(origin, config.AllowedOrigins, allowAny, reflectInDev) {
			setCORSHeaders(c, origin, config)
		}
		c.Next()
	}
}

func originAllowedByPolicy(origin string, allowedOrigins []string, allowAny, reflectInDev bool) bool {
	if allowAny {
		return reflectInDev
	}
	return originAllowed(origin, allowedOrigins)
}

func setCORSHeaders(c *gin.Context, origin string, config CORSConfig) {
	c.Header("Access-Control-Allow-Origin", origin)
	c.Header("Access-Control-Allow-Methods", strings.Join(config.AllowedMethods, ", "))
	c.Header("Access-Control-Allow-Headers", strings.Join(config.AllowedHeaders, ", "))
	c.Header("Access-Control-Allow-Credentials", "true")
	c.Header("Access-Control-Max-Age", "86400")
}

func originAllowed(origin string, allowedOrigins []string) bool {
	for _, allowed := range allowedOrigins {
		allowed = strings.TrimSpace(allowed)
		if allowed == "*" || strings.EqualFold(allowed, origin) {
			return true
		}
	}
	return false
}

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

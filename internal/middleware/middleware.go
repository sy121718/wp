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

func Setup(engine *gin.Engine) {
	if engine == nil {
		return
	}

	cfg, err := config.GetViper()
	if err != nil {
		logger.Scene("init").With("err", err).Warn("中间件配置读取失败，使用默认值")
		cfg = nil
	}

	engine.Use(builtin.RequestIDMiddleware())
	engine.Use(builtin.Recovery())
	engine.Use(builtin.CORS())
	engine.Use(builtin.SecurityHeadersMiddleware())
	engine.Use(buildBodyLimit(cfg))
	if isRateLimitEnabled(cfg) {
		engine.Use(builtin.RequestRateLimitMiddleware(getRateLimit(cfg)))
	}
	engine.Use(builtin.RequestLogCaptureMiddleware(isLogCaptureEnabled(cfg)))
}

func buildBodyLimit(cfg *viper.Viper) gin.HandlerFunc {
	requestLimit := int64(2 * 1024 * 1024)
	uploadLimit := int64(32 * 1024 * 1024)

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

func isRateLimitEnabled(cfg *viper.Viper) bool {
	if cfg == nil {
		return true
	}
	if !cfg.IsSet("server.rate_limit_enabled") {
		return true
	}
	return cfg.GetBool("server.rate_limit_enabled")
}

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

func isLogCaptureEnabled(cfg *viper.Viper) bool {
	return cfg != nil && cfg.GetBool("log.capture.http")
}

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

func CloseComponents() error {
	return config.CloseComponents()
}

func ValidateReady() error {
	return config.ValidateReady()
}

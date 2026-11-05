package middleware

import (
	"net/http"
	"strings"
	"time"

	"go_wp/pkg/enums"
	"go_wp/pkg/facing"
	"go_wp/pkg/logger"
	"go_wp/pkg/view"

	"github.com/didip/tollbooth/v7"
	"github.com/didip/tollbooth/v7/limiter"
	"github.com/gin-gonic/gin"
)

const rateLimitBucketTTL = 10 * time.Minute

func RequestRateLimitMiddleware(limit int, window time.Duration) gin.HandlerFunc {

	if limit <= 0 || window <= 0 {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	lmt := newIPRateLimiter(limit, window)

	return func(c *gin.Context) {

		if isStaticAssetRequest(c) {
			c.Next()
			return
		}

		httpError := tollbooth.LimitByRequest(lmt, c.Writer, c.Request)
		if httpError != nil {
			view.FailStatus(c, httpError.StatusCode, "rate-limit", facing.New(enums.ErrRateLimited))
			c.Abort()
			return
		}

		c.Next()
	}
}

func newIPRateLimiter(limit int, window time.Duration) *limiter.Limiter {
	lmt := tollbooth.NewLimiter(float64(limit)/window.Seconds(), &limiter.ExpirableOptions{
		DefaultExpirationTTL: rateLimitBucketTTL,
	})
	lmt.SetBurst(limit).
		SetIPLookups([]string{"RemoteAddr"}).
		SetMessage("请求过于频繁").
		SetMessageContentType("application/json; charset=utf-8").
		SetStatusCode(http.StatusTooManyRequests).
		SetOnLimitReached(func(w http.ResponseWriter, r *http.Request) {
			logger.Scene("middleware").Warn("请求触发限流，已返回 429")
		})
	return lmt
}

func isStaticAssetRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	path := c.Request.URL.Path
	return strings.HasPrefix(path, "/static/") || strings.HasPrefix(path, "/storage/")
}

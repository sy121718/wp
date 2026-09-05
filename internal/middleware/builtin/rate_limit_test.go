package builtin

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// 达到条数上限且存在过期项时：顺带清理过期项，新 key 正常计数。
func TestRateLimitStoreEvictsExpiredEntries(t *testing.T) {
	ResetRateLimitStore()

	// 预填上限个已过期 entry（窗口已结束），新 key 请求应触发 sweep。
	rateLimitMu.Lock()
	rateLimitStore = make(map[string]rateLimitEntry, rateLimitMaxEntries)
	for i := 0; i < rateLimitMaxEntries; i++ {
		rateLimitStore[fmt.Sprintf("expired|%d", i)] = rateLimitEntry{
			count:   1,
			resetAt: time.Now().Add(-time.Hour),
		}
	}
	rateLimitMu.Unlock()

	engine := gin.New()
	engine.GET("/evict", RequestRateLimitMiddleware(10, time.Minute), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/evict", nil)
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("新 key 请求应放行: got=%d want=%d", recorder.Code, http.StatusOK)
	}

	rateLimitMu.Lock()
	got := len(rateLimitStore)
	rateLimitMu.Unlock()
	if got != 1 {
		t.Fatalf("过期 entry 应被清理，map 只保留新 key: got=%d want=%d", got, 1)
	}
}

// 同窗口内条目全部活跃（无过期项可清）时：新 key 不再写入，map 被钳制在上限，请求放行不计数。
func TestRateLimitStoreCapsActiveEntries(t *testing.T) {
	ResetRateLimitStore()

	rateLimitMu.Lock()
	rateLimitStore = make(map[string]rateLimitEntry, rateLimitMaxEntries)
	for i := 0; i < rateLimitMaxEntries; i++ {
		rateLimitStore[fmt.Sprintf("active|%d", i)] = rateLimitEntry{
			count:   1,
			resetAt: time.Now().Add(time.Hour),
		}
	}
	rateLimitMu.Unlock()

	engine := gin.New()
	engine.GET("/cap", RequestRateLimitMiddleware(10, time.Minute), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/cap", nil)
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("达到上限且无过期项时新 key 应放行不计数: got=%d want=%d", recorder.Code, http.StatusOK)
	}

	rateLimitMu.Lock()
	got := len(rateLimitStore)
	rateLimitMu.Unlock()
	if got != rateLimitMaxEntries {
		t.Fatalf("map 大小应被钳制在上限: got=%d want=%d", got, rateLimitMaxEntries)
	}
}

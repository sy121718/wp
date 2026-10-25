package builtin

// request_id_test.go — 请求关联能力的契约。
//
// 为什么这条值得单独测：request id 的价值全在「能捞全」，而「捞不全」是一类**静默**
// 缺陷 —— 少打一个字段日志照写，没有任何测试会因此变红，直到真出事那天才发现关联不上。
// 所以这里断言三件缺一不可的事：
//  1. 同一次请求内，响应头 / gin.Context / request context / 日志行**是同一个值**；
//  2. 上游带来的合法 id 被沿用（网关已生成时不重复造）；
//  3. 上游带来的非法 id 被丢弃并重新生成（否则等于让人往日志里注入任意值）。

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"

	"go_wp/pkg/logger"
)

func newRequestIDRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestIDMiddleware())
	return r
}

func TestRequestIDIsConsistentWithinOneRequest(t *testing.T) {
	r := newRequestIDRouter(t)
	var (
		fromGin   string
		fromCtx   string
		respID    string
		reqHeader string
	)
	r.GET("/probe", func(c *gin.Context) {
		fromGin = RequestID(c)
		fromCtx = RequestIDFromContext(c.Request.Context())
		reqHeader = c.Request.Header.Get(HeaderRequestID)
		c.Status(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/probe", nil))

	respID = rec.Header().Get(HeaderRequestID)
	if respID == "" {
		t.Fatal("响应里没有 X-Request-Id：用户报障时说不出是哪一次请求，日志无从捞起")
	}
	// 等式断言：四处必须是同一个值，否则「按 id 捞全」这件事在某个环节就断了。
	if fromGin != respID {
		t.Errorf("gin.Context 里的 id 与响应头不一致：%q vs %q", fromGin, respID)
	}
	if fromCtx != respID {
		t.Errorf("request context 里的 id 与响应头不一致：%q vs %q —— 业务层日志会关联不上", fromCtx, respID)
	}
	if reqHeader != respID {
		t.Errorf("请求头回写的 id 与响应头不一致：%q vs %q", reqHeader, respID)
	}
	if len(respID) != 32 {
		t.Errorf("自动生成的 id 应为 32 位十六进制，得到 %q", respID)
	}
}

func TestRequestIDReusesUpstreamValue(t *testing.T) {
	r := newRequestIDRouter(t)
	r.GET("/probe", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set(HeaderRequestID, "gateway-abc.123:xyz")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if got := rec.Header().Get(HeaderRequestID); got != "gateway-abc.123:xyz" {
		t.Errorf("网关带来的合法 id 应被沿用，得到 %q —— 否则跨网关联会在这一跳断掉", got)
	}
}

func TestRequestIDRejectsMaliciousUpstreamValue(t *testing.T) {
	r := newRequestIDRouter(t)
	r.GET("/probe", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	for _, bad := range []string{
		"has space",                 // 空格
		"quote\"inject",             // 引号（可撕裂日志行）
		"newline\nfake line",        // 换行（可伪造日志行）
		"<script>alert(1)</script>", // 尖括号
		strings.Repeat("a", 65),     // 超长
	} {
		req := httptest.NewRequest(http.MethodGet, "/probe", nil)
		req.Header.Set(HeaderRequestID, bad)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		got := rec.Header().Get(HeaderRequestID)
		if got == bad {
			t.Errorf("非法上游 id %q 被直接采用了 —— 等于让人往日志与响应头里注入任意值", bad)
		}
		if len(got) != 32 {
			t.Errorf("非法上游 id %q 应触发重新生成（32 位 hex），得到 %q", bad, got)
		}
	}
}

// TestRequestIDReachesLogLine 端到端：一次请求产生的日志行里必须带 request_id。
//
// 这条是「按 id 能捞全」的最终判据 —— 前面几条只证明 id 在链路里传递，只有这条证明
// 它真的落到了**日志文件**里（logger 从 ctx 取的那一段）。
func TestRequestIDReachesLogLine(t *testing.T) {
	dir := t.TempDir()
	v := viper.New()
	v.Set("log.base_dir", dir)
	v.Set("log.level", "info")
	if err := logger.Init(v); err != nil {
		t.Fatalf("初始化 logger 失败: %v", err)
	}
	t.Cleanup(func() {
		restore := viper.New()
		restore.Set("log.base_dir", t.TempDir())
		_ = logger.Init(restore)
	})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestIDMiddleware())
	r.Use(RequestLogCaptureMiddleware(true))
	r.GET("/probe", func(c *gin.Context) {
		// 模拟业务层：用 SceneCtx 打一条，应当自动带上同一个 request_id。
		logger.SceneCtx("probe", c.Request.Context()).Info("业务日志")
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set(HeaderRequestID, "trace-me-123")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if got := rec.Header().Get(HeaderRequestID); got != "trace-me-123" {
		t.Fatalf("前置条件不成立：响应头 id = %q", got)
	}

	// 两个 scene 的日志文件里都要出现这个 id。
	for _, scene := range []string{"http", "probe"} {
		matches, err := filepath.Glob(filepath.Join(dir, scene, "*.log"))
		if err != nil || len(matches) == 0 {
			t.Fatalf("scene=%s 没有日志文件（err=%v）", scene, err)
		}
		var all []byte
		for _, f := range matches {
			b, rerr := os.ReadFile(f)
			if rerr != nil {
				t.Fatalf("读 %s 失败: %v", f, rerr)
			}
			all = append(all, b...)
		}
		if !strings.Contains(string(all), `"request_id":"trace-me-123"`) {
			t.Errorf("scene=%s 的日志里没有 request_id：\n%s", scene, string(all))
		}
	}
}

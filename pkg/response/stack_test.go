package response

import (
	"errors"
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

func initLoggerTo(t *testing.T) string {
	t.Helper()
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
	return dir
}

func readSceneLogs(t *testing.T, dir, scene string) string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, scene, "*.log"))
	var b strings.Builder
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读日志失败: %v", err)
		}
		b.Write(data)
	}
	return b.String()
}

func newProbeCtx(t *testing.T, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/probe", handler)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/probe", nil))
	return rec
}

func sampleBusinessError() error {
	return errors.New("inventory.err.not_found")
}

func TestErrorInternalLogsStackWithoutLeakingIt(t *testing.T) {
	dir := initLoggerTo(t)
	internalErr := errors.New(`pq: relation "products" does not exist`)

	rec := newProbeCtx(t, func(c *gin.Context) {
		ErrorInternal(c, "product", internalErr)
	})

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("应为 500，得到 %d", rec.Code)
	}

	body := rec.Body.String()
	for _, leak := range []string{"pq:", "products", "goroutine", ".go:"} {
		if strings.Contains(body, leak) {
			t.Errorf("响应体漏出了内部信息 %q：%s", leak, body)
		}
	}

	logs := readSceneLogs(t, dir, "product")
	if !strings.Contains(logs, "products") {
		t.Errorf("日志里没有原始错误：%s", logs)
	}
	if !strings.Contains(logs, `"stack"`) {
		t.Fatalf("日志里没有 stack 字段 —— 500 排查只能靠猜：%s", logs)
	}

	if !strings.Contains(logs, "goroutine") || !strings.Contains(logs, ".go:") {
		t.Errorf("stack 字段里没有真正的调用栈：%s", logs)
	}
}

func TestErrorAutoLogsStackForInternalError(t *testing.T) {
	dir := initLoggerTo(t)
	rec := newProbeCtx(t, func(c *gin.Context) {
		ErrorAuto(c, http.StatusBadRequest, "inventory", errors.New("connection refused"))
	})

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("内部错误应为 500，得到 %d", rec.Code)
	}
	logs := readSceneLogs(t, dir, "inventory")
	if !strings.Contains(logs, `"stack"`) || !strings.Contains(logs, "goroutine") {
		t.Errorf("ErrorAuto 的内部错误分支也必须有堆栈：%s", logs)
	}
}

func TestBusinessErrorDoesNotLogStack(t *testing.T) {
	dir := initLoggerTo(t)
	rec := newProbeCtx(t, func(c *gin.Context) {
		ErrorAuto(c, http.StatusBadRequest, "inventory", sampleBusinessError())
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("业务错误应保留 400，得到 %d", rec.Code)
	}
	logs := readSceneLogs(t, dir, "inventory")
	if strings.Contains(logs, `"stack"`) {
		t.Errorf("业务错误不该产生堆栈日志（它是预期内分支）：%s", logs)
	}
}

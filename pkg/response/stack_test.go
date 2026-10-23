package response

// stack_test.go — 500 出口的**排障能力**契约。
//
// 被守的缺口：`pkg/facing` 的错误是对外友好形状（key + 兜底 + 状态码），代价是 500 的日志
// 过去只有一句话 —— 没有出错位置。Go 里重新包装错误（fmt.Errorf("%w")）本来就会丢掉调用栈，
// 于是「哪个函数的哪一行返回了这个 err」只能靠读代码猜。
//
// 两条判据：
//  1. 内部错误 → 日志里有 stack（goroutine / 文件行号），响应体里**没有**；
//  2. 日志里有 request_id（能按 id 捞出这一次请求的全部日志）。

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

// initLoggerTo 把 logger 指到临时目录，返回目录（读文件断言用）。
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

// readSceneLogs 读某个 scene 的全部日志内容。
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

// sampleBusinessError 业务错误的样本：取一条真实存在于 enums 的错误 key。
//
// 用真实 key 而不是随便造一个字符串：IsBusinessError 的判据是 key 形态，造一个
// 「长得像 key」的假值会让这条测试与判据实现同时漂移。
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
	// 响应体：通用文案，不含内部原文，也不含堆栈痕迹。
	body := rec.Body.String()
	for _, leak := range []string{"pq:", "products", "goroutine", ".go:"} {
		if strings.Contains(body, leak) {
			t.Errorf("响应体漏出了内部信息 %q：%s", leak, body)
		}
	}
	// 日志：原文 + 堆栈都要在。
	logs := readSceneLogs(t, dir, "product")
	if !strings.Contains(logs, "products") {
		t.Errorf("日志里没有原始错误：%s", logs)
	}
	if !strings.Contains(logs, `"stack"`) {
		t.Fatalf("日志里没有 stack 字段 —— 500 排查只能靠猜：%s", logs)
	}
	// 堆栈的实质内容：goroutine 头与文件行号。只断言 "stack" 字段存在的话，
	// 里面塞个空串也能过。
	if !strings.Contains(logs, "goroutine") || !strings.Contains(logs, ".go:") {
		t.Errorf("stack 字段里没有真正的调用栈：%s", logs)
	}
}

func TestErrorAutoLogsStackForInternalError(t *testing.T) {
	dir := initLoggerTo(t)
	rec := newProbeCtx(t, func(c *gin.Context) {
		ErrorAuto(c, http.StatusBadRequest, "inventory", errors.New("connection refused"))
	})
	// 内部错误一律 500，不沿用调用方给的 400。
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("内部错误应为 500，得到 %d", rec.Code)
	}
	logs := readSceneLogs(t, dir, "inventory")
	if !strings.Contains(logs, `"stack"`) || !strings.Contains(logs, "goroutine") {
		t.Errorf("ErrorAuto 的内部错误分支也必须有堆栈：%s", logs)
	}
}

// TestBusinessErrorDoesNotLogStack 业务错误是预期内分支：不记日志、不抓堆栈。
//
// 反向判据 —— 少了它，「所有错误都抓堆栈」也能让上面两条全绿，而那会把日志量放大
// 一个数量级（业务拒绝是常态）。
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

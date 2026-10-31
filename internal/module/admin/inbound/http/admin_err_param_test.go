package adminhttp

import (
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

var pageInternalCopyForms = map[string]bool{
	adminenums.ErrInternal:                     true,
	"操作失败，请稍后重试":                               true,
	"Operation failed, please try again later": true,
}

var pageBusinessCopyForms = map[string]bool{
	adminenums.ErrRoleCodeExists: true,
	"角色编码已存在":                    true,
	"Role code already exists":   true,
}

var pageLeakMarkers = []string{
	"uk_sys_i18n_key_lang", "SQLSTATE", "23505", "42P01", "duplicate key",
	"constraint", "sys_i18n", "sys_rule", "pq:", "relation",
}

const pageDBErrText = `pq: duplicate key value violates unique constraint "uk_sys_i18n_key_lang" (SQLSTATE 23505): INSERT INTO sys_i18n (item_key, lang) VALUES ($1, $2)`

func newPageErrContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	req := httptest.NewRequest("POST", "/admin/i18n/save", nil)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	return c
}

func withPageTempLogDir(t *testing.T) func() string {
	t.Helper()
	dir := t.TempDir()
	cfg := viper.New()
	cfg.Set("log.base_dir", dir)
	cfg.Set("log.level", "debug")
	if err := logger.Init(cfg); err != nil {
		t.Fatalf("初始化 logger 失败: %v", err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	return func() string {
		files, _ := filepath.Glob(filepath.Join(dir, "admin", "app-*.log"))
		var sb strings.Builder
		for _, f := range files {
			raw, rerr := os.ReadFile(f)
			if rerr != nil {
				t.Fatalf("读取日志文件 %s 失败: %v", f, rerr)
			}
			sb.Write(raw)
		}
		return sb.String()
	}
}

func TestAdminErrParamBusinessTextStaysVisible(t *testing.T) {
	c := newPageErrContext(t)
	got := adminErrParam(c, errors.New(adminenums.ErrRoleCodeExists))
	if !pageBusinessCopyForms[got] {
		t.Fatalf("业务错误应原样透出 enums 文案，got %q", got)
	}
	for _, m := range pageLeakMarkers {
		if strings.Contains(got, m) {
			t.Fatalf("业务文案里不应出现内部片段 %q: %q", m, got)
		}
	}
}

func TestAdminErrParamInternalErrorIsCollected(t *testing.T) {
	readLog := withPageTempLogDir(t)
	c := newPageErrContext(t)

	got := adminErrParam(c, errors.New(pageDBErrText))
	if !pageInternalCopyForms[got] {
		t.Fatalf("内部错误应返回归口文案，got %q", got)
	}
	for _, m := range pageLeakMarkers {
		if strings.Contains(got, m) {
			t.Fatalf("归口文案里不应出现内部片段 %q: %q", m, got)
		}
	}

	logText := readLog()
	if !strings.Contains(logText, "uk_sys_i18n_key_lang") || !strings.Contains(logText, "SQLSTATE") {
		t.Fatalf("内部错误原文必须进日志（收口 ≠ 吞掉），日志内容=%s", logText)
	}

	if !strings.Contains(logText, "只对外给归口文案") {
		t.Fatalf("日志里应能定位到场景与事件，日志内容=%s", logText)
	}
}

func TestAdminErrParamNilErrorIsCollected(t *testing.T) {
	c := newPageErrContext(t)
	got := adminErrParam(c, nil)
	if !pageInternalCopyForms[got] {
		t.Fatalf("nil 错误应返回归口文案，got %q", got)
	}
}

func TestAdminErrParamCarriesParamKey(t *testing.T) {
	c := newPageErrContext(t)

	got := adminErrParam(c, errors.New(adminenums.ErrAccountLocked+"|30分钟"))
	if got == adminenums.ErrInternal || got == "操作失败，请稍后重试" ||
		got == "Operation failed, please try again later" {
		t.Fatalf("带参的业务文案不应被归口：got %q", got)
	}
	if !strings.Contains(got, "30分钟") {
		t.Fatalf("带参文案应保留参数：got %q", got)
	}
}

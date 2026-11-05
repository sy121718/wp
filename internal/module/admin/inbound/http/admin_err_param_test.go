package adminhttp

import (
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

var pageInternalCopyForms = map[string]bool{"系统内部错误，请稍后重试": true}

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

// 本文件直接测 pkg/response 的取词出口 MessageOf。
//
// 原先测的是 admin 包内的 adminErrParam 薄包装（body 只有一行 return MessageOf），
// 那个包装已随批量删除的逐条回执一起删除 —— 留着测「包装再包装」只会掩盖
// 出口本身的行为。现在测出口：业务错误取词、内部错误归口且日志留原文、nil 精确空串。
func TestMessageOfBusinessTextStaysVisible(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{adminenums.ErrRoleCodeExists: {"zh-CN": "角色编码已存在"}}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	c := newPageErrContext(t)
	got := response.MessageOf(c, "admin", errors.New(adminenums.ErrRoleCodeExists))
	if !pageBusinessCopyForms[got] {
		t.Fatalf("业务错误应原样透出 enums 文案，got %q", got)
	}
	for _, m := range pageLeakMarkers {
		if strings.Contains(got, m) {
			t.Fatalf("业务文案里不应出现内部片段 %q: %q", m, got)
		}
	}
}

func TestMessageOfInternalErrorIsCollected(t *testing.T) {
	readLog := withPageTempLogDir(t)
	c := newPageErrContext(t)

	got := response.MessageOf(c, "admin", errors.New(pageDBErrText))
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

	// 日志事件是 pkg/response 给的那条固定串，并带请求 path 便于定位。
	if !strings.Contains(logText, "handler 内部错误") {
		t.Fatalf("日志里应能定位到事件（handler 内部错误），日志内容=%s", logText)
	}
	if !strings.Contains(logText, "/admin/i18n/save") {
		t.Fatalf("日志里应带请求 path 便于定位，日志内容=%s", logText)
	}
}

// MessageOf(nil) 是「没有错误」而不是「内部错误」：必须精确返回空串。
//
// 早先这条断言期望归口文案，那是把「调用方传了 nil」当成故障；真实语义是
// 没有失败就没有 message，前端拿到空串什么也不弹。
func TestMessageOfNilErrorIsExactlyEmpty(t *testing.T) {
	c := newPageErrContext(t)
	got := response.MessageOf(c, "admin", nil)
	if got != "" {
		t.Fatalf("nil 错误应精确返回空串，got %q", got)
	}
}

func TestMessageOfCarriesParamKey(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{adminenums.ErrAccountLocked: {"zh-CN": "账号已被锁定，请 %s 后重试"}}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	c := newPageErrContext(t)

	got := response.MessageOf(c, "admin", errors.New(adminenums.ErrAccountLocked+"|30分钟"))
	if got == adminenums.ErrInternal || got == "操作失败，请稍后重试" ||
		got == "Operation failed, please try again later" {
		t.Fatalf("带参的业务文案不应被归口：got %q", got)
	}
	if !strings.Contains(got, "30分钟") {
		t.Fatalf("带参文案应保留参数：got %q", got)
	}
}

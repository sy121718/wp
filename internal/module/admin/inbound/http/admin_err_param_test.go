package adminhttp

// admin_err_param_test.go — 页面路径错误文案归口（adminErrParam）的单测。
//
// 为什么单独钉这一层：页面路径不能返回 JSON，错误只能以「整页提示（shell.RenderJump）」
// 或「模板数据」的形态出网，而这两条路都**不经过** response 的 translate —— 一旦把 err.Error()
// 直传，PostgreSQL 原文（表名 sys_i18n、约束名、SQLSTATE 23505）就会被渲染在后台页面上，
// 与 JSON body 里的泄漏面完全等价。本文件把「命中白名单原样、未命中归口」钉死，
// 页面级的提示页断言在 public/test/admin/feature/admin_page_err_param_test.go。

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

// 归口文案与业务文案的全部形态（key / zh-CN / en-US）。i18n 是否已初始化在本包内不确定
// （同包先后顺序不固定），所以按集合断言，不依赖某个具体译文 —— 与 feature 包同样的口径。
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

// pageLeakMarkers 内部错误原文里绝不能出现在页面提示里的片段（照 PostgreSQL 真实报错拼）。
//
// 约束名用**库里真实存在的名字**（`init_schema.sql` 的 `uk_sys_i18n_key_lang`）：
// 以前这里写的是拼错的 `uq_sys_i18n_item_key_lang`——那样样本永远不可能出现在真实报错里，
// 于是一条只会匹配「不存在的名字」的断言永远绿，真正的泄漏形态没人钉。
var pageLeakMarkers = []string{
	"uk_sys_i18n_key_lang", "SQLSTATE", "23505", "42P01", "duplicate key",
	"constraint", "sys_i18n", "sys_rule", "pq:", "relation",
}

// pageDBErrText 一条「长得就像会泄漏」的驱动错误原文（Go 原始字符串，避免转义噪声）。
const pageDBErrText = `pq: duplicate key value violates unique constraint "uk_sys_i18n_key_lang" (SQLSTATE 23505): INSERT INTO sys_i18n (item_key, lang) VALUES ($1, $2)`

// newPageErrContext 组一个带 GET query 的测试上下文（归口助手读 user_id 与语言都从它取）。
func newPageErrContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	req := httptest.NewRequest("POST", "/admin/i18n/save", nil)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	return c
}

// withPageTempLogDir 把 logger 指到临时目录，返回读取该目录全部 admin 场景日志的闭包。
// 「文案不能泄漏」与「原文不能丢」是两条判据：只断言前者，把错误彻底吞掉也能通过。
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

// TestAdminErrParamBusinessTextStaysVisible 命中白名单 → 文案原样（页面要据此提示「哪一项不合法」）。
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

// TestAdminErrParamInternalErrorIsCollected 未命中 → 归口文案，且原文不进返回文本、只进日志。
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
	if !strings.Contains(logText, "admin 接口内部错误") {
		t.Fatalf("日志里应能定位到场景与事件，日志内容=%s", logText)
	}
}

// TestAdminErrParamNilErrorIsCollected nil 错误也必须给出可展示的一句话（页面不能显示空串）。
func TestAdminErrParamNilErrorIsCollected(t *testing.T) {
	c := newPageErrContext(t)
	got := adminErrParam(c, nil)
	if !pageInternalCopyForms[got] {
		t.Fatalf("nil 错误应返回归口文案，got %q", got)
	}
}

// TestAdminErrParamCarriesParamKey 带参形态（key|param）与 JSON 出口同协议：key 命中白名单即放行。
func TestAdminErrParamCarriesParamKey(t *testing.T) {
	c := newPageErrContext(t)
	// ErrAccountLocked 的带参形态由 service 用 fmt.Errorf("%s|%s", key, param) 构造
	got := adminErrParam(c, errors.New(adminenums.ErrAccountLocked+"|30分钟"))
	if got == adminenums.ErrInternal || got == "操作失败，请稍后重试" ||
		got == "Operation failed, please try again later" {
		t.Fatalf("带参的业务文案不应被归口：got %q", got)
	}
	if !strings.Contains(got, "30分钟") {
		t.Fatalf("带参文案应保留参数：got %q", got)
	}
}

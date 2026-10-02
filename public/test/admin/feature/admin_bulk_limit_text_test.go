package feature

// admin_bulk_limit_text_test.go — admin 六领域批量删除的 id 超限路径：文案走 shell 的受控出口。
//
// 背景（本批）：shell.BulkIDs 的超限错误此前是 fmt.Errorf 拼出来的字符串，七个批量入口
// 直传 berr.Error() 只能靠「已知受控」的注释 + 门禁豁免放行。现在它是**带 sentinel 的类型**
//（shell.ErrBulkIDsTooMany / *shell.BulkIDsError，值域只有 Count/Max 两个整数），页面统一走
// shell.BulkIDsFacingText —— 本文件是那条收口的回归门禁，同时钉住「收口不能过头」：
// 受控提示必须仍然可见，且必须带着两个数字（上限与本次条数）。
//
// 与 admin_page_err_param_test.go 的分工：那一份是**静态扫描**（谁都不许再直传原文），
// 这一份是**接口级**（超限时页面上到底显示了什么）。

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	adminhttp "go_wp/internal/module/admin/inbound/http"
	"go_wp/internal/web/shell"
)

// adminBulkOverLimitForm 造一份超过 shell.MaxBulkIDs 的批量表单（多给一条即触发上限）。
//
// 全部由 nil service 的 handle 处理：超限在**读到 service 之前**就被 shell.BulkIDs 拒绝，
// 这条路径本来就不该碰数据库 —— 用 nil 是刻意的（碰了就 panic，而不是悄悄跑通）。
func adminBulkOverLimitForm() url.Values {
	form := url.Values{}
	for i := 0; i < shell.MaxBulkIDs+1; i++ {
		form.Add("ids", strconv.Itoa(i+1))
	}
	return form
}

// adminBulkProbeRawError 反证用：同一份表单交给 shell.BulkIDs，取回原始错误。
func adminBulkProbeRawError(t *testing.T) error {
	t.Helper()
	probe, _ := gin.CreateTestContext(httptest.NewRecorder())
	probe.Request = httptest.NewRequest(http.MethodPost, "/admin/x/bulk-delete",
		strings.NewReader(adminBulkOverLimitForm().Encode()))
	probe.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, rawErr := shell.BulkIDs(probe)
	if rawErr == nil {
		t.Fatalf("反证失败：ids 超过 %d 条时 shell.BulkIDs 应报错", shell.MaxBulkIDs)
	}
	return rawErr
}

// TestAdminBulkDeleteOverLimitUsesControlledText 七个批量入口在 id 超限时都回带受控文案：
// 「一次最多操作 N 项，当前 M 项，请分批进行」—— 两个数字都在，且不含任何内部细节指纹。
//
// 两个数字都要断言：只断言「一次最多操作」会让「当前 M 项 丢了」这种退化悄悄通过，
// 而 M（去重后的条数）正是本批从「模块自己重算」收回到 shell 的东西。
func TestAdminBulkDeleteOverLimitUsesControlledText(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	h := adminhttp.NewAdminPagesHandle(nil, nil, nil, nil, nil, nil)
	engine.POST("/admin/administrators/bulk-delete", h.AdministratorsBulkDelete)
	engine.POST("/admin/roles/bulk-delete", h.RolesBulkDelete)
	engine.POST("/admin/permissions/bulk-delete", h.PermissionsBulkDelete)
	engine.POST("/admin/menus/bulk-delete", h.MenusBulkDelete)
	engine.POST("/admin/departments/bulk-delete", h.DepartmentsBulkDelete)
	engine.POST("/admin/datarules/bulk-delete", h.DatarulesBulkDelete)
	i18nHandle := adminhttp.NewAdminI18nEntryHandle()
	engine.POST("/admin/i18n/bulk-delete", i18nHandle.I18nEntriesBulkDelete)

	// 反证：原始错误是带 sentinel 的类型，且带「本次条数」（它确实知道这个数）。
	rawErr := adminBulkProbeRawError(t)
	if !errors.Is(rawErr, shell.ErrBulkIDsTooMany) {
		t.Fatalf("原始错误应命中 shell.ErrBulkIDsTooMany，实际 %v", rawErr)
	}
	over := strconv.Itoa(shell.MaxBulkIDs + 1)
	if !strings.Contains(rawErr.Error(), over) {
		t.Fatalf("反证失败：原文应带本次条数 %s，实际 %v", over, rawErr)
	}

	paths := []string{
		"/admin/administrators/bulk-delete",
		"/admin/roles/bulk-delete",
		"/admin/permissions/bulk-delete",
		"/admin/menus/bulk-delete",
		"/admin/departments/bulk-delete",
		"/admin/datarules/bulk-delete",
		"/admin/i18n/bulk-delete",
	}
	form := adminBulkOverLimitForm()
	for _, path := range paths {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther && rec.Code != http.StatusFound {
			t.Fatalf("%s 超限应重定向回列表页，实际 %d，body=%s", path, rec.Code, rec.Body.String())
		}
		rawLocation := rec.Header().Get("Location")
		loc, err := url.Parse(rawLocation)
		if err != nil {
			t.Fatalf("%s 的 Location 无法解析：%v（%s）", path, err, rawLocation)
		}
		got := loc.Query().Get("err")
		if !strings.Contains(got, "一次最多操作") {
			t.Errorf("%s 应回带受控提示，实际 ?err=%q", path, got)
		}
		if !strings.Contains(got, strconv.Itoa(shell.MaxBulkIDs)) || !strings.Contains(got, over) {
			t.Errorf("%s 的受控提示应带上限 %d 与本次条数 %s（「当前 M 项」由类型给回），实际 ?err=%q",
				path, shell.MaxBulkIDs, over, got)
		}
		assertPageNoLeak(t, path, rawLocation)
	}
}

// TestAdminBulkLimitTextDoesNotDeriveFromErrorText 文案**不是**从 err.Error() 派生的：
// 把同一个类型包进带内部上下文的错误链后，出口仍只给出受控文案。
//
// 这是「响应里不含原始错误串」这条判据的可验证形态 —— 原始错误串在包装之后
// 与本模块页面显示的文本再无任何关系（去重后的条数由类型字段给出）。
func TestAdminBulkLimitTextDoesNotDeriveFromErrorText(t *testing.T) {
	// 约束名用库里真实存在的那个（`uk_sys_admin_username`，见 init_schema.sql）：
	// 以前写的 `uq_sys_admin_username` 在库里不存在，样本形状与真实报错不符 ——
	// 「真名不许出网」这条判据其实是空的。
	wrapped := fmt.Errorf(`批量删除管理员失败: SQLSTATE 23505 uk_sys_admin_username: %w`,
		&shell.BulkIDsError{Count: shell.MaxBulkIDs + 1, Max: shell.MaxBulkIDs})
	got := shell.BulkIDsFacingText(bulkProbeContext(t), wrapped)
	if !strings.Contains(got, "一次最多操作") {
		t.Fatalf("命中 sentinel 的错误应给出受控提示，实际 %q", got)
	}
	for _, tok := range []string{"SQLSTATE", "23505", "uk_sys_admin_username", "sys_admin"} {
		if strings.Contains(got, tok) {
			t.Fatalf("包装进来的内部上下文 %q 不许出现在文案里：%q", tok, got)
		}
	}
}

// bulkProbeContext 造一个只用于取文案的 gin 上下文（语言协商走请求头，无请求即默认语言）。
func bulkProbeContext(t *testing.T) *gin.Context {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/x", nil)
	return c
}

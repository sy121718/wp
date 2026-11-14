package feature

import (
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	"go_wp/pkg/response"
	"go_wp/internal/templates"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
)

func adminBulkOverLimitForm() url.Values {
	form := url.Values{}
	for i := 0; i < bulkids.MaxIDs+1; i++ {
		form.Add("ids", strconv.Itoa(i+1))
	}
	return form
}

func adminBulkProbeRawError(t *testing.T) error {
	t.Helper()
	probe, _ := gin.CreateTestContext(httptest.NewRecorder())
	probe.Request = httptest.NewRequest(http.MethodPost, "/admin/x/bulk-delete",
		strings.NewReader(adminBulkOverLimitForm().Encode()))
	probe.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, rawErr := bulkids.Normalize(probe.PostFormArray("ids"))
	if rawErr == nil {
		t.Fatalf("反证失败：ids 超过 %d 条时 shell.BulkIDs 应报错", bulkids.MaxIDs)
	}
	return rawErr
}

func TestAdminBulkDeleteOverLimitUsesControlledText(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{bulkids.MsgTooMany: {"zh-CN": bulkids.TooManyTemplate}}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	h := adminhttp.NewAdminPagesHandle(nil, nil, nil, nil, nil, nil)
	engine.POST("/admin/administrators/bulk-delete", h.AdministratorsBulkDelete)
	engine.POST("/admin/roles/bulk-delete", h.RolesBulkDelete)
	engine.POST("/admin/permissions/bulk-delete", h.PermissionsBulkDelete)
	engine.POST("/admin/menus/bulk-delete", h.MenusBulkDelete)
	engine.POST("/admin/departments/bulk-delete", h.DepartmentsBulkDelete)
	engine.POST("/admin/datarules/bulk-delete", h.DatarulesBulkDelete)

	rawErr := adminBulkProbeRawError(t)
	if !errors.Is(rawErr, bulkids.ErrTooMany) {
		t.Fatalf("原始错误应命中 bulkids.ErrTooMany，实际 %v", rawErr)
	}
	over := strconv.Itoa(bulkids.MaxIDs + 1)
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
	}
	form := adminBulkOverLimitForm()
	for _, path := range paths {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		// 非页面请求只回 JSON：超限不是页面失败，不许渲染整页提示。
		got := jsonErrMessage(t, rec.Body.String())
		if rec.Code == http.StatusOK {
			t.Fatalf("%s 超限不应以 200 交回，body=%s", path, rec.Body.String())
		}
		if !strings.Contains(got, "一次最多操作") {
			t.Errorf("%s 应显示受控提示，实际 message=%q", path, got)
		}
		if !strings.Contains(got, strconv.Itoa(bulkids.MaxIDs)) || !strings.Contains(got, over) {
			t.Errorf("%s 的受控提示应带上限 %d 与本次条数 %s（「当前 M 项」由类型给回），实际 message=%q",
				path, bulkids.MaxIDs, over, got)
		}
		assertPageNoLeak(t, path, got)
	}
	body := adminBulkOverLimitForm()
	body.Set("action", "bulkDelete")
	code, message := postAdminI18nWrite(t, newAdminI18nWriteEngine(), body.Encode())
	if code != http.StatusBadRequest || !strings.Contains(message, "一次最多操作") || !strings.Contains(message, strconv.Itoa(bulkids.MaxIDs)) || !strings.Contains(message, over) || strings.Contains(message, bulkids.MsgTooMany) {
		t.Fatalf("词条表单超限应整批拒绝并显示数量：%d %q", code, message)
	}
	assertPageNoLeak(t, "POST /api/i18n/save", message)
}

func jsonErrMessage(t *testing.T, body string) string {
	t.Helper()
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("响应不是 JSON：%v（%s）", err, body)
	}
	return resp.Message
}

func TestAdminBulkLimitTextDoesNotDeriveFromErrorText(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{bulkids.MsgTooMany: {"zh-CN": bulkids.TooManyTemplate}}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	wrapped := fmt.Errorf(`批量删除管理员失败: SQLSTATE 23505 uk_sys_admin_username: %w`,
		&bulkids.IDsError{Count: bulkids.MaxIDs + 1, Max: bulkids.MaxIDs})
	got := response.MessageOf(bulkProbeContext(t), "admin", wrapped)
	if !strings.Contains(got, "一次最多操作") {
		t.Fatalf("命中 sentinel 的错误应给出受控提示，实际 %q", got)
	}
	for _, tok := range []string{"SQLSTATE", "23505", "uk_sys_admin_username", "sys_admin"} {
		if strings.Contains(got, tok) {
			t.Fatalf("包装进来的内部上下文 %q 不许出现在文案里：%q", tok, got)
		}
	}
}

func bulkProbeContext(t *testing.T) *gin.Context {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/x", nil)
	return c
}

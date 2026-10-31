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

	"go_wp/internal/adminui"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	"go_wp/internal/shell"
	"go_wp/internal/templates"
	bulkids "go_wp/pkg/bulkids"
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
	_, rawErr := shell.BulkIDs(probe)
	if rawErr == nil {
		t.Fatalf("反证失败：ids 超过 %d 条时 shell.BulkIDs 应报错", bulkids.MaxIDs)
	}
	return rawErr
}

func TestAdminBulkDeleteOverLimitUsesControlledText(t *testing.T) {
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

	i18nHandle := adminhttp.NewAdminI18nEntryHandle()
	engine.POST("/api/i18n/save", i18nHandle.I18nEntryWrite)

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
		if rec.Code != http.StatusOK {
			t.Fatalf("%s 超限应渲染整页提示（200），实际 %d，body=%s", path, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, `data-jump-state="err"`) {
			t.Fatalf("%s 超限提示页应带 data-jump-state=\"err\"：%s", path, body)
		}
		got := jumpMsgFromBody(body)
		if !strings.Contains(got, "一次最多操作") {
			t.Errorf("%s 应显示受控提示，实际 msg=%q", path, got)
		}
		if !strings.Contains(got, strconv.Itoa(bulkids.MaxIDs)) || !strings.Contains(got, over) {
			t.Errorf("%s 的受控提示应带上限 %d 与本次条数 %s（「当前 M 项」由类型给回），实际 msg=%q",
				path, bulkids.MaxIDs, over, got)
		}
		assertPageNoLeak(t, path, body)
	}

	jsonOver := strconv.Itoa(bulkids.MaxIDs + 1)
	jsonReq := httptest.NewRequest(http.MethodPost, "/api/i18n/save", strings.NewReader(adminBulkOverLimitJSONBody()))
	jsonReq.Header.Set("Content-Type", "application/json")
	jsonRec := httptest.NewRecorder()
	engine.ServeHTTP(jsonRec, jsonReq)
	if jsonRec.Code != http.StatusBadRequest {
		t.Fatalf("JSON 通道超限应整批拒绝（400，不部分执行），实际 %d，body=%s", jsonRec.Code, jsonRec.Body.String())
	}
	jsonMsg := jsonErrMessage(t, jsonRec.Body.String())
	if !strings.HasPrefix(jsonMsg, bulkids.MsgTooMany+"|") {
		t.Fatalf("JSON 通道应回受控 key（%s）而不是原文，实际 %q", bulkids.MsgTooMany, jsonMsg)
	}
	if !strings.Contains(jsonMsg, strconv.Itoa(bulkids.MaxIDs)) || !strings.Contains(jsonMsg, jsonOver) {
		t.Fatalf("JSON 通道的受控提示应带上限 %d 与本次条数 %s，实际 %q", bulkids.MaxIDs, jsonOver, jsonMsg)
	}
	assertPageNoLeak(t, "POST /api/i18n/save", jsonMsg)
}

func adminBulkOverLimitJSONBody() string {
	ids := make([]string, 0, bulkids.MaxIDs+1)
	for i := 0; i < bulkids.MaxIDs+1; i++ {
		ids = append(ids, `"k`+strconv.Itoa(i)+`|zh-CN"`)
	}
	return `{"ids":[` + strings.Join(ids, ",") + `]}`
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

	wrapped := fmt.Errorf(`批量删除管理员失败: SQLSTATE 23505 uk_sys_admin_username: %w`,
		&bulkids.IDsError{Count: bulkids.MaxIDs + 1, Max: bulkids.MaxIDs})
	got := adminui.BulkIDsFacingText(bulkProbeContext(t), wrapped)
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

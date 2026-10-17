package feature

// admin_bulk_limit_text_test.go — admin 六领域批量删除的 id 超限路径：文案走 adminui 的受控出口。
//
// 背景：shell.BulkIDs 的超限错误是**带 sentinel 的类型**（bulkids.ErrTooMany /
// *bulkids.IDsError，值域只有 Count/Max 两个整数），页面统一走 adminui.BulkIDsFacingText ——
// 本文件是那条收口的回归门禁，同时钉住「收口不能过头」：受控提示必须仍然可见，
// 且必须带着两个数字（上限与本次条数）。
//
// 出口形态：本批改成**整页提示**（shell.RenderJump）—— 超限提示渲染在提示页的响应体里，
// 不再经 `?err=` 回带（见 internal/shell/jump.go 的文件头）。
//
// 与 admin_page_err_param_test.go 的分工：那一份是**静态扫描**（谁都不许再直传原文），
// 这一份是**接口级**（超限时页面上到底显示了什么）。

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

// adminBulkOverLimitForm 造一份超过 bulkids.MaxIDs 的批量表单（多给一条即触发上限）。
//
// 全部由 nil service 的 handle 处理：超限在**读到 service 之前**就被 shell.BulkIDs 拒绝，
// 这条路径本来就不该碰数据库 —— 用 nil 是刻意的（碰了就 panic，而不是悄悄跑通）。
func adminBulkOverLimitForm() url.Values {
	form := url.Values{}
	for i := 0; i < bulkids.MaxIDs+1; i++ {
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
		t.Fatalf("反证失败：ids 超过 %d 条时 shell.BulkIDs 应报错", bulkids.MaxIDs)
	}
	return rawErr
}

// TestAdminBulkDeleteOverLimitUsesControlledText 六个页面批量入口 + 词条页的 JSON 端点
// 在 id 超限时都给出受控提示：「一次最多操作 N 项，当前 M 项，请分批进行」—— 两个数字都在，
// 且不含任何内部细节指纹。
//
// 词条页的批量入口在 task-17 迁到 JSON 端点（POST /api/i18n/save，批量档），出口形态随之改变
// （响应体 message 而非整页提示），所以它单独在下面的 JSON 段里断言；其余六个领域仍是页面 POST。
//
// 两个数字都要断言：只断言「一次最多操作」会让「当前 M 项 丢了」这种退化悄悄通过，
// 而 M（去重后的条数）正是本批从「模块自己重算」收回到 shell 的东西。
func TestAdminBulkDeleteOverLimitUsesControlledText(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// 超限提示现在渲染整页（shell.RenderJump → c.HTML），必须有渲染器。
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	h := adminhttp.NewAdminPagesHandle(nil, nil, nil, nil, nil, nil)
	engine.POST("/admin/administrators/bulk-delete", h.AdministratorsBulkDelete)
	engine.POST("/admin/roles/bulk-delete", h.RolesBulkDelete)
	engine.POST("/admin/permissions/bulk-delete", h.PermissionsBulkDelete)
	engine.POST("/admin/menus/bulk-delete", h.MenusBulkDelete)
	engine.POST("/admin/departments/bulk-delete", h.DepartmentsBulkDelete)
	engine.POST("/admin/datarules/bulk-delete", h.DatarulesBulkDelete)
	// 词条页的批量入口已迁到 JSON 端点（页面 POST 在 task-17 退役）：路径与出口形态都不同，
	// 不并进下面的页面路径循环，单独在 JSON 通道上断言同一份受控文案与两个数字。
	i18nHandle := adminhttp.NewAdminI18nEntryHandle()
	engine.POST("/api/i18n/save", i18nHandle.I18nEntryWrite)

	// 反证：原始错误是带 sentinel 的类型，且带「本次条数」（它确实知道这个数）。
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

	// JSON 通道（词条页批量删除）：同一份受控提示的两个数字 —— 上限取服务端的 pkg/bulkids.MaxIDs，
	// 「本次条数」取**去重后**的条数（客户端那次 200 上限拦截只是快速反馈，服务端才是边界）。
	// 两条通道的上限同值由 pkg/bulkids 的单一真源保证（只有一份 MaxIDs），这里不重复一份 ——
	// tests 侧多引一次 shell 会抬高 scripts/check-shell-migration.sh 的 use:test:admin:test 计数。
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

// adminBulkOverLimitJSONBody 造一份超过上限的 JSON 批量请求体（词条页的 id 是 "key|lang" 复合键）。
func adminBulkOverLimitJSONBody() string {
	ids := make([]string, 0, bulkids.MaxIDs+1)
	for i := 0; i < bulkids.MaxIDs+1; i++ {
		ids = append(ids, `"k`+strconv.Itoa(i)+`|zh-CN"`)
	}
	return `{"ids":[` + strings.Join(ids, ",") + `]}`
}

// jsonErrMessage 取 JSON 错误响应里的 message（pkg/response.Response）。
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

// bulkProbeContext 造一个只用于取文案的 gin 上下文（语言协商走请求头，无请求即默认语言）。
func bulkProbeContext(t *testing.T) *gin.Context {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/x", nil)
	return c
}

package feature

// admin_page_write_failed_test.go — 页面写失败出口的「业务文案可见」回归。
//
// 背景：shell.AdminWriteFailed 恒给 400 + MsgInternalError（通用提示）。admin 的页面写端点
// 都在用它，于是「角色编码已存在 / 系统内置角色不可删除 / 用户名已存在」这类**业务**错误在运营侧
// 表现成「系统出错了」—— 该改的是表单里的某一个字段，运营却只能反复重试。
//
// 【出口形态三次收口】页面路径的失败响应演进：
//  1. 最初：400 + JSON（裸 JSON 页面，DOM 仅 2 节点、无页壳、无导航）；
//  2. 2026-09：303 回列表页 + `?err=` 文案，由读侧白名单放行后渲染提示条；
//  3. 本批：**整页提示**（shell.RenderJump，对应 ThinkPHP 的 error()）—— 结论走响应体，
//     不再经查询参数回带。理由见 internal/shell/jump.go 的文件头。
//
// **本文件钉死的两条核心意图没有变**：
//  1. 业务错误文案必须透出（不能被吞成通用提示）—— 现在从提示页的 `.jump-msg` 里读；
//  2. 内部错误原文绝不能进响应 —— 现在查整个响应体（文案改走响应体后，泄漏面就是它）。
//
// 为什么用假 service 再测一遍 handler 层：真实库很难**稳定**造出「重名」与「PostgreSQL 唯一约束
// + SQLSTATE」这两种错误（要并发撞唯一键），而这两条正是本批要钉死的分支。
//
// 反向验证（手工执行、不留在代码里）：把 RolesCreate 临时改回
// `response.ErrorWithMessage(c, 400, ...)`，本文件的 TestAdminPageWriteFailedKeepsBusinessText
// 必须变红（不再是提示页，或文案落进 internalCopyForms）；改回提示页出口后重新变绿。
//
// 复用同包既有资产：fakeRoleService / internalCopyForms / dbErrText / assertNoLeak /
// withTempLogDir（都在 admin_err_response_test.go 与 admin_err_reverse_defect_test.go）。

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	adminenums "go_wp/internal/module/admin/enums"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	"go_wp/internal/templates"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
)

// roleIsSystemCopyForms ErrRoleIsSystem 的全部可能形态（key / zh-CN / en-US）。
var roleIsSystemCopyForms = map[string]bool{
	adminenums.ErrRoleIsSystem:                true,
	"系统内置角色不可删除":                              true,
	"System built-in roles cannot be deleted": true,
}

// newAdminPageWriteEngine 组一个只挂两个页面写端点的最小引擎（其余域注入 nil：本用例不会走到）。
//
// 必须挂真 Jet 渲染器：写失败现在渲染整页提示（shell.RenderJump → c.HTML），
// 没有 HTMLRender 时 gin 会 panic。
func newAdminPageWriteEngine(t *testing.T, svcErr error) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handle := adminhttp.NewAdminPagesHandle(nil, &fakeRoleService{err: svcErr}, nil, nil, nil, nil)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	engine.POST("/admin/roles/create", handle.RolesCreate)
	engine.POST("/admin/roles/delete", handle.RolesDelete)
	return engine
}

// postAdminPageWrite 提交一个页面表单，返回原始响应体与状态码。
func postAdminPageWrite(t *testing.T, engine *gin.Engine, path string, form url.Values) (string, int) {
	t.Helper()
	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method:  http.MethodPost,
		Path:    path,
		RawBody: []byte(form.Encode()),
		Headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
	})
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	return recorder.Body.String(), recorder.Code
}

// jumpMsgFromBody 取提示页 `.jump-msg` 里的文案（缺元素时返回空串）。
//
// 不对整页做 Contains：页壳（侧栏 / 面包屑 / 语言切换隐藏域）里也会出现请求 URL 等文本，
// 本用例要断言的正是**显示出来的那一条提示**。
func jumpMsgFromBody(body string) string {
	const marker = `<p class="jump-msg">`
	i := strings.Index(body, marker)
	if i < 0 {
		return ""
	}
	rest := body[i+len(marker):]
	if j := strings.Index(rest, "</p>"); j >= 0 {
		return rest[:j]
	}
	return rest
}

// TestAdminPageWriteFailedKeepsBusinessText 业务错误在页面写端点上必须渲染提示页 + 原样文案。
//
// 修复前这两条都是 400 + MsgInternalError（运营看不到「角色编码已存在」/「系统内置角色不可删除」）。
func TestAdminPageWriteFailedKeepsBusinessText(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		form    url.Values
		svcErr  error
		allowed map[string]bool
	}{
		{
			name: "页面新建角色重名", path: "/admin/roles/create",
			form:   url.Values{"role_code": {"editor"}, "role_name": {"编辑"}},
			svcErr: errors.New(adminenums.ErrRoleCodeExists), allowed: businessCopyForms,
		},
		{
			name: "页面删除系统内置角色", path: "/admin/roles/delete",
			form:   url.Values{"id": {"7"}},
			svcErr: errors.New(adminenums.ErrRoleIsSystem), allowed: roleIsSystemCopyForms,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine := newAdminPageWriteEngine(t, tc.svcErr)
			body, code := postAdminPageWrite(t, engine, tc.path, tc.form)

			if code != http.StatusOK {
				t.Fatalf("页面写失败应渲染整页提示（200），got %d body=%s", code, body)
			}
			if !strings.Contains(body, `data-jump-state="err"`) {
				t.Fatalf("失败提示页应带 data-jump-state=\"err\"：%s", body)
			}
			msg := jumpMsgFromBody(body)
			if msg == "" {
				t.Fatalf("提示页缺少 .jump-msg 文案：%s", body)
			}
			if internalCopyForms[msg] {
				t.Fatalf("业务错误被页面出口吞成了通用提示（修复前正是这个表现）：msg=%q", msg)
			}
			if !tc.allowed[msg] {
				t.Fatalf("业务错误应原样透出 enums 文案，got msg=%q", msg)
			}
			assertNoLeak(t, tc.name+"（body）", body)
		})
	}
}

// TestAdminPageWriteFailedCollectsInfraError 基础设施错误走归口文案，原文只进日志。
//
// 这一条与上一条是一对：只测「业务文案可见」会让人以为「所有错误都该原样透出」，
// 而 PostgreSQL 原文（uq_ / SQLSTATE / 表名）必须被白名单挡在响应之外。
func TestAdminPageWriteFailedCollectsInfraError(t *testing.T) {
	readLog := withTempLogDir(t)
	engine := newAdminPageWriteEngine(t, errors.New(dbErrText))
	body, code := postAdminPageWrite(t, engine, "/admin/roles/create",
		url.Values{"role_code": {"editor"}, "role_name": {"编辑"}})

	if code != http.StatusOK {
		t.Fatalf("基础设施错误在页面路径应渲染提示页（200），got %d body=%s", code, body)
	}
	msg := jumpMsgFromBody(body)
	if msg == "" {
		t.Fatalf("提示页缺少 .jump-msg 文案：%s", body)
	}
	if !internalCopyForms[msg] {
		t.Fatalf("未命中白名单的错误必须归口，got msg=%q", msg)
	}
	assertNoLeak(t, "基础设施错误分支（body）", body)

	logText := readLog()
	if !strings.Contains(logText, "uk_sys_role_code") || !strings.Contains(logText, "SQLSTATE") {
		t.Fatalf("内部错误原文必须进日志（收口 ≠ 吞掉），日志内容=%s", logText)
	}
}

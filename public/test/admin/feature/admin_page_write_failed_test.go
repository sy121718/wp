package feature

// admin_page_write_failed_test.go — 页面写失败出口的「业务文案可见」回归（本轮任务 1）。
//
// 背景：shell.AdminWriteFailed 恒给 400 + MsgInternalError（通用提示）。admin 的 19 处页面写端点
// 都在用它，于是「角色编码已存在 / 系统内置角色不可删除 / 用户名已存在」这类**业务**错误在运营侧
// 表现成「系统出错了」—— 该改的是表单里的某一个字段，运营却只能反复重试。
// 这与上一批修的 22 处 r.ErrorInternal 是同一类反向缺陷，只是发生在页面路径
//（上一批的回归见 admin_err_reverse_defect_test.go，JSON 出口）。
//
// 【出口形态二次收口（2026-09）】页面路径的失败响应从「400 + JSON」改成 **303 回列表页 + ?err=文案**。
// 原因：页面路径的用户是运营，`c.String`/`response.ErrorWithMessage` 给的是一个**裸 JSON 页面**
//（DOM 仅 2 节点、无页壳、无导航），他只能手改地址栏才能回到能继续操作的地方。
// 改成 303 后错误文案经 `?err=` 回带，由读侧白名单（adminPageErrText / adminErrTexts）放行并在
// 页面顶部渲染提示条 —— 用户看得见错误，且留在可继续操作的页面。
//
// **本文件钉死的两条核心意图没有变，反而更严**：
//  1. 业务错误文案必须透出（不能被吞成通用提示）—— 现在从 Location 的 `?err=` 里读；
//  2. 内部错误原文绝不能进响应 —— 现在**响应体和 Location 都要查**（文案改走 URL 后，
//     泄漏面从「响应体」扩大到「Location」，assertNoLeak 因此被调用两次）。
//
// 为什么用假 service 再测一遍 handler 层：真实库很难**稳定**造出「重名」与「PostgreSQL 唯一约束
// + SQLSTATE」这两种错误（要并发撞唯一键），而这两条正是本批要钉死的分支。
//
// 反向验证（手工执行、不留在代码里）：把 admin_pages_handle.go 的 RolesCreate 临时改回
// `response.ErrorWithMessage(c, 400, ...)`，本文件的 TestAdminPageWriteFailedKeepsBusinessText
// 必须变红（不再 303，或 err 落进 internalCopyForms）；改回 303 出口后重新变绿。
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
func newAdminPageWriteEngine(t *testing.T, svcErr error) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handle := adminhttp.NewAdminPagesHandle(nil, &fakeRoleService{err: svcErr}, nil, nil, nil, nil)
	engine := gin.New()
	engine.POST("/admin/roles/create", handle.RolesCreate)
	engine.POST("/admin/roles/delete", handle.RolesDelete)
	return engine
}

// postAdminPageWrite 提交一个页面表单，返回标准响应、原始响应体与状态码。
func postAdminPageWrite(t *testing.T, engine *gin.Engine, path string, form url.Values) (*support.StandardResponse, string, int) {
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
	std, perr := support.ParseStandardResponse(recorder)
	if perr != nil {
		t.Fatalf("解析响应失败: %v", perr)
	}
	return std, recorder.Body.String(), recorder.Code
}

// postAdminPageWriteRedirect 提交一个页面表单，按**新契约**取结果：303 + Location + 原始响应体。
//
// 不再调 ParseStandardResponse —— 新契约下 303 响应体为空，JSON 解析会直接失败。
// 错误文案从 Location 的 `?err=` 取出（读侧白名单核对后渲染成页面提示条）。
func postAdminPageWriteRedirect(t *testing.T, engine *gin.Engine, path string, form url.Values) (location, body string, code int) {
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
	return recorder.Header().Get("Location"), recorder.Body.String(), recorder.Code
}

// errTextFromLocation 取回跳地址里 `?err=` 的文案（缺参时返回空串，由调用方断言非空）。
func errTextFromLocation(t *testing.T, location string) string {
	t.Helper()
	u, err := url.Parse(location)
	if err != nil {
		t.Fatalf("回跳地址无法解析: %q", location)
	}
	return u.Query().Get("err")
}

// TestAdminPageWriteFailedKeepsBusinessText 业务错误在页面写端点上必须 303 回列表 + ?err=原样文案。
//
// 修复前这两条都是 400 + MsgInternalError（运营看不到「角色编码已存在」/「系统内置角色不可删除」）。
// 出口形态收口后（见文件头）判据从「响应体 message」改为「Location 的 ?err=」。
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
			loc, body, code := postAdminPageWriteRedirect(t, engine, tc.path, tc.form)

			if code != http.StatusSeeOther {
				t.Fatalf("页面写失败应 303 回列表页，got %d（Location=%q body=%s）", code, loc, body)
			}
			errText := errTextFromLocation(t, loc)
			if errText == "" {
				t.Fatalf("回跳地址缺少 ?err= 文案：Location=%q", loc)
			}
			if internalCopyForms[errText] {
				t.Fatalf("业务错误被页面出口吞成了通用提示（修复前正是这个表现）：err=%q Location=%s", errText, loc)
			}
			if !tc.allowed[errText] {
				t.Fatalf("业务错误应原样透出 enums 文案，got err=%q Location=%s", errText, loc)
			}
			// 文案改走 URL 后泄漏面从「响应体」扩大到「Location」，两处都要查。
			assertNoLeak(t, tc.name+"（body）", body)
			assertNoLeak(t, tc.name+"（Location）", loc)
		})
	}
}

// TestAdminPageWriteFailedCollectsInfraError 基础设施错误仍是 303 + 归口文案，原文只进日志。
//
// 这一条与上一条是一对：只测「业务文案可见」会让人以为「所有错误都该原样透出」，
// 而 PostgreSQL 原文（uq_ / SQLSTATE / 表名）必须被白名单挡在响应之外。
func TestAdminPageWriteFailedCollectsInfraError(t *testing.T) {
	readLog := withTempLogDir(t)
	engine := newAdminPageWriteEngine(t, errors.New(dbErrText))
	loc, body, code := postAdminPageWriteRedirect(t, engine, "/admin/roles/create",
		url.Values{"role_code": {"editor"}, "role_name": {"编辑"}})

	if code != http.StatusSeeOther {
		t.Fatalf("基础设施错误在页面路径沿用 303（页面只有「提交失败」一层语义），got %d（Location=%q body=%s）", code, loc, body)
	}
	errText := errTextFromLocation(t, loc)
	if errText == "" {
		t.Fatalf("回跳地址缺少 ?err= 文案：Location=%q", loc)
	}
	if !internalCopyForms[errText] {
		t.Fatalf("未命中白名单的错误必须归口，got err=%q Location=%s", errText, loc)
	}
	assertNoLeak(t, "基础设施错误分支（body）", body)
	assertNoLeak(t, "基础设施错误分支（Location）", loc)

	logText := readLog()
	if !strings.Contains(logText, "uk_sys_role_code") || !strings.Contains(logText, "SQLSTATE") {
		t.Fatalf("内部错误原文必须进日志（收口 ≠ 吞掉），日志内容=%s", logText)
	}
}

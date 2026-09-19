package feature

// admin_page_write_failed_test.go — 页面写失败出口的「业务文案可见」回归（本轮任务 1）。
//
// 背景：shell.AdminWriteFailed 恒给 400 + MsgInternalError（通用提示）。admin 的 19 处页面写端点
// 都在用它，于是「角色编码已存在 / 系统内置角色不可删除 / 用户名已存在」这类**业务**错误在运营侧
// 表现成「系统出错了」—— 该改的是表单里的某一个字段，运营却只能反复重试。
// 这与上一批修的 22 处 r.ErrorInternal 是同一类反向缺陷，只是发生在页面路径
//（上一批的回归见 admin_err_reverse_defect_test.go，JSON 出口）。
//
// 为什么用假 service 再测一遍 handler 层：真实库很难**稳定**造出「重名」与「PostgreSQL 唯一约束
// + SQLSTATE」这两种错误（要并发撞唯一键），而这两条正是本批要钉死的分支。
//
// 反向验证（手工执行、不留在代码里）：把 admin_pages_handle.go 的 RolesCreate 临时改回
// shell.AdminWriteFailed(c, err)，本文件的 TestAdminPageWriteFailedKeepsBusinessText 必须变红
//（message 会落进 internalCopyForms）；改回 adminWriteFailed 后重新变绿。
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

// TestAdminPageWriteFailedKeepsBusinessText 业务错误在页面写端点上必须 400 + 原样文案。
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
			std, body, code := postAdminPageWrite(t, engine, tc.path, tc.form)

			if code != http.StatusBadRequest {
				t.Fatalf("页面写失败应是 400，got %d（body=%s）", code, body)
			}
			if internalCopyForms[std.Message] {
				t.Fatalf("业务错误被页面出口吞成了通用提示（修复前正是这个表现）：message=%q body=%s", std.Message, body)
			}
			if !tc.allowed[std.Message] {
				t.Fatalf("业务错误应原样透出 enums 文案，got message=%q body=%s", std.Message, body)
			}
			assertNoLeak(t, tc.name, body)
		})
	}
}

// TestAdminPageWriteFailedCollectsInfraError 基础设施错误仍是 400 + 归口文案，原文只进日志。
//
// 这一条与上一条是一对：只测「业务文案可见」会让人以为「所有错误都该原样透出」，
// 而 PostgreSQL 原文（uq_ / SQLSTATE / 表名）必须被白名单挡在响应之外。
func TestAdminPageWriteFailedCollectsInfraError(t *testing.T) {
	readLog := withTempLogDir(t)
	engine := newAdminPageWriteEngine(t, errors.New(dbErrText))
	std, body, code := postAdminPageWrite(t, engine, "/admin/roles/create",
		url.Values{"role_code": {"editor"}, "role_name": {"编辑"}})

	if code != http.StatusBadRequest {
		t.Fatalf("基础设施错误在页面路径沿用 400（页面只有「提交失败」一层语义），got %d（body=%s）", code, body)
	}
	if !internalCopyForms[std.Message] {
		t.Fatalf("未命中白名单的错误必须归口，got message=%q body=%s", std.Message, body)
	}
	assertNoLeak(t, "基础设施错误分支", body)

	logText := readLog()
	if !strings.Contains(logText, "uq_sys_role_role_code") || !strings.Contains(logText, "SQLSTATE") {
		t.Fatalf("内部错误原文必须进日志（收口 ≠ 吞掉），日志内容=%s", logText)
	}
}

package feature

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

var roleIsSystemCopyForms = map[string]bool{
	adminenums.ErrRoleIsSystem:                true,
	"系统内置角色不可删除":                              true,
	"System built-in roles cannot be deleted": true,
}

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

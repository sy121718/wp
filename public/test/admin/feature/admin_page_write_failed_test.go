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

func TestAdminPageWriteFailureIsJSONWithDeclaredStatus(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		form   url.Values
		svcErr error
	}{
		{
			name: "页面新建角色重名", path: "/admin/roles/create",
			form:   url.Values{"role_code": {"editor"}, "role_name": {"编辑"}},
			svcErr: errors.New(adminenums.ErrRoleCodeExists),
		},
		{
			name: "页面删除系统内置角色", path: "/admin/roles/delete",
			form:   url.Values{"id": {"7"}},
			svcErr: errors.New(adminenums.ErrRoleIsSystem),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine := newAdminPageWriteEngine(t, tc.svcErr)
			body, code := postAdminPageWrite(t, engine, tc.path, tc.form)

			// 非页面请求只回 JSON + 真实状态码：不再渲染失败提示页，也不再一律 200。
			if code < http.StatusBadRequest {
				t.Fatalf("写失败必须给真实状态码（>=400），got %d body=%s", code, body)
			}
			if strings.Contains(body, "<html") || strings.Contains(body, "data-jump-state") {
				t.Fatalf("写失败不得再渲染 HTML 提示页：%s", body)
			}
			if !strings.Contains(body, `"message"`) || !strings.Contains(body, `"code"`) {
				t.Fatalf("写失败必须回 JSON {code,message,data}：%s", body)
			}
			// message 是取过词的界面文案，不能是裸 key。
			if strings.Contains(body, `"message":"`+keyOfErr(tc.svcErr)+`"`) {
				t.Fatalf("message 不得是裸 key：%s", body)
			}
			assertNoLeak(t, tc.name+"（body）", body)
		})
	}
}

func TestAdminPageWriteFailureCollectsInfraError(t *testing.T) {
	readLog := withTempLogDir(t)
	engine := newAdminPageWriteEngine(t, errors.New(dbErrText))
	body, code := postAdminPageWrite(t, engine, "/admin/roles/create",
		url.Values{"role_code": {"editor"}, "role_name": {"编辑"}})

	if code != http.StatusInternalServerError {
		t.Fatalf("未声明的错误按 500 打回，got %d body=%s", code, body)
	}
	if strings.Contains(body, "<html") || strings.Contains(body, "data-jump-state") {
		t.Fatalf("写失败不得渲染 HTML 提示页：%s", body)
	}
	assertNoLeak(t, "基础设施错误分支（body）", body)

	logText := readLog()
	if !strings.Contains(logText, "uk_sys_role_code") || !strings.Contains(logText, "SQLSTATE") {
		t.Fatalf("内部错误原文必须进日志（收口 ≠ 吞掉），日志内容=%s", logText)
	}
}

// keyOfErr 取错误声明里的 key（业务错误用 errors.New(key) 构造，没有 Facing 就直接用 Error()）。
func keyOfErr(err error) string {
	if err == nil {
		return ""
	}
	var sourced interface{ Facing() (string, string) }
	if errors.As(err, &sourced) {
		key, _ := sourced.Facing()
		return key
	}
	return err.Error()
}

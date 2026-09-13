package runtimefragment

// user_forms_test.go — 访客账号表单片段的端点级测试。
//
// 这一层守的是三件容易静默失效的事：
//   · **CSRF token 必须进到表单里** —— 少了它，表单提交永远 403，而页面看起来完全正常；
//   · 提交目标必须是 user 模块的 POST 路由（片段只渲染，不自己处理登录）；
//   · 账号面板必须区分登录态（未登录时不该出现「退出登录」这种按钮）。

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	usercontract "go_wp/internal/module/user/contract"
)

// callUserForm 直接调片段端点，可选地注入访客身份与 CSRF token。
func callUserForm(t *testing.T, typeName, query string, visitorID uint64, csrf string) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/_fragments/"+typeName+"?"+query, nil)
	c.Params = gin.Params{{Key: "type", Value: typeName}}
	if visitorID != 0 {
		c.Set(usercontract.VisitorContextKey, visitorID)
	}
	if csrf != "" {
		c.Set(usercontract.VisitorCSRFContextKey, csrf)
	}
	FragmentEndpoint(c)
	return w.Body.String()
}

// TestUserFormFragmentsRender 五个形态都能渲染，且提交目标正确。
func TestUserFormFragmentsRender(t *testing.T) {
	tests := []struct {
		typeName string
		action   string
	}{
		{"loginForm", "/user/login"},
		{"registerForm", "/user/register"},
		{"forgotForm", "/user/forgot"},
		{"resetForm", "/user/reset"},
	}
	for _, tt := range tests {
		t.Run(tt.typeName, func(t *testing.T) {
			body := callUserForm(t, tt.typeName, "projectId=proj-1", 0, "csrf-token-1")
			for _, want := range []string{
				`action="` + tt.action + `"`,
				`name="csrf_token" value="csrf-token-1"`,
				`method="post"`,
			} {
				if !strings.Contains(body, want) {
					t.Errorf("片段缺少 %q；实际：%s", want, body)
				}
			}
		})
	}
}

// TestUserFormWithoutCSRFWarns 没有 CSRF token 时页面要能看出来。
//
// 渲染一个必然 403 的表单是最糟的形态：用户填完点提交，得到一句「CSRF 校验失败」，
// 而他根本不知道发生了什么。这里要求片段主动给出「会话未就绪，请刷新」。
func TestUserFormWithoutCSRFWarns(t *testing.T) {
	body := callUserForm(t, "loginForm", "projectId=proj-1", 0, "")
	if !strings.Contains(body, "会话未就绪") {
		t.Fatalf("缺少 CSRF token 时应给出可见提示；实际：%s", body)
	}
}

// TestUserAccountPanelReflectsLoginState 账号面板区分登录态。
func TestUserAccountPanelReflectsLoginState(t *testing.T) {
	guest := callUserForm(t, "accountPanel", "projectId=proj-1", 0, "csrf-1")
	if strings.Contains(guest, "/user/logout") {
		t.Fatalf("未登录时不该出现退出按钮；实际：%s", guest)
	}
	if !strings.Contains(guest, "你还没有登录") {
		t.Fatalf("未登录时应给引导；实际：%s", guest)
	}

	logged := callUserForm(t, "accountPanel", "projectId=proj-1", 77, "csrf-1")
	if !strings.Contains(logged, "你已登录") {
		t.Fatalf("已登录时应说明状态；实际：%s", logged)
	}
	if !strings.Contains(logged, `action="/user/logout"`) {
		t.Fatalf("已登录时应提供退出表单；实际：%s", logged)
	}
}

// TestUserFormCarriesNextAndSlotLinks next 回跳与槽位互链都要落到产物里。
func TestUserFormCarriesNextAndSlotLinks(t *testing.T) {
	body := callUserForm(t, "loginForm", "projectId=proj-1&next=%2Faccount", 0, "csrf-1")
	if !strings.Contains(body, `name="next" value="/account"`) {
		t.Fatalf("next 回跳未进表单；实际：%s", body)
	}
	// 槽位没配（本用例没有注入槽位解析器）时不应输出死链。
	if strings.Contains(body, "去注册") {
		t.Fatalf("槽位没配时不该输出互链；实际：%s", body)
	}
}

// TestUserResetFormPrefillsKey 重置表单的 key 由片段参数带出（邮件链接里的那段）。
func TestUserResetFormPrefillsKey(t *testing.T) {
	body := callUserForm(t, "resetForm", "projectId=proj-1&key=abc123&email=a%40b.c", 0, "csrf-1")
	for _, want := range []string{`name="key" value="abc123"`, `value="a@b.c"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("重置表单缺少 %q；实际：%s", want, body)
		}
	}
}

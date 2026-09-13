package feature

// user_account_fragment_test.go — 账号中心四个片段走**真实链路**的行为。
//
// 与 runtimefragment 包内的端点单测互补：那边直接调 handler 并手工塞 context，
// 证明的是「拿到身份与令牌时渲染得对」；这里经过真实的中间件链与签名 cookie，
// 证明的是「这些东西真的被挂上了」—— 「中间件忘接」这类装配缺失只有这一层看得见，
// 而它的表现是「登录了却永远显示请先登录」。

import (
	"net/http"
	"strings"
	"testing"
)

// TestAccountFragmentRendersOwnAccount 登录后拉资料片段：渲染**本人**的账号。
func TestAccountFragmentRendersOwnAccount(t *testing.T) {
	env := newUserHTTPEnv(t)
	seedActiveUser(t, env.db, "fraguser", "frag@example.com", "password123")
	cookies := loginAs(t, env, "fraguser", "password123")

	rec := env.get(t, "/_fragments/accountProfileForm?projectId=proj-1", cookies)
	if rec.Code != http.StatusOK {
		t.Fatalf("资料片段期望 200，实际 %d：%s", rec.Code, head(rec.Body.String()))
	}
	body := rec.Body.String()
	for _, want := range []string{
		"fraguser",
		"frag@example.com",
		`action="/user/account/profile"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("资料片段缺少 %q：%s", want, head(body))
		}
	}
	if strings.Contains(body, "请先登录") {
		t.Fatalf("已登录却渲染成未登录（访客身份中间件没挂上？）：%s", head(body))
	}
	if csrfFrom(t, body) == "" {
		t.Fatal("片段里的 csrf_token 为空（表单提交必然 403）")
	}
}

// TestAccountFragmentGuidesGuest 未登录拉片段：给引导，且**不是** 401/403。
//
// HTMX 默认不替换 401 响应的目标节点：回 401 的话访客会看到一个毫无变化的面板，
// 完全不知道自己需要登录。
func TestAccountFragmentGuidesGuest(t *testing.T) {
	env := newUserHTTPEnv(t)

	rec := env.get(t, "/_fragments/accountProfileForm?projectId=proj-1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("未登录的片段请求应为 200 且带引导文案，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "请先登录") {
		t.Fatalf("未登录应给引导：%s", head(body))
	}
	if strings.Contains(body, "<form") {
		t.Fatalf("未登录不该渲染提交表单：%s", head(body))
	}
}

// TestAccountSessionsFragmentMarksCurrentDevice 登录设备片段能认出「当前设备」。
//
// 这一条只有走真实链路才成立：台账里存的是会话令牌的 sha256，
// 判定当前设备需要**令牌原值**（中间件把它挂到了 context，再由端点填进 Request）。
func TestAccountSessionsFragmentMarksCurrentDevice(t *testing.T) {
	env := newUserHTTPEnv(t)
	seedActiveUser(t, env.db, "devuser", "dev@example.com", "password123")
	cookies := loginAs(t, env, "devuser", "password123")

	rec := env.get(t, "/_fragments/accountSessionsPanel?projectId=proj-1", cookies)
	if rec.Code != http.StatusOK {
		t.Fatalf("设备片段期望 200，实际 %d：%s", rec.Code, head(rec.Body.String()))
	}
	body := rec.Body.String()
	if !strings.Contains(body, "当前设备") {
		t.Fatalf("没有认出当前设备（令牌原值没传到端口？）：%s", head(body))
	}
}

// TestAccountPreferenceFragmentShowsStoredPreference 偏好片段渲染的是库里的值，不是默认值。
func TestAccountPreferenceFragmentShowsStoredPreference(t *testing.T) {
	env := newUserHTTPEnv(t)
	u := seedActiveUser(t, env.db, "prefuser", "pref@example.com", "password123")
	cookies := loginAs(t, env, "prefuser", "password123")

	// 先经页面接口改一次偏好，再拉片段核对 —— 片段与页面必须看同一份数据。
	token, _ := csrfFromAccount(t, env, cookies)
	save := env.postForm(t, "/user/account/preference", map[string]string{
		"csrf_token": token, "pageSize": "66", "profileVisibility": "private",
		"timezone": "Asia/Shanghai",
	}, cookies)
	if save.Code != http.StatusOK {
		t.Fatalf("保存偏好失败（%d）：%s", save.Code, head(save.Body.String()))
	}
	_ = u

	rec := env.get(t, "/_fragments/accountPreferenceForm?projectId=proj-1", cookies)
	body := rec.Body.String()
	for _, want := range []string{
		`name="pageSize" min="1" max="200" value="66"`,
		`value="private" selected`,
		`value="Asia/Shanghai"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("偏好片段没有反映刚保存的值，缺少 %q：%s", want, head(body))
		}
	}
}

// TestAccountProfileSaveHonoursSameSiteNext 保存后的落点：合法站内路径回跳，其余留在内置页。
//
// 回跳目标最终要写进 Location，所以「只认站内相对路径」这条必须在真实链路上验证 ——
// 单测只证明了那个纯函数，这里证明它真的被接到了保存动作上。
func TestAccountProfileSaveHonoursSameSiteNext(t *testing.T) {
	env := newUserHTTPEnv(t)
	seedActiveUser(t, env.db, "nextuser", "next@example.com", "password123")
	cookies := loginAs(t, env, "nextuser", "password123")

	fragment := env.get(t, "/_fragments/accountProfileForm?projectId=proj-1&next=%2Fmy-account", cookies)
	token := csrfFrom(t, fragment.Body.String())

	ok := env.postForm(t, "/user/account/profile", map[string]string{
		"csrf_token": token, "nickname": "站内回跳", "next": "/my-account",
	}, cookies)
	if ok.Code != http.StatusFound {
		t.Fatalf("带合法站内 next 时应 302 回跳，实际 %d：%s", ok.Code, head(ok.Body.String()))
	}
	if loc := ok.Header().Get("Location"); loc != "/my-account" {
		t.Fatalf("回跳目标应为 /my-account，实际 %q", loc)
	}

	// 站外目标一律不接管响应（继续渲染内置账号页），否则就是开放重定向。
	for _, evil := range []string{"//evil.example", "https://evil.example/x", "evil.example"} {
		rec := env.postForm(t, "/user/account/profile", map[string]string{
			"csrf_token": token, "nickname": "别跳", "next": evil,
		}, cookies)
		if loc := rec.Header().Get("Location"); loc != "" {
			t.Fatalf("next=%q 不该产生跳转，实际 Location=%q", evil, loc)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("next=%q 时应照常渲染内置账号页，实际 %d", evil, rec.Code)
		}
	}
}

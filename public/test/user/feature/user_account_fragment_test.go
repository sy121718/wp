package feature

import (
	"net/http"
	"testing"
)

// TestAccountProfileSaveHonoursSameSiteNext 保存后的落点：合法站内路径回跳，其余留在内置页。
//
// 回跳目标最终要写进 Location，所以「只认站内相对路径」这条必须在真实链路上验证 ——
// 单测只证明了那个纯函数，这里证明它真的被接到了保存动作上。
func TestAccountProfileSaveHonoursSameSiteNext(t *testing.T) {
	env := newUserHTTPEnv(t)
	seedActiveUser(t, env.db, "nextuser", "next@example.com", "password123")
	cookies := loginAs(t, env, "nextuser", "password123")

	token, _ := csrfFromAccount(t, env, cookies)

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

package feature

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/viper"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/upload"
)

// siteForm 是站点写入口的提交形状：标准 form 编码（协议唯一，JSON 会被拒）。
func siteForm(t *testing.T, env *userHTTPEnv, path string, form map[string]string, csrf string, cookies []*http.Cookie, userAgent ...string) *httptest.ResponseRecorder {
	t.Helper()
	values := url.Values{}
	for k, v := range form {
		values.Set(k, v)
	}
	if csrf != "" {
		values.Set("csrf_token", csrf)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
	if len(userAgent) > 0 {
		req.Header.Set("User-Agent", userAgent[0])
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	return rec
}

// siteJSONBody 发 JSON 编码，用于证明写入口已收窄、不再接受双协议。
func siteJSONBody(t *testing.T, env *userHTTPEnv, path, body, csrf string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	return rec
}
func siteCookies(previous []*http.Cookie, rec *httptest.ResponseRecorder) []*http.Cookie {
	all := map[string]*http.Cookie{}
	for _, cookie := range previous {
		all[cookie.Name] = cookie
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.MaxAge < 0 {
			delete(all, cookie.Name)
		} else {
			all[cookie.Name] = cookie
		}
	}
	out := make([]*http.Cookie, 0, len(all))
	for _, cookie := range all {
		out = append(out, cookie)
	}
	return out
}
func siteData(t *testing.T, rec *httptest.ResponseRecorder, status int) map[string]any {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("状态应%d，实际%d：%s", status, rec.Code, head(rec.Body.String()))
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("不是JSON: %s", rec.Body.String())
	}
	if rec.Header().Get("Location") != "" || rec.Header().Get("HX-Redirect") != "" {
		t.Fatal("JSON接口不能指示导航")
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["code"]; !ok {
		t.Fatalf("缺少JSON契约: %+v", payload)
	}
	return payload
}

func TestSiteUserJSONVisitorIsolationAndInputErrors(t *testing.T) {
	env := newUserHTTPEnv(t)
	seedActiveUser(t, env.db, "siteuser", "site-user@example.com", "password123")
	guest := env.get(t, "/api/site/user/session", nil)
	payload := siteData(t, guest, http.StatusOK)
	if payload["data"].(map[string]any)["authenticated"] != false {
		t.Fatal("匿名会话被识别为登录")
	}
	csrf := guest.Header().Get("X-CSRF-Token")
	if csrf == "" {
		t.Fatal("首次只读没有下发访客CSRF")
	}
	if !strings.Contains(guest.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("会话事实不可缓存")
	}
	cookies := siteCookies(nil, guest)
	for _, path := range []string{"/api/site/user/account", "/api/site/user/sessions"} {
		failed := env.get(t, path, cookies)
		body := siteData(t, failed, http.StatusUnauthorized)
		if body["message"] == "" || strings.Contains(failed.Body.String(), "<html") {
			t.Fatal("未登录错误没有稳定JSON文案")
		}
	}
	missing := siteForm(t, env, "/api/site/user/login", map[string]string{"account":"siteuser","password":"password123"}, "", cookies)
	siteData(t, missing, http.StatusForbidden)
	wrong := siteForm(t, env, "/api/site/user/login", map[string]string{"account":"siteuser","password":"incorrect-password"}, csrf, cookies)
	bad := siteData(t, wrong, http.StatusUnauthorized)
	if bad["message"] == "" || strings.Contains(wrong.Body.String(), "bcrypt") || strings.Contains(wrong.Body.String(), "password123") {
		t.Fatal("凭据错误泄漏内部信息")
	}
	login := siteForm(t, env, "/api/site/user/login", map[string]string{"account":"siteuser","password":"password123","remember":"1"}, csrf, cookies)
	got := siteData(t, login, http.StatusOK)
	if got["data"].(map[string]any)["authenticated"] != true {
		t.Fatal("正常登录未建立访客会话")
	}
	if strings.Contains(strings.ToLower(login.Body.String()), "token") || strings.Contains(login.Body.String(), "password") {
		t.Fatal("登录JSON泄漏凭据")
	}
	rotated := login.Header().Get("X-CSRF-Token")
	if rotated == "" || rotated == csrf {
		t.Fatal("登录未轮换访客CSRF")
	}
	cookies = siteCookies(cookies, login)
	account := env.get(t, "/api/site/user/account", cookies)
	info := siteData(t, account, http.StatusOK)["data"].(map[string]any)
	if info["username"] != "siteuser" || info["email"] != "site-user@example.com" {
		t.Fatalf("账号事实不一致: %+v", info)
	}
	if _, ok := info["registeredAt"].(float64); !ok {
		t.Fatalf("时间应为unix毫秒: %+v", info["registeredAt"])
	}
	for key := range info {
		if strings.HasSuffix(strings.ToLower(key), "text") || strings.Contains(strings.ToLower(key), "token") {
			t.Fatalf("回了展示或凭据字段%s", key)
		}
	}

	devices := env.get(t, "/api/site/user/sessions", cookies)
	items := siteData(t, devices, http.StatusOK)["data"].([]any)
	var device map[string]any
	currentCount := 0
	for _, item := range items {
		candidate := item.(map[string]any)
		if len(candidate["id"].(string)) != 64 {
			t.Fatal("设备句柄格式错误")
		}
		if candidate["current"] == true {
			device = candidate
			currentCount++
		}
	}
	if currentCount != 1 {
		t.Fatalf("当前设备标记应恰好1，实际%d", currentCount)
	}
	revoke := siteForm(t, env, "/api/site/user/sessions/revoke", map[string]string{"id": device["id"].(string)}, rotated, cookies)
	siteData(t, revoke, http.StatusNotFound)

	secondGuest := env.get(t, "/api/site/user/session", nil)
	secondCookies := siteCookies(nil, secondGuest)
	secondLogin := siteForm(t, env, "/api/site/user/login", map[string]string{"account":"siteuser","password":"password123"}, secondGuest.Header().Get("X-CSRF-Token"), secondCookies, "SiteDeviceRevokeProbe")
	siteData(t, secondLogin, http.StatusOK)
	secondCookies = siteCookies(secondCookies, secondLogin)
	refreshed := siteData(t, env.get(t, "/api/site/user/sessions", cookies), http.StatusOK)["data"].([]any)
	otherID := ""
	for _, item := range refreshed {
		row := item.(map[string]any)
		if row["userAgent"] == "SiteDeviceRevokeProbe" {
			if row["current"] == true {
				t.Fatal("另一设备错标为当前设备")
			}
			otherID = row["id"].(string)
		}
	}
	if otherID == "" {
		t.Fatal("真实新会话没有进入设备事实")
	}
	siteData(t, siteForm(t, env, "/api/site/user/sessions/revoke", map[string]string{"id": otherID}, rotated, cookies), http.StatusOK)
	if siteData(t, env.get(t, "/api/site/user/session", secondCookies), http.StatusOK)["data"].(map[string]any)["authenticated"] != false {
		t.Fatal("撤销不透明设备句柄后另一设备仍有效")
	}
	if siteData(t, env.get(t, "/api/site/user/session", cookies), http.StatusOK)["data"].(map[string]any)["authenticated"] != true {
		t.Fatal("撤销另一设备误伤当前会话")
	}

	adminOnly := make([]*http.Cookie, 0)
	for _, cookie := range cookies {
		if cookie.Name != "gowp_user_session" {
			clone := *cookie
			adminOnly = append(adminOnly, &clone)
		} else {
			clone := *cookie
			clone.Name = "gowp_session"
			adminOnly = append(adminOnly, &clone)
		}
	}
	isolated := env.get(t, "/api/site/user/session", adminOnly)
	if siteData(t, isolated, http.StatusOK)["data"].(map[string]any)["authenticated"] != false {
		t.Fatal("后台cookie被当作访客身份")
	}
	logout := siteForm(t, env, "/api/site/user/logout", map[string]string{}, rotated, cookies)
	siteData(t, logout, http.StatusOK)
	after := env.get(t, "/api/site/user/session", siteCookies(cookies, logout))
	if siteData(t, after, http.StatusOK)["data"].(map[string]any)["authenticated"] != false {
		t.Fatal("退出后访客身份仍存在")
	}
}

func TestSiteUserFormDataAndTrustedIdentity(t *testing.T) {
	env := newUserHTTPEnv(t)
	seedActiveUser(t, env.db, "profilejson", "profile-json@example.com", "password123")
	other := seedActiveUser(t, env.db, "otherjson", "other-json@example.com", "password123")
	cookies := siteCookies(loginAs(t, env, "profilejson", "password123"), httptest.NewRecorder())
	state := env.get(t, "/api/site/user/session", cookies)
	cookies = siteCookies(cookies, state)
	csrf := state.Header().Get("X-CSRF-Token")
	// 资料保存走标准表单；userId 是客户端伪造的可信字段，服务端必须忽略。
	save := siteForm(t, env, "/api/site/user/account/profile", map[string]string{
		"userId": strconv.FormatUint(other.ID, 10), "nickname": "自己的昵称", "firstName": "Sky",
		"locale": "en-US", "timezone": "Asia/Shanghai", "gender": "2",
	}, csrf, cookies)
	siteData(t, save, http.StatusOK)
	account := siteData(t, env.get(t, "/api/site/user/account", cookies), http.StatusOK)["data"].(map[string]any)
	if account["username"] != "profilejson" || account["nickname"] != "自己的昵称" || account["gender"] != float64(2) || account["profileLocale"] != "en-US" {
		t.Fatalf("表单资料写读链不一致: %+v", account)
	}

	preference := siteForm(t, env, "/api/site/user/account/preference", map[string]string{
		"pageSize": "37", "profileVisibility": "private", "timezone": "Asia/Tokyo",
		"locale": "zh-CN", "theme": "dark", "emailNotify": "1",
	}, csrf, cookies)
	siteData(t, preference, http.StatusOK)
	data := siteData(t, env.get(t, "/api/site/user/account", cookies), http.StatusOK)["data"].(map[string]any)
	if data["pageSize"] != float64(37) || data["profileVisibility"] != "private" || data["theme"] != "dark" || data["emailNotify"] != true || data["smsNotify"] != false {
		t.Fatalf("FormData与服务事实不一致: %+v", data)
	}
	otherCookies := loginAs(t, env, "otherjson", "password123")
	otherData := siteData(t, env.get(t, "/api/site/user/account", otherCookies), http.StatusOK)["data"].(map[string]any)
	if otherData["nickname"] == "自己的昵称" {
		t.Fatal("客户端userId改写了其他用户")
	}
}

func TestSiteUserAvatarUsesTrustedMediaOrigin(t *testing.T) {
	env := newUserHTTPEnv(t)
	user := seedActiveUser(t, env.db, "avatarjson", "avatar-json@example.com", "password123")
	cfg := viper.New()
	cfg.Set("upload.default_provider", "local")
	cfg.Set("upload.local_dir", t.TempDir())
	cfg.Set("upload.base_url", "https://media-a.example.test/storage")
	if err := upload.Init(cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = upload.Close() })
	cookies := loginAs(t, env, "avatarjson", "password123")
	readAvatar := func(raw, want string) {
		t.Helper()
		if err := env.db.Model(user).Update("avatar", raw).Error; err != nil {
			t.Fatal(err)
		}
		got := siteData(t, env.get(t, "/api/site/user/account", cookies), http.StatusOK)["data"].(map[string]any)["avatar"]
		if got != want {
			t.Fatalf("头像完整URL不一致: raw=%q got=%v want=%q", raw, got, want)
		}
	}
	readAvatar("/storage/483.jpg", "https://media-a.example.test/storage/483.jpg")
	cfg.Set("upload.base_url", "https://media-b.example.test/storage")
	if err := upload.Init(cfg); err != nil {
		t.Fatal(err)
	}
	readAvatar("/storage/483.jpg", "https://media-b.example.test/storage/483.jpg")
	readAvatar("https://external.example/storage/483.jpg", "https://external.example/storage/483.jpg")
	readAvatar("javascript:alert(1)", "")
}

// 站点写入口协议唯一：urlencoded 与 multipart 两种表单编码都必须走通
// （ParseForm 按 Content-Type 在 FormPost / FormMultipart 间选择）。
func TestSiteUserCodeFlowFormEncodings(t *testing.T) {
	for _, encoding := range []string{"urlencoded", "multipart"} {
		t.Run(encoding, func(t *testing.T) {
			env := newUserHTTPEnv(t)
			guest := env.get(t, "/api/site/user/session", nil)
			cookies := siteCookies(nil, guest)
			csrf := guest.Header().Get("X-CSRF-Token")
			post := func(path string, values map[string]string, withCSRF bool) *httptest.ResponseRecorder {
				t.Helper()
				var req *http.Request
				if encoding == "urlencoded" {
					form := url.Values{}
					for key, value := range values {
						form.Set(key, value)
					}
					req = httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
					req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				} else {
					var body bytes.Buffer
					writer := multipart.NewWriter(&body)
					for key, value := range values {
						if err := writer.WriteField(key, value); err != nil {
							t.Fatal(err)
						}
					}
					if err := writer.Close(); err != nil {
						t.Fatal(err)
					}
					req = httptest.NewRequest(http.MethodPost, path, &body)
					req.Header.Set("Content-Type", writer.FormDataContentType())
				}
				if withCSRF {
					req.Header.Set("X-CSRF-Token", csrf)
				}
				for _, cookie := range cookies {
					req.AddCookie(cookie)
				}
				rec := httptest.NewRecorder()
				env.router.ServeHTTP(rec, req)
				cookies = siteCookies(cookies, rec)
				if rotated := rec.Header().Get("X-CSRF-Token"); rotated != "" {
					csrf = rotated
				}
				return rec
			}
			codeOf := func(template string) string {
				t.Helper()
				for index := len(env.mail.calls) - 1; index >= 0; index-- {
					mail := env.mail.calls[index]
					if mail.TemplateKey == template {
						code, _ := mail.Vars["code"].(string)
						if len(code) != 8 {
							t.Fatalf("验证码长度应8，实际%d", len(code))
						}
						return code
					}
				}
				t.Fatalf("测试邮件接收器未收到%s", template)
				return ""
			}
			noSecret := func(rec *httptest.ResponseRecorder, secret string) {
				t.Helper()
				if strings.Contains(rec.Body.String(), secret) {
					t.Fatal("JSON泄漏邮箱验证码")
				}
			}
			siteData(t, post("/api/site/user/activate", map[string]string{"key": "BAD-CODE"}, false), http.StatusForbidden)
			registration := post("/api/site/user/register", map[string]string{"username": "codeflow", "email": "codeflow@example.invalid", "password": "initial-password"}, true)
			registered := siteData(t, registration, http.StatusOK)["data"].(map[string]any)
			if registered["mailQueued"] != true || registered["email"] != "codeflow@example.invalid" {
				t.Fatalf("注册邮件事实不完整: %+v", registered)
			}
			originalCode := codeOf("register_verify")
			noSecret(registration, originalCode)
			siteData(t, post("/api/site/user/resend", map[string]string{"email": "codeflow@example.invalid"}, true), http.StatusOK)
			activationCode := codeOf("register_verify")
			if originalCode == activationCode {
				t.Fatal("重发未轮换激活码")
			}
			siteData(t, post("/api/site/user/activate", map[string]string{"key": originalCode}, true), http.StatusBadRequest)
			activated := post("/api/site/user/activate", map[string]string{"key": activationCode}, true)
			siteData(t, activated, http.StatusOK)
			noSecret(activated, activationCode)
			siteData(t, post("/api/site/user/activate", map[string]string{"key": activationCode}, true), http.StatusBadRequest)
			logged := siteData(t, post("/api/site/user/login", map[string]string{"account": "codeflow", "password": "initial-password"}, true), http.StatusOK)
			if logged["data"].(map[string]any)["authenticated"] != true {
				t.Fatal("激活后不能登录")
			}
			siteData(t, post("/api/site/user/forgot", map[string]string{"email": "codeflow@example.invalid"}, true), http.StatusOK)
			resetCode := codeOf("password_reset")
			siteData(t, post("/api/site/user/reset", map[string]string{"email": "codeflow@example.invalid", "key": "BAD-CODE", "password": "new-password-123"}, true), http.StatusBadRequest)
			reset := post("/api/site/user/reset", map[string]string{"email": "codeflow@example.invalid", "key": resetCode, "password": "new-password-123"}, true)
			siteData(t, reset, http.StatusOK)
			noSecret(reset, resetCode)
			state := siteData(t, env.get(t, "/api/site/user/session", cookies), http.StatusOK)
			if state["data"].(map[string]any)["authenticated"] != false {
				t.Fatal("重置密码后旧访客会话仍有效")
			}
			siteData(t, post("/api/site/user/login", map[string]string{"account": "codeflow", "password": "initial-password"}, true), http.StatusUnauthorized)
			login := siteData(t, post("/api/site/user/login", map[string]string{"account": "codeflow", "password": "new-password-123"}, true), http.StatusOK)
			if login["data"].(map[string]any)["authenticated"] != true {
				t.Fatal("重置后新密码不能登录")
			}
		})
	}
}

// 站点写入口已收窄为显式表单：JSON 编码必须被拒，不能退回双协议。
func TestSiteUserWriteEndpointsRejectJSONBody(t *testing.T) {
	env := newUserHTTPEnv(t)
	seedActiveUser(t, env.db, "jsonreject", "json-reject@example.com", "password123")
	guest := env.get(t, "/api/site/user/session", nil)
	cookies := siteCookies(nil, guest)
	csrf := guest.Header().Get("X-CSRF-Token")

	rec := siteJSONBody(t, env, "/api/site/user/login",
		`{"account":"jsonreject","password":"password123"}`, csrf, cookies)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("JSON 登录应被拒为 400，实际 %d：%s", rec.Code, head(rec.Body.String()))
	}

	rec = siteJSONBody(t, env, "/api/site/user/register",
		`{"username":"jsonreg","email":"jsonreg@example.com","password":"password123"}`, csrf, cookies)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("JSON 注册应被拒为 400，实际 %d：%s", rec.Code, head(rec.Body.String()))
	}
}

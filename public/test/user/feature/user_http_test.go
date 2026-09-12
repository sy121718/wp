package feature

// user_http_test.go -- 访客账号的 HTTP 链路（issue #36）。
//
// 这是**唯一能验证模板真的渲染得出来**的地方：service 层的测试走不到 Jet，
// 而模板里的语法错误（未闭合的 block、map 里取不到的键、range 写法不对）
// 只会在真的渲染时暴露 —— 那时访客看到的是 500 或者半截页面。
//
// 环境用真实的 PostgreSQL + Redis（不可用时 Skip）：会话在 Redis 里，
// 而「登录后能拿到身份」这件事正是本模块的核心，用替身测它等于没测。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	userhttp "go_wp/internal/module/user/inbound/http"
	usermodel "go_wp/internal/module/user/model"
	"go_wp/internal/templates"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

type userHTTPEnv struct {
	router *gin.Engine
	db     *gorm.DB
	mail   *fakeMail
}

// newUserHTTPEnv 装配一个只含访客路由的 engine（真实模板渲染）。
func newUserHTTPEnv(t *testing.T) *userHTTPEnv {
	t.Helper()

	if err := support.SetupRedisForTest(t); err != nil {
		t.Skipf("本地 Redis 不可用，跳过访客 HTTP 链路测试: %v", err)
	}
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用，跳过访客 HTTP 链路测试: %v", err)
	}
	if err := migrations.Run(db); err != nil {
		t.Fatalf("执行迁移失败: %v", err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	// 模板加载器按**相对路径**取模板（生产由工作目录＝仓库根保证），
	// 而 go test 的工作目录是包目录，这里显式指到仓库根下的模板目录。
	router.HTMLRender = templates.NewJetHTMLRender(filepath.Join(repoRoot(t), "internal/templates"), true)

	mail := &fakeMail{}
	userhttp.SetupUserRoutes(router, db, mail, "测试站")
	return &userHTTPEnv{router: router, db: db, mail: mail}
}

// repoRoot 从当前工作目录向上找 go.mod，定位仓库根。
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("取工作目录失败: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("从 %s 向上没有找到 go.mod", dir)
		}
		dir = parent
	}
}

func (e *userHTTPEnv) get(t *testing.T, path string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func (e *userHTTPEnv) postForm(
	t *testing.T, path string, form map[string]string, cookies []*http.Cookie,
) *httptest.ResponseRecorder {
	t.Helper()
	values := url.Values{}
	for k, v := range form {
		values.Set(k, v)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

// csrfInputRe 从渲染结果里抠出 CSRF 隐藏域的值。
var csrfInputRe = regexp.MustCompile("name=\"csrf_token\" value=\"([^\"]*)\"")

func csrfFrom(t *testing.T, body string) string {
	t.Helper()
	m := csrfInputRe.FindStringSubmatch(body)
	if len(m) < 2 {
		t.Fatalf("页面里没有 csrf_token 隐藏域（表单提交必然 403）:\n%s", head(body))
	}
	return m[1]
}

// head 截断响应体，避免断言失败时把整页 HTML 打出来。
func head(s string) string {
	if len(s) > 900 {
		return s[:900] + "\n...(截断)"
	}
	return s
}

// strEq 比较可空文本列（用户资料表的文本列几乎都是 *string）。
//
// 直接写 p.FirstName != "三" 编译不过；写成 p.FirstName != nil && *p.FirstName == "三"
// 会把「字段为 NULL」和「字段值不对」混进同一条失败信息，这个助手让断言只表达一件事。
func strEq(p *string, want string) bool {
	return p != nil && *p == want
}

// seedActiveUser 直接造一个已激活、密码已知的账号（跳过注册与邮箱验证）。
func seedActiveUser(t *testing.T, db *gorm.DB, username, email, password string) *usermodel.UserEntity {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("生成密码哈希失败: %v", err)
	}
	nick := username
	u := &usermodel.UserEntity{
		Username: username,
		Email:    email,
		Password: string(hash),
		Nickname: &nick,
		Status:   usermodel.UserStatusActive,
	}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("写入测试账号失败: %v", err)
	}
	return u
}

// TestUserGuestPagesRender 三个匿名页面都能渲染出真实页面（模板语法 + CSRF 注入）。
func TestUserGuestPagesRender(t *testing.T) {
	env := newUserHTTPEnv(t)

	cases := []struct {
		path string
		want string
	}{
		{"/user/register", "注册账号"},
		{"/user/login", "登录"},
		{"/user/forgot", "找回密码"},
	}
	for _, tc := range cases {
		rec := env.get(t, tc.path, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s 期望 200，实际 %d，body=%s", tc.path, rec.Code, head(rec.Body.String()))
		}
		body := rec.Body.String()
		if !strings.Contains(body, tc.want) {
			t.Fatalf("%s 渲染结果里没有 %q", tc.path, tc.want)
		}
		if !strings.Contains(body, "<form") {
			t.Fatalf("%s 页面里没有表单", tc.path)
		}
		if csrfFrom(t, body) == "" {
			t.Fatalf("%s 的 csrf_token 为空", tc.path)
		}
	}
}

// TestUserAccountRequiresLogin 未登录访问账号中心跳登录页，且带得回原地址。
func TestUserAccountRequiresLogin(t *testing.T) {
	env := newUserHTTPEnv(t)

	rec := env.get(t, "/user/account", nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("未登录访问账号中心期望 302，实际 %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/user/login") {
		t.Fatalf("跳转目标应为登录页，实际 %q", loc)
	}
	if !strings.Contains(loc, "next=") {
		t.Fatalf("跳转目标应带回原地址，实际 %q", loc)
	}
}

// TestUserRegisterRejectsMissingCSRF 缺 CSRF token 的注册请求必须被拒，且不落库。
func TestUserRegisterRejectsMissingCSRF(t *testing.T) {
	env := newUserHTTPEnv(t)

	rec := env.postForm(t, "/user/register", map[string]string{
		"username": "nocsrf",
		"email":    "nocsrf@example.com",
		"password": "password123",
	}, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("缺 CSRF token 的注册应被拒（403），实际 %d，body=%s", rec.Code, head(rec.Body.String()))
	}

	var count int64
	if err := env.db.Model(&usermodel.UserEntity{}).Where("username = ?", "nocsrf").Count(&count).Error; err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("被 CSRF 拦下的请求仍然写入了账号（count=%d）", count)
	}
}

// TestUserRegisterThroughHTTP 走真实表单提交：入库为待验证 + 通过邮件模块发出验证信。
func TestUserRegisterThroughHTTP(t *testing.T) {
	env := newUserHTTPEnv(t)

	page := env.get(t, "/user/register", nil)
	token := csrfFrom(t, page.Body.String())

	rec := env.postForm(t, "/user/register", map[string]string{
		"csrf_token": token,
		"username":   "httpuser",
		"email":      "httpuser@example.com",
		"password":   "password123",
		"nickname":   "HTTP 用户",
	}, page.Result().Cookies())

	if rec.Code != http.StatusOK {
		t.Fatalf("注册期望 200，实际 %d，body=%s", rec.Code, head(rec.Body.String()))
	}
	if !strings.Contains(rec.Body.String(), "验证邮件") {
		t.Fatalf("注册结果页没有提到验证邮件：%s", head(rec.Body.String()))
	}

	var u usermodel.UserEntity
	if err := env.db.Where("username = ?", "httpuser").First(&u).Error; err != nil {
		t.Fatalf("账号没有落库: %v", err)
	}
	if u.Status != usermodel.UserStatusPending {
		t.Fatalf("新注册账号应为待验证（%d），实际 %d", usermodel.UserStatusPending, u.Status)
	}
	if len(env.mail.calls) != 1 {
		t.Fatalf("注册应当恰好发出一封验证邮件，实际 %d 封", len(env.mail.calls))
	}
	if env.mail.calls[0].To != "httpuser@example.com" {
		t.Fatalf("验证邮件收件人不符: %s", env.mail.calls[0].To)
	}
}

// TestUserLoginAndAccountCenter 登录 → 带 cookie 进账号中心 → 登出。
func TestUserLoginAndAccountCenter(t *testing.T) {
	env := newUserHTTPEnv(t)
	seedActiveUser(t, env.db, "activeuser", "active@example.com", "password123")

	page := env.get(t, "/user/login", nil)
	token := csrfFrom(t, page.Body.String())

	login := env.postForm(t, "/user/login", map[string]string{
		"csrf_token": token,
		"account":    "activeuser",
		"password":   "password123",
	}, page.Result().Cookies())

	if login.Code != http.StatusFound {
		t.Fatalf("登录期望 302，实际 %d，body=%s", login.Code, head(login.Body.String()))
	}
	sessionCookies := login.Result().Cookies()
	if len(sessionCookies) == 0 {
		t.Fatalf("登录成功但没有下发任何 cookie")
	}

	account := env.get(t, "/user/account", sessionCookies)
	if account.Code != http.StatusOK {
		t.Fatalf("登录后访问账号中心期望 200，实际 %d，body=%s", account.Code, head(account.Body.String()))
	}
	body := account.Body.String()
	if !strings.Contains(body, "activeuser") {
		t.Fatalf("账号中心没有显示当前用户名：%s", head(body))
	}
	if !strings.Contains(body, "登录设备") {
		t.Fatalf("账号中心没有渲染登录设备区块")
	}
	if !strings.Contains(body, "/user/logout") {
		t.Fatalf("登录态页面里没有退出入口")
	}

	logoutToken := csrfFrom(t, body)
	logout := env.postForm(t, "/user/logout", map[string]string{
		"csrf_token": logoutToken,
	}, sessionCookies)
	if logout.Code != http.StatusFound {
		t.Fatalf("登出期望 302，实际 %d", logout.Code)
	}

	// 登出后再访问账号中心：带着旧 cookie 也必须被挡回登录页。
	after := env.get(t, "/user/account", sessionCookies)
	if after.Code != http.StatusFound {
		t.Fatalf("登出后访问账号中心应被重定向，实际 %d", after.Code)
	}
}

// TestUserLoginRejectsWrongPassword 密码错误时不放行。
func TestUserLoginRejectsWrongPassword(t *testing.T) {
	env := newUserHTTPEnv(t)
	seedActiveUser(t, env.db, "wrongpwd", "wrongpwd@example.com", "password123")

	page := env.get(t, "/user/login", nil)
	token := csrfFrom(t, page.Body.String())

	rec := env.postForm(t, "/user/login", map[string]string{
		"csrf_token": token,
		"account":    "wrongpwd",
		"password":   "not-the-password",
	}, page.Result().Cookies())

	if rec.Code == http.StatusFound {
		t.Fatalf("密码错误却登录成功了")
	}
	if !strings.Contains(rec.Body.String(), "登录") {
		t.Fatalf("密码错误后应回到登录页：%s", head(rec.Body.String()))
	}
}

// TestUserPendingAccountCannotLogin 未完成邮箱验证的账号不能登录。
func TestUserPendingAccountCannotLogin(t *testing.T) {
	env := newUserHTTPEnv(t)

	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("生成密码哈希失败: %v", err)
	}
	pendingNick := "待验证"
	pending := &usermodel.UserEntity{
		Username: "pendinguser",
		Email:    "pending@example.com",
		Password: string(hash),
		Nickname: &pendingNick,
		Status:   usermodel.UserStatusPending,
	}
	if err := env.db.Create(pending).Error; err != nil {
		t.Fatalf("写入待验证账号失败: %v", err)
	}

	page := env.get(t, "/user/login", nil)
	token := csrfFrom(t, page.Body.String())

	rec := env.postForm(t, "/user/login", map[string]string{
		"csrf_token": token,
		"account":    "pendinguser",
		"password":   "password123",
	}, page.Result().Cookies())

	if rec.Code == http.StatusFound {
		t.Fatalf("待验证账号不应该能登录")
	}
}

// loginAs 走真实登录表单，返回登录后的 cookie（含访客会话）。
func loginAs(t *testing.T, env *userHTTPEnv, account, password string) []*http.Cookie {
	t.Helper()
	page := env.get(t, "/user/login", nil)
	token := csrfFrom(t, page.Body.String())
	rec := env.postForm(t, "/user/login", map[string]string{
		"csrf_token": token,
		"account":    account,
		"password":   password,
	}, page.Result().Cookies())
	if rec.Code != http.StatusFound {
		t.Fatalf("登录失败（期望 302，实际 %d）: %s", rec.Code, head(rec.Body.String()))
	}
	return rec.Result().Cookies()
}

// csrfFromAccount 取当前登录态账号中心页面上的 CSRF token。
func csrfFromAccount(t *testing.T, env *userHTTPEnv, cookies []*http.Cookie) (string, string) {
	t.Helper()
	rec := env.get(t, "/user/account", cookies)
	if rec.Code != http.StatusOK {
		t.Fatalf("打开账号中心失败（%d）: %s", rec.Code, head(rec.Body.String()))
	}
	body := rec.Body.String()
	return csrfFrom(t, body), body
}

// TestUserUpdateProfileThroughHTTP 账号中心保存资料：改昵称写回 users，其余写 user_profiles。
func TestUserUpdateProfileThroughHTTP(t *testing.T) {
	env := newUserHTTPEnv(t)
	seedActiveUser(t, env.db, "profileuser", "profile@example.com", "password123")
	cookies := loginAs(t, env, "profileuser", "password123")
	token, _ := csrfFromAccount(t, env, cookies)

	rec := env.postForm(t, "/user/account/profile", map[string]string{
		"csrf_token": token,
		"nickname":   "新昵称",
		"firstName":  "三",
		"lastName":   "张",
		"gender":     "1",
		"birthday":   "1990-05-20",
		"bio":        "写点介绍",
		"city":       "杭州",
		"company":    "示例公司",
	}, cookies)
	if rec.Code != http.StatusOK {
		t.Fatalf("保存资料期望 200，实际 %d: %s", rec.Code, head(rec.Body.String()))
	}

	var u usermodel.UserEntity
	if err := env.db.Where("username = ?", "profileuser").First(&u).Error; err != nil {
		t.Fatalf("读取账号失败: %v", err)
	}
	if u.Nickname == nil || *u.Nickname != "新昵称" {
		t.Fatalf("昵称没有写回 users 表: %v", u.Nickname)
	}

	var p usermodel.UserProfileEntity
	if err := env.db.Where("user_id = ?", u.ID).First(&p).Error; err != nil {
		t.Fatalf("用户资料行没有建立: %v", err)
	}
	if !strEq(p.FirstName, "三") || !strEq(p.City, "杭州") || !strEq(p.Company, "示例公司") {
		t.Fatalf("扩展资料没有正确落库: %+v", p)
	}
	if p.Birthday == nil {
		t.Fatalf("生日没有解析落库（期望 1990-05-20）")
	}
}

// TestUserUpdatePreferenceThroughHTTP 偏好保存（含布尔字段与每页条数）。
func TestUserUpdatePreferenceThroughHTTP(t *testing.T) {
	env := newUserHTTPEnv(t)
	seedActiveUser(t, env.db, "prefuser", "pref@example.com", "password123")
	cookies := loginAs(t, env, "prefuser", "password123")
	token, _ := csrfFromAccount(t, env, cookies)

	rec := env.postForm(t, "/user/account/preference", map[string]string{
		"csrf_token":        token,
		"pageSize":          "50",
		"profileVisibility": "members",
		"timezone":          "Asia/Shanghai",
		"emailNotify":       "1",
	}, cookies)
	if rec.Code != http.StatusOK {
		t.Fatalf("保存偏好期望 200，实际 %d: %s", rec.Code, head(rec.Body.String()))
	}

	var u usermodel.UserEntity
	if err := env.db.Where("username = ?", "prefuser").First(&u).Error; err != nil {
		t.Fatalf("读取账号失败: %v", err)
	}
	var pref usermodel.UserPreferenceEntity
	if err := env.db.Where("user_id = ?", u.ID).First(&pref).Error; err != nil {
		t.Fatalf("用户偏好行没有建立: %v", err)
	}
	if pref.PageSize != 50 {
		t.Fatalf("每页条数没有保存: %d", pref.PageSize)
	}
	if pref.ProfileVisibility != "members" {
		t.Fatalf("可见性没有保存: %s", pref.ProfileVisibility)
	}
	if !pref.EmailNotify {
		t.Fatalf("邮件通知没有保存")
	}
	// 未勾选的复选框不会随表单提交，必须被写成 false 而不是保持原值。
	if pref.SmsNotify {
		t.Fatalf("未勾选的短信通知应为 false，实际 true")
	}
}

// TestUserChangePasswordRevokesSessions 改密码后所有会话失效（含当前设备）。
func TestUserChangePasswordRevokesSessions(t *testing.T) {
	env := newUserHTTPEnv(t)
	seedActiveUser(t, env.db, "pwduser", "pwd@example.com", "password123")
	cookies := loginAs(t, env, "pwduser", "password123")
	token, _ := csrfFromAccount(t, env, cookies)

	rec := env.postForm(t, "/user/account/password", map[string]string{
		"csrf_token":  token,
		"oldPassword": "password123",
		"newPassword": "newpassword456",
	}, cookies)
	if rec.Code != http.StatusOK {
		t.Fatalf("改密码期望 200，实际 %d: %s", rec.Code, head(rec.Body.String()))
	}

	// 当前 cookie 立刻失效 —— 这是「改完密码所有设备都要重新登录」的落地口径。
	after := env.get(t, "/user/account", cookies)
	if after.Code != http.StatusFound {
		t.Fatalf("改密码后旧会话应失效（期望 302），实际 %d", after.Code)
	}

	// 新密码可以登录，旧密码不行。
	fresh := loginAs(t, env, "pwduser", "newpassword456")
	if env.get(t, "/user/account", fresh).Code != http.StatusOK {
		t.Fatalf("用新密码登录后打不开账号中心")
	}

	page := env.get(t, "/user/login", nil)
	oldToken := csrfFrom(t, page.Body.String())
	old := env.postForm(t, "/user/login", map[string]string{
		"csrf_token": oldToken,
		"account":    "pwduser",
		"password":   "password123",
	}, page.Result().Cookies())
	if old.Code == http.StatusFound {
		t.Fatalf("旧密码在改密后仍然可以登录")
	}
}

// TestUserChangePasswordRejectsWrongOldPassword 旧密码不对时必须拒绝，且不改动密码。
func TestUserChangePasswordRejectsWrongOldPassword(t *testing.T) {
	env := newUserHTTPEnv(t)
	seedActiveUser(t, env.db, "pwdguard", "pwdguard@example.com", "password123")
	cookies := loginAs(t, env, "pwdguard", "password123")
	token, _ := csrfFromAccount(t, env, cookies)

	rec := env.postForm(t, "/user/account/password", map[string]string{
		"csrf_token":  token,
		"oldPassword": "wrong-old-password",
		"newPassword": "newpassword456",
	}, cookies)
	if rec.Code == http.StatusOK {
		t.Fatalf("旧密码错误却改密成功了")
	}

	// 原密码必须仍然可用（改密被拒时不能把密码写坏）。
	if env.get(t, "/user/account", loginAs(t, env, "pwdguard", "password123")).Code != http.StatusOK {
		t.Fatalf("改密被拒后原密码不能登录了")
	}
}

// TestUserRevokeOtherSessionsThroughHTTP 踢出其它设备，保留当前设备。
func TestUserRevokeOtherSessionsThroughHTTP(t *testing.T) {
	env := newUserHTTPEnv(t)
	seedActiveUser(t, env.db, "multidev", "multidev@example.com", "password123")

	first := loginAs(t, env, "multidev", "password123")
	second := loginAs(t, env, "multidev", "password123")

	// 两台设备都应该能用。
	if env.get(t, "/user/account", first).Code != http.StatusOK {
		t.Fatalf("第一台设备打不开账号中心")
	}
	if env.get(t, "/user/account", second).Code != http.StatusOK {
		t.Fatalf("第二台设备打不开账号中心")
	}

	token, body := csrfFromAccount(t, env, second)
	if !strings.Contains(body, "退出其它所有设备") {
		t.Fatalf("有第二台设备时账号中心应显示「退出其它所有设备」入口")
	}

	rec := env.postForm(t, "/user/account/sessions/revoke-others", map[string]string{
		"csrf_token": token,
	}, second)
	if rec.Code != http.StatusOK {
		t.Fatalf("退出其它设备期望 200，实际 %d: %s", rec.Code, head(rec.Body.String()))
	}

	if env.get(t, "/user/account", first).Code != http.StatusFound {
		t.Fatalf("被踢出的设备应当失效")
	}
	if env.get(t, "/user/account", second).Code != http.StatusOK {
		t.Fatalf("发起操作的当前设备不该被踢出")
	}
}

package feature

import (
	mw "go_wp/pkg/middleware"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	userhttp "go_wp/internal/module/user/inbound/http"
	usermodel "go_wp/internal/module/user/model"
	"go_wp/public/test/support"
)

type userHTTPEnv struct {
	router *gin.Engine
	db     *gorm.DB
	mail   *fakeMail
}

func newUserHTTPEnv(t *testing.T) *userHTTPEnv {
	t.Helper()

	if err := support.SetupRedisForTest(t); err != nil {
		t.Skipf("本地 Redis 不可用，跳过访客 HTTP 链路测试: %v", err)
	}
	db := support.NewMigratedPGTestDB(t)

	gin.SetMode(gin.TestMode)
	router := gin.New()

	mail := &fakeMail{}
	userhttp.SetupUserRoutes(mw.NewRegistry(), router, db, mail, "测试站")
	return &userHTTPEnv{router: router, db: db, mail: mail}
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

func head(s string) string {
	if len(s) > 900 {
		return s[:900] + "\n...(截断)"
	}
	return s
}

func strEq(p *string, want string) bool {
	return p != nil && *p == want
}

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

func TestUserRegisterRejectsMissingCSRF(t *testing.T) {
	env := newUserHTTPEnv(t)

	rec := env.postForm(t, "/api/site/user/register", map[string]string{
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

	page := env.get(t, "/api/site/user/session", nil)
	token := page.Header().Get("X-CSRF-Token")

	rec := env.postForm(t, "/api/site/user/login", map[string]string{
		"csrf_token": token,
		"account":    "pendinguser",
		"password":   "password123",
	}, page.Result().Cookies())

	if rec.Code == http.StatusOK {
		t.Fatalf("待验证账号不应该能登录")
	}
}

func loginAs(t *testing.T, env *userHTTPEnv, account, password string) []*http.Cookie {
	t.Helper()
	state := env.get(t, "/api/site/user/session", nil)
	cookies := siteCookies(nil, state)
	rec := siteForm(t, env, "/api/site/user/login",
		map[string]string{"account": account, "password": password},
		state.Header().Get("X-CSRF-Token"), cookies)
	siteData(t, rec, http.StatusOK)
	return siteCookies(cookies, rec)
}

func csrfFromAccount(t *testing.T, env *userHTTPEnv, cookies []*http.Cookie) (string, string) {
	t.Helper()
	rec := env.get(t, "/api/site/user/session", cookies)
	siteData(t, rec, http.StatusOK)
	return rec.Header().Get("X-CSRF-Token"), rec.Body.String()
}

func TestUserUpdateProfileThroughHTTP(t *testing.T) {
	env := newUserHTTPEnv(t)
	seedActiveUser(t, env.db, "profileuser", "profile@example.com", "password123")
	cookies := loginAs(t, env, "profileuser", "password123")
	token, _ := csrfFromAccount(t, env, cookies)

	rec := env.postForm(t, "/api/site/user/account/profile", map[string]string{
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

func TestUserUpdatePreferenceThroughHTTP(t *testing.T) {
	env := newUserHTTPEnv(t)
	seedActiveUser(t, env.db, "prefuser", "pref@example.com", "password123")
	cookies := loginAs(t, env, "prefuser", "password123")
	token, _ := csrfFromAccount(t, env, cookies)

	rec := env.postForm(t, "/api/site/user/account/preference", map[string]string{
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

	if pref.SmsNotify {
		t.Fatalf("未勾选的短信通知应为 false，实际 true")
	}
}

func TestUserChangePasswordRevokesSessions(t *testing.T) {
	env := newUserHTTPEnv(t)
	seedActiveUser(t, env.db, "pwduser", "pwd@example.com", "password123")
	cookies := loginAs(t, env, "pwduser", "password123")
	token, _ := csrfFromAccount(t, env, cookies)

	rec := env.postForm(t, "/api/site/user/account/password", map[string]string{
		"csrf_token":  token,
		"oldPassword": "password123",
		"newPassword": "newpassword456",
	}, cookies)
	if rec.Code != http.StatusOK {
		t.Fatalf("改密码期望 200，实际 %d: %s", rec.Code, head(rec.Body.String()))
	}

	after := env.get(t, "/api/site/user/account", cookies)
	if after.Code != http.StatusUnauthorized {
		t.Fatalf("改密码后旧会话应失效（期望 401），实际 %d", after.Code)
	}

	fresh := loginAs(t, env, "pwduser", "newpassword456")
	if env.get(t, "/api/site/user/account", fresh).Code != http.StatusOK {
		t.Fatalf("用新密码登录后打不开账号中心")
	}

	page := env.get(t, "/api/site/user/session", nil)
	oldToken := page.Header().Get("X-CSRF-Token")
	old := env.postForm(t, "/api/site/user/login", map[string]string{
		"csrf_token": oldToken,
		"account":    "pwduser",
		"password":   "password123",
	}, page.Result().Cookies())
	if old.Code == http.StatusOK {
		t.Fatalf("旧密码在改密后仍然可以登录")
	}
}

func TestUserChangePasswordRejectsWrongOldPassword(t *testing.T) {
	env := newUserHTTPEnv(t)
	seedActiveUser(t, env.db, "pwdguard", "pwdguard@example.com", "password123")
	cookies := loginAs(t, env, "pwdguard", "password123")
	token, _ := csrfFromAccount(t, env, cookies)

	rec := env.postForm(t, "/api/site/user/account/password", map[string]string{
		"csrf_token":  token,
		"oldPassword": "wrong-old-password",
		"newPassword": "newpassword456",
	}, cookies)
	if rec.Code == http.StatusOK {
		t.Fatalf("旧密码错误却改密成功了")
	}

	if env.get(t, "/api/site/user/account", loginAs(t, env, "pwdguard", "password123")).Code != http.StatusOK {
		t.Fatalf("改密被拒后原密码不能登录了")
	}
}

func TestUserRevokeOtherSessionsThroughHTTP(t *testing.T) {
	env := newUserHTTPEnv(t)
	seedActiveUser(t, env.db, "multidev", "multidev@example.com", "password123")

	first := loginAs(t, env, "multidev", "password123")
	baseline := siteData(t, env.get(t, "/api/site/user/sessions", first), http.StatusOK)["data"].([]any)
	second := loginAs(t, env, "multidev", "password123")

	if env.get(t, "/api/site/user/account", first).Code != http.StatusOK {
		t.Fatalf("第一台设备打不开账号中心")
	}
	if env.get(t, "/api/site/user/account", second).Code != http.StatusOK {
		t.Fatalf("第二台设备打不开账号中心")
	}

	token, _ := csrfFromAccount(t, env, second)
	devices := siteData(t, env.get(t, "/api/site/user/sessions", second), http.StatusOK)["data"].([]any)
	if len(devices) != len(baseline)+1 {
		t.Fatalf("第二次登录应新增一台独立设备，之前%d，之后%d", len(baseline), len(devices))
	}

	rec := env.postForm(t, "/api/site/user/sessions/revoke-others", map[string]string{
		"csrf_token": token,
	}, second)
	if rec.Code != http.StatusOK {
		t.Fatalf("退出其它设备期望 200，实际 %d: %s", rec.Code, head(rec.Body.String()))
	}

	if env.get(t, "/api/site/user/account", first).Code != http.StatusUnauthorized {
		t.Fatalf("被踢出的设备应当失效")
	}
	if env.get(t, "/api/site/user/account", second).Code != http.StatusOK {
		t.Fatalf("发起操作的当前设备不该被踢出")
	}
}

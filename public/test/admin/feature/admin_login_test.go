package feature

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"go_wp/internal/middleware/builtin"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	adminmodel "go_wp/internal/module/admin/model"
	adminservice "go_wp/internal/module/admin/service"
	"go_wp/pkg/auth"
	"go_wp/pkg/cache"
	"go_wp/pkg/captcha"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func newLoginEngine(t *testing.T, db *gorm.DB) (*gin.Engine, *adminmodel.AdminModel) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	handle := adminhttp.NewHandle(adminservice.NewService(db))
	engine := gin.New()
	engine.POST("/api/admin/login", handle.AdminLogin)
	return engine, adminmodel.NewAdminModel(db)
}

func seedAdmin(t *testing.T, m *adminmodel.AdminModel, username, password string) *adminmodel.AdminEntity {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("生成 bcrypt 哈希失败: %v", err)
	}
	entity := &adminmodel.AdminEntity{
		Username: username,
		Password: string(hash),
		Status:   adminmodel.AdminStatusActive,
	}
	if err := m.DB(context.Background()).Create(entity).Error; err != nil {
		t.Fatalf("写入测试管理员失败: %v", err)
	}
	return entity
}

func postLogin(engine *gin.Engine, username, password, captchaID, code string) (*support.StandardResponse, error) {
	raw := fmt.Sprintf(`{"username":%q,"password":%q,"captcha_id":%q,"captcha":%q}`,
		username, password, captchaID, code)
	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method:  http.MethodPost,
		Path:    "/api/admin/login",
		RawBody: []byte(raw),
	})
	if err != nil {
		return nil, err
	}
	std, err := support.ParseStandardResponse(recorder)
	if err != nil {
		return nil, err
	}
	return std, nil
}

func requireLoginFailure(t *testing.T, step string, std *support.StandardResponse, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s：请求失败: %v", step, err)
	}
	if std.Code == 200 {
		t.Fatalf("%s：应登录失败，got 成功响应", step)
	}
}

func TestAdminLoginFiveFailuresLocksTemporarilyNotBan(t *testing.T) {

	_ = newAdminShellEngine(t)

	db := support.NewMigratedPGTestDB(t)
	if err := auth.Init(viper.New()); err != nil {
		t.Fatalf("初始化会话存储失败: %v", err)
	}
	t.Cleanup(func() { _ = auth.Close() })

	engine, m := newLoginEngine(t, db)
	const (
		username = "lock_feature_user"
		password = "right-password"
	)
	seeded := seedAdmin(t, m, username, password)
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		captchaID, code := captcha.Get().Generate()
		std, err := postLogin(engine, username, "wrong-password", captchaID, code)
		requireLoginFailure(t, fmt.Sprintf("第 %d 次失败登录", i), std, err)
	}

	entity, err := m.GetByID(ctx, seeded.ID)
	if err != nil || entity == nil {
		t.Fatalf("查询测试管理员失败: %v", err)
	}
	if entity.Status != adminmodel.AdminStatusActive {
		t.Fatalf("H1 回归失败：status 被改为 %d，失败锁定不得修改 status", entity.Status)
	}
	if entity.LockedUntilTime == nil {
		t.Fatal("连续失败 5 次后应写入 locked_until_time")
	}
	until := time.Until(*entity.LockedUntilTime)
	if until < 29*time.Minute || until > 31*time.Minute {
		t.Fatalf("锁定时长应约 30 分钟，got %v", until.Round(time.Second))
	}
	if !entity.IsLocked() {
		t.Fatal("锁定窗口内 IsLocked 应为 true")
	}

	captchaID, code := captcha.Get().Generate()
	std, err := postLogin(engine, username, password, captchaID, code)
	requireLoginFailure(t, "锁定窗口内登录", std, err)
	const lockMessagePrefix = "账号已被锁定，请 "
	if std.Code != http.StatusBadRequest || !strings.HasPrefix(std.Message, lockMessagePrefix) ||
		!strings.HasSuffix(std.Message, " 后重试") {
		t.Fatalf("锁定窗口内应返回可读锁定文案，got code=%d message=%q", std.Code, std.Message)
	}
	remainingText := strings.TrimSuffix(strings.TrimPrefix(std.Message, lockMessagePrefix), " 后重试")
	remaining, err := time.ParseDuration(remainingText)
	if err != nil || remaining < 29*time.Minute || remaining > 30*time.Minute {
		t.Fatalf("锁定响应应包含约 30 分钟倒计时，got message=%q: %v", std.Message, err)
	}

	if err := m.DB(ctx).Where("id = ?", entity.ID).
		Update("locked_until_time", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatalf("更新锁定时间为过去失败: %v", err)
	}
	fresh, err := m.GetByID(ctx, entity.ID)
	if err != nil || fresh == nil {
		t.Fatalf("复查测试管理员失败: %v", err)
	}
	if fresh.IsLocked() {
		t.Fatal("locked_until_time 过期后 IsLocked 应自动为 false")
	}
	if !fresh.IsActive() {
		t.Fatal("解锁后 IsActive 应为 true（账号可重新登录，而非永久封禁）")
	}
}

func TestAdminLoginSuccessReturnsCSRFToken(t *testing.T) {

	db := support.NewMigratedPGTestDB(t)

	cfg := viper.New()
	cfg.Set("auth.session_secret", "test-secret-admin-login-feature")
	if err := auth.Init(cfg); err != nil {
		t.Fatalf("初始化会话存储失败: %v", err)
	}
	t.Cleanup(func() { _ = auth.Close() })

	if err := cache.Init(viper.New()); err != nil {
		support.FailIfRequiredRedis(t, err)
		t.Skipf("跳过（Redis 不可用）: %v", err)
	}
	t.Cleanup(func() { _ = cache.Close() })

	gin.SetMode(gin.TestMode)
	handle := adminhttp.NewHandle(adminservice.NewService(db))
	engine := gin.New()
	engine.POST("/api/admin/login", handle.AdminLogin)

	engine.POST("/api/admin/logout",
		builtin.SessionAuthMiddleware(),
		builtin.CSRFMiddleware(),
		handle.AdminLogout,
	)

	const (
		username = "csrf_feature_user"
		password = "right-password"
	)
	seedAdmin(t, adminmodel.NewAdminModel(db), username, password)

	captchaID, code := captcha.Get().Generate()
	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method: http.MethodPost,
		Path:   "/api/admin/login",
		RawBody: []byte(fmt.Sprintf(`{"username":%q,"password":%q,"captcha_id":%q,"captcha":%q}`,
			username, password, captchaID, code)),
	})
	if err != nil {
		t.Fatalf("登录请求失败: %v", err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("登录应成功，got %d: %s", recorder.Code, recorder.Body.String())
	}
	var loginResp struct {
		Code int `json:"code"`
		Data struct {
			CSRFToken string `json:"csrf_token"`
		} `json:"data"`
	}
	if err := support.DecodeResponseBody(recorder, &loginResp); err != nil {
		t.Fatalf("解析登录响应失败: %v", err)
	}
	if loginResp.Data.CSRFToken == "" {
		t.Fatal("H2 回归失败：登录成功响应 data 应携带 csrf_token")
	}

	cookieHeader := ""
	for _, ck := range recorder.Result().Cookies() {
		if ck.Name == "gowp_session" {
			cookieHeader = ck.Name + "=" + ck.Value
		}
	}
	if cookieHeader == "" {
		t.Fatal("登录响应应下发会话 cookie（gowp_session）")
	}

	logoutRequest := func(withToken bool) (*support.StandardResponse, int, error) {
		options := support.RequestOptions{
			Method:  http.MethodPost,
			Path:    "/api/admin/logout",
			Headers: map[string]string{"Cookie": cookieHeader},
		}
		if withToken {
			options.Headers["X-CSRF-Token"] = loginResp.Data.CSRFToken
		}
		recorder, err := support.SendRequest(engine, options)
		if err != nil {
			return nil, 0, err
		}
		std, err := support.ParseStandardResponse(recorder)
		return std, recorder.Code, err
	}

	_, statusNoToken, err := logoutRequest(false)
	if err != nil {
		t.Fatalf("无 token 登出请求失败: %v", err)
	}
	if statusNoToken != http.StatusForbidden {
		t.Fatalf("不带 CSRF token 的 POST 应被 403 拦截，got %d", statusNoToken)
	}

	stdLogout, statusWithToken, err := logoutRequest(true)
	if err != nil {
		t.Fatalf("带 token 登出请求失败: %v", err)
	}
	if statusWithToken != http.StatusOK || stdLogout.Code != 200 {
		t.Fatalf("携带登录响应 csrf_token 的 POST 应放行，got http=%d body=%s",
			statusWithToken, recorder.Body.String())
	}
}

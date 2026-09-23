// Package feature admin 登录链路 feature 测试（表结构由 support.NewMigratedPGTestDB 跑生产迁移建立）：
//   - H1：连续失败 ≥5 次只写 locked_until_time（30 分钟自动过期），绝不修改 status
//   - H2：登录成功响应 data 返回 csrf_token，配合 CSRFMiddleware 后续 POST 不再 403
//
// 验证码答案通过同进程直调 pkg/captcha Get().Generate() 获取（C4 后接口不下发明文）。
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

// newLoginEngine 组装最小登录链路（login 路由无认证中间件，与 admin_router 绑定方式一致）。
func newLoginEngine(t *testing.T, db *gorm.DB) (*gin.Engine, *adminmodel.AdminModel) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	handle := adminhttp.NewHandle(adminservice.NewService(db))
	engine := gin.New()
	engine.POST("/api/admin/login", handle.AdminLogin)
	return engine, adminmodel.NewAdminModel(db)
}

// seedAdmin 写入一枚启用状态的管理员（IsAdmin=0 避开超管唯一校验）。
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

// postLogin 提交登录请求（JSON body，字段名与 AdminLoginReq 对齐）。
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

// loginStatus 断言失败登录响应状态（400 + 非成功 code）。
func requireLoginFailure(t *testing.T, step string, std *support.StandardResponse, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s：请求失败: %v", step, err)
	}
	if std.Code == 200 {
		t.Fatalf("%s：应登录失败，got 成功响应", step)
	}
}

// TestAdminLoginFiveFailuresLocksTemporarilyNotBan H1 回归：
// 连续 5 次失败后只写 locked_until_time（约 30 分钟），status 保持启用；
// 锁定到期后 IsLocked 自动为 false、IsActive 为 true，账号可重新登录。
func TestAdminLoginFiveFailuresLocksTemporarilyNotBan(t *testing.T) {
	// 响应走真实 i18n 词条，避免独立运行时把裸错误 key 误判为成功。
	_ = newAdminShellEngine(t)
	// 表结构走生产迁移（sys_admin 是真实 DDL），不再手抄建表。
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

	// 5 次错误密码（验证码答案同进程直调 Generate 获取）
	for i := 1; i <= 5; i++ {
		captchaID, code := captcha.Get().Generate()
		std, err := postLogin(engine, username, "wrong-password", captchaID, code)
		requireLoginFailure(t, fmt.Sprintf("第 %d 次失败登录", i), std, err)
	}

	// 断言：status 未被修改（核心回归点），锁定只落在 locked_until_time
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

	// 锁定窗口内再次登录（即使用户名密码正确）→ 返回锁定错误而非凭据错误
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

	// 过期语义：locked_until_time 落在过去 → 自动解锁，且 status 仍启用
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

// TestAdminLoginSuccessReturnsCSRFToken H2 回归：
// 登录成功响应 data 携带 csrf_token；该 token 与会话绑定，
// 后续 POST 缺失 token 被 CSRF 中间件 403，携带 token 放行。
func TestAdminLoginSuccessReturnsCSRFToken(t *testing.T) {
	// 表结构走生产迁移（sys_admin 是真实 DDL），不再手抄建表。
	db := support.NewMigratedPGTestDB(t)

	cfg := viper.New()
	cfg.Set("auth.session_secret", "test-secret-admin-login-feature")
	if err := auth.Init(cfg); err != nil {
		t.Fatalf("初始化会话存储失败: %v", err)
	}
	t.Cleanup(func() { _ = auth.Close() })

	// 登录成功链路写 Redis 用户会话；Redis 不可用时跳过而非误报
	if err := cache.Init(viper.New()); err != nil {
		t.Skipf("跳过（Redis 不可用）: %v", err)
	}
	t.Cleanup(func() { _ = cache.Close() })

	gin.SetMode(gin.TestMode)
	handle := adminhttp.NewHandle(adminservice.NewService(db))
	engine := gin.New()
	engine.POST("/api/admin/login", handle.AdminLogin)
	// 登出路由：中间件组合与 admin_router 的 auth 组一致（SessionAuth + CSRF）
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

	// 1) 登录成功 → data.csrf_token 非空 + 会话 cookie 下发
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

	// 提取会话 cookie（gowp_session），后续请求携带
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

	// 2) 不带 X-CSRF-Token → CSRF 中间件 403
	_, statusNoToken, err := logoutRequest(false)
	if err != nil {
		t.Fatalf("无 token 登出请求失败: %v", err)
	}
	if statusNoToken != http.StatusForbidden {
		t.Fatalf("不带 CSRF token 的 POST 应被 403 拦截，got %d", statusNoToken)
	}

	// 3) 携带登录响应中的 token → 放行（登录登出闭环成功）
	stdLogout, statusWithToken, err := logoutRequest(true)
	if err != nil {
		t.Fatalf("带 token 登出请求失败: %v", err)
	}
	if statusWithToken != http.StatusOK || stdLogout.Code != 200 {
		t.Fatalf("携带登录响应 csrf_token 的 POST 应放行，got http=%d body=%s",
			statusWithToken, recorder.Body.String())
	}
}

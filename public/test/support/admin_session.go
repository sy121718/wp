// admin_session.go — feature 测试的管理员登录辅助。
//
// 通过真实链路构造登录态：同进程生成验证码（pkg/captcha.Get().Generate()，与
// handler 共享 MemoryStore）→ POST /api/admin/login（走真实 captcha + PG + Redis
// 校验）→ 从 Set-Cookie 提取 gowp_session，后续请求回填 Cookie 即视为已登录。
//
// 依赖真实 Redis 与 PostgreSQL：二者任一不可用时，调用方应 t.Skip（与
// support.NewPGTestDB 的 ErrPGUnavailable Skip 模式一致）。
package support

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"go_wp/pkg/auth"
	"go_wp/pkg/cache"
	"go_wp/pkg/captcha"

	adminmodel "go_wp/internal/module/admin/model"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// DefaultTestRedisAddr 测试默认 Redis 地址（本地实例），可用 TEST_REDIS_ADDR 覆盖。
const DefaultTestRedisAddr = "127.0.0.1:6379"

// TestAdminUsername / TestAdminPassword 测试管理员的默认凭证。
const (
	TestAdminUsername = "feature_admin"
	TestAdminPassword = "Feature@123456"
)

// ErrRedisUnavailable 表示本地 Redis 不可达，相关测试应 Skip。
var ErrRedisUnavailable = errors.New("本地 Redis 不可用")

// SetupRedisForTest 初始化测试用 cache（Redis）与 auth（Cookie 会话）组件并探测连通性。
//
// 失败时返回 ErrRedisUnavailable 包装错误（内部已清理已初始化的组件），
// 调用方据此 t.Skip；成功时注册 t.Cleanup 逆序关闭组件。
func SetupRedisForTest(t *testing.T) error {
	t.Helper()

	addr := strings.TrimSpace(os.Getenv("TEST_REDIS_ADDR"))
	if addr == "" {
		addr = DefaultTestRedisAddr
	}

	cfg := viper.New()
	cfg.Set("redis.addrs", []string{addr})
	cfg.Set("auth.session_secret", "wp-feature-test-session-secret")

	if err := cache.Init(cfg); err != nil {
		return fmt.Errorf("%w: %v", ErrRedisUnavailable, err)
	}
	if err := auth.Init(cfg); err != nil {
		_ = cache.Close()
		return fmt.Errorf("%w: %v", ErrRedisUnavailable, err)
	}

	client, err := cache.GetRedis()
	if err != nil {
		teardownRedisTest(t)
		return fmt.Errorf("%w: %v", ErrRedisUnavailable, err)
	}
	pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		teardownRedisTest(t)
		return fmt.Errorf("%w: %v", ErrRedisUnavailable, err)
	}

	t.Cleanup(func() { teardownRedisTest(t) })
	return nil
}

// teardownRedisTest 逆序关闭 auth 与 cache 组件（幂等）。
func teardownRedisTest(t *testing.T) {
	t.Helper()
	_ = auth.Close()
	_ = cache.Close()
}

// seedAdminPGDDL PostgreSQL 兼容的 sys_admin 建表语句。
//
// AdminEntity 的 gorm tag 写死 MySQL 类型（tinyint(4)/datetime(3)/bigint unsigned），
// AutoMigrate 在 PostgreSQL 上会生成非法 DDL，这里按实体字段手工建 PG 兼容表
// （列名与 adminmodel.AdminEntity 的 column tag 一一对应）。
const seedAdminPGDDL = `
CREATE TABLE IF NOT EXISTS sys_admin (
    id                   bigserial PRIMARY KEY,
    dept_id              bigint DEFAULT 0,
    username             varchar(50),
    password             varchar(100),
    name                 varchar(50),
    avatar               varchar(255),
    email                varchar(100),
    phone                varchar(20),
    status               smallint DEFAULT 1,
    is_admin             smallint DEFAULT 0,
    login_failure_count  smallint DEFAULT 0,
    locked_until_time    timestamptz,
    metadata             jsonb,
    last_failure_time    timestamptz,
    register_ip          varchar(50),
    register_location    varchar(100),
    last_login_ip        varchar(50),
    last_login_location  varchar(100),
    last_login_isp       varchar(50),
    last_login_time      timestamptz,
    create_by            bigint,
    create_time          timestamptz,
    update_by            bigint,
    update_time          timestamptz,
    remark               varchar(255)
)`

// SeedTestAdmin 在测试库中准备 sys_admin 表并写入一个启用状态的测试管理员。
// 密码使用 bcrypt 加密，与生产登录链路一致。
// PostgreSQL 走手工 DDL（见 seedAdminPGDDL），其他方言回退 AutoMigrate。
func SeedTestAdmin(t *testing.T, db *gorm.DB, username, password string) error {
	t.Helper()

	if db.Dialector.Name() == "postgres" {
		if err := db.Exec(seedAdminPGDDL).Error; err != nil {
			return fmt.Errorf("建 sys_admin 表失败: %w", err)
		}
	} else if err := db.AutoMigrate(&adminmodel.AdminEntity{}); err != nil {
		return fmt.Errorf("迁移 sys_admin 失败: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		return fmt.Errorf("生成测试密码哈希失败: %w", err)
	}

	entity := adminmodel.AdminEntity{
		ID:       1,
		Username: username,
		Password: string(hash),
		Status:   adminmodel.AdminStatusActive,
		IsAdmin:  1,
	}
	if err := db.Where("username = ?", username).
		FirstOrCreate(&entity).Error; err != nil {
		return fmt.Errorf("写入测试管理员失败: %w", err)
	}
	return nil
}

// LoginAdmin 通过真实登录链路获取已登录会话 Cookie。
//
// 返回值可直接填入 support.RequestOptions.Headers 的 "Cookie" 键。
// 登录链路任一环节失败（验证码、凭证、Redis/PG 不可用等）返回非 nil error，
// 调用方按环境不可用处理（t.Skip）或按用例语义断言。
func LoginAdmin(t *testing.T, engine *gin.Engine, username, password string) (string, error) {
	t.Helper()
	sess, err := LoginAdminSession(t, engine, username, password)
	if err != nil {
		return "", err
	}
	return sess.Cookie, nil
}

// AdminSession 已登录管理员会话：Cookie 与登录响应下发的 CSRF token。
// 业务 API 与后台页面写路由挂载 CSRFMiddleware 后，
// 后续 POST 请求必须携带 X-CSRF-Token 头（或表单字段 csrf_token），
// 否则被 403 拦截——测试请用 CSRFToken 构造写请求。
type AdminSession struct {
	Cookie    string
	CSRFToken string
}

// LoginAdminSession 通过真实登录链路获取已登录会话 Cookie 与 CSRF token。
// 与 LoginAdmin 同一实现；登录成功后从响应 data.csrf_token 提取 token
// （admin_handle.go 登录成功响应注入，与 cookie session 绑定）。
func LoginAdminSession(t *testing.T, engine *gin.Engine, username, password string) (*AdminSession, error) {
	t.Helper()

	if engine == nil {
		return nil, errors.New("engine 不能为空")
	}

	// 同进程生成验证码：MemoryStore 与 handler 共享，code 直接可用于登录。
	captchaID, captchaCode := captcha.Get().Generate()

	recorder, err := SendRequest(engine, RequestOptions{
		Method: http.MethodPost,
		Path:   "/api/admin/login",
		Body: map[string]any{
			"username":   username,
			"password":   password,
			"captcha_id": captchaID,
			"captcha":    captchaCode,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("发送登录请求失败: %w", err)
	}
	if recorder.Code != http.StatusOK {
		return nil, fmt.Errorf("登录接口返回 %d: %s", recorder.Code, recorder.Body.String())
	}

	sess := &AdminSession{}
	for _, ck := range recorder.Result().Cookies() {
		if ck.Name == "gowp_session" && ck.Value != "" {
			// cookie 名对齐 pkg/auth cookie_session.go 的 sessionName（包内私有常量）。
			sess.Cookie = ck.Name + "=" + ck.Value
		}
	}
	if sess.Cookie == "" {
		return nil, errors.New("登录响应未写入 gowp_session cookie")
	}

	var loginResp struct {
		Data struct {
			CSRFToken string `json:"csrf_token"`
		} `json:"data"`
	}
	if err := DecodeResponseBody(recorder, &loginResp); err != nil {
		return nil, fmt.Errorf("解析登录响应失败: %w", err)
	}
	sess.CSRFToken = loginResp.Data.CSRFToken
	if sess.CSRFToken == "" {
		return nil, errors.New("登录响应未下发 csrf_token")
	}
	return sess, nil
}

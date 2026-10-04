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
	return SetupRedisForTestAt(t, addr)
}

// SetupRedisForTestAt 用显式 Redis 地址初始化测试用 cache（Redis）与 auth 组件，
// 失败语义与 SetupRedisForTest 一致（support/testenv.go 容器回退路径复用）。
func SetupRedisForTestAt(t *testing.T, addr string) error {
	t.Helper()

	cfg := viper.New()
	cfg.Set("redis.addrs", []string{addr})
	cfg.Set("auth.session_secret", "sky-feature-test-session-secret")

	if err := cache.Init(cfg); err != nil {
		wrapped := fmt.Errorf("%w: %v", ErrRedisUnavailable, err)
		FailIfRequiredRedis(t, wrapped)
		return wrapped
	}
	if err := auth.Init(cfg); err != nil {
		_ = cache.Close()
		wrapped := fmt.Errorf("%w: %v", ErrRedisUnavailable, err)
		FailIfRequiredRedis(t, wrapped)
		return wrapped
	}

	client, err := cache.GetRedis()
	if err != nil {
		teardownRedisTest(t)
		wrapped := fmt.Errorf("%w: %v", ErrRedisUnavailable, err)
		FailIfRequiredRedis(t, wrapped)
		return wrapped
	}
	pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		teardownRedisTest(t)
		wrapped := fmt.Errorf("%w: %v", ErrRedisUnavailable, err)
		FailIfRequiredRedis(t, wrapped)
		return wrapped
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

// SeedTestAdmin 在测试库中写入一个启用状态的测试管理员（bcrypt 加密，与生产登录链路一致）。
//
// 前置：目标库必须已跑过生产迁移（migrations.Run，或直接用 support.NewMigratedPGTestDB）。
// 这里不再手抄 sys_admin 的 DDL：手抄版本与生产 DDL 分叉后会静默失配（audit 已记录这类缺陷），
// 而 sys_admin 的真实结构由 public/migrations/init_schema.sql 定义。
func SeedTestAdmin(t *testing.T, db *gorm.DB, username, password string) error {
	t.Helper()

	// 前置检查给出可定位的错误：迁移没跑时这里会明确说「表不存在」，
	// 而不是让后面的 INSERT 报一堆列不存在。
	var regclass *string
	if err := db.Raw("SELECT to_regclass('sys_admin')::text").Scan(&regclass).Error; err != nil {
		return fmt.Errorf("检查 sys_admin 是否存在失败: %w", err)
	}
	if regclass == nil || *regclass == "" {
		return fmt.Errorf("sys_admin 不存在：请先对测试库跑生产迁移（support.NewMigratedPGTestDB 或 migrations.Run）")
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

	// 上面显式指定了主键 id = 1：BIGSERIAL 的序列不会因此推进，之后任何走 nextval 的插入
	// 仍会拿到 1 并撞 sys_admin_pkey。真实触发过：030a 默认超管 seed（对测试库跑 RunSeeds 时）
	// 报 duplicate key value violates unique constraint "sys_admin_pkey" —— 而生产全新库里
	// 序列是干净的，只有这种「先手工插固定 id、再跑 seed」的测试序才会踩到。
	// 把序列对齐到当前最大值，让后续插入从 max+1 开始。
	if err := db.Exec(
		"SELECT setval(pg_get_serial_sequence('sys_admin', 'id'), COALESCE((SELECT MAX(id) FROM sys_admin), 1))",
	).Error; err != nil {
		return fmt.Errorf("同步 sys_admin id 序列失败: %w", err)
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

package support

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"runtime"
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

const DefaultTestRedisAddr = "127.0.0.1:6379"

var testRedisDBByDir = map[string]int{
	"internal/module/publication/inbound/http": 1,
	"public/test/feature":                      2,
	"public/test/navigation/feature":           3,
	"public/test/user/feature":                 4,
	"public/test/architecture":                 5,
	"public/test/order/feature":                6,
	"public/test/user/unit":                    7,
	"public/test/publication/feature":          8,
	"public/test/admin/feature":                9,
}

func testRedisDBForCaller(t *testing.T) int {
	t.Helper()
	for i := 2; i < 12; i++ {
		_, file, _, ok := runtime.Caller(i)
		if !ok {
			break
		}
		dir := repoRelativeDir(file)
		if strings.HasSuffix(dir, "public/test/support") {
			continue
		}
		db, found := testRedisDBByDir[dir]
		if !found {
			t.Fatalf("测试 Redis DB 未分配：%s —— 请在 public/test/support/admin_session.go 的 testRedisDBByDir 领一个未用过的号（1..15）", dir)
		}
		return db
	}
	t.Fatalf("无法定位 SetupRedisForTest 的调用方目录")
	return 0
}

func repoRelativeDir(file string) string {
	dir := path.Dir(file)
	for _, marker := range []string{"/public/", "/internal/", "/cmd/", "/pkg/"} {
		if i := strings.LastIndex(dir, marker); i >= 0 {
			return dir[i+1:]
		}
	}
	return dir
}

const (
	TestAdminUsername = "feature_admin"
	TestAdminPassword = "Feature@123456"
)

var ErrRedisUnavailable = errors.New("本地 Redis 不可用")

func SetupRedisForTest(t *testing.T) error {
	t.Helper()

	addr := strings.TrimSpace(os.Getenv("TEST_REDIS_ADDR"))
	if addr == "" {
		addr = DefaultTestRedisAddr
	}
	return setupRedisForTestWithDB(t, addr, testRedisDBForCaller(t))
}

func SetupRedisForTestAt(t *testing.T, addr string) error {
	t.Helper()

	return setupRedisForTestWithDB(t, addr, testRedisDBForCaller(t))
}

func setupRedisForTestWithDB(t *testing.T, addr string, db int) error {
	t.Helper()

	cfg := viper.New()
	cfg.Set("redis.addrs", []string{addr})
	cfg.Set("auth.session_secret", "sky-feature-test-session-secret")
	cfg.Set("redis.db", db)

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

func teardownRedisTest(t *testing.T) {
	t.Helper()
	_ = auth.Close()
	_ = cache.Close()
}

func SeedTestAdmin(t *testing.T, db *gorm.DB, username, password string) error {
	t.Helper()

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

	if err := db.Exec(
		"SELECT setval(pg_get_serial_sequence('sys_admin', 'id'), COALESCE((SELECT MAX(id) FROM sys_admin), 1))",
	).Error; err != nil {
		return fmt.Errorf("同步 sys_admin id 序列失败: %w", err)
	}
	return nil
}

func LoginAdmin(t *testing.T, engine *gin.Engine, username, password string) (string, error) {
	t.Helper()
	sess, err := LoginAdminSession(t, engine, username, password)
	if err != nil {
		return "", err
	}
	return sess.Cookie, nil
}

type AdminSession struct {
	Cookie    string
	CSRFToken string
}

func LoginAdminSession(t *testing.T, engine *gin.Engine, username, password string) (*AdminSession, error) {
	t.Helper()

	if engine == nil {
		return nil, errors.New("engine 不能为空")
	}

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

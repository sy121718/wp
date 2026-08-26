package response

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"go_wp/pkg/database"
	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// i18nRecord 测试用 sys_i18n 表结构（仅覆盖 translate 用例所需字段）。
type i18nRecord struct {
	ID        uint64 `gorm:"primaryKey"`
	ItemKey   string `gorm:"column:item_key"`
	Lang      string `gorm:"column:lang"`
	ItemValue string `gorm:"column:item_value"`
	HttpCode  int    `gorm:"column:http_code"`
	Status    int    `gorm:"column:status"`
}

func (i18nRecord) TableName() string {
	return "sys_i18n"
}

// translate 测试种子：与 TestTranslateForms 用例一一对应。
var translateSeeds = []i18nRecord{
	{ItemKey: "ErrAdminNotFound", Lang: "zh-CN", ItemValue: "管理员不存在", HttpCode: 404, Status: 1},
	{ItemKey: "ErrAdminNotFound", Lang: "en-US", ItemValue: "Admin not found", HttpCode: 404, Status: 1},
	{ItemKey: "ErrAccountLocked", Lang: "zh-CN", ItemValue: "账号已被锁定，请 %s 后重试", HttpCode: 423, Status: 1},
	{ItemKey: "ErrAccountLocked", Lang: "en-US", ItemValue: "Account locked, retry in %s", HttpCode: 423, Status: 1},
	{ItemKey: "ErrInvalidParams", Lang: "zh-CN", ItemValue: "请求参数错误", HttpCode: 400, Status: 1},
	{ItemKey: "ErrInvalidParams", Lang: "en-US", ItemValue: "Invalid parameters", HttpCode: 400, Status: 1},
	// key|param 协议外占位符（%d）验证：不应注入，按 key 原文降级
	{ItemKey: "ErrTestIntPlaceholder", Lang: "zh-CN", ItemValue: "已失败 %d 次，请稍后再试", HttpCode: 429, Status: 1},
	{ItemKey: "ErrTestIntPlaceholder", Lang: "en-US", ItemValue: "Failed %d times, retry later", HttpCode: 429, Status: 1},
	// 通用操作成功消息（Success 响应）
	{ItemKey: "msg_operation_success", Lang: "zh-CN", ItemValue: "操作成功", HttpCode: 200, Status: 1},
	{ItemKey: "msg_operation_success", Lang: "en-US", ItemValue: "Operation successful", HttpCode: 200, Status: 1},
}

var (
	translateOnce    sync.Once
	translateInitErr error
)

// ensureTranslateFixture 初始化 translate 测试所需的 i18n 缓存（全局单例，仅执行一次）。
func ensureTranslateFixture(t *testing.T) {
	t.Helper()

	translateOnce.Do(func() {
		translateInitErr = initTranslateFixture(t)
	})
	if translateInitErr != nil {
		t.Fatalf("初始化 translate 测试数据失败: %v", translateInitErr)
	}
}

// initTranslateFixture 创建 MySQL 临时测试库并加载 i18n 缓存。
// 不依赖 public/test/support（避免 pkg/response → routers 的 import cycle），建库逻辑内联。
func initTranslateFixture(t *testing.T) error {
	t.Helper()

	// 1) 建独立临时库（测试结束自动 drop，不污染开发库）
	dbName := "go_test_resp_" + randomSuffix()
	admin, err := gorm.Open(mysql.Open("root:root@tcp(127.0.0.1:3306)/?charset=utf8mb4&parseTime=True&loc=Local"), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("连接 MySQL 失败: %w", err)
	}
	if err := admin.Exec("CREATE DATABASE `" + dbName + "`").Error; err != nil {
		return fmt.Errorf("创建临时库 %s 失败: %w", dbName, err)
	}
	t.Cleanup(func() {
		_ = admin.Exec("DROP DATABASE IF EXISTS `" + dbName + "`")
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	// 2) 初始化 database 组件并写入种子
	cfg := viper.New()
	cfg.Set("server.mode", "test")
	cfg.Set("database.driver", "mysql")
	cfg.Set("database.host", "127.0.0.1")
	cfg.Set("database.port", 3306)
	cfg.Set("database.user", "root")
	cfg.Set("database.password", "root")
	cfg.Set("database.dbname", dbName)
	cfg.Set("database.max_idle_conns", 1)
	cfg.Set("database.max_open_conns", 1)

	if err := database.Init(cfg); err != nil {
		return err
	}

	db, err := database.GetDB()
	if err != nil {
		return err
	}
	if err := db.AutoMigrate(&i18nRecord{}); err != nil {
		return err
	}
	if err := db.Create(&translateSeeds).Error; err != nil {
		return err
	}

	// 3) 加载 i18n 缓存并注册清理
	t.Cleanup(func() {
		_ = i18n.Close()
		_ = database.Close()
	})
	return i18n.Init(cfg)
}

func randomSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// newTestCtx 构造带 Accept-Language 的测试上下文。
func newTestCtx(acceptLang string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	if acceptLang != "" {
		c.Request = newRequestWithHeader("Accept-Language", acceptLang)
	}
	return c
}

func TestRequestLanguage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// query lang 优先
	c, _ := gin.CreateTestContext(nil)
	c.Request = newRequestWithHeader("Accept-Language", "en-US")
	if got := requestLanguage(c); got != "en-US" {
		t.Fatalf("query 为空时应取 Accept-Language，got %q", got)
	}

	// 带优先级参数的 Accept-Language（zh-CN;q=0.8）应取首段
	c.Request = newRequestWithHeader("Accept-Language", "zh-CN;q=0.8, en-US;q=0.5")
	if got := requestLanguage(c); got != "zh-CN" {
		t.Fatalf("应取首段语言，got %q", got)
	}

	if got := requestLanguage(nil); got == "" {
		t.Fatal("nil 上下文应回退默认语言，不应为空")
	}
}

// TestTranslateForms 覆盖三种消息形态：
//  1. 纯 key（库中存在 → 翻译；不存在 → 原样返回）
//  2. key|param（翻译模板 + 参数注入）
//  3. key: detail（仅翻译 key 前缀）
func TestTranslateForms(t *testing.T) {
	ensureTranslateFixture(t)

	cases := []struct {
		name    string
		lang    string
		message string
		want    string
	}{
		// 形态 1：纯 key 命中
		{"纯key命中-中文", "zh-CN", "ErrAdminNotFound", "管理员不存在"},
		{"纯key命中-英文", "en-US", "ErrAdminNotFound", "Admin not found"},
		// 形态 1：未知 key 原样返回（降级）
		{"未知key原样", "zh-CN", "ErrNotExistInDB", "ErrNotExistInDB"},
		// 形态 2：key|param 参数注入
		{"key|param-中文", "zh-CN", "ErrAccountLocked|5m0s", "账号已被锁定，请 5m0s 后重试"},
		{"key|param-英文", "en-US", "ErrAccountLocked|5m0s", "Account locked, retry in 5m0s"},
		// 形态 3：key: detail 拼接消息
		{"key:detail-中文", "zh-CN", "ErrInvalidParams: json: 字段缺失", "请求参数错误: json: 字段缺失"},
		// 普通中文消息原样（未 key 化的历史消息，如 pkg 层直值）
		{"中文直值原样", "zh-CN", "请求的资源不存在", "请求的资源不存在"},
		// 协议外占位符（%d）：模板命中但拒绝注入，按 key 原文降级
		{"协议外占位符", "zh-CN", "ErrTestIntPlaceholder|3", "ErrTestIntPlaceholder|3"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestCtx(tc.lang)
			if got := translate(c, tc.message); got != tc.want {
				t.Fatalf("translate(%q) = %q, want %q", tc.message, got, tc.want)
			}
		})
	}
}

func TestRequestLanguageNormalize(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name  string
		query string
		lang  string
		want  string
	}{
		{"en 简码规范化为 en-US", "", "en", "en-US"},
		{"EN-us 大小写规范化", "", "EN-us", "en-US"},
		{"zh 简码规范化为 zh-CN", "", "zh", "zh-CN"},
		{"zh-Hans 规范化为 zh-CN", "", "zh-Hans", "zh-CN"},
		{"en-GB 归入 en-US", "", "en-GB", "en-US"},
		{"不支持的语言回退默认", "", "ja-JP", "zh-CN"},
		{"超长语言回退默认", "", strings.Repeat("a", 11), "zh-CN"},
		{"query 优先于头", "en-US", "zh-CN", "en-US"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestCtx(tc.lang)
			if tc.query != "" {
				q := c.Request.URL.Query()
				q.Set("lang", tc.query)
				c.Request.URL.RawQuery = q.Encode()
			}
			if got := requestLanguage(c); got != tc.want {
				t.Fatalf("requestLanguage = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSuccessMessageKeyed 验证通用 Success 消息走翻译（i18n-issues 2-3）。
func TestSuccessMessageKeyed(t *testing.T) {
	ensureTranslateFixture(t)

	engine := gin.New()
	engine.GET("/test", func(c *gin.Context) { Success(c, nil) })

	fetch := func(acceptLang string) string {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Accept-Language", acceptLang)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		var resp struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("解析响应失败：%v", err)
		}
		return resp.Message
	}

	if got := fetch("en-US"); got != "Operation successful" {
		t.Fatalf("英文 Success message = %q, want %q", got, "Operation successful")
	}
	if got := fetch("zh-CN"); got != "操作成功" {
		t.Fatalf("中文 Success message = %q, want %q", got, "操作成功")
	}
}

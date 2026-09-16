package response

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"
)

// translateSeeds 测试词条：与 TestTranslateForms 用例一一对应。
//
// 直接以 i18n 内存缓存的形态声明 —— 用例要验证的是 translate 的行为，
// 不需要中间那张表的往返（原先为此建真库，见 initTranslateFixture 的说明）。
var translateSeeds = map[string]map[string]string{
	"ErrAdminNotFound": {"zh-CN": "管理员不存在", "en-US": "Admin not found"},
	"ErrAccountLocked": {"zh-CN": "账号已被锁定，请 %s 后重试", "en-US": "Account locked, retry in %s"},
	"ErrInvalidParams": {"zh-CN": "请求参数错误", "en-US": "Invalid parameters"},
	// key|param 协议外占位符（%d）验证：不应注入，按 key 原文降级
	"ErrTestIntPlaceholder": {"zh-CN": "已失败 %d 次，请稍后再试", "en-US": "Failed %d times, retry later"},
	// 通用操作成功消息（Success 响应）
	"msg_operation_success": {"zh-CN": "操作成功", "en-US": "Operation successful"},
}

// translateHttpCodes 与 translateSeeds 同批 key 的 http_code（缓存里两者分开存）。
var translateHttpCodes = map[string]int{
	"ErrAdminNotFound":      404,
	"ErrAccountLocked":      423,
	"ErrInvalidParams":      400,
	"ErrTestIntPlaceholder": 429,
	"msg_operation_success": 200,
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

// initTranslateFixture 把测试词条直接注入 i18n 内存缓存。
//
// 这里以前是「建 MySQL 临时库 → AutoMigrate sys_i18n → 写种子 → i18n.Init 加载」。
// 项目早已移除 MySQL 驱动、CI 里也没有 3306 实例，所以那组用例实际只在旧环境跑得起来 ——
// CI 第一次真正执行 pkg 测试时立刻报 dial tcp 127.0.0.1:3306: connection refused。
// 用例要验证的是 response 的 translate 行为，缓存里有词条就够了，不必绕一层真库；
// 改完之后它不再依赖任何外部服务。
func initTranslateFixture(t *testing.T) error {
	t.Helper()

	// 只注入、不在 t.Cleanup 里清理：缓存是进程级的全局状态，而注入走 sync.Once
	// （只发生一次）。若在第一个用例结束时清空，后面的用例拿到的就是 key 原文 ——
	// 表现为「同一个 fixture 有的用例过、有的不过」。进程退出时缓存自然释放。
	i18n.SetDefaultLang("zh-CN")
	i18n.InjectForTest(translateSeeds, translateHttpCodes)
	return nil
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

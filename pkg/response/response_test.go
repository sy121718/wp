package response

import (
	json "go_wp/pkg/jsonx"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"
)

var translateSeeds = map[string]map[string]string{
	"ErrAdminNotFound": {"zh-CN": "管理员不存在", "en-US": "Admin not found"},
	"ErrAccountLocked": {"zh-CN": "账号已被锁定，请 %s 后重试", "en-US": "Account locked, retry in %s"},
	"ErrInvalidParams": {"zh-CN": "请求参数错误", "en-US": "Invalid parameters"},

	"ErrTestIntPlaceholder": {"zh-CN": "已失败 %d 次，请稍后再试", "en-US": "Failed %d times, retry later"},

	"msg_operation_success": {"zh-CN": "操作成功", "en-US": "Operation successful"},
}

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

func ensureTranslateFixture(t *testing.T) {
	t.Helper()

	translateOnce.Do(func() {
		translateInitErr = initTranslateFixture(t)
	})
	if translateInitErr != nil {
		t.Fatalf("初始化 translate 测试数据失败: %v", translateInitErr)
	}
}

func initTranslateFixture(t *testing.T) error {
	t.Helper()

	i18n.SetDefaultLang("zh-CN")
	i18n.InjectForTest(translateSeeds, translateHttpCodes)
	return nil
}

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

	c, _ := gin.CreateTestContext(nil)
	c.Request = newRequestWithHeader("Accept-Language", "en-US")
	if got := requestLanguage(c); got != "en-US" {
		t.Fatalf("query 为空时应取 Accept-Language，got %q", got)
	}

	c.Request = newRequestWithHeader("Accept-Language", "zh-CN;q=0.8, en-US;q=0.5")
	if got := requestLanguage(c); got != "zh-CN" {
		t.Fatalf("应取首段语言，got %q", got)
	}

	if got := requestLanguage(nil); got == "" {
		t.Fatal("nil 上下文应回退默认语言，不应为空")
	}
}

func TestTranslateForms(t *testing.T) {
	ensureTranslateFixture(t)

	cases := []struct {
		name    string
		lang    string
		message string
		want    string
	}{

		{"纯key命中-中文", "zh-CN", "ErrAdminNotFound", "管理员不存在"},
		{"纯key命中-英文", "en-US", "ErrAdminNotFound", "Admin not found"},

		{"未知key原样", "zh-CN", "ErrNotExistInDB", "ErrNotExistInDB"},

		{"key|param-中文", "zh-CN", "ErrAccountLocked|5m0s", "账号已被锁定，请 5m0s 后重试"},
		{"key|param-英文", "en-US", "ErrAccountLocked|5m0s", "Account locked, retry in 5m0s"},

		{"key:detail-中文", "zh-CN", "ErrInvalidParams: json: 字段缺失", "请求参数错误: json: 字段缺失"},

		{"中文直值原样", "zh-CN", "请求的资源不存在", "请求的资源不存在"},

		{"协议外占位符", "zh-CN", "ErrTestIntPlaceholder|3", "ErrTestIntPlaceholder|3"},

		{"内部错误key未命中给兜底", "en-US", InternalKey, InternalFallback},
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

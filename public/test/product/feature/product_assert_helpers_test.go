package feature

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/templates"
	json "go_wp/pkg/jsonx"
)

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func tailAround(s, needle string, span int) string {
	i := strings.Index(s, needle)
	if i < 0 {
		return "（未出现）"
	}
	lo, hi := i-span, i+span
	if lo < 0 {
		lo = 0
	}
	if hi > len(s) {
		hi = len(s)
	}
	return oneLine(s[lo:hi])
}

func jsonMap(t *testing.T, body []byte) map[string]any {
	t.Helper()
	out := map[string]any{}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("响应不是合法 JSON：%v（%s）", err, string(body))
	}
	return out
}

func asMap(t *testing.T, value any, what string) map[string]any {
	t.Helper()
	m, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s 应是对象，实际 %#v", what, value)
	}
	return m
}

func asList(t *testing.T, value any, what string) []any {
	t.Helper()
	list, ok := value.([]any)
	if !ok {
		t.Fatalf("%s 应是数组，实际 %#v", what, value)
	}
	return list
}

// mountStaticAssets 按生产正式装配把 /static 挂到唯一公共资产源上。
//
// 复合 FS 同时含 pkg 公共控件资产与应用专用脚本：只挂磁盘 internal/templates/static
// 会丢掉 pkg 层（write-base、ui/index 等），真实浏览器里就会出现 WBUI 未定义。
// 每个测试引擎只调用一次；package 内共享同一路由夹具时不要重复挂载。
func mountStaticAssets(router *gin.Engine) {
	staticFS, err := templates.EmbeddedStaticFS()
	if err != nil {
		panic("静态资源装配失败: " + err.Error())
	}
	router.Group("/static", builtin.StaticGzipMiddleware(), builtin.StaticCacheMiddleware()).StaticFS("/", staticFS)
}

// assertStaticAssetsServed 直接经真实 HTTP 校验静态资产出口能力面。
//
// 已知脚本必须 200 且 Content-Type 与扩展名一致；未知资源仍 404。
func assertStaticAssetsServed(t *testing.T, server *httptest.Server) {
	t.Helper()
	// 本地 httptest 也要有上限：客户端超时覆盖取完响应体的全过程，避免挂住整个验收。
	client := &http.Client{Timeout: 5 * time.Second}
	get := func(path string) *http.Response {
		t.Helper()
		res, err := client.Get(server.URL + path)
		if err != nil {
			t.Fatalf("静态资产请求失败 %s：%v", path, err)
		}
		_ = res.Body.Close()
		return res
	}
	for _, tc := range []struct {
		path        string
		contentType string
	}{
		{"/static/js/ui/write-base.js", "text/javascript"},
		{"/static/js/ui/index.js", "text/javascript"},
		{"/static/js/product-create-form.js", "text/javascript"},
		{"/static/css/ui.css", "text/css"},
		{"/static/vendor/trix/trix.umd.js", "text/javascript"},
	} {
		res := get(tc.path)
		if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), tc.contentType) {
			t.Fatalf("静态资产 %s 应 200 且 Content-Type=%s，实际 %d %q", tc.path, tc.contentType, res.StatusCode, res.Header.Get("Content-Type"))
		}
	}
	if res := get("/static/js/ui/not-exists.js"); res.StatusCode != http.StatusNotFound {
		t.Fatalf("未知静态资产必须 404，实际 %d", res.StatusCode)
	}
}

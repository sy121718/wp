package adminhttp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"go_wp/internal/adminui"
	"go_wp/pkg/safepath"

	"github.com/gin-gonic/gin"
)

func adminLangRedirectCtx(t *testing.T, uri string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	u := &url.URL{Path: uri}
	if i := strings.IndexByte(uri, '?'); i >= 0 {
		u = &url.URL{Path: uri[:i], RawQuery: uri[i+1:]}
	}
	c.Request = &http.Request{Method: http.MethodGet, URL: u, RequestURI: uri}
	if got := u.RequestURI(); got != uri {
		t.Fatalf("测试构造的 URL 不能还原目标：got %q want %q", got, uri)
	}
	return c
}

func TestAdminLangRedirectMatchesPrepareCriterion(t *testing.T) {
	cases := []struct {
		uri      string
		want     bool
		fallback string
	}{
		{"/", true, ""},
		{"/admin/roles?page=2&keyword=x", true, ""},
		{"/admin/products?project=" + strings.Repeat("x", 300), true, ""},
		{"/admin/products?project=" + strings.Repeat("x", 520), false, "/admin/products"},
		{"/admin/" + strings.Repeat("y", 600), false, "/admin"},
		{"//evil.example.com/x", false, "/admin"},
		{"https://evil.example.com/x", false, "/admin"},
		{"admin/system/roles", false, "/admin"},
	}
	for _, tc := range cases {
		c := adminLangRedirectCtx(t, tc.uri)
		rendered := adminui.PageFacts(c).LangRedirect
		if rendered == "" {
			t.Fatalf("lang_redirect 必须注入非空字符串，实际 %q", rendered)
		}
		if !tc.want && rendered != tc.fallback {
			t.Fatalf("被拒绝的 URI %q 应在隐藏域回落到 %q，实际 %q", tc.uri, tc.fallback, rendered)
		}
		prepareAccepts := rendered == tc.uri
		if prepareAccepts != tc.want {
			t.Fatalf("渲染侧判据与预期不符：%q Prepare=%v want=%v（这条测试的取材已经不成立了）", tc.uri, prepareAccepts, tc.want)
		}

		if got := safepath.LangRedirectPath(tc.uri) != ""; got != tc.want {
			t.Errorf("导出判据与渲染侧不一致：%q LangRedirectPath=%v Prepare=%v", tc.uri, got, prepareAccepts)
		}

		want := "/admin"
		if tc.want {
			want = tc.uri
		}
		if got := adminSafeLangRedirect(tc.uri); got != want {
			t.Errorf("消费侧回跳落点不对：%q admin=%q want=%q", tc.uri, got, want)
		}
	}
}

func TestAdminLangRedirectRejectsDecodedJunk(t *testing.T) {
	for _, raw := range []string{
		"/admin/roles\\x",
		"/admin/roles" + "\n" + "Set-Cookie: a=b",
		"/admin/roles\x00",
		"/admin/roles\x1b[31m",
		"/admin/roles\x7f",
	} {
		if safepath.LangRedirectPath(raw) != "" {
			t.Errorf("解码后才出现的字符同样要拒：%q", raw)
		}
		if got := adminSafeLangRedirect(raw); got != "/admin" {
			t.Errorf("拒绝时应回控制面首页，实际 %q（raw=%q）", got, raw)
		}
	}
	if got := adminSafeLangRedirect("  /admin/roles?page=2  "); got != "/admin/roles?page=2" {
		t.Errorf("首尾空白先 trim 再判，实际 %q", got)
	}
}

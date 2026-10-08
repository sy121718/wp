package adminhttp

// admin_notice_test.go — 语言回跳判据的同源回归。
//
// 说明：本文件原名下还钉着列表页 ?done= / ?saved= 两条**读侧**受控出口（词条页「已保存：」、
// 7 个列表页的「已删除 N 个角色」提示条）。写动作的结论改成由 shell.RenderJump 渲染整页提示
// 之后（见 admin_jump.go），查询参数不再喂给页面，那两条读侧判定（adminPageDone /
// adminPageSaved / adminDoneTexts）已整批删除，对应用例随之退役 —— 它们的覆盖目标
//（「手拼 URL 不能伪造系统提示」）改由 feature 层的「?err= 不再渲染任何内容」用例承担。
//
// 留下的这一条与传输通道无关：语言切换的回跳地址判据。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"go_wp/internal/shell"

	"github.com/gin-gonic/gin"
)

// adminLangRedirectCtx 组一个「当前请求 URI == uri」的上下文。
//
// 不用 httptest.NewRequest：带换行 / 反斜杠的取值会让请求行本身非法（构造不出来），
// 而这里要的正是「shell 与 admin 对同一个字符串各自怎么判」。构造后用 RequestURI() 自检，
// 还原不出来就直接失败，免得测试在错误的输入上给出「通过」。
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

// TestAdminLangRedirectMatchesShellCriterion 语言回跳的两处判据必须逐项一致。
//
// 渲染侧（shell.LangRedirect，把值写进隐藏域）与消费侧（adminSafeLangRedirect，
// 校验 query 里带回来的值）是同一个协议的两端：一端放宽就多一个开放重定向面，
// 一端收紧就让运营「切了语言回不到原页面」。
//
// 判据已归口到 shell.LangRedirectPath（导出入口，按字符串判定），消费侧不再重抄 ——
// 所以这条用例退化成「同源确认」：同一批取值喂给渲染侧（经 RequestURI）与导出判据
// （按字符串），取舍必须逐项一致，且消费侧的回跳落点与之一致。断言一条都没删：
// 取材不成立（渲染侧判据变了）时仍然直接 Fatal。
func TestAdminLangRedirectMatchesShellCriterion(t *testing.T) {
	cases := []struct {
		uri  string
		want bool
	}{
		{"/", true},
		{"/admin/roles?page=2&keyword=x", true},
		{"/admin/products?project=" + strings.Repeat("x", 300), true},
		{"/admin/products?project=" + strings.Repeat("x", 520), false}, // 超过 512 字节
		{"/admin/" + strings.Repeat("y", 600), false},
		{"//evil.example.com/x", false},
		{"https://evil.example.com/x", false},
		{"admin/system/roles", false},
	}
	for _, tc := range cases {
		c := adminLangRedirectCtx(t, tc.uri)
		shellAccepts := shell.LangRedirect(c) == tc.uri
		if shellAccepts != tc.want {
			t.Fatalf("渲染侧判据与预期不符：%q shell=%v want=%v（这条测试的取材已经不成立了）", tc.uri, shellAccepts, tc.want)
		}
		// 判据只有一份：导出入口按字符串判定，渲染侧与消费侧都调它。
		if got := shell.LangRedirectPath(tc.uri) != ""; got != tc.want {
			t.Errorf("导出判据与渲染侧不一致：%q LangRedirectPath=%v shell=%v", tc.uri, got, shellAccepts)
		}
		// 消费侧语义：合法原样回跳、不合法回**控制面首页** —— 两条都要在。
		//
		// 回退落点是 /admin 而不是 "/"：站点独占域名根之后 "/" 是前台首页，
		// 后台的一次输入不合法把用户甩到前台站点上，是比原地不动更糟的结果。
		want := "/admin"
		if tc.want {
			want = tc.uri
		}
		if got := adminSafeLangRedirect(tc.uri); got != want {
			t.Errorf("消费侧回跳落点不对：%q admin=%q want=%q", tc.uri, got, want)
		}
	}
}

// TestAdminLangRedirectRejectsDecodedJunk 消费侧比渲染侧多一类输入：解码后的查询参数。
//
// 渲染侧拿到的是 RequestURI（已转义，天然不含反斜杠与控制字符），消费侧拿到的是 gin
// 解码后的值 —— `\`、换行、NUL 都能出现。这些一律拒（回首页），否则就是一个可以往
// Location 头里塞控制字符的入口。
func TestAdminLangRedirectRejectsDecodedJunk(t *testing.T) {
	for _, raw := range []string{
		"/admin/roles\\x",
		"/admin/roles" + "\n" + "Set-Cookie: a=b",
		"/admin/roles\x00",
		"/admin/roles\x1b[31m",
		"/admin/roles\x7f",
	} {
		if shell.LangRedirectPath(raw) != "" {
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

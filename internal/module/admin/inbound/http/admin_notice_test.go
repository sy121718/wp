package adminhttp

// admin_notice_test.go — admin 列表页 ?done= / ?saved= 两条回执通道的受控出口单测。
//
// 为什么单独钉这一层：这两条此前都是 `strings.TrimSpace(c.Query("done"))` / `("saved")`
// 原样进模板（7 个列表页的「已删除 N 个角色」提示条 + 词条页「已保存：<key> · <lang>」）。
// 值现在都由服务端构造，但**页面不是可信边界** —— ?done=任意文案 谁都能手写，
// 渲染出来就是一条顶着「成功」样式的伪造消息。
//
// 断言的是「写侧放进去的每一种形态都能被认出来」+「不是写侧放的形态一律不认」：
// 前者漏了 = 运营看到「删完了却没有回执」，后者漏了 = 这条通道等于没管。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"go_wp/internal/web/shell"

	"github.com/gin-gonic/gin"
)

// adminDoneValues 写侧会放进 ?done= 的全部取值（从写侧的构造函数里取，不手抄文案）。
func adminDoneValues(t *testing.T, c *gin.Context) []string {
	t.Helper()
	values := make([]string, 0, len(adminBulkNouns)+2)
	for _, noun := range adminBulkNouns {
		for _, deleted := range []int{1, 3, 12} {
			loc := adminBulkResultURL(c, "/admin/x", noun, deleted, 0)
			parsed, err := url.Parse(loc)
			if err != nil {
				t.Fatalf("写侧回跳地址无法解析：%v", err)
			}
			if got := parsed.Query().Get("done"); got == "" {
				t.Fatalf("全成功的批量删除应当走 ?done=（noun=%q deleted=%d）：%s", noun.key, deleted, loc)
			} else {
				values = append(values, got)
			}
		}
	}
	// 词条页的两条 ?done= 分支（skipped == 0 的两支）。
	values = append(values, adminI18nBulkDeleteResult(c, 0, 0), adminI18nBulkDeleteResult(c, 3, 0))
	return values
}

// TestAdminPageDoneAcceptsEveryWriterShape 写侧每一种取值都必须被读侧整体认出来。
func TestAdminPageDoneAcceptsEveryWriterShape(t *testing.T) {
	c := newPageErrContext(t)
	for _, raw := range adminDoneValues(t, c) {
		if got := adminPageDone(c, raw); got != raw {
			t.Errorf("写侧文案应被受控出口原样放行，实际 %q（raw=%q）—— 这条提示会在页面上消失", got, raw)
		}
	}
}

// TestAdminPageDoneRejectsForged 不是写侧放的取值一律落空串（不落归口文案：
// 成功位置上顶一条错误提示比什么都不显示更糟）。
func TestAdminPageDoneRejectsForged(t *testing.T) {
	c := newPageErrContext(t)
	rejected := []string{
		"",
		"   ",
		"系统维护中，请稍后重试",
		"已删除 3 个角色，2 个未能删除（受保护或被引用）", // 这条走 ?err= 通道，不是 ?done=
		"已删除 3 个用户",  // 名词不在登记表里（写了但没登记）
		"已删除 3 个角色。", // 多一个句号就不是同一句话
		strings.Repeat("已删除 3 个角色", 200),
	}
	for _, raw := range rejected {
		if got := adminPageDone(c, raw); got != "" {
			t.Errorf("未命中应返回空串，实际 %q（raw=%q）", got, raw)
		}
	}
}

// TestAdminPageDoneRequiresWholeMatch 必须整体相等，不许用 Contains 绕过白名单。
//
// 形态 3（受控文案 + 「：」+ 定位信息）是 shell.FacingNotice 的既有判据 —— 服务端会在
// 业务文案后补一句定位信息，所以「已删除 0 个角色」后面跟「：」是合法的；
// 但**前缀 / 后缀夹带**必须不命中，否则手拼 URL 就能变成「夹一段已知文案 + 任意内容」。
func TestAdminPageDoneRequiresWholeMatch(t *testing.T) {
	c := newPageErrContext(t)
	base := adminBulkResultURL(c, "/admin/roles", adminBulkNounRole, 3, 0)
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatalf("解析回跳地址失败：%v", err)
	}
	msg := parsed.Query().Get("done")
	if msg == "" {
		t.Fatal("测试素材缺失：写侧应当产出 ?done=")
	}
	for _, raw := range []string{
		"<script>alert(1)</script>" + msg,
		msg + "<script>alert(1)</script>",
		"伪造前缀 " + msg,
	} {
		if got := adminPageDone(c, raw); got != "" {
			t.Errorf("夹带内容不该命中（必须整体相等），实际 %q", got)
		}
	}
	if got := adminPageDone(c, msg+"：外部编码 xyz"); got != msg+"：外部编码 xyz" {
		t.Errorf("「受控文案 + ：+ 定位信息」是 shell 的形态 3，应当放行，实际 %q", got)
	}
}

// TestAdminPageDoneIsDigitInsensitive 计数可以变，措辞不能变。
func TestAdminPageDoneIsDigitInsensitive(t *testing.T) {
	c := newPageErrContext(t)
	if got := adminPageDone(c, "已删除 999 个数据规则"); got == "" {
		t.Error("计数不同不该让整句话失配（归一后应与模板相等）")
	}
}

// TestAdminPageSavedShapes ?saved= 只放行写侧构造的词条身份形状。
func TestAdminPageSavedShapes(t *testing.T) {
	t.Run("写侧身份串原样放行", func(t *testing.T) {
		for _, tc := range [][2]string{{"site.name", "zh-CN"}, {"admin.i18n.title", "en-US"}, {"a_b-c.d", "en"}} {
			want := adminI18nEntryIdentity(tc[0], tc[1])
			if want == "" {
				t.Fatalf("测试素材缺失：%q · %q 应当是合法身份串", tc[0], tc[1])
			}
			if got := adminPageSaved(want); got != want {
				t.Errorf("保存回执应原样放行，实际 %q（want %q）", got, want)
			}
			deleted := adminI18nDeletedIdentity(tc[0], tc[1])
			if deleted == "" || adminPageSaved(deleted) != deleted {
				t.Errorf("删除回执应原样放行，实际 %q（want %q）", adminPageSaved(deleted), deleted)
			}
		}
	})

	t.Run("形状不合的取值一律落空串", func(t *testing.T) {
		for _, raw := range []string{
			"",
			"   ",
			"已保存成功",
			"site.name",                 // 缺语言段
			"zh-CN",                     // 缺 key 段
			"site.name · zh-CN · 附加",    // 多一段
			"site.name · 中文",            // 语言标签不是 ASCII
			"邮件.自动 · zh-CN",             // key 里出现非 ASCII
			"site name · zh-CN",         // key 里有空格
			"site.name · zh-CN<script>", // 夹带
			"<script>" + adminI18nEntryIdentity("site.name", "zh-CN"),
			strings.Repeat("a", adminI18nKeyMaxLen+1) + " · zh-CN",
		} {
			if got := adminPageSaved(raw); got != "" {
				t.Errorf("未命中应返回空串，实际 %q（raw=%q）", got, raw)
			}
		}
	})

	t.Run("形状不合的输入写侧也不构造身份（页面宁可不显示）", func(t *testing.T) {
		for _, tc := range [][2]string{{"邮件.自动", "zh-CN"}, {"site.name", "中文"}, {"", "zh-CN"}, {"site.name", ""}} {
			if got := adminI18nEntryIdentity(tc[0], tc[1]); got != "" {
				t.Errorf("不合形状的输入应当返回空串（写侧只好不带身份回跳），实际 %q", got)
			}
		}
	})
}

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
// 判据本批已归口到 shell.LangRedirectPath（导出入口，按字符串判定），消费侧不再重抄 ——
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
		{"admin/roles", false},
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
		// 消费侧语义：合法原样回跳、不合法回首页 —— 两条都要在。
		want := "/"
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
		if got := adminSafeLangRedirect(raw); got != "/" {
			t.Errorf("拒绝时应回首页，实际 %q（raw=%q）", got, raw)
		}
	}
	if got := adminSafeLangRedirect("  /admin/roles?page=2  "); got != "/admin/roles?page=2" {
		t.Errorf("首尾空白先 trim 再判，实际 %q", got)
	}
}

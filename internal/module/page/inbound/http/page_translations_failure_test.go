package pagehttp

// page_translations_failure_test.go — 翻译工作台整页 GET 的失败出口回归（task-122，批次 B）。
//
// 被守的边界（docs/rules/template-boundary.md 的「整页 GET 的失败出口」）：
//  1. 每一种结论都是**同一页**的完整 HTML —— 整页 GET 由浏览器导航发起，回 JSON / 裸文本 /
//     半截渲染，用户拿到的都是一个坏掉的页面：没有标题栏与菜单，地址栏还停在原处；
//  2. 状态码是结论的一部分：参数缺失 400、资源不存在 404、取数失败 500。
//     200 会让失败看起来像成功（监控也看不见），把三种结论压成一种则让人不知道下一步该干什么；
//  3. 失败页 no-store：否则「重试」只是把同一份失败从缓存里再拿回来；
//  4. 内部原文只进日志：service 一旦把 PostgreSQL 原文（表名 / SQLSTATE）上抛，
//     它会出现在运营眼前的页面上。
//
// 为什么参数缺失档与取数失败档**分别**有用例：前者是「改地址就能好」，后者是「稍后重试」——
// 合成一条断言，档位写错（400 当 500、500 当 200）也照样绿。
//
// 为什么不连库：本文件断言的是**出口形态**（状态码 / 头 / 整页壳 / 槽位内容），
// 取数失败要的是「service 报了一个非业务错误」这一事实，真库给不出来；404 与 500 由同一个
// 字段开关，装配只写一份。真实数据路径由 public/test/page/feature 的整页用例覆盖。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"

	pagecontract "go_wp/internal/module/page/contract"
	pageenums "go_wp/internal/module/page/enums"
	pageservice "go_wp/internal/module/page/service"
	"go_wp/internal/templates"
	"go_wp/pkg/auth"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// translationsFailurePages 只覆盖失败路径用到的那个方法（其余走嵌入的 nil 接口）。
//
// 走桩而不是真库：`ProjectOfPage` 是 `buildPageTranslationsData` 的第一跳（页面 → 工程 scope），
// 让它报错就等于「主数据没读到」，而不用把数据库拆掉。
type translationsFailurePages struct {
	pagecontract.PageService
	projectErr error
}

func (s translationsFailurePages) ProjectOfPage(_ context.Context, _ string) (string, error) {
	if s.projectErr != nil {
		return "", s.projectErr
	}
	return "project-1", nil
}

// translationsFailureRawErr 模拟被上抛的 PostgreSQL 原文（用例用它反证「原文只进日志」）。
var translationsFailureRawErr = errors.New(`pq: relation "pages" does not exist (SQLSTATE 42P01)`)

// newTranslationsFailureRouter 装配与线上同一条渲染路径的测试路由（页面壳来自 adminui.Prepare，
// 所以断言里的取词口径就是请求语言）。
func newTranslationsFailureRouter(t *testing.T, pages pagecontract.PageService) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	conf := viper.New()
	conf.Set("log.base_dir", t.TempDir())
	if err := logger.Init(conf); err != nil {
		t.Fatal(err)
	}
	if err := auth.Init(nil); err != nil {
		t.Fatal(err)
	}
	store := cookie.NewStore([]byte("page-translations-failure-test-secret-32b"))
	router.Use(sessions.Sessions("page-translations-test", store))
	router.Use(func(c *gin.Context) {
		s := sessions.Default(c)
		s.Set("csrf_token", "test-csrf-token")
		if err := s.Save(); err != nil {
			t.Fatal(err)
		}
		// 键名是 internal/middleware/adminctx 的私有约定（perm_set / button_set），该包不导出键
		// 常量，故此处按既有先例（public/test/admin/feature）写字面量：模板读的是按钮码，
		// 权限码集合只留给 Go 侧判断。
		c.Set("perm_set", map[string]bool{"page.translation": true})
		c.Set("button_set", map[string]bool{})
		c.Next()
	})
	router.GET("/admin/pages/translations", NewPagesAdminHandle(pages, nil, nil, nil).PageTranslations)
	return router
}

// translationsFailureBody 打一次 GET 并返回响应。
func translationsFailureBody(t *testing.T, router *gin.Engine, query string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/pages/translations"+query, nil))
	return rec
}

// assertTranslationsFailurePage 失败页的公共形态：
// 整页壳（含 </body></html>，挡住「渲染中断、尾部截断」）+ no-store + 不是 JSON + 无原文。
func assertTranslationsFailurePage(t *testing.T, rec *httptest.ResponseRecorder, status int, want, forbidden []string) string {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("状态码应为 %d（用户能不能修、怎么修取决于它），实际 %d：%s", status, rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("失败页必须是页面（Content-Type 含 text/html），实际 %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Fatalf("失败页不该被缓存（Cache-Control 含 no-store），实际 %q", cc)
	}
	body := rec.Body.String()
	for _, want := range []string{"<!DOCTYPE", `class="admin-layout"`, "</body>", "</html>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("失败页不是完整的一页（缺 %q）：%s", want, body)
		}
	}
	if strings.Contains(body, `{"code"`) {
		t.Fatalf("失败页回了 JSON 报文（浏览器里是一段文本，不是这一页）：%s", body)
	}
	for _, want := range want {
		if !strings.Contains(body, want) {
			t.Fatalf("失败页缺少 %q：%s", want, body)
		}
	}
	for _, bad := range append([]string{"SQLSTATE", "42P01", "pq: relation"}, forbidden...) {
		if strings.Contains(body, bad) {
			t.Fatalf("内部原文漏到页面上（命中 %q）：%s", bad, body)
		}
	}
	return body
}

// TestPageTranslationsMissingPageIDRenders400WithParamSlot 参数缺失档：400 + 参数错误槽 + 回列表入口。
func TestPageTranslationsMissingPageIDRenders400WithParamSlot(t *testing.T) {
	// 服务传 nil：参数缺失在取数**之前**就判掉（这本身就是要守的顺序 —— 否则「没带参数」
	// 会被渲染成一次「服务端故障」）。
	router := newTranslationsFailureRouter(t, nil)
	rec := translationsFailureBody(t, router, "")

	// 期望文案用与产出同一条取词路径算，不手抄：手抄的串在有人改词条时会以「谁都没写错」的方式变红。
	paramText := i18n.Translate(pageenums.ErrInvalidParam, pagesLocalNoticeMissingPageIDGet.Fallback, "zh-CN")
	body := assertTranslationsFailurePage(t, rec, http.StatusBadRequest, []string{
		`<p class="empty-title">` + paramText + `</p>`,
		`href="/admin/pages"`,
	}, nil)

	// 参数档不该摆出「取数失败」的那句话（两档文案混用 = 让人去重试一个改地址就能好的问题）。
	loadFailedTitle := i18n.Translate("admin.common.list.loadFailedTitle", "这一页的数据没能读出来", "zh-CN")
	if strings.Contains(body, loadFailedTitle) {
		t.Fatalf("参数缺失档误用了取数失败的文案 %q：%s", loadFailedTitle, body)
	}
}

// TestPageTranslationsLoadFailureRenders500WithFailureSlot 取数失败档：500 + 失败槽 + no-store。
func TestPageTranslationsLoadFailureRenders500WithFailureSlot(t *testing.T) {
	router := newTranslationsFailureRouter(t, translationsFailurePages{projectErr: translationsFailureRawErr})
	rec := translationsFailureBody(t, router, "?pageId=page-1&lang=en-US")

	loadFailedTitle := i18n.Translate("admin.common.list.loadFailedTitle", "这一页的数据没能读出来", "zh-CN")
	loadFailedDesc := i18n.Translate("admin.common.list.loadFailedDesc", "页顶的提示说明了原因；稍后重试即可，也可以先切到别的菜单。", "zh-CN")
	body := assertTranslationsFailurePage(t, rec, http.StatusInternalServerError, []string{
		`<p class="badge badge-danger" role="alert">`,
		`<p class="empty-title">` + loadFailedTitle + `</p>`,
		`<p class="empty-desc">` + loadFailedDesc + `</p>`,
	}, nil)

	// 失败优先于空态：没读到与「读到了、只是没有可翻译的行」是两回事，混在一起运营会以为
	// 文本被删了，然后去重建一遍（契约的「失败槽：失败优先于空态」）。
	emptyTitle := i18n.Translate("admin.page_translations.empty.title", "没有可翻译的行", "zh-CN")
	if strings.Contains(body, emptyTitle) {
		t.Fatalf("取数失败时仍渲染了空态文案 %q（失败被显示成「没有数据」）：%s", emptyTitle, body)
	}
	// 归口文字槽非空：只给标题不说结论，用户不知道是自己点错还是服务端坏了。
	if !strings.Contains(body, `<p class="badge badge-danger" role="alert">`) {
		t.Fatalf("失败槽缺归口文字位：%s", body)
	}
}

// TestPageTranslationsMissingPageRenders404 NotFound 档：资源不存在是 404，不是 500。
//
// 这一档与取数失败共用同一个槽（同一句「这里没有你要的页面」），差别只在状态码与文案 ——
// 判据复用模块既有的错误 → 状态码映射（pageErrorStatus），不另建一套分类。
func TestPageTranslationsMissingPageRenders404(t *testing.T) {
	router := newTranslationsFailureRouter(t, translationsFailurePages{projectErr: pageservice.ErrPageNotFound})
	rec := translationsFailureBody(t, router, "?pageId=page-missing&lang=en-US")

	assertTranslationsFailurePage(t, rec, http.StatusNotFound, []string{
		`<p class="badge badge-danger" role="alert">`,
	}, nil)
}

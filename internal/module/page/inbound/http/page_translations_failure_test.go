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

// assertTranslationsFailureJSON 失败响应的公共形态：JSON 错误体 + 真实状态码 + 无原文。
//
// **失败不渲染页壳**（用户裁定：所有提示只需要一个提示）：从前这里渲染「同一页 + 错误槽」,
// 同一张模板要维护两套必需键，而失败时页壳给的信息（筛选项、语言切换、整张表）
// 没有一条是这次失败需要的。状态码仍然按错误性质分档（400 / 404 / 500）——
// 「改地址就能好」和「稍后重试」不是同一件事，这个区分现在只剩状态码一处。
func assertTranslationsFailureJSON(t *testing.T, rec *httptest.ResponseRecorder, status int, want, forbidden []string) string {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("状态码应为 %d（用户能不能修、怎么修取决于它），实际 %d：%s", status, rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("失败出口应为 JSON（Content-Type 含 application/json），实际 %q", ct)
	}
	body := rec.Body.String()
	// 页壳缺席：这是「失败不渲染整页」的分水岭。
	for _, banned := range []string{"<!DOCTYPE", `class="admin-layout"`, "</body>", "</html>"} {
		if strings.Contains(body, banned) {
			t.Fatalf("失败出口不该渲染页壳（出现 %q）：%s", banned, body)
		}
	}
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Fatalf("失败响应缺少 %q：%s", w, body)
		}
	}
	for _, bad := range append([]string{"SQLSTATE", "42P01", "pq: relation"}, forbidden...) {
		if strings.Contains(body, bad) {
			t.Fatalf("内部原文漏到响应里（命中 %q）：%s", bad, body)
		}
	}
	return body
}

// TestPageTranslationsMissingPageIDReturns400WithParamText 参数缺失档：400 + 参数文案。
func TestPageTranslationsMissingPageIDReturns400WithParamText(t *testing.T) {
	// 服务传 nil：参数缺失在取数**之前**就判掉（这本身就是要守的顺序 —— 否则「没带参数」
	// 会被报成一次「服务端故障」）。
	router := newTranslationsFailureRouter(t, nil)
	rec := translationsFailureBody(t, router, "")

	// 期望文案用与产出同一条取词路径算，不手抄：手抄的串在有人改词条时会以「谁都没写错」的方式变红。
	paramText := i18n.Translate(pageenums.ErrInvalidParam, pagesLocalNoticeMissingPageIDGet.Fallback, "zh-CN")
	body := assertTranslationsFailureJSON(t, rec, http.StatusBadRequest, []string{paramText}, nil)

	// 参数档不该摆出「取数失败」的那句话（两档文案混用 = 让人去重试一个改地址就能好的问题）。
	loadFailedTitle := i18n.Translate("admin.common.list.loadFailedTitle", "这一页的数据没能读出来", "zh-CN")
	if strings.Contains(body, loadFailedTitle) {
		t.Fatalf("参数缺失档误用了取数失败的文案 %q：%s", loadFailedTitle, body)
	}
}

// TestPageTranslationsLoadFailureReturns500 取数失败档：500 + 归口文案。
func TestPageTranslationsLoadFailureReturns500(t *testing.T) {
	router := newTranslationsFailureRouter(t, translationsFailurePages{projectErr: translationsFailureRawErr})
	rec := translationsFailureBody(t, router, "?pageId=page-1&lang=en-US")

	body := assertTranslationsFailureJSON(t, rec, http.StatusInternalServerError, []string{
		"系统内部错误，请稍后重试",
	}, nil)
	// 失败优先于空态：没读到与「读到了、只是没有可翻译的行」是两回事，混在一起运营会以为
	// 文本被删了，然后去重建一遍。
	emptyTitle := i18n.Translate("admin.page_translations.empty.title", "没有可翻译的行", "zh-CN")
	if strings.Contains(body, emptyTitle) {
		t.Fatalf("取数失败时仍出现空态文案 %q（失败被显示成「没有数据」）：%s", emptyTitle, body)
	}
}

// TestPageTranslationsMissingPageReturns404 NotFound 档：资源不存在是 404，不是 500。
//
// 这一档与取数失败共用同一句文案，差别只在状态码 —— 判据复用模块既有的
// 错误 → 状态码映射（pageErrorStatus），不另建一套分类。
func TestPageTranslationsMissingPageReturns404(t *testing.T) {
	router := newTranslationsFailureRouter(t, translationsFailurePages{projectErr: pageservice.ErrPageNotFound})
	rec := translationsFailureBody(t, router, "?pageId=page-missing&lang=en-US")

	assertTranslationsFailureJSON(t, rec, http.StatusNotFound, nil, nil)
}


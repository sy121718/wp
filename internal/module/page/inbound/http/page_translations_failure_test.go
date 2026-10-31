package pagehttp

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

var translationsFailureRawErr = errors.New(`pq: relation "pages" does not exist (SQLSTATE 42P01)`)

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

		c.Set("perm_set", map[string]bool{"page.translation": true})
		c.Set("button_set", map[string]bool{})
		c.Next()
	})
	router.GET("/admin/pages/translations", NewPagesAdminHandle(pages, nil, nil, nil).PageTranslations)
	return router
}

func translationsFailureBody(t *testing.T, router *gin.Engine, query string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/pages/translations"+query, nil))
	return rec
}

func assertTranslationsFailureJSON(t *testing.T, rec *httptest.ResponseRecorder, status int, want, forbidden []string) string {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("状态码应为 %d（用户能不能修、怎么修取决于它），实际 %d：%s", status, rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("失败出口应为 JSON（Content-Type 含 application/json），实际 %q", ct)
	}
	body := rec.Body.String()

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

func TestPageTranslationsMissingPageIDReturns400WithParamText(t *testing.T) {

	router := newTranslationsFailureRouter(t, nil)
	rec := translationsFailureBody(t, router, "")

	paramText := i18n.Translate(pageenums.ErrInvalidParam, pagesLocalNoticeMissingPageIDGet.Fallback, "zh-CN")
	body := assertTranslationsFailureJSON(t, rec, http.StatusBadRequest, []string{paramText}, nil)

	loadFailedTitle := i18n.Translate("admin.common.list.loadFailedTitle", "这一页的数据没能读出来", "zh-CN")
	if strings.Contains(body, loadFailedTitle) {
		t.Fatalf("参数缺失档误用了取数失败的文案 %q：%s", loadFailedTitle, body)
	}
}

func TestPageTranslationsLoadFailureReturns500(t *testing.T) {
	router := newTranslationsFailureRouter(t, translationsFailurePages{projectErr: translationsFailureRawErr})
	rec := translationsFailureBody(t, router, "?pageId=page-1&lang=en-US")

	body := assertTranslationsFailureJSON(t, rec, http.StatusInternalServerError, []string{
		"系统内部错误，请稍后重试",
	}, nil)

	emptyTitle := i18n.Translate("admin.page_translations.empty.title", "没有可翻译的行", "zh-CN")
	if strings.Contains(body, emptyTitle) {
		t.Fatalf("取数失败时仍出现空态文案 %q（失败被显示成「没有数据」）：%s", emptyTitle, body)
	}
}

func TestPageTranslationsMissingPageReturns404(t *testing.T) {
	router := newTranslationsFailureRouter(t, translationsFailurePages{projectErr: pageservice.ErrPageNotFound})
	rec := translationsFailureBody(t, router, "?pageId=page-missing&lang=en-US")

	assertTranslationsFailureJSON(t, rec, http.StatusNotFound, nil, nil)
}

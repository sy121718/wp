package feature

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	contenthttp "go_wp/internal/module/content/inbound/http"
)

func TestArticleTranslationsFilterSearchesBeforePagination(t *testing.T) {
	engine, db, svc := newArticlePagingEnv(t)
	if engine == nil {
		return
	}
	engine.GET("/admin/articles/translations", contenthttp.NewArticleTranslationHandle(svc).ArticleTranslations)
	seedArticles(t, db, 65)

	request := func(query string) string {
		t.Helper()
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/articles/translations?lang=en-US&"+query, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("译文页状态 %d: %s", rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}

	last := request("keyword=" + url.QueryEscape("文章 0064"))
	if !strings.Contains(last, "文章 0064") || strings.Contains(last, "文章 0000") || !strings.Contains(last, `value="文章 0064"`) {
		t.Fatal("旧前 50 篇以外的匹配应由数据库筛出并回显")
	}
	if strings.Contains(last, `class="pagination"`) {
		t.Fatal("单条匹配不应显示分页")
	}
	second := request("limit=20&page=2")
	if !strings.Contains(second, "文章 0020") || strings.Contains(second, "文章 0000") || !strings.Contains(second, `name="page" value="2"`) {
		t.Fatal("第二页应展示文章 0020 起的数据，并在保存表单保留页码")
	}
	none := request("keyword=" + url.QueryEscape("不存在的标题"))
	if !strings.Contains(none, "没有匹配的文章") || !strings.Contains(none, "清除筛选") || strings.Contains(none, "没有可翻译的文章内容") {
		t.Fatal("筛选无匹配应与无文章数据分档")
	}
}

func TestArticleTranslationsFilterTreatsLikeWildcardsLiterally(t *testing.T) {
	engine, db, svc := newArticlePagingEnv(t)
	if engine == nil {
		return
	}
	engine.GET("/admin/articles/translations", contenthttp.NewArticleTranslationHandle(svc).ArticleTranslations)
	seedArticles(t, db, 2)
	for _, keyword := range []string{"%", "_"} {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/articles/translations?lang=en-US&keyword="+url.QueryEscape(keyword), nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "没有匹配的文章") {
			t.Fatalf("通配符 %q 应按字面量匹配，响应 %d", keyword, rec.Code)
		}
	}
}

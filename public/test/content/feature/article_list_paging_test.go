package feature

import (
	pageview "go_wp/pkg/view"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	contentcontract "go_wp/internal/module/content/contract"
	contenthttp "go_wp/internal/module/content/inbound/http"
	contentmodel "go_wp/internal/module/content/model"
	contentservice "go_wp/internal/module/content/service"
	"go_wp/internal/templates"

	"go_wp/public/test/support"
)

func newArticlePagingEnv(t *testing.T) (*gin.Engine, *gorm.DB, contentcontract.ContentService) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil, nil, nil
	}
	svc := contentservice.NewService(contentmodel.NewModel(db))
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	page := contenthttp.NewArticlePageHandle(svc, nil, nil, nil, nil)
	engine.GET("/admin/articles", pageview.Slot(contenthttp.ArticlesPageTemplate, pageview.RenderPage), page.ArticlesPage)
	return engine, db, svc
}

func seedArticles(t *testing.T, db *gorm.DB, n int) {
	t.Helper()
	values := make([]string, 0, n)
	args := make([]any, 0, n*4)
	for i := 0; i < n; i++ {
		values = append(values, "(?, 'article', ?, 1, ?::jsonb, now() - (? * interval '1 minute'), now() - (? * interval '1 minute'))")
		args = append(args,
			uuid.NewString(),
			fmt.Sprintf("art-%04d", i),
			fmt.Sprintf(`{"title":"文章 %04d","excerpt":"摘要 %04d"}`, i, i),
			i, i,
		)
	}
	sql := "INSERT INTO contents (id, entity_type, slug, revision, data, create_time, update_time) VALUES " +
		strings.Join(values, ", ")
	if err := db.Exec(sql, args...).Error; err != nil {
		t.Fatalf("批量造文章失败：%v", err)
	}
}

func articlePageBody(t *testing.T, engine *gin.Engine, query string) string {
	t.Helper()
	target := "/admin/articles"
	if query != "" {
		target += "?" + query
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("文章列表应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func articleRowCount(body string) int {
	return strings.Count(body, `name="ids"`)
}

func paginationText(body string) string {
	idx := strings.Index(body, `class="pagination"`)
	if idx < 0 {
		return "（页面没有分页条）"
	}
	end := idx + 400
	if end > len(body) {
		end = len(body)
	}
	return body[idx:end]
}

func TestArticlesListPagingUsesSourceCountAndClampsPage(t *testing.T) {
	engine, db, _ := newArticlePagingEnv(t)
	if engine == nil {
		return
	}
	seedArticles(t, db, 45)

	first := articlePageBody(t, engine, "")
	if got := articleRowCount(first); got != 20 {
		t.Fatalf("第 1 页应渲染 20 行，实际 %d 行", got)
	}
	if !strings.Contains(first, "共 45 条，第 1-20 条") {
		t.Fatalf("分页信息应取契约 Count 的真源总数（共 45 条）：%s", paginationText(first))
	}

	if !strings.Contains(first, "全部文章（45）") {
		t.Fatalf("列表工具栏应显示真源总数 45，而不是当页行数：%s", first[:min(len(first), 400)])
	}
	if !strings.Contains(first, "文章 0000") || strings.Contains(first, "文章 0044") {
		t.Fatalf("第 1 页应是最近更新的 20 篇（文章 0000-0019）")
	}

	third := articlePageBody(t, engine, "page=3")
	if got := articleRowCount(third); got != 5 {
		t.Fatalf("第 3 页应渲染 5 行（45 = 20 + 20 + 5），实际 %d 行", got)
	}
	if !strings.Contains(third, "共 45 条，第 41-45 条") {
		t.Fatalf("第 3 页的分页信息应为「第 41-45 条」：%s", paginationText(third))
	}
	if !strings.Contains(third, "文章 0044") {
		t.Fatalf("第 3 页应包含最后一篇（文章 0044）")
	}

	overflow := articlePageBody(t, engine, "page=9")
	if got := articleRowCount(overflow); got != 5 {
		t.Fatalf("越界页码应收敛到最后一页（5 行），实际 %d 行", got)
	}
	if !strings.Contains(overflow, "共 45 条，第 41-45 条") {
		t.Fatalf("越界页码收敛后分页信息应与最后一页一致：%s", paginationText(overflow))
	}

	small := articlePageBody(t, engine, "limit=10")
	if got := articleRowCount(small); got != 10 {
		t.Fatalf("limit=10 时应渲染 10 行，实际 %d 行", got)
	}
	if !strings.Contains(small, "共 45 条，第 1-10 条") {
		t.Fatalf("limit=10 的分页信息应为「第 1-10 条」：%s", paginationText(small))
	}
}

func TestArticlesListPagingSinglePageHasNoPager(t *testing.T) {
	engine, db, _ := newArticlePagingEnv(t)
	if engine == nil {
		return
	}
	seedArticles(t, db, 5)

	body := articlePageBody(t, engine, "")
	if got := articleRowCount(body); got != 5 {
		t.Fatalf("5 篇文章应渲染 5 行，实际 %d 行", got)
	}

	if strings.Contains(body, `class="pagination"`) {
		t.Fatalf("单页列表不应渲染分页条：%s", paginationText(body))
	}
	if !strings.Contains(body, "全部文章（5）") {
		t.Fatalf("单页时列表工具栏仍应显示总数 5")
	}
}

package feature

// article_list_paging_test.go — 文章列表真源分页的行为契约（审计 02-L §2 P1-14）。
//
// 改动前的现状：articleListLimit = 50 硬编码、无分页参数，第 50 篇之后的文章在页面上
// 根本不存在；契约（contentcontract.ContentService）也只有 List、没有任何计数能力，
// 于是「总数」只能由当页行数冒充。本文件钉住改动后的四件事：
//
//  1. 总数来自契约的 Count（真源），不是当页行数 ——「共 45 条，第 41-45 条」；
//  2. 取数顺序是「先计数 → 收敛页码 → 再取当页」—— 越界页码渲染的是最后一页，不是空表
//     （反过来「先取页再 count」就会出现「表格为空、分页条却显示第 9 页」）；
//  3. 分页条只在超过一页时渲染（单页列表上挂一条「上一页 / 下一页」是噪声）；
//  4. ?limit= 由 shell.PageParams 归一后透传到契约（每页条数直接决定 presentation 查询次数）。
//
// 为什么不能走「全量拉取 + handler 切片」：articlesPublished 对每篇逐条查 presentation
// 实例，全量取数等于把 N 次跨模块查询当成一次页面访问的代价 —— 这正是本批否决那条路的理由，
// 所以这里断言的是**按页取**（行数按 limit 变化），而不是「页面上能看到全部文章」。
//
// 夹具手法与 content_error_leak_test.go 一致：真 PG（生产迁移）+ 真 service + 真 Jet 模板，
// 契约只给 content 一个（projects / templates / instances / pages 传 nil，页面按
// 「能力未装配」渲染，不影响列表与分页）。

import (
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

// newArticlePagingEnv 装配只挂文章列表页的测试引擎（真实 service + 真实 PG + 真实模板）。
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
	engine.GET("/admin/articles", page.ArticlesPage)
	return engine, db, svc
}

// seedArticles 批量造 n 篇文章：序号越小 update_time 越新（排序键是 update_time DESC, id DESC，
// 所以第 1 页永远是「文章 0000…」这一批，页码与内容的对应关系确定）。
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

// articlePageBody 请求文章列表页并返回 HTML（非 200 直接失败）。
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

// articleRowCount 页面渲染出的文章行数（每行一个批量勾选框 name="ids"）。
func articleRowCount(body string) int {
	return strings.Count(body, `name="ids"`)
}

// paginationText 摘出分页条那一行（断言失败时给出可读的实际值，而不是整页 HTML）。
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

// TestArticlesListPagingUsesSourceCountAndClampsPage 多页场景：真源总数、按页取、越界收敛。
func TestArticlesListPagingUsesSourceCountAndClampsPage(t *testing.T) {
	engine, db, _ := newArticlePagingEnv(t)
	if engine == nil {
		return
	}
	seedArticles(t, db, 45)

	// —— 第 1 页：默认每页 20（shell.PageParams）——
	first := articlePageBody(t, engine, "")
	if got := articleRowCount(first); got != 20 {
		t.Fatalf("第 1 页应渲染 20 行，实际 %d 行", got)
	}
	if !strings.Contains(first, "共 45 条，第 1-20 条") {
		t.Fatalf("分页信息应取契约 Count 的真源总数（共 45 条）：%s", paginationText(first))
	}
	// 总数不是当页行数：列表工具栏的「全部文章（N）」也必须是 45。
	if !strings.Contains(first, "全部文章（45）") {
		t.Fatalf("列表工具栏应显示真源总数 45，而不是当页行数：%s", first[:min(len(first), 400)])
	}
	if !strings.Contains(first, "文章 0000") || strings.Contains(first, "文章 0044") {
		t.Fatalf("第 1 页应是最近更新的 20 篇（文章 0000-0019）")
	}

	// —— 第 3 页：余数页 ——
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

	// —— 越界页码：收敛到最后一页，而不是渲染空表 ——
	// 这是「先计数 → 收敛页码 → 再取当页」的直接判据：顺序反过来时 service 会老实返回空页，
	// 而分页条按收敛后的页码渲染，页面就成了「表格为空、分页条显示第 3 页」。
	overflow := articlePageBody(t, engine, "page=9")
	if got := articleRowCount(overflow); got != 5 {
		t.Fatalf("越界页码应收敛到最后一页（5 行），实际 %d 行", got)
	}
	if !strings.Contains(overflow, "共 45 条，第 41-45 条") {
		t.Fatalf("越界页码收敛后分页信息应与最后一页一致：%s", paginationText(overflow))
	}

	// —— ?limit= 透传：每页条数归一后进契约（它同时是一页的 presentation 查询预算）——
	small := articlePageBody(t, engine, "limit=10")
	if got := articleRowCount(small); got != 10 {
		t.Fatalf("limit=10 时应渲染 10 行，实际 %d 行", got)
	}
	if !strings.Contains(small, "共 45 条，第 1-10 条") {
		t.Fatalf("limit=10 的分页信息应为「第 1-10 条」：%s", paginationText(small))
	}
}

// TestArticlesListPagingSinglePageHasNoPager 单页场景：不渲染分页条，且不丢内容。
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
	// BuildPagination 在 total 不超过一页时返回 nil、TemplateKeys 给空 map，
	// 模板的 {{if .["PaginationLinks"]}} 自然跳过。
	if strings.Contains(body, `class="pagination"`) {
		t.Fatalf("单页列表不应渲染分页条：%s", paginationText(body))
	}
	if !strings.Contains(body, "全部文章（5）") {
		t.Fatalf("单页时列表工具栏仍应显示总数 5")
	}
}

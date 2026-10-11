package contenthttp

// article_pagination_facts_test.go — task-117（P3-B3b）：content 模块两个整页分页站点
// 的证据。
//
// · 文章列表站点的 **handler 级链接字节断言已存在**：article_page_render_test.go 的
//   TestArticlesPageKeywordFiltersBeforePagination 断言了
//   `href="/admin/articles?keyword=needle&amp;page=1&amp;limit=1"` 等整串链接（迁移后仍绿），
//   本文件不重复一遍。
// · 译文工作台站点的数据不走顶层 gin.H，而是 `articleTranslationsData` 结构体的
//   `templateMap()` 合并（旧形态是 `Pagination gin.H` + `for k, v := range`），
//   所以这里在**映射层**钉住：交出的是 pagination.Page 事实、旧成品键一个不留。

import (
	"testing"

	"go_wp/pkg/pagination"
)

// TestArticleTranslationsMapperCarriesPaginationFacts 译文工作台：映射层交出事实键，
// 基址（含本页钉死的 limit）原样带出，旧成品键不存在。
func TestArticleTranslationsMapperCarriesPaginationFacts(t *testing.T) {
	pg := pagination.Facts(45, 2, 20)
	pg.Base = "/admin/articles/translations?lang=zh-CN&project=p1&keyword=&limit=20"
	d := &articleTranslationsData{
		Title: "译文工作台", Menu: "content", Lang: "zh-CN",
		Page: 2, Limit: 20, Total: 45, HasData: true,
		Pagination: pg,
	}

	m := d.templateMap()

	got, ok := m["Pagination"].(pagination.Page)
	if !ok {
		t.Fatalf("模板键 Pagination 不是 pagination.Page 事实：%T", m["Pagination"])
	}
	if got != pg {
		t.Errorf("交出的事实与 handler 构造的不一致：%+v", got)
	}
	if !got.HasPagination() {
		t.Errorf("45 条 / 每页 20 ⇒ 三页，事实应判定为多页：%+v", got)
	}
	// 本页 limit 由筛选表单钉死，基址显式带上；partial 会再补一次 limit
	//（值相同），这是与旧链接逐字节一致的已知遗留（见卡面登记）。
	if got.Base != pg.Base {
		t.Errorf("基址应原样带出（含 limit）：%q", got.Base)
	}
	for _, legacy := range []string{"PaginationInfo", "PaginationLinks"} {
		if v, exists := m[legacy]; exists {
			t.Errorf("旧成品键 %s 仍在：%v", legacy, v)
		}
	}
	// 单页 / 空集时零值事实不渲染分页条：HasPagination 为假是唯一判据。
	// （模板里剩下的坑：空集的 zero Page 也必须为假 —— 见下一条。）
}

// TestArticleTranslationsMapperNoDataRendersNoPagination 没数据（未构造 Pagination）时
// 交出的是零值事实：HasPagination 为假，partial 自然不渲染 —— 旧形态靠「键不存在」，
// 新形态靠「零值判假」，两者对模板的可见结果一致。
func TestArticleTranslationsMapperNoDataRendersNoPagination(t *testing.T) {
	d := &articleTranslationsData{Title: "译文工作台", Lang: "zh-CN", HasData: false}
	m := d.templateMap()

	pg, ok := m["Pagination"].(pagination.Page)
	if !ok {
		t.Fatalf("模板键 Pagination 不是 pagination.Page 事实：%T", m["Pagination"])
	}
	if pg.HasPagination() {
		t.Errorf("零值事实不该判定为多页：%+v", pg)
	}
}

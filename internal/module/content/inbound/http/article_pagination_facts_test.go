package contenthttp

import (
	"go_wp/internal/adminui"
	"testing"

	"go_wp/internal/templates"
	"go_wp/pkg/pagination"
)

func TestArticleTranslationsMapperCarriesPaginationFacts(t *testing.T) {
	pg := pagination.Page{Total: 45, Page: 2, Limit: 20}
	pg.Base = "/admin/articles/translations?lang=zh-CN&project=p1&keyword=&limit=20"
	d := &articleTranslationsData{
		Title: "译文工作台", Menu: "content", Lang: "zh-CN",
		Page: 2, Limit: 20, Total: 45, HasData: true,
		Pagination: pg,
	}

	m := adminui.ViewMap(d)

	got, ok := m["Pagination"].(pagination.Page)
	if !ok {
		t.Fatalf("模板键 Pagination 不是 pagination.Page 事实：%T", m["Pagination"])
	}
	if got != pg {
		t.Errorf("交出的事实与 handler 构造的不一致：%+v", got)
	}
	if !templates.PagerOf(got).HasPagination() {
		t.Errorf("45 条 / 每页 20 ⇒ 三页，事实应判定为多页：%+v", got)
	}

	if got.Base != pg.Base {
		t.Errorf("基址应原样带出（含 limit）：%q", got.Base)
	}
	for _, legacy := range []string{"PaginationInfo", "PaginationLinks"} {
		if v, exists := m[legacy]; exists {
			t.Errorf("旧成品键 %s 仍在：%v", legacy, v)
		}
	}

}

func TestArticleTranslationsMapperNoDataRendersNoPagination(t *testing.T) {
	d := &articleTranslationsData{Title: "译文工作台", Lang: "zh-CN", HasData: false}
	m := adminui.ViewMap(d)

	pg, ok := m["Pagination"].(pagination.Page)
	if !ok {
		t.Fatalf("模板键 Pagination 不是 pagination.Page 事实：%T", m["Pagination"])
	}
	if templates.PagerOf(pg).HasPagination() {
		t.Errorf("零值事实不该判定为多页：%+v", pg)
	}
}

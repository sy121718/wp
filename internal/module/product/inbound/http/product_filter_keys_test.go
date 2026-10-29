package producthttp

import (
	"strings"
	"testing"
)

// 筛选字段由模板消费原请求；分页链接由 TestTaxonomyPagesPaginationFactsRenderLinks 验证。
func TestTaxonomyFiltersRetainQuery(t *testing.T) {
	for _, page := range taxonomyPages {
		t.Run(page.name, func(t *testing.T) {
			rec := productPageRequestFor(t, taxonomyHandle(page.success()), page.route, page.path+"&keyword=linen&unknown=discard")
			if rec.Code != 200 {
				t.Fatalf("渲染失败: %s", rec.Body.String())
			}
			for _, want := range []string{`name="project" value="p1"`, `name="keyword" value="linen"`} {
				if !strings.Contains(rec.Body.String(), want) {
					t.Errorf("缺少筛选字段 %s", want)
				}
			}
			if strings.Contains(rec.Body.String(), `name="unknown"`) {
				t.Fatal("未声明参数进入筛选表单")
			}
		})
	}
}

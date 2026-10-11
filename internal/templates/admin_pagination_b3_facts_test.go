package templates

// admin_pagination_b3_facts_test.go — task-116（P3-B3）：四个小模块整页分页站点的
// 「旧成品 vs 新事实」**逐字节**差分。
//
// 这一卡要证明的不是「大概一样」，而是**渲染字节完全相同**：迁移前 handler 交给模板的是
// BuildPagination 预制好的信息行句子 + 链接（PaginationInfo / PaginationLinks），
// 迁移后只交 pagination.Page 事实，句子与链接由 admin/partials/pagination.html 用
// sprintf / queryURL / pageNumbers 现算。任何一处口径漂移（夹紧、页码窗口、链接参数顺序、
// limit 取舍、空 white space 槽位）都会在这里红。
//
// 只读复用 P2 的 p2RenderLegacy / p2RenderFacts / p2FirstDiff（**不改**
// admin_pagination_dual_shape_p2_test.go —— 它带 const baseline = 31 的「只减不增」计数判据）。
//
// 范围限定：本文件只覆盖 B3 四站点（membership / masterdata / comments / analytics）
// 的**整页**站点形态；这四处的模板都是裸 include（读顶层 .Pagination 键），
// 所以不需要按站点改模板 —— 差分本身就是在 partial 这一层做的。

import (
	"strings"
	"testing"
)

// b3Site 一个站点的两种基址形态：带筛选 / 不带筛选。
//
// 之所以两种都测：基址是 handler 算出来交给事实层的自由字符串，链接参数顺序由
// url.Values.Encode() 决定 —— 只有把「有筛选」与「无筛选」两种字节都钉住，
// 才算钉住「翻页不丢条件」与「不凭空多出参数」。
type b3Site struct {
	name     string
	filtered string
	plain    string
}

func b3Sites() []b3Site {
	const pid = "11111111-1111-1111-1111-111111111111"
	return []b3Site{
		{
			name:     "membership-assignments",
			filtered: "/admin/membership/assignments?project=" + pid + "&tierId=7&userId=1024",
			plain:    "/admin/membership/assignments?project=" + pid,
		},
		{
			name:     "masterdata-changes",
			filtered: "/admin/masterdata/changes?keyword=%E4%BB%B7%E6%A0%BC&project=" + pid,
			plain:    "/admin/masterdata/changes?project=" + pid,
		},
		{
			name:     "comments",
			filtered: "/admin/comments?project=" + pid + "&status=pending",
			plain:    "/admin/comments?project=" + pid,
		},
		{
			// analytics 的基址恒带 view=paths（分页条只属于「按路径」面板，见
			// analytics_page.go 的那段注释），因此两种形态是「带时间范围」与「不带」。
			name:     "analytics",
			filtered: "/admin/analytics?project=" + pid + "&from=2026-01-01&to=2026-01-31&view=paths",
			plain:    "/admin/analytics?project=" + pid + "&view=paths",
		},
	}
}

// b3Shapes 形态矩阵：单页 / 两页（末页）/ 多页中段（双省略号）/ 末页 / 空集 / 不足一页。
func b3Shapes() []struct {
	name  string
	total int64
	page  int
	limit int
} {
	return []struct {
		name  string
		total int64
		page  int
		limit int
	}{
		{"单页", 45, 1, 20},
		{"两页-第二页", 45, 2, 20},
		{"多页中段双省略号", 1000, 25, 20},
		{"末页", 1000, 50, 20},
		{"空集", 0, 1, 20},
		{"不足一页", 5, 1, 20},
	}
}

// TestB3PaginationSitesRenderIdenticallyToLegacy 四站点 × 两基址 × 六形态：逐字节相同。
func TestB3PaginationSitesRenderIdenticallyToLegacy(t *testing.T) {
	for _, site := range b3Sites() {
		for baseName, base := range map[string]string{"带筛选": site.filtered, "无筛选": site.plain} {
			for _, sh := range b3Shapes() {
				name := site.name + "/" + baseName + "/" + sh.name
				s := p0PaginationShape{
					name:  name,
					total: sh.total,
					page:  sh.page,
					limit: sh.limit,
					base:  base,
				}
				legacy := p2RenderLegacy(t, s)
				facts := p2RenderFacts(t, s, nil)
				if legacy != facts {
					t.Errorf("[%s] 旧成品与新事实的渲染字节不同：\n%s", name, p2FirstDiff(legacy, facts))
				}
			}
		}
	}
}

// TestB3PaginationSitesSinglePageAndEmptyRenderNothing 单页与空集两边都渲染空（不是「渲染了但看不见」）。
//
// 单独列一条的意义：上面那条断言的是「两边相同」，若两边同时渲染出一段多余的空白/空 nav，
// 逐字节比较仍然会通过。这条把「什么都没有」本身钉住。
func TestB3PaginationSitesSinglePageAndEmptyRenderNothing(t *testing.T) {
	for _, site := range b3Sites() {
		for _, sh := range []struct {
			name  string
			total int64
			page  int
		}{
			{"单页", 5, 1},
			{"空集", 0, 1},
		} {
			s := p0PaginationShape{name: site.name + "/" + sh.name, total: sh.total, page: sh.page, limit: 20, base: site.filtered}
			for _, out := range []struct {
				kind string
				html string
			}{
				{"旧成品", p2RenderLegacy(t, s)},
				{"新事实", p2RenderFacts(t, s, nil)},
			} {
				if strings.Contains(out.html, "<nav") || strings.Contains(out.html, "pagination") {
					t.Errorf("[%s/%s] %s 档不该渲染分页条，实际:\n%s", site.name, sh.name, out.kind, out.html)
				}
			}
		}
	}
}

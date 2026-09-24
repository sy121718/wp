package analyticshttp

// analytics_page_render_test.go — 访问统计页的渲染冒烟（BIZ-8）+ 维度页签的结构与视图回归。
//
// 钉住四件事：
//   - admin/analytics.html 的 Jet 语法与 layout 数据契约成立（模板错了只会在用户点开时 500）；
//   - 页面把服务端给的聚合结果真的渲染出来（不是「接口对了、页面空白」）；
//   - 5 张同构维度表收进 .tabs 后**仍全部由服务端渲染**（切换是纯前端行为，不再取数），
//     且同一时刻恰好一个面板可见；
//   - URL 的 ?view= 只决定初始选中：分页链接**固定**带 view=paths，其余会重载页面的入口
//     带当前视图 —— 这一条是本页独有的陷阱（见下方 TestAnalyticsPaginationLinksCarryViewPaths）。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	analyticscontract "go_wp/internal/module/analytics/contract"
	analyticsdto "go_wp/internal/module/analytics/dto"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/templates"
)

// TestAnalyticsPageTemplateRenders 用真实 Jet 渲染器渲染统计页并断言关键内容。
func TestAnalyticsPageTemplateRenders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	lang := "zh-CN"
	data := gin.H{
		"title": analyticsPageTitle, "menu": "analytics",
		"csrf_token": "test-token", "lang": lang,
		"t": templates.TranslateFunc(lang), "langs": templates.LanguageOptions(lang),
		"lang_redirect": "/admin/analytics",
		// 两个工程：单工程时页面不渲染工程下拉（唯一选项的下拉是纯占位，见项目既有约定），
		// 给一个工程就断言不到「工程选择」这条渲染路径。
		"Projects": []projectcontract.ProjectResp{
			{ID: "p-1", Name: "演示站"}, {ID: "p-2", Name: "备用站"},
		},
		"SelectedProject": "p-1",
		"FilterFrom":      "2026-01-01", "FilterTo": "2026-01-15",
		"RangeFrom": "2026-01-01", "RangeTo": "2026-01-15",
		"Total": 3, "Visitors": 2, "PathTotal": 2,
		"Daily": []analyticscontract.DailyCount{{Day: "2026-01-15", Views: 3, Visitors: 2}},
		"Paths": []analyticscontract.PathCount{{Path: "/about", Views: 2, Visitors: 1}},
		// 来源域 / 设备分类 / 语言：其中一行故意给空取值，钉住「空值渲染成占位文案」
		// 而不是留一个空格子（空串是合法取值，见 model.CountByDimension）。
		"Referrers": []analyticscontract.RankCount{
			{Value: "ref.example.com", Views: 2, Visitors: 1},
			{Value: "", Views: 1, Visitors: 1},
		},
		"UAClasses": []analyticscontract.RankCount{{Value: "desktop", Views: 2, Visitors: 2}},
		"Langs":     []analyticscontract.RankCount{{Value: "zh-CN", Views: 3, Visitors: 2}},
		// 视图（tabs 初始选中）：handler 恒给；不给也能渲染（模板有兜底，见下面第二个用例）。
		"ViewMode":       "daily",
		"RankLimit":      20,
		"PaginationInfo": "共 2 条", "PaginationLinks": nil,
		"Err": "",
	}
	body := renderAnalyticsPage(t, data)
	for _, want := range []string{
		"访问统计", "演示站", "2026-01-15", "/about",
		"总浏览数", "独立访客", "按天", "按路径",
		// 三个维度区块（SEO-021 的读路径）—— 页面上真的渲染出来，而不是「接口对了、区块空白」。
		"来源域", "设备分类", "语言",
		"ref.example.com", "desktop", "zh-CN",
		// 空取值渲染为占位文案，不是空格子。
		"（无来源）",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("渲染结果缺少 %q", want)
		}
	}
	// 5 张维度表 → 5 个标签 + 5 个面板 + 5 张表，只有 1 个面板可见。
	assertTabsStructure(t, body, 5)
	if got := visiblePanels(body); got[0] != "an-panel-daily" {
		t.Errorf("无 view 参数时应停在默认视图「按天」，实际可见面板 %v", got)
	}
}

// TestAnalyticsPageRendersWithoutViewMode 模板对 ViewMode 缺键必须有兜底。
//
// 别的渲染路径（单测、将来的片段复用）不一定传这个键，而缺键的表现是
// 「HTTP 200 + 那一行之后整块 HTML 消失」——几乎无法从表象定位。
// 判据：不给 ViewMode 时 5 个面板仍然全渲染，且默认视图可见。
func TestAnalyticsPageRendersWithoutViewMode(t *testing.T) {
	body := renderAnalyticsPage(t, map[string]any{
		"title": analyticsPageTitle, "menu": "analytics",
		"SelectedProject": "p-1", "Projects": []projectcontract.ProjectResp{{ID: "p-1", Name: "演示站"}},
		"FilterFrom": "", "FilterTo": "", "RangeFrom": "", "RangeTo": "",
		"Total": 0, "Visitors": 0, "PathTotal": 0,
		"Daily": []any{}, "Paths": []any{}, "Referrers": []any{}, "UAClasses": []any{}, "Langs": []any{},
		"RankLimit": 20, "Err": "",
	})
	assertTabsStructure(t, body, 5)
	if got := visiblePanels(body); got[0] != "an-panel-daily" {
		t.Errorf("缺 ViewMode 时应回落默认视图，实际可见面板 %v", got)
	}
}

// TestAnalyticsEmptyViewsDistinguishMissingTrafficFromPurgedDetail 钉住各视图的空态语义。
func TestAnalyticsEmptyViewsDistinguishMissingTrafficFromPurgedDetail(t *testing.T) {
	base := map[string]any{
		"title": analyticsPageTitle, "menu": "analytics",
		"SelectedProject": "p-1", "Projects": []projectcontract.ProjectResp{{ID: "p-1", Name: "演示站"}},
		"FilterFrom": "", "FilterTo": "", "RangeFrom": "", "RangeTo": "",
		"Total": 0, "Visitors": 0, "PathTotal": 0,
		"Daily": []any{}, "Paths": []any{}, "Referrers": []any{}, "UAClasses": []any{}, "Langs": []any{},
		"RankLimit": 20, "Err": "",
	}
	for _, tc := range []struct {
		name, view, title string
	}{
		{"按天", "daily", "这个窗口里还没有浏览记录。"},
		{"按路径", "paths", "没有路径数据。"},
		{"来源", "referrers", "没有来源数据。"},
		{"设备", "ua", "没有设备数据。"},
		{"语言", "langs", "没有语言数据。"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base["ViewMode"] = tc.view
			panel := analyticsPanel(renderAnalyticsPage(t, base), tc.view)
			if !strings.Contains(panel, tc.title) || !strings.Contains(panel, `href="/admin/pages"`) {
				t.Errorf("%s 空态缺对应标题或下一步动作", tc.view)
			}
		})
	}
	base["BreakdownUnavailable"] = true
	base["Total"] = 12
	for _, view := range []string{"referrers", "ua", "langs"} {
		t.Run("明细清理/"+view, func(t *testing.T) {
			base["ViewMode"] = view
			panel := analyticsPanel(renderAnalyticsPage(t, base), view)
			emptyStart := strings.Index(panel, `<div class="empty-state">`)
			if emptyStart < 0 {
				t.Fatal("保留期空态缺少 empty-state 容器")
			}
			emptyEnd := strings.Index(panel[emptyStart:], `</div>`)
			if emptyEnd < 0 {
				t.Fatal("保留期空态未闭合")
			}
			empty := panel[emptyStart : emptyStart+emptyEnd]
			if got := strings.Count(empty, `<p class="empty-desc">该时间段的访问明细已过保留期并被清理`); got != 1 {
				t.Errorf("明细清理说明应位于 .empty-desc 内一次，实际 %d", got)
			}
			if strings.Contains(panel, `href="/admin/pages"`) {
				t.Error("已清理明细不能引导重新发布页面")
			}
		})
	}
}

// analyticsPanel 按固定面板 id 的次序截取，避免内部节点影响边界。
func analyticsPanel(body, view string) string {
	views := []string{"daily", "paths", "referrers", "ua", "langs"}
	for i, candidate := range views {
		if candidate != view {
			continue
		}
		start := strings.Index(body, `id="an-panel-`+view+`"`)
		if start < 0 {
			return ""
		}
		for _, next := range views[i+1:] {
			if end := strings.Index(body[start:], `id="an-panel-`+next+`"`); end >= 0 {
				return body[start : start+end]
			}
		}
		return body[start:]
	}
	return ""
}

// TestAnalyticsViewParamSelectsPanel ?view= 只决定初始选中，且不认识的取值回默认视图。
//
// 分页那一条是**本页独有的回归**：路径表有分页，而它的分页条物理上落在「按路径」面板里。
// 若分页链接不带 view=paths，用户切到「按路径」点「下一页」→ 整页重载 → 回到第一个标签，
// 路径表落在 hidden 面板里 —— 「点了下一页却看不到任何变化」。
func TestAnalyticsViewParamSelectsPanel(t *testing.T) {
	cases := []struct {
		name, target, wantPanel string
	}{
		{"无 view → 默认按天", "/admin/analytics", "an-panel-daily"},
		{"view=paths", "/admin/analytics?view=paths", "an-panel-paths"},
		{"view=referrers", "/admin/analytics?view=referrers", "an-panel-referrers"},
		{"view=ua", "/admin/analytics?view=ua", "an-panel-ua"},
		{"view=langs", "/admin/analytics?view=langs", "an-panel-langs"},
		{"不认识的 view → 回默认视图", "/admin/analytics?view=nope", "an-panel-daily"},
		{"view 带空白也认", "/admin/analytics?view=%20langs%20", "an-panel-langs"},
		// 分页回归：点「下一页」重载后必须停在路径面板，否则第 2 页的数据看不见。
		{"view=paths&page=2 直达", "/admin/analytics?view=paths&page=2", "an-panel-paths"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := analyticsPageBody(t, c.target, analyticsSummaryWithPages())
			got := visiblePanels(body)
			if len(got) != 1 || got[0] != c.wantPanel {
				t.Fatalf("%s：可见面板 = %v，期望 [%s]", c.target, got, c.wantPanel)
			}
			assertTabsStructure(t, body, 5)
		})
	}
}

// TestAnalyticsPaginationLinksCarryViewPaths 分页链接固定带 view=paths ——
// 无论用户此刻停在哪个标签（分页条只属于路径面板，所以它必须把页面带回路径视图）。
//
// 顺带钉住另外两个「会重载页面的入口」：工程选择与时间范围表单都要带回当前视图，
// 否则在那个标签下改筛选会被弹回第一个标签。
func TestAnalyticsPaginationLinksCarryViewPaths(t *testing.T) {
	paginationHrefRe := regexp.MustCompile(`href="([^"]*page=\d+[^"]*)"`)

	// 用户停在默认视图（URL 无 view）时，路径表照样是多页的 —— 分页链接也必须带 view=paths。
	for _, target := range []string{"/admin/analytics", "/admin/analytics?view=paths&page=2"} {
		body := analyticsPageBody(t, target, analyticsSummaryWithPages())
		links := paginationHrefRe.FindAllStringSubmatch(body, -1)
		if len(links) == 0 {
			t.Fatalf("%s：分页条没有渲染出任何页码链接（PathTotal 45 / limit 20 应有 3 页）", target)
		}
		for _, l := range links {
			if !strings.Contains(l[1], "view=paths") {
				t.Errorf("%s：分页链接 %q 没带 view=paths —— 点它会回到第一个标签，"+
					"而路径表在 hidden 面板里（表现为「点了下一页没反应」）", target, l[1])
			}
			if strings.Contains(l[1], "view=daily") {
				t.Errorf("%s：分页链接 %q 带的是当前视图而不是 paths", target, l[1])
			}
		}
	}

	// 会重载页面的入口带**当前**视图：在「语言」标签下改筛选不该被弹回「按天」。
	body := analyticsPageBody(t, "/admin/analytics?view=langs", analyticsSummaryWithPages())
	if got := strings.Count(body, `name="view" value="langs"`); got != 2 {
		t.Errorf("工程选择与时间范围两个表单都应回带当前视图，实际 %d 处", got)
	}
	if !strings.Contains(body, "&view=langs") {
		t.Error("「最近 30 天」链接没有回带当前视图")
	}
}

// ===== 辅助 =====

// tabPanelTagRe 匹配面板的起始标签（属性跨行，[^>]* 含换行）。
var tabPanelTagRe = regexp.MustCompile(`(?s)<div class="tab-panel" role="tabpanel" id="(an-panel-[a-z]+)"[^>]*>`)

// tabSelectedRe 匹配「选中态的标签 → 它控制的面板」。
var tabSelectedRe = regexp.MustCompile(`(?s)<button[^>]*id="(an-tab-[a-z]+)"[^>]*aria-controls="(an-panel-[a-z]+)"[^>]*aria-selected="true"`)

// visiblePanels 返回没有 hidden 的面板 id。
func visiblePanels(body string) []string {
	var out []string
	for _, m := range tabPanelTagRe.FindAllStringSubmatch(body, -1) {
		if !strings.Contains(m[0], "hidden") {
			out = append(out, m[1])
		}
	}
	return out
}

// selectedTabPanels 返回 aria-selected="true" 的标签所控制的面板 id。
func selectedTabPanels(body string) []string {
	var out []string
	for _, m := range tabSelectedRe.FindAllStringSubmatch(body, -1) {
		out = append(out, m[2])
	}
	return out
}

// assertTabsStructure 断言页签结构完整：1 个 tabs 容器、want 个标签 / 面板 / 数据表，
// 恰好 1 个面板可见，且可见面板正是选中标签所控制的那个。
func assertTabsStructure(t *testing.T, body string, want int) {
	t.Helper()
	if got := strings.Count(body, "data-tabs"); got != 1 {
		t.Errorf("期望恰好 1 个 [data-tabs] 容器，实际 %d（这是页签生效的判据）", got)
	}
	if got := strings.Count(body, `class="tab" role="tab"`); got != want {
		t.Errorf("期望 %d 个标签，实际 %d", want, got)
	}
	if got := len(tabPanelTagRe.FindAllStringSubmatch(body, -1)); got != want {
		t.Errorf("期望 %d 个面板，实际 %d", want, got)
	}
	// 服务端全渲染：面板数与维度表数必须相等（切标签不再取数）。
	if got := strings.Count(body, `class="data-table"`); got != want {
		t.Errorf("期望 %d 张服务端渲染的维度表，实际 %d", want, got)
	}
	visible := visiblePanels(body)
	if len(visible) != 1 {
		t.Fatalf("同一时刻应恰好 1 个面板可见，实际 %d 个：%v", len(visible), visible)
	}
	selected := selectedTabPanels(body)
	if len(selected) != 1 || selected[0] != visible[0] {
		t.Errorf("可见面板 %v 与 aria-selected=true 的标签所控制的面板 %v 不一致", visible, selected)
	}
}

// renderAnalyticsPage 用真实 Jet 渲染器渲染统计页模板（不经 handler）。
func renderAnalyticsPage(t *testing.T, over map[string]any) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	lang := "zh-CN"
	data := gin.H{
		"lang": lang, "t": templates.TranslateFunc(lang), "langs": templates.LanguageOptions(lang),
		"csrf_token": "test-token", "lang_redirect": "/admin/analytics",
	}
	for k, v := range over {
		data[k] = v
	}
	engine := gin.New()
	// 模板根目录相对包目录：internal/module/analytics/inbound/http → internal/templates。
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/analytics", func(c *gin.Context) {
		c.HTML(http.StatusOK, "admin/analytics/analytics.html", data)
	})
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/analytics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("页面渲染失败，状态 %d", rec.Code)
	}
	return rec.Body.String()
}

// analyticsPageBody 用真实 handler（含分页与视图参数解析）+ 真实模板渲染统计页。
func analyticsPageBody(t *testing.T, target string, resp *analyticscontract.SummaryResp) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handle := NewAnalyticsPageHandle(fakeSummaryAnalytics{resp: resp}, fakeProjects{items: []projectcontract.ProjectResp{
		{ID: "p-1", Name: "演示站"}, {ID: "p-2", Name: "备用站"},
	}})
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/analytics", handle.AnalyticsPage)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s 渲染失败，状态 %d", target, rec.Code)
	}
	return rec.Body.String()
}

// analyticsSummaryWithPages 一份「路径表多页」的 Summary 响应（45 条 / 每页 20 ⇒ 3 页）。
func analyticsSummaryWithPages() *analyticscontract.SummaryResp {
	return &analyticscontract.SummaryResp{
		ProjectID: "p-1", From: "2026-01-01", To: "2026-01-15",
		Total: 45, Visitors: 9,
		Daily:     []analyticscontract.DailyCount{{Day: "2026-01-15", Views: 45, Visitors: 9}},
		Paths:     []analyticscontract.PathCount{{Path: "/about", Views: 20, Visitors: 5}},
		PathTotal: 45,
		Referrers: []analyticscontract.RankCount{{Value: "ref.example.com", Views: 3, Visitors: 2}},
		UAClasses: []analyticscontract.RankCount{{Value: "desktop", Views: 3, Visitors: 2}},
		Langs:     []analyticscontract.RankCount{{Value: "zh-CN", Views: 3, Visitors: 2}},
		RankLimit: 20,
		// 与 service 的形态一致：维度排行固定读明细（见 dto 的 BreakdownSource 注释）。
		BreakdownSource: analyticsdto.SourceDetail,
	}
}

// fakeSummaryAnalytics 最小统计契约桩：返回预设响应，并回显本次分页参数。
type fakeSummaryAnalytics struct {
	analyticscontract.AnalyticsService // 本用例只用 Summary
	resp                               *analyticscontract.SummaryResp
}

func (f fakeSummaryAnalytics) Summary(_ context.Context, req *analyticsdto.SummaryReq) (*analyticscontract.SummaryResp, error) {
	if f.resp == nil {
		return nil, nil
	}
	// 复制一份再回显分页参数：同一个桩会被多个请求共用，改动共享对象会让用例之间互相污染。
	out := *f.resp
	out.PathPage, out.PathLimit = req.PathPage, req.PathLimit
	return &out, nil
}

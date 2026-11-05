package pagehttp

import (
	pageview "go_wp/pkg/view"
	"context"
	"go_wp/internal/adminui"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	projectcontract "go_wp/internal/module/project/contract"
)

type filterProjectList struct {
	projectcontract.ProjectService
	list []projectcontract.ProjectResp

	enabled []string

	themeFor *string
}

func (f filterProjectList) List(context.Context) ([]projectcontract.ProjectResp, error) {
	return f.list, nil
}

func (f filterProjectList) GetActiveTheme(_ context.Context, projectID string) (*projectcontract.ThemeResp, error) {
	if f.themeFor != nil {
		*f.themeFor = projectID
	}
	return nil, nil
}

func (f filterProjectList) EnabledLangs(context.Context, string) ([]string, error) {
	return f.enabled, nil
}

type filterPageList struct {
	pagecontract.PageService
	got  *pagedto.ListReq
	rows []pagedto.PageResp

	staleRes *pagedto.StalePageListResp
	staleErr error

	staleGot *pagedto.StalePageListReq
}

func (f *filterPageList) List(_ context.Context, req *pagedto.ListReq) ([]pagedto.PageResp, error) {
	f.got = req
	return f.rows, nil
}

func (f *filterPageList) ListStalePages(_ context.Context, req *pagedto.StalePageListReq) (*pagedto.StalePageListResp, error) {
	f.staleGot = req
	if f.staleErr != nil {
		return nil, f.staleErr
	}
	if f.staleRes != nil {
		return f.staleRes, nil
	}
	return &pagedto.StalePageListResp{Pages: []pagedto.StalePageResp{}, Limit: staleOverviewTestLimit}, nil
}

const staleOverviewTestLimit = 8

func twoProjects() []projectcontract.ProjectResp {
	return []projectcontract.ProjectResp{
		{ID: "p1", Name: "官网"},
		{ID: "p2", Name: "活动站"},
	}
}

func renderPagesList(t *testing.T, h *pagesAdminHandle, query string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = newPageErrTestRender(t)
	router.GET("/admin/pages", pageview.Slot(PagesListPageTemplate, adminui.RenderPage), h.PagesList)

	target := "/admin/pages"
	if query != "" {
		target += "?" + query
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("渲染 /admin/pages?%s 失败：%d，body=%s", query, rec.Code, rec.Body.String())
	}
	return rec
}

func newFilterPagesHandle(t *testing.T, projects []projectcontract.ProjectResp) (*pagesAdminHandle, *filterPageList, *string) {
	t.Helper()
	themeFor := new(string)
	pages := &filterPageList{}
	h := &pagesAdminHandle{
		projects: filterProjectList{list: projects, themeFor: themeFor},
		pages:    pages,
	}
	return h, pages, themeFor
}

func TestPagesListFilterPassesProjectToService(t *testing.T) {
	t.Run("指定第二个工程→service 收到它", func(t *testing.T) {
		h, pages, themeFor := newFilterPagesHandle(t, twoProjects())
		renderPagesList(t, h, "project=p2")

		if pages.got == nil {
			t.Fatal("handler 没有调用 service.List：筛选条件没有进查询")
		}
		if pages.got.ProjectID != "p2" {
			t.Fatalf("ListReq.ProjectID = %q，期望 p2（筛选栏选了第二个工程，查询却还停在别的工程）", pages.got.ProjectID)
		}
		if *themeFor != "p2" {
			t.Fatalf("激活主题仍按 %q 取：主题作用域没跟着筛选走，会列出「B 工程 + A 主题」的混合结果", *themeFor)
		}
	})

	t.Run("未指定→仍是第一个工程（默认语义不变）", func(t *testing.T) {
		h, pages, themeFor := newFilterPagesHandle(t, twoProjects())
		renderPagesList(t, h, "")

		if pages.got == nil || pages.got.ProjectID != "p1" {
			t.Fatalf("未指定时 ListReq.ProjectID = %+v，期望第一个工程 p1（改筛选不能改默认语义）", pages.got)
		}
		if *themeFor != "p1" {
			t.Fatalf("未指定时激活主题按 %q 取，期望 p1", *themeFor)
		}
	})

	t.Run("指定一个不存在的工程→回退到第一个（陈旧 URL 不是错误页）", func(t *testing.T) {
		h, pages, _ := newFilterPagesHandle(t, twoProjects())
		rec := renderPagesList(t, h, "project=gone")

		if pages.got == nil || pages.got.ProjectID != "p1" {
			t.Fatalf("不存在的工程应回退到 p1，实际 %+v", pages.got)
		}

		if strings.Contains(rec.Body.String(), "这个站点工程还没有页面") {
			t.Error("伪造 / 陈旧的工程 id 被当成一次真筛选：空态会说「这个工程还没有页面」，而用户并没有筛过")
		}
	})
}

func TestPagesListFilterEchoesSelection(t *testing.T) {
	h, _, _ := newFilterPagesHandle(t, twoProjects())

	t.Run("指定 p2", func(t *testing.T) {
		body := renderPagesList(t, h, "project=p2").Body.String()
		if !strings.Contains(body, `class="filter-bar"`) {
			t.Fatal("页面里没有 .filter-bar：筛选栏整个缺失（02-L P1-12 的原缺陷）")
		}
		if !strings.Contains(body, `name="project"`) {
			t.Fatal("筛选栏里没有 name=\"project\" 的控件：提交出去的 query 与 handler 读的键对不上")
		}
		if !strings.Contains(body, `<option value="p2" selected>活动站</option>`) {
			t.Fatalf("选中的工程没有回显（期望 p2 带 selected），body 片段=%s", optionSnippet(body))
		}
	})

	t.Run("未指定→第一个工程回显", func(t *testing.T) {
		body := renderPagesList(t, h, "").Body.String()
		if !strings.Contains(body, `<option value="p1" selected>官网</option>`) {
			t.Fatalf("未指定工程时应回显默认聚焦的那个，body 片段=%s", optionSnippet(body))
		}
	})
}

func optionSnippet(body string) string {
	idx := strings.Index(body, `name="project"`)
	if idx < 0 {
		return "（没有 name=\"project\" 的控件）"
	}
	end := idx + 400
	if end > len(body) {
		end = len(body)
	}
	return body[idx:end]
}

func TestPagesListEmptyStateDistinguishesFiltered(t *testing.T) {
	t.Run("指定了工程且没有页面→该工程档（含下一步动作）", func(t *testing.T) {
		h, _, _ := newFilterPagesHandle(t, twoProjects())
		body := renderPagesList(t, h, "project=p2").Body.String()

		if !strings.Contains(body, "这个站点工程还没有页面") {
			t.Error("指定工程后没有页面，应给「这个站点工程还没有页面」这一档")
		}
		if !strings.Contains(body, "切换上方") {
			t.Error("该档必须给出下一步动作（换工程 / 就地新建），只说「没有」等于把用户留在这里")
		}

		if !strings.Contains(body, "<thead") || !strings.Contains(body, `colspan="7"`) {
			t.Error("筛选空态把表头一起吃掉了：用户看不到这一页有哪些列")
		}
		if strings.Contains(body, "还没有页面，先用上方表单创建一个") {
			t.Error("同一屏出现了两档空态文案：区分失效（用户不知道该按哪句做）")
		}
	})

	t.Run("没指定工程且没有页面→全站档（文案不变）", func(t *testing.T) {
		h, _, _ := newFilterPagesHandle(t, twoProjects())
		body := renderPagesList(t, h, "").Body.String()

		if !strings.Contains(body, "还没有页面，先用上方表单创建一个") {
			t.Error("未筛选时的空态文案被改掉了：这不是本批要动的东西（两档的第一档保持原样）")
		}
		if strings.Contains(body, "这个站点工程还没有页面") {
			t.Error("没筛过的空态显示了「这个工程还没有页面」：用户会去换一个本来就没有问题的工程")
		}
	})
}

func TestPagesListHidesFilterBarWithSingleProject(t *testing.T) {
	h, _, _ := newFilterPagesHandle(t, []projectcontract.ProjectResp{{ID: "p1", Name: "官网"}})
	body := renderPagesList(t, h, "").Body.String()

	if strings.Contains(body, `class="filter-bar"`) {
		t.Error("只有一个工程时不应渲染筛选栏（恒选项下拉是噪声）")
	}
	if !strings.Contains(body, "还没有页面") {
		t.Error("单工程时列表照常渲染（收口不能过头）")
	}
}

func newLangFilterPagesHandle(t *testing.T, langs []string) (*pagesAdminHandle, *filterPageList) {
	t.Helper()
	themeFor := new(string)
	pages := &filterPageList{}
	h := &pagesAdminHandle{
		projects: filterProjectList{list: oneProject(), themeFor: themeFor, enabled: langs},
		pages:    pages,
	}
	return h, pages
}

func oneProject() []projectcontract.ProjectResp {
	return []projectcontract.ProjectResp{{ID: "p1", Name: "官网"}}
}

func TestPagesListLangFilter(t *testing.T) {
	twoLangs := []string{"zh-CN", "en-US"}

	t.Run("条件进 service", func(t *testing.T) {
		h, pages := newLangFilterPagesHandle(t, twoLangs)
		renderPagesList(t, h, "lang=en-US&state=draft")

		if pages.got == nil {
			t.Fatal("handler 没有调用 service.List")
		}
		if pages.got.Lang != "en-US" || pages.got.LangState != "draft" {
			t.Fatalf("ListReq = {Lang:%q, LangState:%q}，期望 {en-US, draft}", pages.got.Lang, pages.got.LangState)
		}
	})

	t.Run("只给语言时状态留空交给 service 取默认", func(t *testing.T) {
		h, pages := newLangFilterPagesHandle(t, twoLangs)
		renderPagesList(t, h, "lang=en-US")
		if pages.got == nil || pages.got.Lang != "en-US" || pages.got.LangState != "" {
			t.Fatalf("ListReq = %+v，期望 Lang=en-US 且 LangState 留空（默认已发布由 service 判定）", pages.got)
		}
	})

	t.Run("回显：语言与状态都停在已选项", func(t *testing.T) {
		h, _ := newLangFilterPagesHandle(t, twoLangs)
		body := renderPagesList(t, h, "lang=en-US&state=excluded").Body.String()
		if !strings.Contains(body, `<option value="en-US" selected>`) {
			t.Fatal("语言下拉没有回显 en-US：用户改了筛选，下拉却显示别的语言")
		}
		if !strings.Contains(body, `<option value="excluded" selected>`) {
			t.Fatal("状态下拉没有回显 excluded")
		}
	})

	t.Run("未知状态回退成已发布（手改 URL 等于没筛）", func(t *testing.T) {
		h, pages := newLangFilterPagesHandle(t, twoLangs)
		body := renderPagesList(t, h, "lang=en-US&state=%E4%B9%B1%E5%86%99").Body.String()
		if !strings.Contains(body, `<option value="published" selected>`) {
			t.Error("未知状态没有回显到「已发布」：下拉会显示成没有任何一项被选中")
		}
		if pages.got == nil || pages.got.LangState != "乱写" {

			t.Logf("ListReq.LangState = %q（service 负责收敛）", pages.got.LangState)
		}
	})

	t.Run("不存在于启用清单的语言等于没筛", func(t *testing.T) {
		h, pages := newLangFilterPagesHandle(t, twoLangs)
		body := renderPagesList(t, h, "lang=ja-JP").Body.String()
		if pages.got == nil || pages.got.Lang != "ja-JP" {
			t.Fatalf("ListReq.Lang = %q，query 应原样下传（收敛不在控制器）", pages.got.Lang)
		}
		if strings.Contains(body, "这个语言下没有符合状态的页面") {
			t.Error("未启用的语言被当成一次真筛选：空态会说是「这个语言下没有页面」，而用户筛的语言站点根本没启用")
		}
	})

	t.Run("站点只有一种语言→整条语言筛选不渲染", func(t *testing.T) {
		h, _ := newLangFilterPagesHandle(t, []string{"zh-CN"})
		body := renderPagesList(t, h, "").Body.String()
		if strings.Contains(body, `id="lang-filter"`) {
			t.Error("单语言站点仍渲染语言下拉：恒为单值的下拉是噪声，不是能力")
		}
	})
}

func TestPagesListLangRowBadge(t *testing.T) {
	h, pages := newLangFilterPagesHandle(t, []string{"zh-CN", "en-US"})
	pages.rows = []pagedto.PageResp{{
		ID: "pg1", ProjectID: "p1", Kind: "page", DraftPath: "/about",
		DraftVersion: 2, LangStatus: pageenums.LangStatusPublished,
	}}
	body := renderPagesList(t, h, "lang=en-US&state=published").Body.String()
	if !strings.Contains(body, "en-US · 已发布") {
		t.Fatalf("行上没有语言维度徽标（en-US · 已发布），列表页没把 LangScope 渲染到状态列：%s", body)
	}
}

func TestPagesListLangEmptyState(t *testing.T) {
	h, _ := newLangFilterPagesHandle(t, []string{"zh-CN", "en-US"})
	body := renderPagesList(t, h, "lang=en-US&state=published").Body.String()
	if strings.Contains(body, "还没有页面，先用上方表单创建一个") {
		t.Error("筛了语言却给出「还没有页面」：用户会去建重复的页面，而真相是这个语言没排上产出")
	}
}

func TestPagesListActionsOneOpenEntry(t *testing.T) {
	published := pageRowForOpen(true)
	draft := pageRowForOpen(false)

	t.Run("已发布→线上真页面", func(t *testing.T) {
		h, pages, _ := newFilterPagesHandle(t, oneProject())
		pages.rows = []pagedto.PageResp{published}
		body := renderPagesList(t, h, "").Body.String()
		if !strings.Contains(body, `href="/zh/about"`) {
			t.Fatalf("已发布的行没有指向线上产物：%s", body)
		}
		if strings.Contains(body, "/workbench/preview?id=") {
			t.Error("已发布的行仍给了草稿预览入口：同一行两个「看内容」入口又回来了")
		}
	})

	t.Run("未发布→草稿渲染", func(t *testing.T) {
		h, pages, _ := newFilterPagesHandle(t, oneProject())
		pages.rows = []pagedto.PageResp{draft}
		body := renderPagesList(t, h, "").Body.String()
		if !strings.Contains(body, "/workbench/preview?id=pg1") {
			t.Fatalf("没有线上产物的行没有退回草稿渲染：%s", body)
		}
		if strings.Contains(body, `href="/zh/about"`) {
			t.Error("未发布的行链到了线上产物：点开是 404，而列表看起来正常")
		}
	})

	t.Run("缺少真实激活路径不猜URL", func(t *testing.T) {
		h, pages, _ := newFilterPagesHandle(t, oneProject())
		row := published
		row.ActivePath = nil
		pages.rows = []pagedto.PageResp{row}
		body := renderPagesList(t, h, "").Body.String()
		if !strings.Contains(body, "/workbench/preview?id=pg1") || strings.Contains(body, `href="/site/about/"`) {
			t.Fatalf("缺少发布路径必须回草稿入口: %s", body)
		}
	})
	t.Run("两个按钮名都不再出现", func(t *testing.T) {
		h, pages, _ := newFilterPagesHandle(t, oneProject())
		pages.rows = []pagedto.PageResp{published}
		body := renderPagesList(t, h, "").Body.String()
		if !strings.Contains(body, "打开页面") {
			t.Fatalf("没有「打开页面」入口：%s", body)
		}
		if strings.Contains(body, "预览") || strings.Contains(body, ">线上<") {
			t.Error("旧的「预览」「线上」按钮名仍出现在操作列")
		}
	})
}

func pageRowForOpen(active bool) pagedto.PageResp {
	row := pagedto.PageResp{ID: "pg1", ProjectID: "p1", Kind: "page", DraftPath: "/about", DraftVersion: 1}
	if active {
		artifact := "a1"
		row.ActiveArtifactID = &artifact
		activePath := "/zh/about"
		row.ActivePath = &activePath
	}
	return row
}

package pagehttp

// pages_filter_test.go — 列表页筛选栏的硬判据（审计 02-L §2 P1-12）。
//
// 为什么值得单独钉住「条件真的进了查询」：本项目已经有过一次相反的教训（审计 02-L §0.3 M-3）——
// administrators / roles / datarules 三页模板里摆着输入框、提交按钮与 `{{.["FilterX"]}}` 回显位，
// 注释写着「条件进 SQL」，而 handler 从不读 query：控件看起来完全正常，输入什么都不报错，
// 只是**什么都不会发生**。所以下面断言的不是「筛选栏长什么样」，而是条件走完了全程：
//
//	1. 读 query      → ?project= 被 handler 读到；
//	2. 进 service    → 传进 page Service.ListReq.ProjectID（不是只改了模板上的 selected）；
//	3. 回显          → 选中工程在 <option> 上带 selected；
//	4. 与默认值可分  → 未指定时仍是第一个工程（不能把「没筛」当成「筛了第一个」），
//	                   空态据此分档（指定工程的空 ≠ 全站还没有页面）。
//
// 全都用真实 Jet 渲染器（磁盘模板）+ 只覆写 List 的契约替身，不碰数据库。

import (
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

// filterProjectList 站点工程契约替身：只覆写本页真正调用的两个方法。
//
// 内嵌 nil 接口让其余方法自动满足（调用即 panic）—— 本组用例只走 List / GetActiveTheme
// 两条路径，不必为了一个筛选场景手写十几个空方法（与 page_page_err_test.go 的
// failingProjectList 同一手法）。
type filterProjectList struct {
	projectcontract.ProjectService
	list []projectcontract.ProjectResp
	// enabled 站点启用语言清单（nil = 读不到，语言下拉不渲染）。
	enabled []string
	// themeFor 记录「按哪个工程取激活主题」——筛选必须连取数的工程作用域一起换，
	// 只换 List 的 projectID 而主题仍取第一个工程，会列出「B 工程 + A 工程主题」的混合结果。
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

// EnabledLangs 站点启用语言：语言筛选栏的候选来源。
//
// 必须显式实现（内嵌 nil 接口调用即 panic）：默认返回 nil = 「读不到清单」，
// enabledLangsOf 回退成站点默认语言一种 → 语言下拉不渲染，既有用例不受影响。
// 要测语言筛选的用例用 enabled 字段给多语言清单。
func (f filterProjectList) EnabledLangs(context.Context, string) ([]string, error) {
	return f.enabled, nil
}

// filterPageList 页面契约替身：记录 List 收到的请求（这就是「条件真的进了查询」的证据）。
type filterPageList struct {
	pagecontract.PageService
	got  *pagedto.ListReq
	rows []pagedto.PageResp
	// staleRes / staleErr 「全站待重建」区块的取数结果（nil + nil = 读到但为空，默认）。
	staleRes *pagedto.StalePageListResp
	staleErr error
	// staleGot 区块取数收到的请求：全站口径必须 ProjectID 传空（与列表的单工程作用域不同）。
	staleGot *pagedto.StalePageListReq
}

func (f *filterPageList) List(_ context.Context, req *pagedto.ListReq) ([]pagedto.PageResp, error) {
	f.got = req
	return f.rows, nil
}

// ListStalePages 顶部区块的取数。
//
// 必须**显式实现**：内嵌的 nil 接口不会兜住它 —— 方法集包含（提升）不等于有实现，
// 调用即 panic，症状是整组筛选用例以「nil pointer dereference」失败。
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

// staleOverviewTestLimit 替身默认返回的 Limit（与 handler 的 staleOverviewLimit = 8 对齐）。
const staleOverviewTestLimit = 8

// twoProjects 两个工程：筛选栏只在 >1 时渲染（单工程的下拉是噪声）。
func twoProjects() []projectcontract.ProjectResp {
	return []projectcontract.ProjectResp{
		{ID: "p1", Name: "官网"},
		{ID: "p2", Name: "活动站"},
	}
}

// renderPagesList 用真实模板渲染 /admin/pages?<query>。
func renderPagesList(t *testing.T, h *pagesAdminHandle, query string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = newPageErrTestRender(t)
	router.GET("/admin/pages", adminui.PageSlot(PagesListPageTemplate), h.PagesList)

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

// newFilterPagesHandle 组装一个「工程列表 + 页面列表都就绪」的句柄（页面列表默认为空）。
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

// TestPagesListFilterPassesProjectToService 筛选条件必须真的进查询（不是死控件）。
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
		// 回退不算「筛过了」：空态要落「还没有页面」那一档，而不是「这个工程还没有页面」。
		if strings.Contains(rec.Body.String(), "这个站点工程还没有页面") {
			t.Error("伪造 / 陈旧的工程 id 被当成一次真筛选：空态会说「这个工程还没有页面」，而用户并没有筛过")
		}
	})
}

// TestPagesListFilterEchoesSelection 回显：下拉必须停在用户选的那个工程上。
//
// 不回显的筛选栏比没有筛选栏更糟 —— 用户看到的是 A 工程的数据，下拉却显示 B。
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

// optionSnippet 截出下拉区片段，失败信息里能直接看到实际渲染成了什么。
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

// TestPagesListEmptyStateDistinguishesFiltered 两档空态：筛选无结果 ≠ 本来就没有数据。
//
// 判据错了的方向是危险的那一个：把「这个工程还没有页面」显示给「全站一篇页面都没有」的用户，
// 他会一直换工程而想不到要新建。
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
		// 表头仍在（静态门禁脚本管形状，这里管「空态没把表格吃掉」这一条运行时事实）。
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

// TestPagesListHidesFilterBarWithSingleProject 只有一个工程时不渲染筛选栏。
//
// 一个恒选项的下拉不提供任何能力，只占一行高度；这与 blocks / navigations 页
// 对工程切换器（.page-context）的处理一致。
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

// newLangFilterPagesHandle 同 newFilterPagesHandle，但带上站点启用语言清单。
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

// oneProject 单工程：语言维度的用例不该被工程下拉干扰（单工程时工程下拉不渲染）。
func oneProject() []projectcontract.ProjectResp {
	return []projectcontract.ProjectResp{{ID: "p1", Name: "官网"}}
}

// TestPagesListLangFilter 语言维度：条件要真的进查询、要回显、要能分档空态。
//
// 这条轴与工程轴分开测：页面按工程作用域取数是既有语义，语言维度是新加的第二条轴 ——
// 「哪些页面还没出日语」只在这条轴上答得出来，装成死控件（控件在、query 没人读）
// 的失败形态与 02-L §0.3 M-3 那次一模一样。
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
			// query 原样下传是对的：值域收敛在 service（pageenums.LangStatusParse），
			// 控制器只负责把「认不出的东西」交给唯一定义处，不自建第二份白名单。
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

// TestPagesListLangRowBadge 按语言筛选时，行上要说明「这行为什么出现在这里」。
//
// 筛选条件只说「我筛了什么」，徽标才说明「这页在这门语言下是什么状态」——
// 少了它，「已发布」筛选结果里出现一行，用户无从判断它是真已发布还是筛错了。
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

// TestPagesListLangEmptyState 筛了语言却为空时，空态要说清下一步（不是「还没有页面」）。
func TestPagesListLangEmptyState(t *testing.T) {
	h, _ := newLangFilterPagesHandle(t, []string{"zh-CN", "en-US"})
	body := renderPagesList(t, h, "lang=en-US&state=published").Body.String()
	if strings.Contains(body, "还没有页面，先用上方表单创建一个") {
		t.Error("筛了语言却给出「还没有页面」：用户会去建重复的页面，而真相是这个语言没排上产出")
	}
}
// TestPagesListActionsOneOpenEntry 操作列的「看内容」只留一个入口，且已发布/未发布各走各的目标。
//
// 曾经是并排的「预览」（草稿渲染）与「线上」（静态产物）两个按钮。它们回答的是同一个问题
//（这页现在长什么样），差别只在看不看得到线上那份 —— 而这一差别状态列已经写了，
// 并排两个按钮的结果是让用户先猜哪个对应自己想看的那个。
//
// 断言的是**分流**而不是「只有一个 <a>」：数量对、目标错（未发布的行却链到 /site）
// 比两个按钮更糟 —— 点开是 404，而列表看起来完全正常。
func TestPagesListActionsOneOpenEntry(t *testing.T) {
	published := pageRowForOpen(true)
	draft := pageRowForOpen(false)

	t.Run("已发布→线上真页面", func(t *testing.T) {
		h, pages, _ := newFilterPagesHandle(t, oneProject())
		pages.rows = []pagedto.PageResp{published}
		body := renderPagesList(t, h, "").Body.String()
		if !strings.Contains(body, `href="/site/about/"`) {
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
		if strings.Contains(body, `href="/site/about/"`) {
			t.Error("未发布的行链到了线上产物：点开是 404，而列表看起来正常")
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

// pageRowForOpen 造一行列表数据（只填「打开页面」入口判据用到的字段）。
func pageRowForOpen(active bool) pagedto.PageResp {
	row := pagedto.PageResp{ID: "pg1", ProjectID: "p1", Kind: "page", DraftPath: "/about", DraftVersion: 1}
	if active {
		artifact := "a1"
		row.ActiveArtifactID = &artifact
	}
	return row
}


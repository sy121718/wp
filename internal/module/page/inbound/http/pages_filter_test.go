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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
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
	router.GET("/admin/pages", h.PagesList)

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

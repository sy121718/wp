package pagehttp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	pagedto "go_wp/internal/module/page/dto"
	pageservice "go_wp/internal/module/page/service"
	projectcontract "go_wp/internal/module/project/contract"
)

func staleOverviewHandle(t *testing.T, projects []projectcontract.ProjectResp,
	res *pagedto.StalePageListResp, err error) (*pagesAdminHandle, *filterPageList) {
	t.Helper()
	h, pages, _ := newFilterPagesHandle(t, projects)
	pages.staleRes = res
	pages.staleErr = err
	return h, pages
}

func TestPagesStaleOverviewRendersWholeSiteList(t *testing.T) {
	res := &pagedto.StalePageListResp{
		Total: 12, Limit: 8, Truncated: true,
		Pages: []pagedto.StalePageResp{
			{ID: "pg1", ProjectID: "p1", ProjectName: "官网", Title: "首页", Path: "/about", Published: true, Stale: true},
			{ID: "pg2", ProjectID: "p2", ProjectName: "活动站", Path: "/draft-only", Published: false, Stale: true},
		},
	}
	h, pages := staleOverviewHandle(t, twoProjects(), res, nil)

	body := renderPagesList(t, h, "project=p2").Body.String()

	if pages.staleGot == nil {
		t.Fatal("列表页没有调用 ListStalePages：全站待重建区块没接线")
	}
	if pages.staleGot.ProjectID != "" {
		t.Fatalf("区块取数的 ProjectID = %q，期望空串：传了工程 id 就变成「本工程待重建」，与页面列表的单工程口径混成一个数",
			pages.staleGot.ProjectID)
	}
	if pages.staleGot.Limit != staleOverviewLimit {
		t.Fatalf("区块取数的 Limit = %d，期望 %d：不填会落到 model 的 DefaultStaleListLimit = 50，"+
			"50 行折叠清单会把页面列表顶出首屏（审计 02-L P1-10 的原缺陷）", pages.staleGot.Limit, staleOverviewLimit)
	}
	if !pages.staleGot.Descending {
		t.Error("区块取数应按标记时间倒序（最近被标记的排在最前）；默认升序会把最老的一条排在最前")
	}
	for _, want := range []string{
		"全站待重建",
		"口径：全部站点工程",
		"12 个页面有更新未发布",
		"/about", "官网",
		"/draft-only", "活动站",
		"未上线",
		"/workbench?id=pg1",
		"清单只列了前 8 条",
		"</html>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("有 stale 数据时页面缺少 %q", want)
		}
	}
}

func TestPagesStaleOverviewEmptyState(t *testing.T) {
	h, _ := staleOverviewHandle(t, twoProjects(),
		&pagedto.StalePageListResp{Pages: []pagedto.StalePageResp{}, Limit: staleOverviewTestLimit}, nil)
	body := renderPagesList(t, h, "").Body.String()

	if !strings.Contains(body, "当前没有待重建的页面") {
		t.Error("读到空清单时应显示空态（「当前没有待重建的页面」），而不是整块消失")
	}
	if strings.Contains(body, "全站待重建") {
		t.Error("空清单时渲染了折叠清单：一个空的清单块只会占位")
	}
	if strings.Contains(body, "本次读不到") {
		t.Error("空清单不是读取失败：显示失败态会让用户去查日志，而其实没有任何问题")
	}
	if !strings.Contains(body, "</html>") {
		t.Fatalf("空态下整页没有渲染完（缺 </html>）")
	}
}

func TestPagesStaleOverviewUnavailableIsNotZeroConclusion(t *testing.T) {
	raw := `pq: relation "pages" does not exist (SQLSTATE 42P01)`
	h, _ := staleOverviewHandle(t, twoProjects(), nil, errors.New(raw))
	body := renderPagesList(t, h, "").Body.String()

	if !strings.Contains(body, "本次读不到全站待重建清单") {
		t.Error("取数失败必须显示失败态：静默隐藏会让「读不到」看起来像「一切正常」")
	}
	if strings.Contains(body, "当前没有待重建的页面") {
		t.Error("取数失败被渲染成「当前没有待重建的页面」：那是假的乐观结论（把读取失败说成影响面为 0）")
	}
	for _, tok := range []string{"SQLSTATE", `relation "`, "does not exist"} {
		if strings.Contains(body, tok) {
			t.Errorf("响应体泄漏内部细节 %q", tok)
		}
	}
	if !strings.Contains(body, "</html>") {
		t.Fatalf("失败态下整页没有渲染完（缺 </html>）：模板在那一行中断了")
	}
}

func TestPagesStaleOverviewWithoutProjectsIsEmptyNotFailure(t *testing.T) {
	h, _ := staleOverviewHandle(t, nil, nil, pageservice.ErrProjectRequired)
	body := renderPagesList(t, h, "").Body.String()

	if !strings.Contains(body, "当前没有待重建的页面") {
		t.Error("没有站点工程时全站确实没有页面：这是空态")
	}
	if strings.Contains(body, "本次读不到") {
		t.Error("「没有工程」被当成读取失败：用户会去查日志，而问题是他还没建工程")
	}
}

func TestPagesStaleOverviewOptionalKeyKeepsPageIntact(t *testing.T) {
	t.Run("缺键（handler 之外的调用方直接渲染模板）", func(t *testing.T) {
		assertNoStaleOverviewBlock(t, renderPagesBareData(t, barePagesMap()))
	})

	t.Run("键存在但值为 nil", func(t *testing.T) {
		data := barePagesMap()
		data["StaleOverview"] = gin.H(nil)
		assertNoStaleOverviewBlock(t, renderPagesBareData(t, data))
	})

	t.Run("整页装载失败降级渲染", func(t *testing.T) {
		h := &pagesAdminHandle{projects: failingProjectList{
			err: errors.New(`pq: relation "projects" does not exist (SQLSTATE 42P01)`),
		}}
		assertNoStaleOverviewBlock(t, renderPagesList(t, h, "").Body.String())
	})
}

func assertNoStaleOverviewBlock(t *testing.T, body string) {
	t.Helper()
	if !strings.Contains(body, "</html>") {
		t.Fatalf("整页没有渲染完（缺 </html>）：区块的可选键让模板中断了")
	}
	for _, unwanted := range []string{"全站待重建", "本次读不到全站待重建清单", "当前没有待重建的页面"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("没有装配这份数据时渲染出了区块文案 %q", unwanted)
		}
	}
}

func renderPagesBareData(t *testing.T, data gin.H) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = newPageErrTestRender(t)
	router.GET("/admin/pages", func(c *gin.Context) {
		c.HTML(http.StatusOK, "admin/page/pages", adminui.Prepare(c, data))
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/pages", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("渲染 /admin/pages 失败：%d，body=%s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func barePagesMap() gin.H {
	return gin.H{
		"title": "页面管理", "menu": "pages",
		"Projects": []projectcontract.ProjectResp{{ID: "p1", Name: "官网"}},
		"Pages":    []PageRowResp{},
	}
}

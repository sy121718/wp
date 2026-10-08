package pagehttp

// pages_stale_overview_test.go — /admin/pages 顶部「全站待重建」区块的渲染级判据。
//
// 这一块是 page.Service.ListStalePages 的**唯一调用方**（决策简报 02-V §6 的方案 C：
// 能力早写好了却零调用）。它要守住的四件事都**不会**以编译错误或既有测试变红的形式暴露：
//
//  1. 作用域：区块取数必须传空 ProjectID（全部站点工程），而页面列表是单工程聚焦 ——
//     传了工程 id 就悄悄变成「本工程待重建」，页面上两个数看起来都合理，只是含义变了；
//  2. 不降级：ListStalePages 失败必须显示「本次读不到」，**不能**渲染成「当前没有待重建的页面」
//     ——「影响面 0」与「读不到影响面」是两件事，混起来等于把故障静默掉；
//  3. 可选键：StaleOverview 是可选键，缺键（handler 之外的调用方直接渲染模板）时整块不渲染、
//     整页照常渲染完（含 </html>）—— 缺键直接参与 {{if}} 会让模板在那一行中断；
//  4. 不越界：区块只是只读观测，页面列表的行级徽章与 /admin/blocks、/admin/articles 上的
//     就地反馈都不受影响（后两者的文件不在本批清单里，这里只断言本页行为）。
//
// 全部用例走真实 Jet 渲染器（磁盘模板）+ 只覆写 List / ListStalePages 的契约替身，不碰数据库。

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	pagedto "go_wp/internal/module/page/dto"
	pageservice "go_wp/internal/module/page/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/shell"
)

// staleOverviewHandle 组装句柄：工程列表就绪，页面列表为空，stale 取数按参数给定。
//
// 页面列表**故意留空**：区块的计数（全站）与列表的行数（本工程）本来就不是一个数，
// 列表为空而区块有数，正好把「两处的数各自独立」变成可断言的事实。
func staleOverviewHandle(t *testing.T, projects []projectcontract.ProjectResp,
	res *pagedto.StalePageListResp, err error) (*pagesAdminHandle, *filterPageList) {
	t.Helper()
	h, pages, _ := newFilterPagesHandle(t, projects)
	pages.staleRes = res
	pages.staleErr = err
	return h, pages
}

// TestPagesStaleOverviewRendersWholeSiteList 有 stale 数据：区块出现、计数正确、跨工程可辨。
func TestPagesStaleOverviewRendersWholeSiteList(t *testing.T) {
	res := &pagedto.StalePageListResp{
		Total: 12, Limit: 8, Truncated: true,
		Pages: []pagedto.StalePageResp{
			{ID: "pg1", ProjectID: "p1", ProjectName: "官网", Title: "首页", Path: "/about", Published: true, Stale: true},
			{ID: "pg2", ProjectID: "p2", ProjectName: "活动站", Path: "/draft-only", Published: false, Stale: true},
		},
	}
	h, pages := staleOverviewHandle(t, twoProjects(), res, nil)
	// 页面聚焦在 p2：区块仍必须按**全站**口径取数（这是本用例的核心判据）。
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
		"全站待重建",        // 标题写明全站口径（区别于列表的行级徽章）
		"口径：全部站点工程",    // 说明里再点一次，避免读成「本工程的待重建数」
		"12 个页面有更新未发布", // Total（不是清单条数：2 条清单 / 12 是总数）
		"/about", "官网", // 跨工程清单必须带工程名
		"/draft-only", "活动站", // 第二个工程的页面同样在列
		"未上线",               // Published=false 的那条：重建也还不会出现在访问面
		"/workbench?id=pg1", // 行内直达入口
		"清单只列了前 8 条",        // Truncated：被截断的条数必须显式说明
		"</html>",           // 整页渲染完
	} {
		if !strings.Contains(body, want) {
			t.Errorf("有 stale 数据时页面缺少 %q", want)
		}
	}
}

// TestPagesStaleOverviewEmptyState 读到空清单：说清「当前没有待重建的页面」，不渲染折叠清单。
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

// TestPagesStaleOverviewUnavailableIsNotZeroConclusion 取数失败：失败态，不是「0 个待重建」。
//
// 判据错的方向是危险的那一个：把读取失败渲染成空态，运维会看到「一切正常」然后不再去看。
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

// TestPagesStaleOverviewWithoutProjectsIsEmptyNotFailure 「一个工程都没有」是空态，不是失败。
//
// ListStalePages 在工程表为空时按语义返回 ErrProjectRequired（没有可作用域的工程），
// 而那时全站确实没有任何页面 —— 报「读不到」会让用户去查一条并不存在的故障。
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

// TestPagesStaleOverviewOptionalKeyKeepsPageIntact 可选键三档：缺键 / 值为 nil / 装载失败。
//
// 三档都必须**整页渲染完**（含 </html>）：缺键直接参与 {{if}} 会让模板在那一行中断，
// 整页响应变 500 + 通用错误文案（buffer 里的半截内容被丢弃）。
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

// assertNoStaleOverviewBlock 区块不渲染时：整页仍完整，且不出现任何一条区块文案。
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

// renderPagesBareData 用手写数据渲染列表页（模拟 handler 之外的调用方：就近单测 / 片段复用）。
func renderPagesBareData(t *testing.T, data gin.H) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = newPageErrTestRender(t)
	router.GET("/admin/pages", func(c *gin.Context) {
		c.HTML(http.StatusOK, "admin/page/pages", shell.Prepare(c, data))
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/pages", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("渲染 /admin/pages 失败：%d，body=%s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// barePagesMap handler 之外的调用方给得齐的最小键集（**不含** StaleOverview）。
func barePagesMap() gin.H {
	return gin.H{
		"title": "页面管理", "menu": "pages",
		"Projects": []projectcontract.ProjectResp{{ID: "p1", Name: "官网"}},
		"Pages":    []pageRow{},
	}
}

package pagehttp

// page_page_err_test.go — page 域后台页面失败出口的判据守卫（就近单测，不碰数据库）。
//
// 守四件事，每一件错了都**不会报错也不会记日志**，只会在运营眼前少一句话或显示假话：
//
//  1. 判定表（pageErrorStatus）判成业务错误的 sentinel，页面出口（pageFacingText）必须认得出
//     —— 漏一个的症状是「用户能自己修的问题被说成『系统内部错误』」；
//  2. 写侧回执（批量结论文案与自造提示）取词必须非空；
//  3. 缺参写出口真的渲染**整页提示**（HTTP 200 + data-jump-state="err" + 受控文案），
//     而不是 c.String 纯文本页、也不再是 303 + ?err=；
//  4. 手拼的 ?err= / ?done= / ?ok= 不得被渲染成「系统说的话」（读侧判定已随
//     「结论走 shell.RenderJump」整批删除，列表页根本不读这三个键）。
//
// 两条需要真实服务的集成契约（真实 service 抛基础设施错误时的端到端表现）在
// public/test/page/feature/page_error_leak_test.go。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/render"

	pageenums "go_wp/internal/module/page/enums"
	pageservice "go_wp/internal/module/page/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/templates"
	"go_wp/internal/shell"
)

// pageErrCtx 构造一个测试上下文（无 i18n 词条时取词一律回落 fallback，
// 因此这些断言不依赖数据库）。
func pageErrCtx(t *testing.T, rawErr string) *gin.Context {
	t.Helper()
	return pageQueryCtx(t, "err", rawErr)
}

// pageQueryCtx 按 query 键构造上下文（用于验证读侧判定已删除后不再读这些键）。
func pageQueryCtx(t *testing.T, key, raw string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	q := url.Values{}
	if raw != "" {
		q.Set(key, raw)
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/pages?"+q.Encode(), nil)
	return c
}

// TestPageFacingTextCoversBusinessSentinels 判定表与页面出口的双向对账。
//
// 方向：pageErrorStatus 判成 **非 500** 的每个 sentinel，pageFacingText 都要认（ok=true）
// 且给非空文案 —— 缺了就是「业务错误的文案被吞成归口」，用户看到「系统内部错误，
// 请稍后重试」，而实际原因是自己填错了字段。
func TestPageFacingTextCoversBusinessSentinels(t *testing.T) {
	sentinels := []error{
		pageservice.ErrInvalidParam,
		pageservice.ErrInvalidKind,
		pageservice.ErrInvalidDocument,
		pageservice.ErrInvalidPath,
		pageservice.ErrProjectRequired,
		pageservice.ErrPageNotFound,
		pageservice.ErrProjectNotFound,
		pageservice.ErrRollbackTargetMiss,
		pageservice.ErrDraftVersionConflict,
		pageservice.ErrPathOccupied,
		pageservice.ErrRebuildRequired,
		pageservice.ErrNoStagedArtifact,
		// 定时上下线（PIPE-7）：五个业务 sentinel 都要在判定表里被认成非 500，
		// 且文案能被出口认出来 —— 漏一个的症状是「用户能自己修的问题被说成系统内部错误」。
		pageservice.ErrScheduleNotFound,
		pageservice.ErrScheduleInPast,
		pageservice.ErrScheduleActionInvalid,
		pageservice.ErrScheduleRunning,
		pageservice.ErrScheduleOccupied,
	}
	c := pageErrCtx(t, "")
	for _, err := range sentinels {
		if pageErrorStatus(err) == http.StatusInternalServerError {
			t.Errorf("sentinel %v 被判定成 500：页面出口会把它吞成归口文案（用户看到「请稍后重试」，实际是自己填错了）", err)
			continue
		}
		text, ok := pageFacingText(c, err)
		if !ok || strings.TrimSpace(text) == "" {
			t.Errorf("sentinel %v 的业务文案没有被 pageFacingText 认出来（页面上会静默变成归口文案）", err)
		}
	}
}

// TestBulkDeleteResultSentences 批量结论与自造提示都要给出非空整句。
//
// 写侧直接把它们渲染进提示页（见 page_jump.go），读侧判定已随 ?done= 通道删除 ——
// 取词失败会让提示页显示一句空话，用户以为操作成功了。
func TestBulkDeleteResultSentences(t *testing.T) {
	c := pageErrCtx(t, "")
	for _, tc := range []struct{ deleted, skipped int }{{0, 0}, {3, 0}, {0, 3}, {2, 3}} {
		msg := pagesBulkDeleteResult(c, tc.deleted, tc.skipped)
		if strings.TrimSpace(msg) == "" {
			t.Errorf("批量结论为空串（deleted=%d skipped=%d）", tc.deleted, tc.skipped)
		}
	}
	for _, notice := range []pageBulkText{
		pagesLocalNoticeMissingID,
		pagesLocalNoticeMissingPageID,
		pagesLocalNoticeProjectNameRequired,
		pagesLocalNoticePathRequired,
	} {
		if strings.TrimSpace(pageBulkTextOf(c, notice)) == "" {
			t.Errorf("自造回执 %+v 取词为空串（缺词条且 fallback 也为空）", notice)
		}
	}
	// 归口文案也要能取到：装载失败降级渲染时它要能渲染出来。
	if strings.TrimSpace(shell.PageInternalText(c)) == "" {
		t.Error("归口文案取词为空串：装载失败降级渲染的提示会是一条空话")
	}
}

// TestCreateFormFailuresJumpWithNotice 三处缺参出口的形态。
//
// 断言的是**形态**：HTTP 200 + data-jump-state="err" + 本域受控文案 + 回列表页链接。
// 不是 400 纯文本，也不是 303 + ?err= —— 表单是原生 `<form method="post">`，
// 结论由 shell.RenderJump 渲染成整页提示（文案走响应体，不进 URL）。
func TestCreateFormFailuresJumpWithNotice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = newPageErrTestRender(t)
	// 缺参分支在调用 service 之前就返回，因此句柄不需要注入任何契约
	//（这正是「先校验后调用」的一个副产品：这条路径不需要数据库）。
	h := &pagesAdminHandle{}
	engine.POST("/admin/projects/create", h.CreateProject)
	engine.POST("/admin/pages/create", h.CreatePage)

	post := func(path string, form url.Values) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s 应渲染提示页（200），实际 %d，body=%s", path, rec.Code, rec.Body.String())
		}
		return rec
	}

	c := pageErrCtx(t, "")
	cases := []struct {
		name string
		path string
		form url.Values
		want string
	}{
		{
			name: "新建工程缺名称",
			path: "/admin/projects/create",
			form: url.Values{"name": {"   "}},
			want: pageBulkTextOf(c, pagesLocalNoticeProjectNameRequired),
		},
		{
			name: "新建页面缺工程",
			path: "/admin/pages/create",
			form: url.Values{"projectId": {""}, "draftPath": {"/about"}},
			want: pageFacingKey(c, pageenums.ErrProjectRequired),
		},
		{
			name: "新建页面缺路径",
			path: "/admin/pages/create",
			form: url.Values{"projectId": {"p1"}, "draftPath": {"  "}},
			want: pageBulkTextOf(c, pagesLocalNoticePathRequired),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := post(tc.path, tc.form).Body.String()
			if !strings.Contains(body, `data-jump-state="err"`) {
				t.Fatalf("失败应渲染失败提示页（data-jump-state=err）：%s", body)
			}
			if !strings.Contains(body, tc.want) {
				t.Fatalf("提示页应含受控文案 %q", tc.want)
			}
			if !strings.Contains(body, "/admin/pages") {
				t.Fatal("提示页应给回列表页的链接（否则用户只能按后退键）")
			}
			// 三条都是参数级提示，不该带任何内部细节。
			for _, tok := range []string{"SQLSTATE", "relation \"", "constraint", "does not exist"} {
				if strings.Contains(body, tok) {
					t.Fatalf("提示页泄漏内部细节 %q", tok)
				}
			}
		})
	}

	// 两条必填分开报：合成句会让「没选工程」与「没填路径」看起来是同一个问题。
	if pageBulkTextOf(c, pagesLocalNoticeProjectNameRequired) == pageBulkTextOf(c, pagesLocalNoticePathRequired) {
		t.Fatal("缺工程名称与缺页面路径应是两条不同的回执（合成一句等于什么都没说）")
	}
}

// TestSiteSlotPageDataDistinguishesLoadFailure 「这一页没读出来」与「真的没有工程」必须可区分。
//
// 两者的处置完全相反：前者要用户刷新 / 找人看日志，后者要用户先去建一个工程。
// 判据算错的方向是**危险的那个**：把装载失败渲染成「还没有站点工程」，会让用户
// 去建一个本来就存在的工程。
func TestSiteSlotPageDataDistinguishesLoadFailure(t *testing.T) {
	c := pageErrCtx(t, "")
	internal := shell.PageInternalText(c)

	t.Run("真的没有工程", func(t *testing.T) {
		data := siteSlotPageData(nil, "", nil, nil, "")
		if data["NoProjectEmpty"] != true {
			t.Fatalf("工程列表为空且本次没有出错 → 应显示「还没有站点工程」，实际 NoProjectEmpty=%v", data["NoProjectEmpty"])
		}
	})

	t.Run("装载失败", func(t *testing.T) {
		data := siteSlotPageData(nil, "", nil, nil, internal)
		if data["NoProjectEmpty"] != false {
			t.Fatal("装载失败时工程列表同样是空的，但绝不能显示「还没有站点工程」（用户明明有工程）")
		}
		if data["Err"] != internal {
			t.Fatalf("装载失败应把归口文案放进 Err，实际 %v", data["Err"])
		}
	})
}

// TestSiteSlotsPageDegradesOnProjectLoadFailure 装载失败不再脱页壳（原先 c.String(500, …)）。
//
// 用真实模板引擎渲染：判据是「整页渲染完（有 </html>）+ 归口文案在 + 内部细节不在」。
// 少了 </html> 就说明模板在某一行中断了 —— 那种情况 HTTP 仍是 200，
// 从「少了一行数据」的表象几乎定位不到模板中间那一行。
func TestSiteSlotsPageDegradesOnProjectLoadFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = newPageErrTestRender(t)
	h := &siteSlotPageHandle{projects: failingProjectList{err: errors.New(`pq: relation "projects" does not exist (SQLSTATE 42P01)`)}}
	router.GET("/admin/site-slots", h.SiteSlotsPage)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/site-slots", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("装载失败应降级渲染（HTTP 200 + 页壳），实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "</html>") {
		t.Fatalf("页面没有渲染完（缺 </html>）：模板在某一行中断了，body=%s", body)
	}
	if !strings.Contains(body, shell.PageInternalText(pageErrCtx(t, ""))) {
		t.Fatalf("页面上没有归口提示：用户看不出「是这一页没读出来」")
	}
	for _, tok := range []string{"SQLSTATE", "relation \"", "does not exist"} {
		if strings.Contains(body, tok) {
			t.Fatalf("响应体泄漏内部细节 %q", tok)
		}
	}
	if strings.Contains(body, "还没有站点工程") {
		t.Fatalf("装载失败被渲染成「还没有站点工程」：用户会去建一个本来就存在的工程")
	}
	if strings.Contains(body, "先在上面选一个站点工程") {
		t.Fatalf("装载失败被渲染成「先选工程」：用户会去找一个本来就有的工程")
	}
}

// TestPagesListIgnoresForgedNoticeQuery 手拼的 ?err= / ?done= / ?ok= 一律不出现在页面上。
//
// 读侧判定（pagePageErr / pagePageDone / pageNoticeTexts）已随「结论走 shell.RenderJump」
// 整批删除：列表页根本不读这三个键，伪造串自然也不会被渲染成「系统说的话」。
func TestPagesListIgnoresForgedNoticeQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = newPageErrTestRender(t)
	h := &pagesAdminHandle{projects: filterProjectList{list: twoProjects()}, pages: &filterPageList{}}
	router.GET("/admin/pages", h.PagesList)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/pages?err="+url.QueryEscape("<script>alert(1)</script>")+
			"&done="+url.QueryEscape("伪造的批量摘要")+
			"&ok="+url.QueryEscape("伪造的成功提示"), nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("列表页返回 %d", rec.Code)
	}
	body := rec.Body.String()
	for _, forged := range []string{"<script>alert(1)</script>", "伪造的批量摘要", "伪造的成功提示"} {
		if strings.Contains(body, forged) {
			t.Fatalf("伪造的提示 %q 被渲染到页面上 —— 读侧判定没有删干净", forged)
		}
	}
}

// TestPagesListReceiptNoticeHidesWhenObservabilityUnknown 回执状态条不得报假的乐观结论。
//
// 装载失败走降级渲染时 ReceiptPending 等是零值，模板若照旧渲染「发布回执收敛正常」，
// 就是在错误页面上追加一句**明确的假结论**（这一页根本没读到回执状态）。
func TestPagesListReceiptNoticeHidesWhenObservabilityUnknown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = newPageErrTestRender(t)

	var current *pagesPageData
	router.GET("/admin/pages", func(c *gin.Context) {
		c.HTML(http.StatusOK, "admin/page/pages", shell.Prepare(c, current.templateMap()))
	})
	render := func(known, alert bool) string {
		t.Helper()
		current = &pagesPageData{Title: pageenums.MsgPagesTitle, Menu: "pages", ReceiptKnown: known, ReceiptAlert: alert}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/pages", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("渲染失败: %d", rec.Code)
		}
		return rec.Body.String()
	}

	unknown := render(false, false)
	if !strings.Contains(unknown, "</html>") {
		t.Fatalf("装载失败的降级渲染没有渲染完（缺 </html>）")
	}
	if strings.Contains(unknown, "发布回执收敛正常") {
		t.Error("这一页没读到回执状态，却报了「发布回执收敛正常」——那是假的乐观结论")
	}

	fine := render(true, false)
	if !strings.Contains(fine, "发布回执收敛正常") {
		t.Error("装载成功且无积压时应显示「发布回执收敛正常」（收口不能过头）")
	}

	pending := render(true, true)
	if !strings.Contains(pending, "待收敛发布回执") {
		t.Error("有积压时应显示待收敛提示条")
	}
}

// TestPagesListDegradesOnProjectLoadFailure 列表页装载失败不再给一块 JSON。
//
// 原先这里是 `response.ErrorWithMessage(c, 500, …)`：浏览器停在一条 JSON 上，
// 用户既看不到列表，也无从判断「是这一页没读出来、还是整个后台坏了」。
func TestPagesListDegradesOnProjectLoadFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = newPageErrTestRender(t)
	h := &pagesAdminHandle{projects: failingProjectList{err: errors.New(`pq: relation "projects" does not exist (SQLSTATE 42P01)`)}}
	router.GET("/admin/pages", h.PagesList)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/pages", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("装载失败应降级渲染（HTTP 200 + 页壳），实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "</html>") {
		t.Fatalf("页面没有渲染完（缺 </html>）：模板在某一行中断了，body=%s", body)
	}
	if !strings.Contains(body, shell.PageInternalText(pageErrCtx(t, ""))) {
		t.Fatalf("页面上没有归口提示：用户看不出「是这一页没读出来」")
	}
	for _, tok := range []string{"SQLSTATE", "relation \"", "does not exist"} {
		if strings.Contains(body, tok) {
			t.Fatalf("响应体泄漏内部细节 %q", tok)
		}
	}
	if strings.Contains(body, "发布回执收敛正常") {
		t.Fatal("这一页没读到回执状态，却报了「发布回执收敛正常」——那是假的乐观结论")
	}
}

// failingProjectList 只实现 List 的站点工程契约替身。
//
// 内嵌 nil 接口让其余方法自动满足（调用即 panic）——本用例只走 List 这一条路径，
// 这样就不必为了一个「让装载失败」的场景手写十几个空方法。
type failingProjectList struct {
	projectcontract.ProjectService
	err error
}

// List 恒失败：模拟工程表读不出来（表被改名 / 连接掉线这类基础设施故障）。
func (f failingProjectList) List(context.Context) ([]projectcontract.ProjectResp, error) {
	return nil, f.err
}

// newPageErrTestRender 真实 Jet 渲染器（磁盘模板，dev 模式）。
func newPageErrTestRender(t *testing.T) render.HTMLRender {
	t.Helper()
	return templates.NewJetHTMLRender("../../../../templates", true)
}

package pagehttp

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

	"go_wp/internal/adminui"
	pageenums "go_wp/internal/module/page/enums"
	pageservice "go_wp/internal/module/page/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/templates"
)

func pageErrCtx(t *testing.T, rawErr string) *gin.Context {
	t.Helper()
	return pageQueryCtx(t, "err", rawErr)
}

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

func TestBusinessSentinelsAreRecognisable(t *testing.T) {
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
		pageservice.ErrInputMoved,
	}
	c := pageErrCtx(t, "")
	for _, err := range sentinels {
		if pageErrorStatus(err) == http.StatusInternalServerError {
			t.Errorf("sentinel %v 被判定成 500：页面出口会把它吞成归口文案（用户看到「请稍后重试」，实际是自己填错了）", err)
			continue
		}
		fact := adminui.PageErrorFactQuiet(c, pageErrScene, err)
		if !fact.Business || strings.TrimSpace(fact.Key) == "" {
			t.Errorf("sentinel %v 没被判定成业务错误（取值必须是 Err* 常量名形态，页面上会静默变成归口文案）", err)
		}
		if strings.TrimSpace(adminui.ErrorFactText(c, fact)) == "" {
			t.Errorf("sentinel %v 取词为空串（页面上会是一条空话）", err)
		}
	}
}

func TestBulkDeleteResultSentences(t *testing.T) {
	c := pageErrCtx(t, "")
	for _, tc := range []struct{ deleted, skipped int }{{0, 0}, {3, 0}, {0, 3}, {2, 3}} {
		msg := pageMsgText(c, outcomeMsg(pageenums.PagesBulkDeleteOutcome(tc.deleted, tc.skipped)))
		if strings.TrimSpace(msg) == "" {
			t.Errorf("批量结论为空串（deleted=%d skipped=%d）", tc.deleted, tc.skipped)
		}
	}
	for _, notice := range []pageMsg{
		pagesLocalNoticeMissingID,
		pagesLocalNoticeMissingPageID,
		pagesLocalNoticeProjectNameRequired,
		pagesLocalNoticePathRequired,
	} {
		if strings.TrimSpace(pageMsgText(c, notice)) == "" {
			t.Errorf("自造回执 %+v 取词为空串（缺词条且 fallback 也为空）", notice)
		}
	}

	if strings.TrimSpace(adminui.PageInternalText(c)) == "" {
		t.Error("归口文案取词为空串：装载失败降级渲染的提示会是一条空话")
	}
}

func TestCreateFormFailuresJumpWithNotice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = newPageErrTestRender(t)

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
			want: pageMsgText(c, pagesLocalNoticeProjectNameRequired),
		},
		{
			name: "新建页面缺工程",
			path: "/admin/pages/create",
			form: url.Values{"projectId": {""}, "draftPath": {"/about"}},
			want: pageMsgText(c, pageMsg{Key: pageenums.ErrProjectRequired, Fallback: pageenums.ErrProjectRequired}),
		},
		{
			name: "新建页面缺路径",
			path: "/admin/pages/create",
			form: url.Values{"projectId": {"p1"}, "draftPath": {"  "}},
			want: pageMsgText(c, pagesLocalNoticePathRequired),
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

			for _, tok := range []string{"SQLSTATE", "relation \"", "constraint", "does not exist"} {
				if strings.Contains(body, tok) {
					t.Fatalf("提示页泄漏内部细节 %q", tok)
				}
			}
		})
	}

	if pageMsgText(c, pagesLocalNoticeProjectNameRequired) == pageMsgText(c, pagesLocalNoticePathRequired) {
		t.Fatal("缺工程名称与缺页面路径应是两条不同的回执（合成一句等于什么都没说）")
	}
}

func TestPagesListIgnoresForgedNoticeQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = newPageErrTestRender(t)
	h := &pagesAdminHandle{projects: filterProjectList{list: twoProjects()}, pages: &filterPageList{}}
	router.GET("/admin/pages", adminui.PageSlot(PagesListPageTemplate), h.PagesList)

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

func TestPagesListReceiptNoticeHidesWhenObservabilityUnknown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = newPageErrTestRender(t)

	var current *pagesPageData
	router.GET("/admin/pages", func(c *gin.Context) {
		c.HTML(http.StatusOK, "admin/page/pages", adminui.PrepareView(c, current))
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

func TestPagesListDegradesOnProjectLoadFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = newPageErrTestRender(t)
	h := &pagesAdminHandle{projects: failingProjectList{err: errors.New(`pq: relation "projects" does not exist (SQLSTATE 42P01)`)}}
	router.GET("/admin/pages", adminui.PageSlot(PagesListPageTemplate), h.PagesList)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/pages", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("装载失败应降级渲染（HTTP 200 + 页壳），实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "</html>") {
		t.Fatalf("页面没有渲染完（缺 </html>）：模板在某一行中断了，body=%s", body)
	}
	if !strings.Contains(body, adminui.PageInternalText(pageErrCtx(t, ""))) {
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

type failingProjectList struct {
	projectcontract.ProjectService
	err error
}

func (f failingProjectList) List(context.Context) ([]projectcontract.ProjectResp, error) {
	return nil, f.err
}

func newPageErrTestRender(t *testing.T) render.HTMLRender {
	t.Helper()
	return templates.NewJetHTMLRender("../../../../templates", true)
}

package pagehttp

// page_page_err_test.go — page 域后台页面失败出口的判据守卫（就近单测，不碰数据库）。
//
// 守五件事，每一件错了都**不会报错也不会记日志**，只会在运营眼前少一句话或显示假话：
//
//  1. 判定表（pageErrorStatus）判成业务错误的 sentinel，其文案 key 必须在读侧白名单
//     （pageFacingKeys）里 —— 漏一个的症状是「用户能自己修的问题被说成『系统内部错误』」；
//  2. 写侧回执（pagesLocalNotices 与批量结论文案）必须能被读侧原样读回 ——
//     漏一个的症状是「写侧发了提示、页面上静默无提示」；
//  3. 新增一条自造回执却忘了登记进 pagesLocalNotices —— 同上，但连线索都没有；
//  4. 3 处写侧出口真的落在 303 + ?err=（而不是 c.String 的纯文本页）、装载失败真的
//     降级渲染（HTTP 200 + 完整页壳）；
//  5. 手拼的 ?err= 不得被渲染成「系统说的话」、装载失败不得报出假的乐观结论。
//
// 两条需要真实服务的集成契约（真实 service 抛基础设施错误时的端到端表现）在
// public/test/page/feature/page_error_leak_test.go（那边整包依赖 CompilePreview 的签名，
// 当前工作树里它有未收敛的改动，与本批无关）。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/render"

	pageenums "go_wp/internal/module/page/enums"
	pageservice "go_wp/internal/module/page/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/templates"
	"go_wp/internal/web/shell"
)

// pageErrCtx 构造一个带 ?err= 的测试上下文（无 i18n 词条时取词一律回落 fallback，
// 因此这些断言不依赖数据库）。
func pageErrCtx(t *testing.T, rawErr string) *gin.Context {
	t.Helper()
	return pageQueryCtx(t, "err", rawErr)
}

// pageDoneCtx 构造一个带 ?done= 的测试上下文（成功回执走的是另一条 query 通道）。
func pageDoneCtx(t *testing.T, rawDone string) *gin.Context {
	t.Helper()
	return pageQueryCtx(t, "done", rawDone)
}

// pageQueryCtx 按 query 键构造上下文（读侧两条通道的候选集合相同，取值键不同）。
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

// TestPageFacingKeysCoverErrorStatusOutputs 判定表与读侧白名单的双向对账。
//
// 方向一：pageErrorStatus 判成 **非 500** 的每个 sentinel，它的文案 key 都必须在
// pageFacingKeys 里 —— 缺了就是「业务错误的文案被读侧判成伪造/归口」，
// 用户看到「系统内部错误，请稍后重试」，而实际原因是自己填错了字段。
//
// 方向二（反向）：pageFacingKeys 里的每个 key 都必须真的能被读侧候选取到 ——
// 白名单里挂着一条永远取不到的 key，等于给「文案被吞」留了一个看不见的坑。
func TestPageFacingKeysCoverErrorStatusOutputs(t *testing.T) {
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
		// 且文案 key 在读侧白名单里 —— 漏一个的症状是「用户能自己修的问题被说成系统内部错误」。
		pageservice.ErrScheduleNotFound,
		pageservice.ErrScheduleInPast,
		pageservice.ErrScheduleActionInvalid,
		pageservice.ErrScheduleRunning,
		pageservice.ErrScheduleOccupied,
	}
	registered := make(map[string]bool, len(pageFacingKeys))
	for _, k := range pageFacingKeys {
		registered[k] = true
	}

	c := pageErrCtx(t, "")
	for _, err := range sentinels {
		if pageErrorStatus(err) == http.StatusInternalServerError {
			t.Errorf("sentinel %v 被判定成 500：页面出口会把它吞成归口文案（用户看到「请稍后重试」，实际是自己填错了）", err)
			continue
		}
		key := err.Error()
		if !registered[key] {
			t.Errorf("判定表的产物 %q 不在 pageFacingKeys 里：这个业务错误在页面上会静默变成归口文案", key)
			continue
		}
		// 方向二：读侧候选必须真的取得到它（当前语言译文形态）。
		text := pageFacingKey(c, key)
		if text == "" {
			t.Errorf("key %q 取词为空串：页面上会出现一条空提示（用户以为操作成功了）", key)
			continue
		}
		if got := shell.FacingNotice(text, pageNoticeTexts(c)); got == "" {
			t.Errorf("key %q 的译文 %q 不在 pageNoticeTexts 的候选里：写侧发了提示、读侧会判成伪造", key, text)
		}
	}
}

// TestPageLocalNoticesSurviveReadSide 写侧自造回执 → 读侧必须原样读回。
//
// 遍历 pagesLocalNotices（写读共用的那一份）而不是在测试里手抄文案清单：
// 手抄的那一份在有人改了候选结构时会静默失配，而这条断言正是为了抓失配。
func TestPageLocalNoticesSurviveReadSide(t *testing.T) {
	c := pageErrCtx(t, "")
	for _, notice := range pagesLocalNotices {
		text := pageBulkTextOf(c, notice)
		if strings.TrimSpace(text) == "" {
			t.Errorf("候选 %+v 取词为空串（缺词条且 fallback 也为空）", notice)
			continue
		}
		if got := pagePageErr(pageErrCtx(t, text)); got != text {
			t.Errorf("写侧回执 %q 被读侧判成伪造：pagePageErr = %q（症状是页面上静默无提示）", text, got)
		}
	}

	// 批量结论文案走真实写侧拼装（含计数），成功回执走 ?done= 通道。
	for _, tc := range []struct{ deleted, skipped int }{{0, 0}, {3, 0}, {0, 3}, {2, 3}} {
		msg := pagesBulkDeleteResult(c, tc.deleted, tc.skipped)
		if got := pagePageDone(pageDoneCtx(t, msg)); got != msg {
			t.Errorf("批量回执 %q 被读侧判成伪造：pagePageDone = %q", msg, got)
		}
	}

	// 归口文案本身也在候选里：装载失败降级渲染时它要能渲染出来。
	internal := shell.PageInternalText(c)
	if got := pagePageErr(pageErrCtx(t, internal)); got != internal {
		t.Errorf("归口文案 %q 不在读侧候选里：装载失败降级渲染的提示会被判成伪造", internal)
	}
}

// pagesLocalNoticeUsePattern 抓写侧对「自造回执」变量的引用（pagesLocalNotice*）。
var pagesLocalNoticeUsePattern = regexp.MustCompile(`\bpagesLocalNotice[A-Za-z]+\b`)

// TestPagesLocalNoticeIdentifiersAreRegistered 新增一条自造回执却忘了登记 → 立刻红。
//
// 判据不是「文案长什么样」而是**标识符有没有进 pagesLocalNotices 切片**：
// 只有进了切片，读侧 pagePageErr 才认它。漏登记的症状与「文案写错」完全一样
// （页面上什么都不显示），而排查时看代码却一切正常 —— 所以必须在编译测试里钉住。
func TestPagesLocalNoticeIdentifiersAreRegistered(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读取本包目录失败: %v", err)
	}
	used := map[string]bool{}
	var src strings.Builder
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		b, rerr := os.ReadFile(filepath.Join(".", e.Name()))
		if rerr != nil {
			t.Fatalf("读 %s 失败: %v", e.Name(), rerr)
		}
		src.Write(b)
		src.WriteString("\n")
		for _, m := range pagesLocalNoticeUsePattern.FindAllString(string(b), -1) {
			used[m] = true
		}
	}
	if len(used) == 0 {
		t.Fatal("没有抓到任何 pagesLocalNotice* 标识符：命名变了要同步改这条测试的判据")
	}

	// 从 pagesLocalNotices 的定义处取「已登记」集合（解析 Go 源文本，避免手抄一份清单）。
	registered := map[string]bool{}
	phase := 0
	for _, line := range strings.Split(src.String(), "\n") {
		if strings.Contains(line, "var pagesLocalNotices = []pageBulkText{") {
			phase = 1
			continue
		}
		if phase == 1 {
			if strings.Contains(line, "}") {
				break
			}
			for _, m := range pagesLocalNoticeUsePattern.FindAllString(line, -1) {
				registered[m] = true
			}
		}
	}
	if len(registered) == 0 {
		t.Fatal("没有解析出 pagesLocalNotices 的登记清单：定义形态变了要同步改这条测试")
	}

	for name := range used {
		if name == "pagesLocalNotices" {
			continue
		}
		if !registered[name] {
			t.Errorf("%s 被写侧引用但没有登记进 pagesLocalNotices：读侧 pagePageErr 会把它判成伪造，页面上静默无提示", name)
		}
	}
}

// TestCreateFormFailuresRedirectWithNoticeCreateProjectCreatePage 三处缺参出口的形态。
//
// 断言的是**形态**：303 + 回 /admin/pages + ?err=<本域受控文案>。
// 不是 400 纯文本，也不是 JSON —— 表单是原生 `<form method="post">`，收到片段或 JSON
// 会把用户导航到一块不是页面的东西上，抽屉里填的内容同时丢失。
func TestCreateFormFailuresRedirectWithNotice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// 缺参分支在调用 service 之前就返回，因此句柄不需要注入任何契约
	//（这正是「先校验后调用」的一个副产品：这条路径不需要数据库）。
	h := &pagesAdminHandle{}
	engine.POST("/admin/projects/create", h.CreateProject)
	engine.POST("/admin/pages/create", h.CreatePage)

	post := func(path string, form url.Values) (*httptest.ResponseRecorder, string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("%s 应为 303（PRG），实际 %d，body=%s", path, rec.Code, rec.Body.String())
		}
		u, perr := url.Parse(rec.Header().Get("Location"))
		if perr != nil {
			t.Fatalf("Location 无法解析: %v（%s）", perr, rec.Header().Get("Location"))
		}
		if u.Path != "/admin/pages" {
			t.Fatalf("%s 应回到列表页 /admin/pages，实际 %s", path, u.Path)
		}
		return rec, u.Query().Get("err")
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
			_, errText := post(tc.path, tc.form)
			if errText == "" {
				t.Fatal("303 上没有 ?err=：用户回到列表页却看不到「刚才那一下为什么没成」")
			}
			if errText != tc.want {
				t.Fatalf("?err= 应为写侧的受控文案 %q，实际 %q", tc.want, errText)
			}
			// 回带文案必须能被读侧读回（否则写侧发了、页面上没有）。
			if got := pagePageErr(pageErrCtx(t, errText)); got != errText {
				t.Fatalf("回带的 ?err=%q 被读侧判成伪造（got %q）", errText, got)
			}
			// 三条都是参数级提示，不该带任何内部细节。
			for _, tok := range []string{"SQLSTATE", "relation \"", "constraint", "does not exist"} {
				if strings.Contains(errText, tok) {
					t.Fatalf("?err= 泄漏内部细节 %q：%q", tok, errText)
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
		data := siteSlotPageData(nil, "", nil, nil, "", "")
		if data["NoProjectEmpty"] != true {
			t.Fatalf("工程列表为空且本次没有出错 → 应显示「还没有站点工程」，实际 NoProjectEmpty=%v", data["NoProjectEmpty"])
		}
	})

	t.Run("装载失败", func(t *testing.T) {
		data := siteSlotPageData(nil, "", nil, nil, internal, "")
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

// TestPagePageErrRejectsForgedNotice 手拼的 ?err= 不得被渲染成「系统说的话」。
//
// 未命中落的是**归口文案**而不是空串（本域 ?err= 的兜底语义：读不出来也得告诉用户
// 「出过事」），所以判据是「渲染出来的不是伪造串本身」。
//
// 特别挡住 `strings.Contains` 那种写法：只要夹带一段已知文案就能把任意前缀 / 后缀
// 显示到页面上，而那些内容看起来同样像系统提示。
func TestPagePageErrRejectsForgedNotice(t *testing.T) {
	internal := shell.PageInternalText(pageErrCtx(t, ""))
	for _, raw := range []string{
		"这条提示是我手写的",
		internal + " 附带一段伪造内容",
		internal + "\n第二行（把一条提示拆成两条系统消息的观感）",
		"<script>alert(1)</script>",
	} {
		got := pagePageErr(pageErrCtx(t, raw))
		if got == raw {
			t.Errorf("手拼的 ?err=%q 被原样渲染成了「系统提示」", raw)
		}
		if got != internal {
			t.Errorf("未命中白名单应落归口文案 %q，实际 %q（raw=%q）", internal, got, raw)
		}
	}

	// 反向：服务端允许的「候选 + ：明细」形态仍然放行（那是写侧自己拼的定位信息），
	// 收口过头会把这一类真实提示也吞掉。
	if got := pagePageErr(pageErrCtx(t, internal+"：写侧补的定位信息")); got == "" {
		t.Error("「候选 + ：明细」是服务端允许的形态，不该被收成空串")
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

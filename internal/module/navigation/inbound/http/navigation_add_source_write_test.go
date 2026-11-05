package navigationhttp

import (
	"context"
	"errors"
	"go_wp/pkg/i18n"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

"go_wp/internal/templates"
"go_wp/internal/text"
"go_wp/internal/adminui"
pageview "go_wp/pkg/view"
"net/url"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/pkg/bulkids"
)

type navigationAddSourceStub struct {
	navigationcontract.NavigationService
	groups    []navigationcontract.SourceGroup
	groupsErr error

	groupCalls  []string
	createCalls []*navigationdto.CreateReq
	failFor     map[string]error
}

func (s *navigationAddSourceStub) SourceGroups(_ context.Context, projectID string) ([]navigationcontract.SourceGroup, error) {
	s.groupCalls = append(s.groupCalls, projectID)
	if s.groupsErr != nil {
		return nil, s.groupsErr
	}
	return s.groups, nil
}

func (s *navigationAddSourceStub) Create(_ context.Context, req *navigationdto.CreateReq) (*navigationdto.NavigationResp, error) {
	s.createCalls = append(s.createCalls, req)
	if req.SourceID != nil {
		if err, ok := s.failFor[*req.SourceID]; ok {
			return nil, err
		}
	}
	return &navigationdto.NavigationResp{ID: "new-" + req.Title}, nil
}

func newNavigationAddSourceRouter(t *testing.T, svc navigationcontract.NavigationService) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	engine.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	engine.Use(pageview.UseSuccess(adminui.NewSuccessWriter()))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.POST("/api/navigation/sources", pageview.HTMLFragment, NewHandle(svc).CreateFromSource)
	return engine
}

func postNavigationAddSource(t *testing.T, engine *gin.Engine, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/sources", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func navigationAddSourceOK(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("应 200 + HTML 片段，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("应回 HTML 片段，实际 Content-Type=%q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `role="status"`) {
		t.Fatalf("成功应回 Jet 成功片段：%s", body)
	}
	return body
}

// addSourceItemMessage 从结果片段里取某条跳过项的原因。
func addSourceItemMessage(t *testing.T, body, id string) string {
	t.Helper()
	marker := id + "："
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("片段里没有 id=%s 的逐条结果：%s", id, body)
	}
	rest := body[i+len(marker):]
	if end := strings.Index(rest, "<"); end >= 0 {
		rest = rest[:end]
	}
	return strings.TrimSpace(rest)
}

type addSourceItem struct {
	ok      bool
	message string
}

var addSourceItemRe = regexp.MustCompile(`data-item-id="([^"]*)" data-item-ok="([01])"`)

// addSourceItems 从结果片段读回逐条事实：ID、是否成功、失败原因。
func addSourceItems(t *testing.T, body string) map[string]addSourceItem {
	t.Helper()
	out := map[string]addSourceItem{}
	for _, m := range addSourceItemRe.FindAllStringSubmatch(body, -1) {
		item := addSourceItem{ok: m[2] == "1"}
		if !item.ok {
			item.message = addSourceItemMessage(t, body, m[1])
		}
		out[m[1]] = item
	}
	return out
}

// addSourceCounts 从结果片段的 data 属性读回计数，断言真实发生的数字。
func addSourceCounts(t *testing.T, body string) (added, skipped int) {
	t.Helper()
	m := regexp.MustCompile(`data-added="(\d+)" data-skipped="(\d+)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("结果片段缺少计数属性：%s", body)
	}
	added, _ = strconv.Atoi(m[1])
	skipped, _ = strconv.Atoi(m[2])
	return added, skipped
}

func addSourceGroups() []navigationcontract.SourceGroup {
	return []navigationcontract.SourceGroup{
		{Type: "page", Title: "admin.navigations.source.page", Items: []navigationcontract.SourceCandidate{
			{ID: "pg-1", Label: "/about", Title: "关于我们", URL: "/about"},
			{ID: "pg-2", Label: "/draft", Title: "草稿页", URL: ""},
		}},
		{Type: "article", Title: "admin.navigations.source.article", Items: []navigationcontract.SourceCandidate{
			{ID: "ar-1", Label: "hello", Title: "你好世界", URL: "/blog/hello"},
		}},
	}
}

func addSourceForm(ids ...string) url.Values {
	form := url.Values{}
	form.Set("projectId", "p1")
	form.Set("kind", "header")
	form.Set("sourceType", "page")
	for _, id := range ids {
		form.Add("ids", id)
	}
	return form
}

func TestNavigationAddSourceBulkShape(t *testing.T) {
	stub := &navigationAddSourceStub{groups: addSourceGroups()}
	engine := newNavigationAddSourceRouter(t, stub)

	rec := postNavigationAddSource(t, engine, addSourceForm("pg-1", "pg-2", "ar-1", "nope"))
	if rec.Code != http.StatusOK {
		t.Fatalf("来源批量应 200（逐条结果在 data 里），实际 %d：%s", rec.Code, rec.Body.String())
	}
	if got := strings.Join(stub.groupCalls, ","); got != "p1" {
		t.Fatalf("应按工程读一次候选，实际 %v", stub.groupCalls)
	}
	if len(stub.createCalls) != 1 {
		t.Fatalf("只有 pg-1 该落到 create，实际 %d 次：%+v", len(stub.createCalls), stub.createCalls)
	}
	req := stub.createCalls[0]
	if req.ProjectID != "p1" || req.Kind != "header" || req.SourceType != "page" || req.Path != "/about" ||
		req.Title != "关于我们" || req.SourceID == nil || *req.SourceID != "pg-1" {
		t.Fatalf("create 入参应逐字透传候选事实，实际 %+v", req)
	}
	body := navigationAddSourceOK(t, rec)
	added, skipped := addSourceCounts(t, body)
	if added != 1 || skipped != 3 {
		t.Fatalf("回执应为 added=1 skipped=3，实际 added=%d skipped=%d", added, skipped)
	}
	items := addSourceItems(t, body)
	if len(items) != 4 {
		t.Fatalf("逐条结果应为 4 条，实际 %d：%s", len(items), body)
	}
	if !items["pg-1"].ok || items["pg-1"].message != "" {
		t.Fatalf("第 1 条应标记为已加入且无原因，实际 %+v", items["pg-1"])
	}
	for id, item := range items {
		if id == "pg-1" {
			continue
		}
		if item.ok || item.message == "" {
			t.Fatalf("跳过项必须带上原因（前端逐条列出来），实际 %s=%+v", id, item)
		}
	}
}

func TestNavigationAddSourceTitleFallsBackToLabel(t *testing.T) {
	stub := &navigationAddSourceStub{groups: []navigationcontract.SourceGroup{
		{Type: "page", Items: []navigationcontract.SourceCandidate{
			{ID: "pg-1", Label: "/about", Title: "", URL: "/about"},
		}},
	}}
	engine := newNavigationAddSourceRouter(t, stub)

	rec := postNavigationAddSource(t, engine, addSourceForm("pg-1"))
	if rec.Code != http.StatusOK {
		t.Fatalf("应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if len(stub.createCalls) != 1 || stub.createCalls[0].Title != "/about" {
		t.Fatalf("标题应回退到 Label，实际 %+v", stub.createCalls)
	}
}

func TestNavigationAddSourceKindSemantics(t *testing.T) {
	cases := []struct {
		name    string
		kind string
		want string
	}{
		{"缺省回落 header", "", "header"},
		{"空串回落 header", "", "header"},
		{"显式 footer", "footer", "footer"},
		{"不在枚举里的类型回落 header", "sidebar", "header"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &navigationAddSourceStub{groups: addSourceGroups()}
			engine := newNavigationAddSourceRouter(t, stub)
			form := addSourceForm("pg-1")
			form.Set("kind", tc.kind)
			rec := postNavigationAddSource(t, engine, form)
			if rec.Code != http.StatusOK {
				t.Fatalf("应 200，实际 %d：%s", rec.Code, rec.Body.String())
			}
			if len(stub.createCalls) != 1 || stub.createCalls[0].Kind != tc.want {
				t.Fatalf("kind 应为 %q，实际 %+v", tc.want, stub.createCalls)
			}
		})
	}
}

func TestNavigationAddSourceCreateFailureIsPerItem(t *testing.T) {
	stub := &navigationAddSourceStub{
		groups: []navigationcontract.SourceGroup{
			{Type: "page", Items: []navigationcontract.SourceCandidate{
				{ID: "pg-1", Title: "关于我们", URL: "/about"},
				{ID: "pg-3", Title: "联系", URL: "/contact"},
			}},
		},
		failFor: map[string]error{"pg-1": errors.New(navigationenums.ErrPathTaken)},
	}
	engine := newNavigationAddSourceRouter(t, stub)

	rec := postNavigationAddSource(t, engine, addSourceForm("pg-1", "pg-3"))
	if rec.Code != http.StatusOK {
		t.Fatalf("应 200（逐条结果），实际 %d：%s", rec.Code, rec.Body.String())
	}
	if len(stub.createCalls) != 2 {
		t.Fatalf("一条失败不应中断整批，实际调了 %d 次", len(stub.createCalls))
	}
	body := navigationAddSourceOK(t, rec)
	added, skipped := addSourceCounts(t, body)
	if added != 1 || skipped != 1 {
		t.Fatalf("回执应为 added=1 skipped=1，实际 added=%d skipped=%d", added, skipped)
	}
	items := addSourceItems(t, body)
	if items["pg-1"].ok || !items["pg-3"].ok {
		t.Fatalf("第 1 条应失败、第 2 条应成功，实际 %+v", items)
	}
}

func TestNavigationAddSourceReasonsAreControlled(t *testing.T) {
	rawErr := errors.New(`ERROR: duplicate key value violates unique constraint "uq_navigation_path" (SQLSTATE 23505)`)
	stub := &navigationAddSourceStub{
		groups: []navigationcontract.SourceGroup{
			{Type: "page", Items: []navigationcontract.SourceCandidate{
				{ID: "pg-1", Title: "关于我们", URL: "/about"},
				{ID: "pg-2", Title: "草稿页", URL: ""},
			}},
		},
		failFor: map[string]error{"pg-1": rawErr},
	}
	engine := newNavigationAddSourceRouter(t, stub)

	rec := postNavigationAddSource(t, engine, addSourceForm("pg-1", "pg-2", "nope"))
	if rec.Code != http.StatusOK {
		t.Fatalf("应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := navigationAddSourceOK(t, rec)
	added, skipped := addSourceCounts(t, body)
	if added != 0 || skipped != 3 {
		t.Fatalf("三条都应跳过，实际 added=%d skipped=%d", added, skipped)
	}
	for _, leak := range []string{"SQLSTATE", "uq_navigation_path", "ERROR:", "duplicate key"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Fatalf("回执泄漏了原始异常（%s）：%s", leak, rec.Body.String())
		}
	}

	items := addSourceItems(t, body)
	if got := items["pg-1"].message; got == "" || got == rawErr.Error() {
		t.Fatalf("第 1 条原因应归口，实际 %q", got)
	}
	for _, id := range []string{"pg-2", "nope"} {
		if items[id].message == "" {
			t.Fatalf("跳过项 %s 必须带原因，实际 %+v", id, items)
		}
	}
}

func TestNavigationAddSourceReasonsFollowRequestLanguage(t *testing.T) {
	injectNavigationEntries(t, map[string]map[string]string{
		navigationenums.ErrNotFound:  {"zh-CN": "找不到该条目", "en-US": "No such item"},
		navigationAddSourceNoPathKey: {"zh-CN": "该内容暂无公开路径", "en-US": "No public path yet"},
		navigationenums.ErrPathTaken: {"zh-CN": "路径已被占用", "en-US": "Path already taken"},
	})
	stub := &navigationAddSourceStub{
		groups:  addSourceGroups(),
		failFor: map[string]error{},
	}
	engine := newNavigationAddSourceRouter(t, stub)

	post := func(lang string) map[string]addSourceItem {
		t.Helper()
		form := addSourceForm("pg-2", "ar-1", "nope")
		req := httptest.NewRequest(http.MethodPost, "/api/navigation/sources",
			strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept-Language", lang)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s：应 200，实际 %d：%s", lang, rec.Code, rec.Body.String())
		}
		body := navigationAddSourceOK(t, rec)
		return addSourceItems(t, body)
	}

	zh := post("zh-CN")
	if got := zh["pg-2"].message; got != "该内容暂无公开路径" {
		t.Fatalf("中文请求应拿中文原因，实际 %q", got)
	}
	if zh["ar-1"].message != "找不到该条目" || zh["nope"].message != "找不到该条目" {
		t.Fatalf("不属于本类型 / 不在候选里都应给「不存在」原因，实际 %+v", zh)
	}
	en := post("en-US")
	if en["pg-2"].message != "No public path yet" || en["ar-1"].message != "No such item" {
		t.Fatalf("英文请求应拿英文原因，实际 %+v", en)
	}
}

func TestNavigationAddSourceEmptyIDsIsNoop(t *testing.T) {
	stub := &navigationAddSourceStub{groups: addSourceGroups()}
	engine := newNavigationAddSourceRouter(t, stub)

	rec := postNavigationAddSource(t, engine, addSourceForm())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("空 ids 应 400（没有任何候选可处理），实际 %d：%s", rec.Code, rec.Body.String())
	}
	if len(stub.createCalls) != 0 || len(stub.groupCalls) != 0 {
		t.Fatalf("空批量不该读候选 / 写库，实际 groups=%v creates=%d", stub.groupCalls, len(stub.createCalls))
	}
}

func TestNavigationAddSourceUnknownTypeSkipsAll(t *testing.T) {
	stub := &navigationAddSourceStub{groups: addSourceGroups()}
	engine := newNavigationAddSourceRouter(t, stub)

	blockForm := addSourceForm("pg-1", "pg-2")
	blockForm.Set("sourceType", "block")
	blockForm.Del("kind")
	rec := postNavigationAddSource(t, engine, blockForm)
	if rec.Code != http.StatusOK {
		t.Fatalf("候选为空是合法结果，应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if len(stub.createCalls) != 0 {
		t.Fatalf("类型不匹配不该落库，实际 %+v", stub.createCalls)
	}
	body := navigationAddSourceOK(t, rec)
	added, skipped := addSourceCounts(t, body)
	if added != 0 || skipped != 2 {
		t.Fatalf("应 added=0 skipped=2，实际 added=%d skipped=%d", added, skipped)
	}
}

func TestNavigationAddSourceGroupsFailureIs500(t *testing.T) {
	stub := &navigationAddSourceStub{groupsErr: errors.New(`relation "navigations" does not exist`)}
	engine := newNavigationAddSourceRouter(t, stub)

	rec := postNavigationAddSource(t, engine, addSourceForm("pg-1"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("读候选失败应 500，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if len(stub.createCalls) != 0 {
		t.Fatalf("读候选失败必须零写入，实际 %+v", stub.createCalls)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-ui-error`) {
		t.Fatalf("失败应渲染 Jet 错误片段：%s", body)
	}
	if strings.Contains(body, "navigations") || strings.Contains(body, "SQLSTATE") {
		t.Fatalf("500 回执不该带原始异常：%s", body)
	}
}

func TestNavigationAddSourceShapeRejections(t *testing.T) {
	// 表单编码下所有值都是字符串，形状错误只剩「必填缺失 / 值为空」两类；
	// 类型严格性（null / 数字 / 对象）是 JSON 时代的概念，不再适用。
	cases := []struct {
		name string
		form url.Values
	}{
		{"缺 projectId", url.Values{"sourceType": {"page"}, "ids": {"pg-1"}}},
		{"projectId 空串", url.Values{"projectId": {""}, "sourceType": {"page"}, "ids": {"pg-1"}}},
		{"缺 sourceType", url.Values{"projectId": {"p1"}, "ids": {"pg-1"}}},
		{"sourceType 空串", url.Values{"projectId": {"p1"}, "sourceType": {""}, "ids": {"pg-1"}}},
		{"缺 ids", url.Values{"projectId": {"p1"}, "sourceType": {"page"}}},
		{"ids 只有空串", url.Values{"projectId": {"p1"}, "sourceType": {"page"}, "ids": {""}}},
		{"空体", url.Values{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &navigationAddSourceStub{groups: addSourceGroups()}
			engine := newNavigationAddSourceRouter(t, stub)
			rec := postNavigationAddSource(t, engine, tc.form)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("形状错应 400，实际 %d：%s", rec.Code, rec.Body.String())
			}
			if len(stub.createCalls) != 0 {
				t.Fatalf("形状错不该落库，实际 %+v", stub.createCalls)
			}
			if !strings.Contains(rec.Body.String(), `data-ui-error`) {
				t.Fatalf("失败应渲染 Jet 错误片段：%s", rec.Body.String())
			}
		})
	}
}

func TestNavigationAddSourceLimits(t *testing.T) {
	stub := &navigationAddSourceStub{groups: addSourceGroups()}
	engine := newNavigationAddSourceRouter(t, stub)

	ids := make([]string, 0, bulkids.MaxIDs+1)
	for i := 0; i < bulkids.MaxIDs+1; i++ {
		ids = append(ids, "pg-"+strconv.Itoa(i))
	}
	form := addSourceForm(ids...)
	i18n.InjectForTest(map[string]map[string]string{
		bulkids.MsgTooMany: {"zh-CN": "一次最多操作 %s 项，当前 %s 项，请分批进行"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	rec := postNavigationAddSource(t, engine, form)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("超限应整批拒绝（400，不部分执行），实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, strconv.Itoa(bulkids.MaxIDs)) ||
		!strings.Contains(body, strconv.Itoa(bulkids.MaxIDs+1)) {
		t.Fatalf("超限提示应带上限与本次条数：%s", body)
	}
	if len(stub.createCalls) != 0 || len(stub.groupCalls) != 0 {
		t.Fatalf("超限不该读候选 / 写库，实际 %v / %d", stub.groupCalls, len(stub.createCalls))
	}

	hugeForm := url.Values{"projectId": {"p1"}, "sourceType": {"page"},
		"ids": {strings.Repeat("x", navigationWriteMaxBodyBytes)}}
	rec = postNavigationAddSource(t, engine, hugeForm)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("超过请求体上限应 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if len(stub.createCalls) != 0 {
		t.Fatalf("超大体不该落库，实际 %+v", stub.createCalls)
	}
}

func TestNavigationCreateUsesFormBinding(t *testing.T) {
	// 普通创建是标准表单：一条明确的输入协议，与「从来源加入」是两个端点。
	cases := []struct {
		name     string
		form     url.Values
		wantCode int
	}{
		{"合法创建", url.Values{"projectId": {"p1"}, "title": {"关于我们"}, "path": {"/about"}, "kind": {"header"}}, http.StatusOK},
		{"合法创建带来源字段", url.Values{"projectId": {"p1"}, "title": {"关于我们"}, "path": {"/about"},
			"kind": {"header"}, "sourceType": {"page"}, "sourceId": {"pg-1"}}, http.StatusOK},
		{"缺 title", url.Values{"projectId": {"p1"}, "path": {"/about"}, "kind": {"header"}}, http.StatusBadRequest},
		{"缺 path", url.Values{"projectId": {"p1"}, "title": {"关于我们"}, "kind": {"header"}}, http.StatusBadRequest},
		{"缺 kind", url.Values{"projectId": {"p1"}, "title": {"关于我们"}, "path": {"/about"}}, http.StatusBadRequest},
		{"缺 projectId", url.Values{"title": {"关于我们"}, "path": {"/about"}, "kind": {"header"}}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &navigationAddSourceStub{groups: addSourceGroups()}
			gin.SetMode(gin.TestMode)
			engine := gin.New()
			engine.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
			engine.Use(pageview.FailureMiddleware(adminui.RenderFailure))
			engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
			engine.POST("/api/navigation/create", pageview.HTMLFragment, NewHandle(stub).Create)

			req := httptest.NewRequest(http.MethodPost, "/api/navigation/create", strings.NewReader(tc.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Fatalf("应 %d，实际 %d：%s", tc.wantCode, rec.Code, rec.Body.String())
			}
			if tc.wantCode == http.StatusOK {
				if len(stub.createCalls) != 1 {
					t.Fatalf("普通创建应落到 service.Create，实际 %+v", stub.createCalls)
				}
				if !strings.Contains(rec.Body.String(), `role="status"`) {
					t.Fatalf("成功应回 Jet 成功片段：%s", rec.Body.String())
				}
			} else if len(stub.createCalls) != 0 {
				t.Fatalf("校验失败不该落库，实际 %+v", stub.createCalls)
			}
		})
	}
}

func TestNavigationAddSourceTemplateContract(t *testing.T) {
	readFile := func(path string) string {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读 %s 失败: %v", path, err)
		}
		return string(raw)
	}

	router := readFile("navigation_page_router.go")
	if strings.Contains(router, "add-source") {
		t.Fatalf("页面 add-source 路由应已退役：%s", router)
	}
	page := readFile("navigation_page_handle.go")
	if strings.Contains(page, "func (h *navigationPageHandle) NavigationAddSource") {
		t.Fatalf("NavigationAddSource 页面 handler 应已退役")
	}
	tpl := readFile("../../../../templates/admin/navigation/navigations.html")
	for _, want := range []string{
		`hx-post="/api/navigation/sources"`,
		`name="sourceType"`,
		`name="ids"`,
		`data-write-status`,
		`data-write-result`,
	} {
		if !strings.Contains(tpl, want) {
			t.Fatalf("模板应含 %q", want)
		}
	}

	for _, forbidden := range []string{
		`action="/admin/navigations/add-source`,
		`name="sourceIds"`,
	} {
		if strings.Contains(tpl, forbidden) {
			t.Fatalf("模板不该再出现 %q", forbidden)
		}
	}
}

func addSourceFormOf(sourceType string, ids ...string) url.Values {
	form := addSourceForm(ids...)
	form.Set("sourceType", sourceType)
	return form
}

func TestNavigationAddSourceReasonsFallBackWhenEntryMissing(t *testing.T) {
	// 声明处不再带成品兜底，业务 key 的文案只从词库来：子测试各自注入中文，验真实句子。
	const detail = "2026-10-09T00:00:00Z"
	noPath := navigationReasonFallbacks[navigationAddSourceNoPathKey]
	if noPath == "" {
		t.Fatalf("界面文案兜底表里应有 %s，否则「暂无公开路径」会退化成通用失败句", navigationAddSourceNoPathKey)
	}
	cases := []struct {
		name     string
		source   string
		failFor  map[string]error
		ids      []string
		want     map[string]string
		wantGone []string
	}{
		{
			name:     "业务文案",
			source:   "page",
			failFor:  map[string]error{"pg-1": errors.New(navigationenums.ErrPathTaken)},
			ids:      []string{"pg-1"},
			want:     map[string]string{"pg-1": "该链接已被占用"},
			wantGone: []string{navigationenums.ErrPathTaken},
		},
		{
			name:    "跳过类界面文案",
			source:  "page",
			failFor: nil,
			ids:     []string{"pg-2"},
			want:    map[string]string{"pg-2": noPath},

			wantGone: []string{navigationAddSourceNoPathKey, navigationInternalFallback},
		},
		{
			name:     "带定位形态",
			source:   "page",
			failFor:  map[string]error{"pg-1": navigationenums.ErrDetail(navigationenums.ErrStaleVersion, detail)},
			ids:      []string{"pg-1"},
			want:     map[string]string{"pg-1": "页面已被他人修改: " + detail},
			wantGone: []string{navigationenums.ErrStaleVersion},
		},
		{
			name:    "带参形态",
			source:  "page",
			failFor: map[string]error{"pg-1": errors.New(navigationenums.ErrPathTaken + "|/about")},
			ids:     []string{"pg-1"},
			// 该 key 的译文不含占位符，附带的参数不会被拼进句子。
			// 带参串不是词条键，逐条原因退成归口文案（参数不上屏）。
			want:     map[string]string{"pg-1": navigationInternalFallback},
			wantGone: []string{navigationenums.ErrPathTaken, "|", "/about"},
		},
		{
			name:    "未归口的内部错误",
			source:  "article",
			failFor: map[string]error{"ar-1": errors.New(`ERROR: duplicate key value violates unique constraint "uq_navigation_path" (SQLSTATE 23505)`)},
			ids:     []string{"ar-1"},
			want:    map[string]string{"ar-1": navigationInternalFallback},
			wantGone: []string{
				"SQLSTATE", "23505", "uq_navigation_path", "constraint", "duplicate key",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i18n.InjectForTest(map[string]map[string]string{
				navigationenums.ErrPathTaken:    {"zh-CN": "该链接已被占用"},
				navigationenums.ErrStaleVersion: {"zh-CN": "页面已被他人修改"},
			}, nil)
			t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
			stub := &navigationAddSourceStub{groups: addSourceGroups(), failFor: tc.failFor}
			engine := newNavigationAddSourceRouter(t, stub)

			rec := postNavigationAddSource(t, engine, addSourceFormOf(tc.source, tc.ids...))
			if rec.Code != http.StatusOK {
				t.Fatalf("逐条失败应 200，实际 %d：%s", rec.Code, rec.Body.String())
			}
			body := navigationAddSourceOK(t, rec)
			added, skipped := addSourceCounts(t, body)
			if added != 0 || skipped != len(tc.ids) {
				t.Fatalf("应 added=0 skipped=%d，实际 added=%d skipped=%d", len(tc.ids), added, skipped)
			}
			items := addSourceItems(t, body)
			for id, wantMsg := range tc.want {
				got := items[id].message
				if got != wantMsg {
					t.Fatalf("id=%s 的逐条原因应为 %q，实际 %q", id, wantMsg, got)
				}
				for _, tok := range tc.wantGone {
					if strings.Contains(got, tok) {
						t.Errorf("id=%s 的逐条原因不该出现 %q：%q", id, tok, got)
					}
				}
			}
		})
	}
}

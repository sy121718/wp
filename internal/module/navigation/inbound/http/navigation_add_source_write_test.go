package navigationhttp

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

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
	engine.POST("/api/navigation/create", NewHandle(svc).Create)
	return engine
}

func postNavigationAddSource(t *testing.T, engine *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/create", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func navigationAddSourceResponse(t *testing.T, rec *httptest.ResponseRecorder) (int, string, navigationAddSourceResult) {
	t.Helper()
	var resp struct {
		Code    int                       `json:"code"`
		Message string                    `json:"message"`
		Data    navigationAddSourceResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是 JSON：%v（%s）", err, rec.Body.String())
	}
	return resp.Code, resp.Message, resp.Data
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

func addSourceBody(ids ...string) string {
	quoted := make([]string, 0, len(ids))
	for _, id := range ids {
		quoted = append(quoted, `"`+id+`"`)
	}
	return `{"action":"addSource","projectId":"p1","kind":"header","sourceType":"page","ids":[` +
		strings.Join(quoted, ",") + `]}`
}

func TestNavigationAddSourceBulkShape(t *testing.T) {
	stub := &navigationAddSourceStub{groups: addSourceGroups()}
	engine := newNavigationAddSourceRouter(t, stub)

	rec := postNavigationAddSource(t, engine,
		`{"action":"addSource","projectId":"p1","kind":"header","sourceType":"page",`+
			`"ids":["pg-1","pg-2","ar-1","nope"]}`)
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
	_, _, data := navigationAddSourceResponse(t, rec)
	if data.Added != 1 || data.Skipped != 3 || len(data.Items) != 4 {
		t.Fatalf("回执应为 added=1 skipped=3 items=4，实际 %+v", data)
	}
	if !data.Items[0].Added || data.Items[0].ID != "pg-1" || data.Items[0].Message != "" {
		t.Fatalf("第 1 条应标记为已加入且无原因，实际 %+v", data.Items[0])
	}
	for _, item := range data.Items[1:] {
		if item.Added || item.Message == "" {
			t.Fatalf("跳过项必须带上原因（前端逐条列出来），实际 %+v", item)
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

	rec := postNavigationAddSource(t, engine, addSourceBody("pg-1"))
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
		body    string
		want    string
		wantBad bool
	}{
		{"缺省回落 header",
			`{"action":"addSource","projectId":"p1","sourceType":"page","ids":["pg-1"]}`, "header", false},
		{"空串回落 header",
			`{"action":"addSource","projectId":"p1","kind":"","sourceType":"page","ids":["pg-1"]}`, "header", false},
		{"显式 footer",
			`{"action":"addSource","projectId":"p1","kind":"footer","sourceType":"page","ids":["pg-1"]}`, "footer", false},
		{"不在枚举里的类型回落 header",
			`{"action":"addSource","projectId":"p1","kind":"sidebar","sourceType":"page","ids":["pg-1"]}`, "header", false},
		{"kind 显式 null",
			`{"action":"addSource","projectId":"p1","kind":null,"sourceType":"page","ids":["pg-1"]}`, "", true},
		{"kind 是数字",
			`{"action":"addSource","projectId":"p1","kind":1,"sourceType":"page","ids":["pg-1"]}`, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &navigationAddSourceStub{groups: addSourceGroups()}
			engine := newNavigationAddSourceRouter(t, stub)
			rec := postNavigationAddSource(t, engine, tc.body)
			if tc.wantBad {
				if rec.Code != http.StatusBadRequest {
					t.Fatalf("应 400，实际 %d：%s", rec.Code, rec.Body.String())
				}
				if len(stub.createCalls) != 0 {
					t.Fatalf("形状错不该调用 create，实际 %+v", stub.createCalls)
				}
				return
			}
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

	rec := postNavigationAddSource(t, engine, addSourceBody("pg-1", "pg-3"))
	if rec.Code != http.StatusOK {
		t.Fatalf("应 200（逐条结果），实际 %d：%s", rec.Code, rec.Body.String())
	}
	if len(stub.createCalls) != 2 {
		t.Fatalf("一条失败不应中断整批，实际调了 %d 次", len(stub.createCalls))
	}
	_, _, data := navigationAddSourceResponse(t, rec)
	if data.Added != 1 || data.Skipped != 1 {
		t.Fatalf("回执应为 added=1 skipped=1，实际 %+v", data)
	}
	if data.Items[0].Added || data.Items[1].Added != true {
		t.Fatalf("第 1 条应失败、第 2 条应成功，实际 %+v", data.Items)
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

	rec := postNavigationAddSource(t, engine, addSourceBody("pg-1", "pg-2", "nope"))
	if rec.Code != http.StatusOK {
		t.Fatalf("应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	_, _, data := navigationAddSourceResponse(t, rec)
	if data.Skipped != 3 {
		t.Fatalf("三条都应跳过，实际 %+v", data)
	}
	for _, leak := range []string{"SQLSTATE", "uq_navigation_path", "ERROR:", "duplicate key"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Fatalf("回执泄漏了原始异常（%s）：%s", leak, rec.Body.String())
		}
	}

	if got := data.Items[0].Message; got == "" || got == rawErr.Error() {
		t.Fatalf("第 1 条原因应归口，实际 %q", got)
	}
	if data.Items[1].Message == "" || data.Items[2].Message == "" {
		t.Fatalf("跳过项都必须带原因，实际 %+v", data.Items)
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

	post := func(lang string) navigationAddSourceResult {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/navigation/create",
			strings.NewReader(`{"action":"addSource","projectId":"p1","sourceType":"page",`+
				`"ids":["pg-2","ar-1","nope"]}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept-Language", lang)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s：应 200，实际 %d：%s", lang, rec.Code, rec.Body.String())
		}
		_, _, data := navigationAddSourceResponse(t, rec)
		return data
	}

	zh := post("zh-CN")
	if zh.Items[0].Message != "该内容暂无公开路径" {
		t.Fatalf("中文请求应拿中文原因，实际 %+v", zh.Items[0])
	}
	if zh.Items[1].Message != "找不到该条目" || zh.Items[2].Message != "找不到该条目" {
		t.Fatalf("不属于本类型 / 不在候选里都应给「不存在」原因，实际 %+v", zh.Items)
	}
	en := post("en-US")
	if en.Items[0].Message != "No public path yet" || en.Items[1].Message != "No such item" {
		t.Fatalf("英文请求应拿英文原因，实际 %+v", en.Items)
	}
}

func TestNavigationAddSourceEmptyIDsIsNoop(t *testing.T) {
	stub := &navigationAddSourceStub{groups: addSourceGroups()}
	engine := newNavigationAddSourceRouter(t, stub)

	rec := postNavigationAddSource(t, engine, addSourceBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("空数组应 200（页面据此显示「请至少勾选一项」），实际 %d：%s", rec.Code, rec.Body.String())
	}
	if len(stub.createCalls) != 0 || len(stub.groupCalls) != 0 {
		t.Fatalf("空批量不该读候选 / 写库，实际 groups=%v creates=%d", stub.groupCalls, len(stub.createCalls))
	}
	_, _, data := navigationAddSourceResponse(t, rec)
	if data.Added != 0 || data.Skipped != 0 || data.Items == nil {
		t.Fatalf("空批量回执应为 0/0 且 items 为空数组（不是 null），实际 %+v", data)
	}
}

func TestNavigationAddSourceUnknownTypeSkipsAll(t *testing.T) {
	stub := &navigationAddSourceStub{groups: addSourceGroups()}
	engine := newNavigationAddSourceRouter(t, stub)

	rec := postNavigationAddSource(t, engine,
		`{"action":"addSource","projectId":"p1","sourceType":"block","ids":["pg-1","pg-2"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("候选为空是合法结果，应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if len(stub.createCalls) != 0 {
		t.Fatalf("类型不匹配不该落库，实际 %+v", stub.createCalls)
	}
	_, _, data := navigationAddSourceResponse(t, rec)
	if data.Added != 0 || data.Skipped != 2 {
		t.Fatalf("应 added=0 skipped=2，实际 %+v", data)
	}
}

func TestNavigationAddSourceGroupsFailureIs500(t *testing.T) {
	stub := &navigationAddSourceStub{groupsErr: errors.New(`relation "navigations" does not exist`)}
	engine := newNavigationAddSourceRouter(t, stub)

	rec := postNavigationAddSource(t, engine, addSourceBody("pg-1"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("读候选失败应 500，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if len(stub.createCalls) != 0 {
		t.Fatalf("读候选失败必须零写入，实际 %+v", stub.createCalls)
	}
	_, message, _ := navigationAddSourceResponse(t, rec)
	if message != navigationInternalKey {
		t.Fatalf("应归口到 ErrInternal（key 形态，翻译交 response），实际 %q", message)
	}
	if strings.Contains(rec.Body.String(), "navigations") {
		t.Fatalf("500 回执不该带原始异常：%s", rec.Body.String())
	}
}

func TestNavigationAddSourceShapeRejections(t *testing.T) {
	cases := []struct{ name, body string }{
		{"未知 action", `{"action":"nope","projectId":"p1","sourceType":"page","ids":["pg-1"]}`},
		{"action 显式 null", `{"action":null,"projectId":"p1","sourceType":"page","ids":["pg-1"]}`},
		{"action 是数字", `{"action":1,"projectId":"p1","sourceType":"page","ids":["pg-1"]}`},
		{"action 是布尔", `{"action":true,"projectId":"p1","sourceType":"page","ids":["pg-1"]}`},
		{"action 是空串", `{"action":"","projectId":"p1","sourceType":"page","ids":["pg-1"]}`},
		{"action 是对象", `{"action":{},"projectId":"p1","sourceType":"page","ids":["pg-1"]}`},
		{"顶层未知键", `{"action":"addSource","projectId":"p1","sourceType":"page","ids":["pg-1"],"extra":1}`},
		{"缺 projectId", `{"action":"addSource","sourceType":"page","ids":["pg-1"]}`},
		{"projectId 显式 null", `{"action":"addSource","projectId":null,"sourceType":"page","ids":["pg-1"]}`},
		{"projectId 空串", `{"action":"addSource","projectId":"","sourceType":"page","ids":["pg-1"]}`},
		{"projectId 是数字", `{"action":"addSource","projectId":1,"sourceType":"page","ids":["pg-1"]}`},
		{"缺 sourceType", `{"action":"addSource","projectId":"p1","ids":["pg-1"]}`},
		{"sourceType 显式 null", `{"action":"addSource","projectId":"p1","sourceType":null,"ids":["pg-1"]}`},
		{"sourceType 空串", `{"action":"addSource","projectId":"p1","sourceType":"","ids":["pg-1"]}`},
		{"sourceType 是数字", `{"action":"addSource","projectId":"p1","sourceType":1,"ids":["pg-1"]}`},
		{"缺 ids", `{"action":"addSource","projectId":"p1","sourceType":"page"}`},
		{"ids 显式 null", `{"action":"addSource","projectId":"p1","sourceType":"page","ids":null}`},
		{"ids 数组里有 null", `{"action":"addSource","projectId":"p1","sourceType":"page","ids":["pg-1",null]}`},
		{"ids 里有数字", `{"action":"addSource","projectId":"p1","sourceType":"page","ids":["pg-1",1]}`},
		{"ids 不是数组", `{"action":"addSource","projectId":"p1","sourceType":"page","ids":"pg-1"}`},
		{"ids 是对象", `{"action":"addSource","projectId":"p1","sourceType":"page","ids":{}}`},
		{"无 action 却带 ids（混形）", `{"projectId":"p1","title":"t","path":"/p","kind":"header","ids":["pg-1"]}`},
		{"非法 JSON", `{`},
		{"空体", ``},
		{"顶层数组", `[]`},
		{"顶层 null", `null`},
		{"顶层字符串", `"x"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &navigationAddSourceStub{groups: addSourceGroups()}
			engine := newNavigationAddSourceRouter(t, stub)
			rec := postNavigationAddSource(t, engine, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("形状错应 400，实际 %d：%s", rec.Code, rec.Body.String())
			}
			if len(stub.createCalls) != 0 {
				t.Fatalf("形状错不该落库，实际 %+v", stub.createCalls)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
				t.Fatalf("出口应是 JSON 报文，实际 Content-Type=%q", ct)
			}
		})
	}
}

func TestNavigationAddSourceLimits(t *testing.T) {
	stub := &navigationAddSourceStub{groups: addSourceGroups()}
	engine := newNavigationAddSourceRouter(t, stub)

	ids := make([]string, 0, bulkids.MaxIDs+1)
	for i := 0; i < bulkids.MaxIDs+1; i++ {
		ids = append(ids, `"pg-`+strconv.Itoa(i)+`"`)
	}
	rec := postNavigationAddSource(t, engine,
		`{"action":"addSource","projectId":"p1","sourceType":"page","ids":[`+strings.Join(ids, ",")+`]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("超限应整批拒绝（400，不部分执行），实际 %d：%s", rec.Code, rec.Body.String())
	}
	_, message, _ := navigationAddSourceResponse(t, rec)
	if !strings.HasPrefix(message, bulkids.MsgTooMany+"|") {
		t.Fatalf("超限应回受控 key（%s）的带参协议，实际 %q", bulkids.MsgTooMany, message)
	}
	if !strings.Contains(message, strconv.Itoa(bulkids.MaxIDs)) ||
		!strings.Contains(message, strconv.Itoa(bulkids.MaxIDs+1)) {
		t.Fatalf("超限协议应带上限与本次条数，实际 %q", message)
	}
	if len(stub.createCalls) != 0 || len(stub.groupCalls) != 0 {
		t.Fatalf("超限不该读候选 / 写库，实际 %v / %d", stub.groupCalls, len(stub.createCalls))
	}

	huge := `{"action":"addSource","projectId":"p1","sourceType":"page","ids":["` +
		strings.Repeat("x", navigationWriteMaxBodyBytes) + `"]}`
	rec = postNavigationAddSource(t, engine, huge)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("超过请求体上限应 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if len(stub.createCalls) != 0 {
		t.Fatalf("超大体不该落库，实际 %+v", stub.createCalls)
	}
}

func TestNavigationCreateLegacyShapeUnchanged(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantCode int
	}{
		{"合法创建", `{"projectId":"p1","title":"关于我们","path":"/about","kind":"header"}`, http.StatusOK},
		{"合法创建带来源字段", `{"projectId":"p1","title":"关于我们","path":"/about","kind":"header",` +
			`"sourceType":"page","sourceId":"pg-1"}`, http.StatusOK},
		{"未知字段仍被忽略", `{"projectId":"p1","title":"关于我们","path":"/about","kind":"header","unknown":1}`,
			http.StatusOK},
		{"缺 title", `{"projectId":"p1","path":"/about","kind":"header"}`, http.StatusBadRequest},
		{"缺 path", `{"projectId":"p1","title":"关于我们","kind":"header"}`, http.StatusBadRequest},
		{"缺 kind", `{"projectId":"p1","title":"关于我们","path":"/about"}`, http.StatusBadRequest},
		{"缺 projectId", `{"title":"关于我们","path":"/about","kind":"header"}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &navigationAddSourceStub{groups: addSourceGroups()}
			engine := newNavigationAddSourceRouter(t, stub)
			rec := postNavigationAddSource(t, engine, tc.body)
			if rec.Code != tc.wantCode {
				t.Fatalf("应 %d，实际 %d：%s", tc.wantCode, rec.Code, rec.Body.String())
			}
			if tc.wantCode == http.StatusOK {
				if len(stub.createCalls) != 1 {
					t.Fatalf("普通创建应落到 service.Create，实际 %+v", stub.createCalls)
				}
				if _, message, _ := navigationAddSourceResponse(t, rec); message != navigationenums.MsgCreateSuccess {
					t.Fatalf("普通创建回执应为 MsgCreateSuccess，实际 %q", message)
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
		`data-write-url="/api/navigation/create"`,
		`data-write="bulk"`,
		`data-write-outcome="add"`,
		`data-write-extra="action,projectId,kind,sourceType"`,
		`<input type="hidden" name="action" value="addSource">`,
		`name="ids"`,
		`data-write-status`,
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

func addSourceBodyOf(sourceType string, ids ...string) string {
	quoted := make([]string, 0, len(ids))
	for _, id := range ids {
		quoted = append(quoted, `"`+id+`"`)
	}
	return `{"action":"addSource","projectId":"p1","kind":"header","sourceType":"` + sourceType +
		`","ids":[` + strings.Join(quoted, ",") + `]}`
}

func addSourceItemMessage(t *testing.T, res navigationAddSourceResult, id string) string {
	t.Helper()
	for _, item := range res.Items {
		if item.ID == id {
			return item.Message
		}
	}
	t.Fatalf("回执里没有 id=%s 的逐条结果：%+v", id, res.Items)
	return ""
}

func TestNavigationAddSourceReasonsFallBackWhenEntryMissing(t *testing.T) {
	clearNavigationEntries(t)

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
			want:     map[string]string{"pg-1": navigationenums.FallbackFor(navigationenums.ErrPathTaken)},
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
			want:     map[string]string{"pg-1": navigationenums.FallbackFor(navigationenums.ErrStaleVersion) + ": " + detail},
			wantGone: []string{navigationenums.ErrStaleVersion},
		},
		{
			name:     "带参形态",
			source:   "page",
			failFor:  map[string]error{"pg-1": errors.New(navigationenums.ErrPathTaken + "|/about")},
			ids:      []string{"pg-1"},
			want:     map[string]string{"pg-1": navigationenums.FallbackFor(navigationenums.ErrPathTaken)},
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
			stub := &navigationAddSourceStub{groups: addSourceGroups(), failFor: tc.failFor}
			engine := newNavigationAddSourceRouter(t, stub)

			rec := postNavigationAddSource(t, engine, addSourceBodyOf(tc.source, tc.ids...))
			if rec.Code != http.StatusOK {
				t.Fatalf("逐条失败应 200（结果在 data 里），实际 %d：%s", rec.Code, rec.Body.String())
			}
			_, _, data := navigationAddSourceResponse(t, rec)
			if data.Added != 0 || data.Skipped != len(tc.ids) {
				t.Fatalf("应 added=0 skipped=%d，实际 %+v", len(tc.ids), data)
			}
			for id, wantMsg := range tc.want {
				got := addSourceItemMessage(t, data, id)
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

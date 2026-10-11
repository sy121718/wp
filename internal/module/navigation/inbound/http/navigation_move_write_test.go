// navigation_move_write_test.go — move 档（同级上移 / 下移）的形状、状态映射与模板契约。
//
// 本卡（task-91）把页面的 POST /admin/navigations/move 并进 POST /api/navigation/update 的
// action="move"：权限锚点仍是 navigation:update，作用域与并发语义仍全在 service.SwapMove。
// 这里钉四件事：
//   - 形状严格：白名单外的键（含页面旧表单发过的 kind）、null / 非字符串、缺键、未知 action、
//     以及「没有 action 却带 move 专属键」的混形 → 一律 400 且不落 service；
//   - 成功档：恰好一次 service 调用，请求字段逐字（projectId 只是 locator，move 不做客户端版本承诺）；
//   - 错误档：ErrNotFound→404、ErrStaleVersion→409、ErrInvalidParam / ErrSortTied→400、未归口→500
//     且原始错误不进响应体（服务器细节只进日志）；
//   - 模板契约：行内 up / down 表单的**具名字段集合 == 服务端白名单**（多一个字段就会被严格白名单
//     整条拒掉），并且页面里不再存在 /admin/navigations/move。
package navigationhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/pkg/logger"
)

// moveWriteNavigationStub 只实现 move 档需要的那一个 service 方法（其余方法由内嵌接口兜住）。
//
// Update 也实现：**旧分支**必须能被观测到。否则「缺 action 却带 move 键」这类混形若漏过守卫，
// 请求会落到内嵌的 nil 接口上 panic —— panic 虽然也算红，但它只会告诉后来者「测试炸了」，
// 而记下调用次数能让断言直接说出「这条形态不该走到 service.Update」。
type moveWriteNavigationStub struct {
	navigationcontract.NavigationService
	calls       []*navigationdto.SwapMoveReq
	updateCalls []*navigationdto.UpdateReq
	resp        *navigationdto.SwapMoveResp
	err         error
}

func (s *moveWriteNavigationStub) Update(_ context.Context, req *navigationdto.UpdateReq) (*navigationdto.NavigationResp, error) {
	s.updateCalls = append(s.updateCalls, req)
	return &navigationdto.NavigationResp{ID: req.ID}, nil
}

func (s *moveWriteNavigationStub) SwapMove(_ context.Context, req *navigationdto.SwapMoveReq) (*navigationdto.SwapMoveResp, error) {
	s.calls = append(s.calls, req)
	if s.err != nil {
		return nil, s.err
	}
	if s.resp != nil {
		return s.resp, nil
	}
	return &navigationdto.SwapMoveResp{Moved: true}, nil
}

// initMoveWriteLogger 初始化进程级 logger：未归口的内部错误那一档**会记日志**
// （细节只进日志、不进响应），日志路径要按生产形态走一遍，否则那一档会打印
// 「logger 未初始化」的噪声警告。
func initMoveWriteLogger(t *testing.T) {
	t.Helper()
	conf := viper.New()
	conf.Set("log.base_dir", t.TempDir())
	if err := logger.Init(conf); err != nil {
		t.Fatalf("初始化 logger 失败: %v", err)
	}
}

// newMoveWriteRouter 只挂 Update（move 与普通更新同一个入口，正是本卡要证的事）。
func newMoveWriteRouter(svc navigationcontract.NavigationService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/navigation/update", NewHandle(svc).Update)
	return router
}

// postMoveWrite 发一条 JSON 请求；lang 非空时带 Accept-Language。
func postMoveWrite(t *testing.T, engine *gin.Engine, lang, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/update", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if lang != "" {
		req.Header.Set("Accept-Language", lang)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// navigationMoveResponse 解析回执信封。
func navigationMoveResponse(t *testing.T, rec *httptest.ResponseRecorder) (int, string, json.RawMessage) {
	t.Helper()
	var resp struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是 JSON：%v（%s）", err, rec.Body.String())
	}
	return resp.Code, resp.Message, resp.Data
}

// moveBody 组一条 move 请求体（字段拼装显式写出，避免用例里散落格式化字符串）。
func moveBody(fields map[string]string) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(fields))
	for _, k := range keys {
		parts = append(parts, `"`+k+`":"`+fields[k]+`"`)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// TestNavigationUpdateMoveShapeIsStrict 形状错一律 400，且**不落 service**。
//
// 每一条都对应一种「猜分支就会挪错位置」的形态。特别保留 kind：页面旧表单曾把筛选上下文
// （kind）一起发给服务端，白名单外的键必须整条拒掉，否则服务端就会开始接受「调用方以为
// 限定过的上下文」。
func TestNavigationUpdateMoveShapeIsStrict(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"缺 id 与 dir", `{"action":"move"}`},
		{"缺 dir", `{"action":"move","id":"nav-1"}`},
		{"空 id", `{"action":"move","id":"","dir":"up"}`},
		{"空 dir", `{"action":"move","id":"nav-1","dir":""}`},
		{"id 显式 null", `{"action":"move","id":null,"dir":"up"}`},
		{"dir 显式 null", `{"action":"move","id":"nav-1","dir":null}`},
		{"id 非字符串", `{"action":"move","id":1,"dir":"up"}`},
		{"dir 非字符串", `{"action":"move","id":"nav-1","dir":true}`},
		{"白名单外的键（页面旧表单的 kind）", `{"action":"move","id":"nav-1","projectId":"p-1","dir":"up","kind":"header"}`},
		{"projectId 显式 null", `{"action":"move","id":"nav-1","dir":"up","projectId":null}`},
		{"projectId 非字符串", `{"action":"move","id":"nav-1","dir":"up","projectId":42}`},
		{"混形 rows", `{"action":"move","id":"nav-1","dir":"up","rows":[]}`},
		{"混形 project（译文分支的键）", `{"action":"move","id":"nav-1","dir":"up","project":"p-1"}`},
		{"混形 lang", `{"action":"move","id":"nav-1","dir":"up","lang":"en-US"}`},
		{"未知 action", `{"action":"turn","id":"nav-1","dir":"up"}`},
		{"action 显式 null", `{"action":null,"id":"nav-1","dir":"up"}`},
		{"无 action 却带 dir", `{"id":"nav-1","dir":"up"}`},
		{"无 action 却带 projectId", `{"id":"nav-1","projectId":"p-1"}`},
		{"不是对象", `[]`},
		{"JSON 非法", `{`},
		{"空体", ``},
		{"显式 null", `null`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &moveWriteNavigationStub{}
			rec := postMoveWrite(t, newMoveWriteRouter(stub), "", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("应 400，实际 %d：%s", rec.Code, rec.Body.String())
			}
			if len(stub.calls) != 0 {
				t.Fatalf("形状不对不得落 service，实际 %+v", stub.calls)
			}
			if len(stub.updateCalls) != 0 {
				t.Fatalf("形状不对不得落到旧更新分支，实际 %+v", stub.updateCalls)
			}
		})
	}
}

// TestNavigationUpdateMoveCallsSwapMoveOnce 成功档：一次调用、字段逐字、回执带 moved。
func TestNavigationUpdateMoveCallsSwapMoveOnce(t *testing.T) {
	stub := &moveWriteNavigationStub{resp: &navigationdto.SwapMoveResp{Moved: true}}
	rec := postMoveWrite(t, newMoveWriteRouter(stub), "", moveBody(map[string]string{
		"action": "move", "id": "nav-9", "projectId": "p-1", "dir": "down",
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if len(stub.calls) != 1 {
		t.Fatalf("应恰好一次 SwapMove，实际 %d 次", len(stub.calls))
	}
	got := stub.calls[0]
	if got.ID != "nav-9" || got.ProjectID != "p-1" || got.Dir != "down" {
		t.Fatalf("请求字段被改动：%+v", got)
	}
	if _, _, data := navigationMoveResponse(t, rec); !strings.Contains(string(data), `"moved":true`) {
		t.Fatalf("回执 data 应带 moved:true，实际 %s", data)
	}

	// projectId 可缺省（= 不校验工程归属）；枚举值 up / down 的判据在 service，不在这里。
	stub.calls = nil
	rec = postMoveWrite(t, newMoveWriteRouter(stub), "", moveBody(map[string]string{"action": "move", "id": "nav-9", "dir": "up"}))
	if rec.Code != http.StatusOK || len(stub.calls) != 1 || stub.calls[0].ProjectID != "" {
		t.Fatalf("缺省 projectId 应放行且落到空工程：code=%d calls=%+v", rec.Code, stub.calls)
	}
}

// TestNavigationUpdateMoveErrorStatus 业务错误 → HTTP 状态（tied 是业务拒绝，不是 500）。
func TestNavigationUpdateMoveErrorStatus(t *testing.T) {
	initMoveWriteLogger(t)
	cases := []struct {
		name     string
		err      error
		wantCode int
	}{
		{"未找到", errors.New(navigationenums.ErrNotFound), http.StatusNotFound},
		{"版本冲突", errors.New(navigationenums.ErrStaleVersion), http.StatusConflict},
		{"参数非法", errors.New(navigationenums.ErrInvalidParam), http.StatusBadRequest},
		{"同级排序并列", errors.New(navigationenums.ErrSortTied), http.StatusBadRequest},
		{"未归口的内部错误", errors.New("boom: SQLSTATE 42P01"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &moveWriteNavigationStub{err: tc.err}
			rec := postMoveWrite(t, newMoveWriteRouter(stub), "", moveBody(map[string]string{
				"action": "move", "id": "nav-1", "dir": "up",
			}))
			if rec.Code != tc.wantCode {
				t.Fatalf("应 %d，实际 %d：%s", tc.wantCode, rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "SQLSTATE") || strings.Contains(rec.Body.String(), "42P01") {
				t.Fatalf("服务器细节不得进响应体：%s", rec.Body.String())
			}
		})
	}
}

// TestNavigationUpdateMoveMessageFollowsRequestLanguage move 的错误文案走同一条取词链。
//
// 词条是合成值：「响应里出现这句」等价于「白名单 key 真的到了取词层」。同一请求换
// Accept-Language 得到不同成品句 —— 语言由请求决定，不钉死在服务层。
func TestNavigationUpdateMoveMessageFollowsRequestLanguage(t *testing.T) {
	injectNavigationEntries(t, map[string]map[string]string{
		navigationenums.ErrSortTied: {"zh-CN": "词条-同级排序并列", "en-US": "entry-sort-tied"},
	})
	for _, tc := range []struct{ lang, want string }{
		{"zh-CN", "词条-同级排序并列"},
		{"en-US", "entry-sort-tied"},
	} {
		stub := &moveWriteNavigationStub{err: errors.New(navigationenums.ErrSortTied)}
		rec := postMoveWrite(t, newMoveWriteRouter(stub), tc.lang, moveBody(map[string]string{
			"action": "move", "id": "nav-1", "dir": "up",
		}))
		_, message, _ := navigationMoveResponse(t, rec)
		if message != tc.want {
			t.Fatalf("%s 的消息应为 %q，实际 %q", tc.lang, tc.want, message)
		}
	}
}

// moveTemplateFormRe 匹配声明了本端点的表单块（模板里只有行内 up / down 两处）。
var moveTemplateFormRe = regexp.MustCompile(`(?s)<form[^>]*data-write-api="/api/navigation/update".*?</form>`)

// moveTemplateNameRe 取表单块里的具名字段。
var moveTemplateNameRe = regexp.MustCompile(`name="([^"]+)"`)

// TestNavigationUpdateMoveTemplateContract 源码级契约：页面侧路由 / handler 退役，
// 模板 declaration 与具名字段集合必须与**服务端白名单逐字一致**。
//
// 这条是防「模板字段漂移 → 400」的钉子：基座 create 档会把这个表单里所有具名字段一起发出去，
// 模板多写一个 name（历史上就有 kind）就会让整条请求被严格白名单拒掉，而且只在浏览器里才看得见。
func TestNavigationUpdateMoveTemplateContract(t *testing.T) {
	readFile := func(path string) string {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读 %s 失败: %v", path, err)
		}
		return string(raw)
	}

	router := readFile("navigation_page_router.go")
	if strings.Contains(router, `/navigations/move`) {
		t.Fatalf("页面 move 路由应已退役（净删 1）：%s", router)
	}
	if strings.Contains(readFile("navigation_page.go"), "func (h *navigationPageHandle) NavigationMove") {
		t.Fatalf("NavigationMove 页面 handler 应已退役")
	}

	tpl := readFile("../../../../templates/admin/navigation/navigations.html")
	for _, want := range []string{
		`data-write-api="/api/navigation/update"`,
		`data-write-mode="create"`,
		`<input type="hidden" name="action" value="move">`,
		`name="dir" value="up"`,
		`name="dir" value="down"`,
		`data-write-status`,
		// 放行标记挂外层 span：基座放行时会摘 disabled，边界语义必须留在按钮上。
		`<span data-requires-js hidden><button type="submit" form="nav-up-`,
		`<span data-requires-js hidden><button type="submit" form="nav-down-`,
	} {
		if !strings.Contains(tpl, want) {
			t.Fatalf("模板应含 %q", want)
		}
	}
	for _, forbidden := range []string{
		`action="/admin/navigations/move`,
		`name="csrf_token" value="{{csrf}}"`,
		`{{csrf := .["csrf_token"]}}`,
	} {
		if strings.Contains(tpl, forbidden) {
			t.Fatalf("模板不该再出现 %q", forbidden)
		}
	}

	// no-JS 缺口登记（task-91）：排序现在也要 JS，但页面级 noscript 复用的是既有词条
	// admin.navigations.write.noJs，而那一条的文案只点名「新建与删除」。改词条要动
	// public/migrations（不在本卡 scope），所以这里钉住现状 + 在模板注释里登记缺口：
	// 谁改了 key 或改了词条口径，这条会红，提醒把登记一起更新。
	if !strings.Contains(tpl, `admin.navigations.write.noJs`) {
		t.Fatalf("页面级 noscript 应复用既有词条 admin.navigations.write.noJs（缺口已在模板注释登记）")
	}

	forms := moveTemplateFormRe.FindAllString(tpl, -1)
	if len(forms) != 2 {
		t.Fatalf("应恰好两个声明了本端点的行内表单（up / down），实际 %d 个", len(forms))
	}
	want := make([]string, 0, len(navigationMoveKeys))
	for key := range navigationMoveKeys {
		want = append(want, key)
	}
	sort.Strings(want)
	for i, form := range forms {
		got := moveTemplateNameRe.FindAllStringSubmatch(form, -1)
		names := make([]string, 0, len(got))
		for _, m := range got {
			names = append(names, m[1])
		}
		sort.Strings(names)
		if strings.Join(names, ",") != strings.Join(want, ",") {
			t.Fatalf("第 %d 个 move 表单的具名字段应为 %v（服务端白名单），实际 %v", i+1, want, names)
		}
	}
}

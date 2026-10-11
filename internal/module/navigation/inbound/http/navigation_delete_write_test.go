package navigationhttp

// navigation_delete_write_test.go — 删除端点（POST /api/navigation/delete）的严格形状 union。
//
// 背景：导航列表页的「行内删除」与「批量删除」两条页面 POST 已迁到同一个真实 JSON 端点
// （见 navigation_delete_write.go 开头「为什么不拆成两条路径」）。两条档位靠**形状**判别：
// 单档只认 id、批档只认 ids，混用 / 未知键 / 类型不对一律 400。
//
// 为什么值得逐条钉住：猜错分支的代价是**删错数据**，而删除没有撤销。这些用例全走无库路径
// （桩 service 记录调用），断言三件事：
//   · 形状对 → 只调用一次 service（单档）或逐条调用（批档），并把结果原样回给前端；
//   · 形状不对 → 400 且**一次都不调用 service**（这是「没有猜」的可验证形态）；
//   · 上限 / 去重 / 逐条失败不中断沿用 pkg/bulkids 与 block 的口径。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"

	"github.com/spf13/viper"
)

// navigationDeleteStub 记录每次 Delete 的入参；failFor 里的 id 返回对应错误。
type navigationDeleteStub struct {
	navigationcontract.NavigationService
	calls   []string
	failFor map[string]error
}

func (s *navigationDeleteStub) Delete(_ context.Context, req *navigationdto.DeleteReq) error {
	s.calls = append(s.calls, req.ID)
	if err, ok := s.failFor[req.ID]; ok {
		return err
	}
	return nil
}

// newNavigationDeleteRouter 只挂删除 handler：用例关心的是形状判别，不涉会话 / 权限 / CSRF
// （那三层的真实链路在 public/test/navigation/feature 里).
func newNavigationDeleteRouter(t *testing.T, svc navigationcontract.NavigationService) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/api/navigation/delete", NewHandle(svc).Delete)
	return engine
}

// postNavigationDelete 发一次删除请求（真实 JSON 形状）。
func postNavigationDelete(t *testing.T, engine *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/delete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// navigationDeleteResponse 解出统一响应结构（data 段按批量回执解）。
func navigationDeleteResponse(t *testing.T, rec *httptest.ResponseRecorder) (int, string, navigationBulkDeleteResult) {
	t.Helper()
	var resp struct {
		Code    int                        `json:"code"`
		Message string                     `json:"message"`
		Data    navigationBulkDeleteResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是 JSON：%v（%s）", err, rec.Body.String())
	}
	return resp.Code, resp.Message, resp.Data
}

// TestNavigationDeleteSingleShape 单档：只发 id，成功回消息且 service 只被调用一次。
func TestNavigationDeleteSingleShape(t *testing.T) {
	stub := &navigationDeleteStub{}
	engine := newNavigationDeleteRouter(t, stub)

	rec := postNavigationDelete(t, engine, `{"id":"row-1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("单档成功应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if got := stub.calls; len(got) != 1 || got[0] != "row-1" {
		t.Fatalf("service 应恰好被调用一次且带 row-1，实际 %v", got)
	}
	_, message, _ := navigationDeleteResponse(t, rec)
	if message != navigationenums.MsgDeleteSuccess {
		t.Fatalf("单档回执应为 enums 的成功文案 key（翻译交 response），实际 %q", message)
	}
}

// TestNavigationDeleteBulkShape 批档：逐条调用 service，回执带计数与逐条结果。
//
// 「一条失败不中断」是刻意的口径：整批回滚会让用户以为一个都没删、然后反复重试。
func TestNavigationDeleteBulkShape(t *testing.T) {
	stub := &navigationDeleteStub{failFor: map[string]error{"b": errors.New(navigationenums.ErrNotFound)}}
	engine := newNavigationDeleteRouter(t, stub)

	rec := postNavigationDelete(t, engine, `{"ids":["a","b"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("批档应 200（逐条结果在 data 里，不是整批失败），实际 %d：%s", rec.Code, rec.Body.String())
	}
	if got := strings.Join(stub.calls, ","); got != "a,b" {
		t.Fatalf("批档应按顺序逐条调用 service，实际 %v", stub.calls)
	}
	_, _, data := navigationDeleteResponse(t, rec)
	if data.Deleted != 1 || data.Skipped != 1 || len(data.Items) != 2 {
		t.Fatalf("批档回执应为 deleted=1 skipped=1 items=2，实际 %+v", data)
	}
	if !data.Items[0].Deleted || data.Items[0].ID != "a" {
		t.Fatalf("第 1 条应标记为已删除，实际 %+v", data.Items[0])
	}
	if data.Items[1].Deleted || data.Items[1].ID != "b" || data.Items[1].Message == "" {
		t.Fatalf("第 2 条应标记为失败并带上原因，实际 %+v", data.Items[1])
	}
	// 「有原因」还不够：原因是显示在列表里的**成品句**，不能是 enums 的裸 key
	//（词条缺席时由本模块的兜底句接管，见 TestNavigationDeleteBulkReasonsFallBackWhenEntryMissing）。
	if data.Items[1].Message == navigationenums.ErrNotFound {
		t.Fatalf("逐条原因不该是裸 key，实际 %+v", data.Items[1])
	}
}

// postNavigationDeleteLang 与 postNavigationDelete 同形，但显式带上请求语言：
// 逐条原因是**按请求语言渲染**的，不带这一步只能测到默认语言那一支。
func postNavigationDeleteLang(t *testing.T, engine *gin.Engine, lang, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/delete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if lang != "" {
		req.Header.Set("Accept-Language", lang)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// clearNavigationEntries 把进程级词条缓存清空，让用例回到「这条词条不在库里」的世界。
//
// i18n 缓存是进程级单例（InjectForTest 直接覆盖），所以退出时同样清空 —— 清空即本包其它用例
// 的默认状态，不会给它们留下合成词条。
func clearNavigationEntries(t *testing.T) {
	t.Helper()
	i18n.InjectForTest(nil, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
}

// initNavigationDeleteLogger 初始化进程级 logger：未归口的内部错误那条**会记日志**
// （细节只进日志、不进响应），日志路径要按生产形态走一遍。
func initNavigationDeleteLogger(t *testing.T) {
	t.Helper()
	conf := viper.New()
	conf.Set("log.base_dir", t.TempDir())
	if err := logger.Init(conf); err != nil {
		t.Fatalf("初始化 logger 失败: %v", err)
	}
}

// bulkItemMessage 取批档回执里某条 id 的原因文案（没有这条就是用例自己的装置坏了）。
func bulkItemMessage(t *testing.T, res navigationBulkDeleteResult, id string) string {
	t.Helper()
	for _, item := range res.Items {
		if item.ID == id {
			return item.Message
		}
	}
	t.Fatalf("回执里没有 id=%s 的逐条结果：%+v", id, res.Items)
	return ""
}

// TestNavigationDeleteBulkReasonsFollowRequestLanguage 逐条原因按**请求语言**出成品句。
//
// 词条是合成值（不是库里的中文），所以「响应里出现了这句」只能来自「enums 的 key 真的走到了
// 取词层」这一条路径：handler 若自己拼文案、或把 key 原样发出去，这一条就会红。
func TestNavigationDeleteBulkReasonsFollowRequestLanguage(t *testing.T) {
	injectNavigationEntries(t, map[string]map[string]string{
		navigationenums.ErrNotFound: {"zh-CN": "词条-导航项不存在", "en-US": "entry-navigation-item-missing"},
	})
	stub := &navigationDeleteStub{failFor: map[string]error{"b": errors.New(navigationenums.ErrNotFound)}}
	engine := newNavigationDeleteRouter(t, stub)

	for _, tc := range []struct{ name, lang, want string }{
		{"中文", "zh-CN", "词条-导航项不存在"},
		{"英文", "en-US", "entry-navigation-item-missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := postNavigationDeleteLang(t, engine, tc.lang, `{"ids":["a","b"]}`)
			if rec.Code != http.StatusOK {
				t.Fatalf("批档应 200（逐条结果在 data 里），实际 %d：%s", rec.Code, rec.Body.String())
			}
			_, _, data := navigationDeleteResponse(t, rec)
			if got := bulkItemMessage(t, data, "b"); got != tc.want {
				t.Fatalf("%s 请求的逐条原因应为 %q，实际 %q", tc.lang, tc.want, got)
			}
		})
	}
}

// TestNavigationDeleteBulkReasonsFallBackWhenEntryMissing 词条缺席时逐条原因**不回落裸 key**。
//
// pkg/response 的 translate 在词条缺席时回落传入的 key 原文（pkg/response/response.go:321）——
// 那对顶层 message 是可接受的协议，但列表里的「原因」是显示给人看的成品句：裸 key 与数据库
// 原文同属泄漏面。四种形态都要挡住：白名单 key、key|param、key: <定位>、未归口的内部错误。
func TestNavigationDeleteBulkReasonsFallBackWhenEntryMissing(t *testing.T) {
	clearNavigationEntries(t)
	initNavigationDeleteLogger(t)

	const detail = "2026-10-09T00:00:00Z"
	cases := []struct {
		name     string
		err      error
		want     string
		wantGone []string
	}{
		{
			name:     "白名单 key",
			err:      errors.New(navigationenums.ErrNotFound),
			want:     navigationFacingFallbacks[navigationenums.ErrNotFound],
			wantGone: []string{navigationenums.ErrNotFound},
		},
		{
			name:     "带参形态",
			err:      errors.New(navigationenums.ErrPathTaken + "|/about"),
			want:     navigationFacingFallbacks[navigationenums.ErrPathTaken],
			wantGone: []string{navigationenums.ErrPathTaken, "|", "/about"},
		},
		{
			name:     "带定位形态",
			err:      errors.New(navigationenums.ErrStaleVersion + navigationenums.FacingDetailSep + detail),
			want:     navigationFacingFallbacks[navigationenums.ErrStaleVersion] + ": " + detail,
			wantGone: []string{navigationenums.ErrStaleVersion},
		},
		{
			name:     "未归口的内部错误",
			err:      errors.New("SQLSTATE 42P01: relation \"navigations\" does not exist"),
			want:     navigationErrPageFallback,
			wantGone: []string{"SQLSTATE", "42P01", "relation", "navigations"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &navigationDeleteStub{failFor: map[string]error{"bad": tc.err}}
			engine := newNavigationDeleteRouter(t, stub)
			rec := postNavigationDelete(t, engine, `{"ids":["bad"]}`)
			if rec.Code != http.StatusOK {
				t.Fatalf("逐条失败应 200（结果在 data 里），实际 %d：%s", rec.Code, rec.Body.String())
			}
			_, _, data := navigationDeleteResponse(t, rec)
			if data.Deleted != 0 || data.Skipped != 1 {
				t.Fatalf("逐条失败回执应为 deleted=0 skipped=1，实际 %+v", data)
			}
			got := bulkItemMessage(t, data, "bad")
			if got != tc.want {
				t.Fatalf("逐条原因应为 %q，实际 %q", tc.want, got)
			}
			for _, tok := range tc.wantGone {
				if strings.Contains(got, tok) {
					t.Errorf("逐条原因不该出现 %q：%q", tok, got)
				}
			}
		})
	}
}

// TestNavigationDeleteDedupesIDs 批档去重：同一条 id 发两次只删一次（pkg/bulkids 口径）。
func TestNavigationDeleteDedupesIDs(t *testing.T) {
	stub := &navigationDeleteStub{}
	engine := newNavigationDeleteRouter(t, stub)

	rec := postNavigationDelete(t, engine, `{"ids":["a","a","b",""]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("批档应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if got := strings.Join(stub.calls, ","); got != "a,b" {
		t.Fatalf("空串应被丢掉、重复 id 只删一次，实际 %v", stub.calls)
	}
	_, _, data := navigationDeleteResponse(t, rec)
	if data.Deleted != 2 || data.Skipped != 0 {
		t.Fatalf("回执计数应为 deleted=2 skipped=0，实际 %+v", data)
	}
}

// TestNavigationDeleteEmptyIDListIsNoop 空数组是**合法**形状：回执 deleted=0，不碰 service。
//
// 与 `{"ids":null}` 的区别是「说了没有」，不是「没说清楚」：前者形状说得通，
// 后者是调用方把「没取到值」当值发过来（下表逐条拒）。
func TestNavigationDeleteEmptyIDListIsNoop(t *testing.T) {
	stub := &navigationDeleteStub{}
	engine := newNavigationDeleteRouter(t, stub)

	rec := postNavigationDelete(t, engine, `{"ids":[]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("空数组应 200（页面据此显示「没有勾选」提示），实际 %d：%s", rec.Code, rec.Body.String())
	}
	if len(stub.calls) != 0 {
		t.Fatalf("空数组不该调用 service，实际 %v", stub.calls)
	}
	_, _, data := navigationDeleteResponse(t, rec)
	if data.Deleted != 0 || data.Skipped != 0 {
		t.Fatalf("空数组回执应为 0/0，实际 %+v", data)
	}
}

// TestNavigationDeleteShapeRejections 形状错的请求一律 400，且**一次都不调用 service**。
//
// 每一条都对应一种「猜错会删错东西」的形态：
//
//	· 同时给 id 与 ids —— 按某一边执行，被删的可能是调用方以为不会删的那一批；
//	· 未知键 —— 调用方按另一份契约在说话；
//	· 显式 null / 类型不对 —— encoding/json 会把它解成零值而不报错，
//	  `{"ids":null}` 于是变成一次静默空批量、`{"ids":["a",null]}` 变成静默忽略 null 项。
func TestNavigationDeleteShapeRejections(t *testing.T) {
	cases := []struct{ name, body string }{
		{"混形 id+ids", `{"id":"a","ids":["b"]}`},
		{"单档未知键", `{"id":"a","extra":1}`},
		{"单档未知键 action", `{"id":"a","action":"translations"}`},
		{"批档未知键", `{"ids":["a"],"extra":1}`},
		{"两个键都没有", `{}`},
		{"ids 显式 null", `{"ids":null}`},
		{"ids 数组里有 null", `{"ids":["a",null]}`},
		{"ids 里有数字", `{"ids":["a",1]}`},
		{"ids 不是数组", `{"ids":"a"}`},
		{"ids 是对象", `{"ids":{}}`},
		{"id 显式 null", `{"id":null}`},
		{"id 是空串", `{"id":""}`},
		{"id 是数字", `{"id":123}`},
		{"非法 JSON", `{`},
		{"空体", ``},
		{"顶层数组", `[]`},
		{"顶层 null", `null`},
		{"顶层字符串", `"x"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &navigationDeleteStub{}
			engine := newNavigationDeleteRouter(t, stub)
			rec := postNavigationDelete(t, engine, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("形状错应 400，实际 %d：%s", rec.Code, rec.Body.String())
			}
			if len(stub.calls) != 0 {
				t.Fatalf("形状错的请求不该调用 service，实际 %v", stub.calls)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
				t.Fatalf("出口应是 JSON 报文，实际 Content-Type=%q", ct)
			}
		})
	}
}

// TestNavigationDeleteBulkLimits 上限与请求体上限：超限整批拒绝、超大体在解析前被拦。
func TestNavigationDeleteBulkLimits(t *testing.T) {
	stub := &navigationDeleteStub{}
	engine := newNavigationDeleteRouter(t, stub)

	ids := make([]string, 0, bulkids.MaxIDs+1)
	for i := 0; i < bulkids.MaxIDs+1; i++ {
		ids = append(ids, `"nav-`+strconv.Itoa(i)+`"`)
	}
	rec := postNavigationDelete(t, engine, `{"ids":[`+strings.Join(ids, ",")+`]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("超限应整批拒绝（400，不部分执行），实际 %d：%s", rec.Code, rec.Body.String())
	}
	_, message, _ := navigationDeleteResponse(t, rec)
	if !strings.HasPrefix(message, bulkids.MsgTooMany+"|") {
		t.Fatalf("超限应回受控 key（%s）的带参协议，实际 %q", bulkids.MsgTooMany, message)
	}
	if !strings.Contains(message, strconv.Itoa(bulkids.MaxIDs)) ||
		!strings.Contains(message, strconv.Itoa(bulkids.MaxIDs+1)) {
		t.Fatalf("超限协议应带上限与本次条数，实际 %q", message)
	}
	if len(stub.calls) != 0 {
		t.Fatalf("超限不该逐条删除，实际 %v", stub.calls)
	}

	// 请求体上限：1 MiB 之上在解析前就被拦（不把整块 body 读进内存再判）。
	huge := `{"ids":["` + strings.Repeat("x", navigationWriteMaxBodyBytes) + `"]}`
	rec = postNavigationDelete(t, engine, huge)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("超过请求体上限应 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if len(stub.calls) != 0 {
		t.Fatalf("超大体不该调用 service，实际 %v", stub.calls)
	}
}

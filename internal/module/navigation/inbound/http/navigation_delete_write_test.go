package navigationhttp

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
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

func newNavigationDeleteRouter(t *testing.T, svc navigationcontract.NavigationService) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/api/navigation/delete", NewHandle(svc).Delete)
	return engine
}

func postNavigationDelete(t *testing.T, engine *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/delete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

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

	if data.Items[1].Message == navigationenums.ErrNotFound {
		t.Fatalf("逐条原因不该是裸 key，实际 %+v", data.Items[1])
	}
}

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

func clearNavigationEntries(t *testing.T) {
	t.Helper()
	i18n.InjectForTest(nil, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
}

func initNavigationDeleteLogger(t *testing.T) {
	t.Helper()
	conf := viper.New()
	conf.Set("log.base_dir", t.TempDir())
	if err := logger.Init(conf); err != nil {
		t.Fatalf("初始化 logger 失败: %v", err)
	}
}

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
			name:     "业务 key",
			err:      errors.New(navigationenums.ErrNotFound),
			want:     navigationenums.FallbackFor(navigationenums.ErrNotFound),
			wantGone: []string{navigationenums.ErrNotFound},
		},
		{
			name:     "带参形态",
			err:      errors.New(navigationenums.ErrPathTaken + "|/about"),
			want:     navigationenums.FallbackFor(navigationenums.ErrPathTaken),
			wantGone: []string{navigationenums.ErrPathTaken, "|", "/about"},
		},
		{
			name:     "带定位形态",
			err:      navigationenums.ErrDetail(navigationenums.ErrStaleVersion, detail),
			want:     navigationenums.FallbackFor(navigationenums.ErrStaleVersion) + ": " + detail,
			wantGone: []string{navigationenums.ErrStaleVersion},
		},
		{
			name:     "未归口的内部错误",
			err:      errors.New("SQLSTATE 42P01: relation \"navigations\" does not exist"),
			want:     navigationInternalFallback,
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

	huge := `{"ids":["` + strings.Repeat("x", navigationWriteMaxBodyBytes) + `"]}`
	rec = postNavigationDelete(t, engine, huge)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("超过请求体上限应 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if len(stub.calls) != 0 {
		t.Fatalf("超大体不该调用 service，实际 %v", stub.calls)
	}
}

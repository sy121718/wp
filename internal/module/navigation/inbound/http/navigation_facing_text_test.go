package navigationhttp

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go_wp/internal/adminui"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

type navigationErrStub struct {
	navigationcontract.NavigationService
	err error
}

func (s *navigationErrStub) Create(context.Context, *navigationdto.CreateReq) (*navigationdto.NavigationResp, error) {
	return nil, s.err
}

func (s *navigationErrStub) Update(context.Context, *navigationdto.UpdateReq) (*navigationdto.NavigationResp, error) {
	return nil, s.err
}

func (s *navigationErrStub) Get(context.Context, *navigationdto.GetReq) (*navigationdto.NavigationResp, error) {
	return nil, s.err
}

func (s *navigationErrStub) List(context.Context, *navigationdto.ListReq) ([]*navigationdto.NavigationResp, error) {
	return nil, s.err
}

func (s *navigationErrStub) Delete(context.Context, *navigationdto.DeleteReq) error {
	return s.err
}

var navigationErrRoutes = []struct {
	name, method, path, body string
}{
	{"create", http.MethodPost, "/create", `{"projectId":"p1","title":"t","path":"/t","kind":"header"}`},
	{"update", http.MethodPost, "/update", `{"id":"row-1"}`},
	{"get", http.MethodGet, "/get?id=row-1", ""},
	{"list", http.MethodGet, "/list?projectId=p1&kind=header", ""},
	{"delete", http.MethodPost, "/delete", `{"id":"row-1"}`},
}

func newNavigationErrRouter(t *testing.T, svc navigationcontract.NavigationService) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	conf := viper.New()
	conf.Set("log.base_dir", "/tmp/navigation-facing-text-test-logs")
	if err := logger.Init(conf); err != nil {
		t.Fatal(err)
	}
	h := NewHandle(svc)
	r := gin.New()
	r.POST("/create", h.Create)
	r.POST("/update", h.Update)
	r.GET("/get", h.Get)
	r.GET("/list", h.List)
	r.POST("/delete", h.Delete)
	return r
}

func callNavigationJSON(t *testing.T, engine *gin.Engine, route struct{ name, method, path, body string }, acceptLang string) (int, response.Response) {
	t.Helper()
	req := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
	req.Header.Set("Accept-Language", acceptLang)
	if route.body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	body := response.Response{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s 响应不是 JSON（%v）：%s", route.name, err, rec.Body.String())
	}
	return rec.Code, body
}

func navigationLangCtx(acceptLang string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/navigations", nil)
	c.Request.Header.Set("Accept-Language", acceptLang)
	return c
}

func injectNavigationEntries(t *testing.T, entries map[string]map[string]string) {
	t.Helper()
	previous := make(map[string]map[string]string, len(entries))
	for key, langs := range entries {
		snapshot := make(map[string]string, len(langs))
		for lang := range langs {
			snapshot[lang] = i18n.GetText(key, lang)
		}
		previous[key] = snapshot
	}
	i18n.InjectForTest(entries, map[string]int{})
	t.Cleanup(func() { i18n.InjectForTest(previous, map[string]int{}) })
}

func TestNavigationJSONErrorTranslatesByRequestLanguage(t *testing.T) {
	injectNavigationEntries(t, map[string]map[string]string{
		navigationenums.ErrNotFound: {"zh-CN": "词条-导航项不存在", "en-US": "entry-item-missing"},
	})

	engine := newNavigationErrRouter(t, &navigationErrStub{err: errors.New(navigationenums.ErrNotFound)})
	for _, lang := range []struct{ accept, want string }{
		{"zh-CN", "词条-导航项不存在"},
		{"en-US", "entry-item-missing"},
	} {
		for _, route := range navigationErrRoutes {
			code, body := callNavigationJSON(t, engine, route, lang.accept)
			if code != http.StatusNotFound {
				t.Errorf("%s[%s] 业务错误状态码应保持 404，实际 %d", route.name, lang.accept, code)
			}
			if body.Message != lang.want {
				t.Errorf("%s[%s] 响应文案应为该语言词条 %q，实际 %q", route.name, lang.accept, lang.want, body.Message)
			}
		}
	}
}

func TestNavigationJSONErrorNormalizesDetailProtocol(t *testing.T) {
	injectNavigationEntries(t, map[string]map[string]string{
		navigationenums.ErrStaleVersion: {"zh-CN": "词条-已被他人修改", "en-US": "entry-stale"},
	})

	err := navigationenums.ErrDetail(navigationenums.ErrStaleVersion, "产品详情页")
	engine := newNavigationErrRouter(t, &navigationErrStub{err: err})

	for _, lang := range []struct{ accept, want string }{

		{"zh-CN", "词条-已被他人修改: 产品详情页"},
		{"en-US", "entry-stale: 产品详情页"},
	} {
		code, body := callNavigationJSON(t, engine, navigationErrRoutes[0], lang.accept)
		if code != http.StatusConflict {
			t.Fatalf("[%s] 乐观锁冲突应保持 409，实际 %d", lang.accept, code)
		}
		if body.Message != lang.want {
			t.Errorf("[%s] 响应文案应为 %q，实际 %q", lang.accept, lang.want, body.Message)
		}
		if strings.Contains(body.Message, navigationenums.ErrStaleVersion) {
			t.Errorf("[%s] 响应里出现了裸 key：%q", lang.accept, body.Message)
		}
	}

	page := adminui.MsgText(navigationLangCtx("zh-CN"), navigationErrMsg(navigationLangCtx("zh-CN"), err))
	if page != "词条-已被他人修改：产品详情页" {
		t.Errorf("页面出口应保留全角分隔符的整句，实际 %q", page)
	}
	if en := adminui.MsgText(navigationLangCtx("en-US"), navigationErrMsg(navigationLangCtx("en-US"), err)); en != "entry-stale: 产品详情页" {
		t.Errorf("英文页面出口应为 %q，实际 %q", "entry-stale: 产品详情页", en)
	}
}

func TestNavigationJSONErrorHidesInternalAndKeepsParamProtocol(t *testing.T) {
	injectNavigationEntries(t, map[string]map[string]string{
		navigationInternalKey:           {"zh-CN": "词条-操作失败", "en-US": "entry-internal"},
		navigationenums.ErrInvalidParam: {"zh-CN": "词条-参数无效：%s", "en-US": "entry-invalid: %s"},
	})

	raw := `sql: no rows in table "navigations" (SQLSTATE 42P01)`
	engine := newNavigationErrRouter(t, &navigationErrStub{err: errors.New(raw)})
	for _, lang := range []struct{ accept, want string }{
		{"zh-CN", "词条-操作失败"},
		{"en-US", "entry-internal"},
	} {
		code, body := callNavigationJSON(t, engine, navigationErrRoutes[0], lang.accept)
		if code != http.StatusInternalServerError {
			t.Fatalf("[%s] 内部错误应为 500，实际 %d", lang.accept, code)
		}
		if body.Message != lang.want {
			t.Errorf("[%s] 内部错误应回落归口文案 %q，实际 %q", lang.accept, lang.want, body.Message)
		}
		for _, leak := range []string{"navigations", "SQLSTATE", "sql:"} {
			if strings.Contains(body.Message, leak) {
				t.Errorf("[%s] 响应泄露了内部原文片段 %q：%q", lang.accept, leak, body.Message)
			}
		}

		if strings.Contains(body.Message, "细节只进日志") {
			t.Errorf("[%s] JSON 出口混入了页面出口的兜底句：%q", lang.accept, body.Message)
		}
	}

	engine = newNavigationErrRouter(t, &navigationErrStub{
		err: errors.New(navigationenums.ErrInvalidParam + "|/about"),
	})
	code, body := callNavigationJSON(t, engine, navigationErrRoutes[0], "zh-CN")
	if code != http.StatusBadRequest {
		t.Fatalf("参数类业务错误应保持 400，实际 %d", code)
	}
	if body.Message != "词条-参数无效：/about" {
		t.Errorf("带参形态应由取词层填参，实际 %q", body.Message)
	}
}

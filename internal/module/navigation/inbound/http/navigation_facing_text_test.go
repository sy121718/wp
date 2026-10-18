package navigationhttp

// navigation_facing_text_test.go — JSON 出口与页面出口的文案归口（审计 CQ-009 后续：消除预翻译）。
//
// 守三件事，全是「换个语言才看得见」或「只在异常路径上出现」的：
//
//	1. JSON 接口发出去的是**未翻译**的 enums key，翻译由 pkg/response 按请求语言做 ——
//	   zh / en 各断言一次（英文界面看到中文，只有切语言才暴露，日志干净、不报错）；
//	2. 「key：<定位>」形态必须先对齐 pkg/response 的协议（半角 ": "）再交出去：
//	   原样交出会三种形态全不命中，前端拿到的就是**裸 key**（迁移 290 的注释记过这个形态）；
//	3. 内部错误（SQL / 表名 / 约束名）只进日志，响应是归口文案；而**页面出口**保留
//	   给人看的一句话（中文仍用全角「：」）—— 两个出口形态不同是有意的。
//
// 断言用的是注入的**合成词条**（不是库里的中文），所以「响应里出现了这句」只能来自
// 「enums 的 key 真的交给了取词层」这一条路径。

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

// navigationErrStub 桩：五个 JSON 接口都返回同一个错误（只驱动错误出口，不碰库）。
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

// navigationErrRoutes 五个接口的调用形态：错误出口是同一个，所以每个都过一遍。
var navigationErrRoutes = []struct {
	name, method, path, body string
}{
	{"create", http.MethodPost, "/create", `{"projectId":"p1","title":"t","path":"/t","kind":"header"}`},
	{"update", http.MethodPost, "/update", `{"id":"row-1"}`},
	{"get", http.MethodGet, "/get?id=row-1", ""},
	{"list", http.MethodGet, "/list?projectId=p1&kind=header", ""},
	{"delete", http.MethodPost, "/delete", `{"id":"row-1"}`},
}

// newNavigationErrRouter 只挂这五个 handler：本用例验证的是出口文案，与权限/会话无关。
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

// callNavigationJSON 带请求语言调一次接口，返回状态码与 JSON 响应体。
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

// navigationLangCtx 造一个带请求语言的上下文（页面出口的直接调用用）。
func navigationLangCtx(acceptLang string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/navigations", nil)
	c.Request.Header.Set("Accept-Language", acceptLang)
	return c
}

// injectNavigationEntries 注入用例要用的词条，并在结束时**还原原值**。
//
// 必须还原：i18n 缓存是进程级单例，同包的页面/片段用例也走它 —— 注进去不管，
// 别处的取词会拿到这里的合成词条，以「与本次改动无关的失败」变红。
// 还原成 key 本身（GetText 对缺失词条的返回）与「没有词条」在取词层等价：
// i18n.Translate 与 pkg/response 的 translate 都只按 `text != key` 判断是否命中。
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

// TestNavigationJSONErrorTranslatesByRequestLanguage JSON 出口按请求语言出文案。
//
// 词条是合成值，所以「响应里出现这句」等价于「enums 的 key 真的到了取词层」——
// 本模块若又在自己这边翻一遍（或翻完再交），这一条不会因此变绿或变红，
// 但协议形态那两条会（见下面两个用例）。
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

// TestNavigationJSONErrorNormalizesDetailProtocol 「key：定位」按 pkg/response 的协议交付。
func TestNavigationJSONErrorNormalizesDetailProtocol(t *testing.T) {
	injectNavigationEntries(t, map[string]map[string]string{
		navigationenums.ErrStaleVersion: {"zh-CN": "词条-已被他人修改", "en-US": "entry-stale"},
	})

	// 写侧（service.staleVersionMessage → enums.ErrDetail）按协议拼的是半角「: 」。
	err := navigationenums.ErrDetail(navigationenums.ErrStaleVersion, "产品详情页")
	engine := newNavigationErrRouter(t, &navigationErrStub{err: err})

	for _, lang := range []struct{ accept, want string }{
		// 「词条 + “: ” + 定位」：分隔符是 pkg/response 形态 3 保留的那一段，
		// 不是写侧的全角「：」—— 这正是「对齐协议」被验证的地方。
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

	// 页面出口不跟着换：那里是要塞进 HTML 的一句话，中文仍用全角「：」。
	// 出口现在给的是**事实**（adminui.Msg），所以这里按渲染期的方式取词再断言。
	page := adminui.MsgText(navigationLangCtx("zh-CN"), navigationErrMsg(navigationLangCtx("zh-CN"), err))
	if page != "词条-已被他人修改：产品详情页" {
		t.Errorf("页面出口应保留全角分隔符的整句，实际 %q", page)
	}
	if en := adminui.MsgText(navigationLangCtx("en-US"), navigationErrMsg(navigationLangCtx("en-US"), err)); en != "entry-stale: 产品详情页" {
		t.Errorf("英文页面出口应为 %q，实际 %q", "entry-stale: 产品详情页", en)
	}
}

// TestNavigationJSONErrorHidesInternalAndKeepsParamProtocol 内部错误归口 + 带参协议不重复实现。
func TestNavigationJSONErrorHidesInternalAndKeepsParamProtocol(t *testing.T) {
	injectNavigationEntries(t, map[string]map[string]string{
		navigationInternalKey:     {"zh-CN": "词条-操作失败", "en-US": "entry-internal"},
		navigationenums.ErrInvalidParam: {"zh-CN": "词条-参数无效：%s", "en-US": "entry-invalid: %s"},
	})

	// 未命中判据 = 内部错误：原文只进日志，响应是归口文案。
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
		// 页面出口的中文兜底句不该出现在 JSON 里（两个出口的文案形态不同是有意的）。
		if strings.Contains(body.Message, "细节只进日志") {
			t.Errorf("[%s] JSON 出口混入了页面出口的兜底句：%q", lang.accept, body.Message)
		}
	}

	// 带参形态：%s 由 pkg/response 填（本模块不实现第二套参数协议）。
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

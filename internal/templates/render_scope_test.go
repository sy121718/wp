package templates_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/CloudyKit/jet/v6"
	"github.com/gin-gonic/gin"

	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
	text "go_wp/pkg/view/jet"
)

// 这一组用例只有一条原则：**不手工拼 VarMap，也不手工调 renderWith**。
// 走的是生产链：请求头/cookie 定语言 → text.AttachScope → ScopeWriter →
// gin c.Render → jetInstance.Render 取 VarMap → Execute。

const (
	langZh = "zh-CN"
	langEn = "en-US"
)

// 词库：同一个 key 两种语言各一份，用来证明「每条响应拿到的是自己请求的语言」。
func injectProbeWords(t *testing.T) {
	t.Helper()
	i18n.InjectForTest(map[string]map[string]string{
		"probe.title": {langZh: "中文标题", langEn: "English Title"},
		"probe.count": {langZh: "共 %s 条", langEn: "%s items"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
}

// newScopeEngine 按生产接线搭引擎：mid 里同时挂取词上下文与失败出口。
func newScopeEngine(t *testing.T, files map[string]string, register func(engine *gin.Engine)) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	loader := jet.NewInMemLoader()
	for name, src := range files {
		loader.Set(name, src)
	}
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	templates.InjectGlobalsForTest(set)

	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRenderWithSet(set)
	engine.Use(templates.FailedRenderMiddleware(func(c *gin.Context, err error) {
		response.ErrorStatusAuto(c, "template", err)
	}))
	engine.Use(func(c *gin.Context) {
		text.AttachScope(c)
		c.Next()
	})

	register(engine)
	return engine
}

func getWithLang(engine *gin.Engine, path, lang string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Accept-Language", lang)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// 跨请求语言隔离：同一个 *jet.Set，多语言并发，**每条响应按它自己请求的语言精确断言**。
//
// 关键在断言方式：不能只数「中文出现 N 次、英文出现 N 次」——
// 那样两种语言互换（中文请求拿到英文标题）也会通过。这里把每个请求的
// 语言和它自己的响应体绑在一个 struct 里回传。
func TestLanguageIsolatedPerRequestThroughRealWiring(t *testing.T) {
	injectProbeWords(t)

	engine := newScopeEngine(t, map[string]string{
		// include 传 struct：同时验证「struct 也能取词」这条在本批成立。
		"page.html": `[{{t("probe.title", "兜底")}}|{{tf("probe.count", "共 %s 条", .Args)}}]`,
	}, func(engine *gin.Engine) {
		engine.GET("/page", func(c *gin.Context) {
			type payload struct{ Args []string }
			c.HTML(http.StatusOK, "page.html", payload{Args: []string{"3"}})
		})
	})

	const rounds = 40
	type result struct {
		lang string
		body string
	}
	ch := make(chan result, rounds*2)

	var wg sync.WaitGroup
	for i := 0; i < rounds; i++ {
		for _, lang := range []string{langZh, langEn} {
			wg.Add(1)
			go func(lang string) {
				defer wg.Done()
				rec := getWithLang(engine, "/page", lang)
				ch <- result{lang: lang, body: rec.Body.String()}
			}(lang)
		}
	}
	wg.Wait()
	close(ch)

	want := map[string]string{
		langZh: "[中文标题|共 3 条]",
		langEn: "[English Title|3 items]",
	}
	seen := map[string]int{}
	for r := range ch {
		seen[r.lang]++
		// 每条响应都必须**恰好**是它自己语言的句子：互换、串语言、半中半英都算失败。
		if r.body != want[r.lang] {
			t.Fatalf("%s 请求拿到 %q，want %q", r.lang, r.body, want[r.lang])
		}
	}
	if seen[langZh] != rounds || seen[langEn] != rounds {
		t.Fatalf("样本数不对：%v（各应 %d）", seen, rounds)
	}
}

// 同一个 Set 上串行交替两种语言也必须各自正确 —— 防「第一次请求的语言被缓存」。
func TestLanguageSwitchesAcrossRequestsOnSameSet(t *testing.T) {
	injectProbeWords(t)

	engine := newScopeEngine(t, map[string]string{
		"page.html": `{{t("probe.title", "兜底")}}`,
	}, func(engine *gin.Engine) {
		engine.GET("/page", func(c *gin.Context) {
			c.HTML(http.StatusOK, "page.html", map[string]any{})
		})
	})

	for i := 0; i < 3; i++ {
		if got := getWithLang(engine, "/page", langZh).Body.String(); got != "中文标题" {
			t.Fatalf("第 %d 轮中文拿到 %q", i+1, got)
		}
		if got := getWithLang(engine, "/page", langEn).Body.String(); got != "English Title" {
			t.Fatalf("第 %d 轮英文拿到 %q", i+1, got)
		}
	}
}

// 语言也可以从 cookie 来（requestLanguage 的 cookie > query > Accept-Language）。
// 这条证明取词上下文读的是「真正决定语言的那一处」，不是请求头随便挑的。
func TestLanguageComesFromCookieNotOnlyHeader(t *testing.T) {
	injectProbeWords(t)

	engine := newScopeEngine(t, map[string]string{
		"page.html": `{{t("probe.title", "兜底")}}`,
	}, func(engine *gin.Engine) {
		engine.GET("/page", func(c *gin.Context) {
			c.HTML(http.StatusOK, "page.html", map[string]any{})
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/page", nil)
	req.Header.Set("Accept-Language", langZh)
	req.AddCookie(&http.Cookie{Name: response.LangCookieName, Value: langEn})
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if got := rec.Body.String(); got != "English Title" {
		t.Fatalf("cookie 应优先于 Accept-Language，拿到 %q", got)
	}
}

// include 传 struct：片段里的 t / tf 必须可用（生产链，非手工 VarMap）。
func TestIncludeStructGetsTranslateFuncsThroughRealWiring(t *testing.T) {
	injectProbeWords(t)

	engine := newScopeEngine(t, map[string]string{
		"page.html":  `{{include "row" .Row}}`,
		"row.html":   `[{{t("probe.title", "兜底")}}|{{tf("probe.count", "共 %s 条", .Args)}}]`,
		"deep1.html": `{{include "deep2" .}}`,
		"deep2.html": `{{include "row" .}}`,
	}, func(engine *gin.Engine) {
		type row struct{ Args []string }
		engine.GET("/struct", func(c *gin.Context) {
			c.HTML(http.StatusOK, "page.html", map[string]any{"Row": row{Args: []string{"7"}}})
		})
		engine.GET("/deep", func(c *gin.Context) {
			c.HTML(http.StatusOK, "deep1.html", row{Args: []string{"7"}})
		})
	})

	if got := getWithLang(engine, "/struct", langZh).Body.String(); got != "[中文标题|共 7 条]" {
		t.Fatalf("include 传 struct 取词失败：%q", got)
	}
	if got := getWithLang(engine, "/deep", langZh).Body.String(); got != "[中文标题|共 7 条]" {
		t.Fatalf("两层 include + struct 根取词失败：%q", got)
	}
}

// 对照：撤掉 AttachScope 后同样页面取不到词 ——
// 证明上面几条通过的原因确实是这条接线，而不是别处兜了底。
func TestWithoutAttachScopeTranslateFallsBack(t *testing.T) {
	injectProbeWords(t)
	gin.SetMode(gin.TestMode)

	loader := jet.NewInMemLoader()
	loader.Set("page.html", `[{{t("probe.title", "兜底")}}]`)
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))

	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRenderWithSet(set)
	engine.GET("/page", func(c *gin.Context) {
		c.HTML(http.StatusOK, "page.html", map[string]any{})
	})

	// 取词函数不存在时 Jet 报错，渲染失败被本包异常路径接住（无出口中间件 → 空响应），
	// 两种形态都可接受；唯一不能接受的是「没有接线却拿到了译文」。
	out := getWithLang(engine, "/page", langZh).Body.String()
	if strings.Contains(out, "中文标题") {
		t.Fatalf("没有 AttachScope 却取到了译文，说明取值来源不是本批接线：%q", out)
	}
}

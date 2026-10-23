package navigationhttp

// navigation_translation_write_test.go — /api/navigation/update 的**严格 tagged union** 形状契约。
//
// 一条路径现在承担两件事：旧的导航项更新，与译文整表提交（action="translations"）。
// 两者共用同一个权限点与同一条真实路由，所以「判别键写得模棱两可」「两条形状混在一起」
// 这类缺陷不会有任何编译期或运行期报错 —— 只会在某天把不该写的数据写进去。
// 本文件把形状判据（含拒绝面）逐条钉住，全部无库：形状判定必须发生在碰数据库**之前**。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
)

// translationWriteNavigationStub 只实现 Update（其余走嵌入的 nil 接口）。
type translationWriteNavigationStub struct {
	navigationcontract.NavigationService
	calls []*navigationdto.UpdateReq
}

func (s *translationWriteNavigationStub) Update(_ context.Context, req *navigationdto.UpdateReq) (*navigationdto.NavigationResp, error) {
	s.calls = append(s.calls, req)
	return &navigationdto.NavigationResp{ID: req.ID, Title: "示例"}, nil
}

func newTranslationWriteRouter(svc navigationcontract.NavigationService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/navigation/update", NewHandle(svc).Update)
	return router
}

func postNavigationWrite(t *testing.T, router *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/update", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// TestNavigationUpdateLegacyShapeIsUnchanged 缺 action = 旧的更新请求，行为不变。
//
// 三条硬事实：`binding:"required"` 的 id 仍然生效（裸 json.Unmarshal 会静默跳过它）、
// 旧契约里多出来的未知字段仍被忽略（不借这次迁移收紧）、译文分支的键一出现就拒（混形）。
func TestNavigationUpdateLegacyShapeIsUnchanged(t *testing.T) {
	stub := &translationWriteNavigationStub{}
	router := newTranslationWriteRouter(stub)

	ok := postNavigationWrite(t, router, `{"id":"nav-1","title":"首页","unknownField":123}`)
	if ok.Code != http.StatusOK {
		t.Fatalf("旧形状（含未知字段）应 200，实际 %d body=%s", ok.Code, ok.Body.String())
	}
	if len(stub.calls) != 1 {
		t.Fatalf("旧形状应走到 service.Update，实际调用 %d 次", len(stub.calls))
	}
	got := stub.calls[0]
	if got.ID != "nav-1" || got.Title == nil || *got.Title != "首页" {
		t.Fatalf("旧形状的绑定结果被改动：%+v", got)
	}

	rejects := []struct {
		name string
		body string
	}{
		{"缺 id（binding:required 必须仍然生效）", `{"title":"没有 id"}`},
		{"空 id", `{"id":""}`},
		{"混形 rows", `{"id":"nav-1","rows":[]}`},
		{"混形 project", `{"id":"nav-1","project":"p-1"}`},
		{"混形 lang", `{"id":"nav-1","lang":"en-US"}`},
		{"不是对象", `[]`},
		{"JSON 非法", `{`},
		{"空体", ``},
		{"显式 null", `null`},
	}
	for _, tc := range rejects {
		t.Run(tc.name, func(t *testing.T) {
			before := len(stub.calls)
			rec := postNavigationWrite(t, router, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("应 400，实际 %d body=%s", rec.Code, rec.Body.String())
			}
			if len(stub.calls) != before {
				t.Fatal("形状被拒时不许走到 service（形状判定必须发生在写之前）")
			}
		})
	}
}

// TestNavigationUpdateTranslationShapeIsStrict 译文分支：判别键与字段白名单一票否决。
func TestNavigationUpdateTranslationShapeIsStrict(t *testing.T) {
	stub := &translationWriteNavigationStub{}
	router := newTranslationWriteRouter(stub)
	// 无论形状多合法，句柄未注入时也只能 fail closed —— 这里断言的是「形状被拒」这一档。
	SetNavigationTranslationWriter(nil)
	defer SetNavigationTranslationWriter(nil)

	valid := `{"action":"translations","project":"p-1","lang":"en-US","rows":[{"context":"navigation.label","hash":"h-1","target":"Home"}]}`

	shapeRejects := []struct {
		name string
		body string
	}{
		{"action 为 null", `{"action":null,"project":"p-1","lang":"en-US","rows":[]}`},
		{"action 为数字", `{"action":5,"project":"p-1","lang":"en-US","rows":[]}`},
		{"action 为布尔", `{"action":true,"project":"p-1","lang":"en-US","rows":[]}`},
		{"action 为空串", `{"action":"","project":"p-1","lang":"en-US","rows":[]}`},
		{"action 未知值", `{"action":"delete","project":"p-1","lang":"en-US","rows":[]}`},
		{"顶层未知键", `{"action":"translations","project":"p-1","lang":"en-US","rows":[],"keyword":"x"}`},
		{"缺 project", `{"action":"translations","lang":"en-US","rows":[]}`},
		{"project 为空", `{"action":"translations","project":"","lang":"en-US","rows":[]}`},
		{"project 非字符串", `{"action":"translations","project":7,"lang":"en-US","rows":[]}`},
		{"缺 lang", `{"action":"translations","project":"p-1","rows":[]}`},
		{"lang 不受支持", `{"action":"translations","project":"p-1","lang":"de-DE","rows":[]}`},
		{"缺 rows", `{"action":"translations","project":"p-1","lang":"en-US"}`},
		{"rows 为 null", `{"action":"translations","project":"p-1","lang":"en-US","rows":null}`},
		{"rows 不是数组", `{"action":"translations","project":"p-1","lang":"en-US","rows":{}}`},
		{"行不是对象", `{"action":"translations","project":"p-1","lang":"en-US","rows":[1]}`},
		{"行有未知键", `{"action":"translations","project":"p-1","lang":"en-US","rows":[{"context":"c","hash":"h","target":"t","extra":1}]}`},
		{"行的值是数字", `{"action":"translations","project":"p-1","lang":"en-US","rows":[{"context":"c","hash":"h","target":7}]}`},
		{"行的值是 null", `{"action":"translations","project":"p-1","lang":"en-US","rows":[{"context":"c","hash":null,"target":"t"}]}`},
		// 缺键与「显式空串」是两件事：只有后者是契约里的「本次不改动这一行」。
		// 缺键若被放行，下游会把零值的 target 当成空译文跳过 → 形状错误静默变成
		// 200 + written:0（客户端看不出一个字段都没提交）。
		{"行缺 target", `{"action":"translations","project":"p-1","lang":"en-US","rows":[{"context":"c","hash":"h"}]}`},
		{"行缺 context", `{"action":"translations","project":"p-1","lang":"en-US","rows":[{"hash":"h","target":"t"}]}`},
		{"行缺 hash", `{"action":"translations","project":"p-1","lang":"en-US","rows":[{"context":"c","target":"t"}]}`},
		{"第二行缺 target", `{"action":"translations","project":"p-1","lang":"en-US","rows":[{"context":"c","hash":"h","target":"t"},{"context":"c","hash":"h2"}]}`},
	}
	// 先确认夹具本身是合法形状（否则「全都 400」也可能是判别键写错了造成的假绿）。
	if rec := postNavigationWrite(t, router, valid); rec.Code != http.StatusInternalServerError {
		t.Fatalf("合法形状 + 未注入句柄应 500（fail closed），实际 %d body=%s", rec.Code, rec.Body.String())
	}
	for _, tc := range shapeRejects {
		t.Run(tc.name, func(t *testing.T) {
			before := len(stub.calls)
			rec := postNavigationWrite(t, router, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("应 400，实际 %d body=%s", rec.Code, rec.Body.String())
			}
			if len(stub.calls) != before {
				t.Fatal("形状被拒时不许走到 service.Update")
			}
		})
	}
}

// TestNavigationUpdateTranslationEmptyTargetPassesShape 显式空 target 是合法形状。
//
// 与上面那条「行缺 target 必须 400」配对：两条一起才说明判据卡的是**键是否存在**，
// 不是「值是否为空」。本用例里句柄未注入，所以合法形状会走到 fail closed 的 500 ——
// 这里断言 500 而不是 200，正是为了证明请求**穿过了形状层**。
func TestNavigationUpdateTranslationEmptyTargetPassesShape(t *testing.T) {
	router := newTranslationWriteRouter(&translationWriteNavigationStub{})
	SetNavigationTranslationWriter(nil)
	defer SetNavigationTranslationWriter(nil)

	body := `{"action":"translations","project":"p-1","lang":"en-US","rows":[{"context":"navigation.label","hash":"h-1","target":""}]}`
	if rec := postNavigationWrite(t, router, body); rec.Code != http.StatusInternalServerError {
		t.Fatalf("显式空 target 是合法形状，应穿过形状层到 fail closed 的 500，实际 %d body=%s",
			rec.Code, rec.Body.String())
	}
}

// TestNavigationUpdateTranslationLimits 行数上限与请求体上限都在解析/写入之前拦下。
func TestNavigationUpdateTranslationLimits(t *testing.T) {
	stub := &translationWriteNavigationStub{}
	router := newTranslationWriteRouter(stub)
	SetNavigationTranslationWriter(nil)
	defer SetNavigationTranslationWriter(nil)

	row := `{"context":"navigation.label","hash":"h","target":"Home"}`
	mk := func(n int) string {
		items := make([]string, 0, n)
		for i := 0; i < n; i++ {
			items = append(items, row)
		}
		return `{"action":"translations","project":"p-1","lang":"en-US","rows":[` + strings.Join(items, ",") + `]}`
	}
	if rec := postNavigationWrite(t, router, mk(navigationTranslationMaxRows+1)); rec.Code != http.StatusBadRequest {
		t.Fatalf("超过 %d 行应 400，实际 %d", navigationTranslationMaxRows, rec.Code)
	}
	// 恰好等于上限：形状合法 → 走到「句柄未注入」这一档（500），说明上限本身没被多算一行。
	if rec := postNavigationWrite(t, router, mk(navigationTranslationMaxRows)); rec.Code != http.StatusInternalServerError {
		t.Fatalf("恰好 %d 行应通过形状判定（500 fail closed），实际 %d body=%s",
			navigationTranslationMaxRows, rec.Code, rec.Body.String())
	}
	// 请求体上限：解析之前就要拒（否则超大 body 会先进内存）。
	huge := `{"action":"translations","project":"p-1","lang":"en-US","rows":[],"pad":"` +
		strings.Repeat("x", navigationWriteMaxBodyBytes) + `"}`
	if rec := postNavigationWrite(t, router, huge); rec.Code != http.StatusBadRequest {
		t.Fatalf("超过 %d 字节的请求体应 400，实际 %d", navigationWriteMaxBodyBytes, rec.Code)
	}
}

// TestNavigationUpdateTranslationsFailClosed 句柄未装配：拒绝，而不是「接受请求但什么都没写」。
func TestNavigationUpdateTranslationsFailClosed(t *testing.T) {
	stub := &translationWriteNavigationStub{}
	router := newTranslationWriteRouter(stub)
	SetNavigationTranslationWriter(nil)
	defer SetNavigationTranslationWriter(nil)

	rec := postNavigationWrite(t, router,
		`{"action":"translations","project":"p-1","lang":"en-US","rows":[{"context":"navigation.label","hash":"h","target":"Home"}]}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("未装配句柄应 500，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":500`) {
		t.Fatalf("500 响应体形态不对：%s", rec.Body.String())
	}
	if len(stub.calls) != 0 {
		t.Fatal("译文分支不许落到导航项更新上")
	}
}

// TestNavigationTranslationWriteRouteAndTemplateContract 路由/权限/模板三条声明事实。
//
// 这一层是**源码级**断言（无库、无装配）：本模块拿不到 authorizedAPI 组（它在
// internal/routers/assembly.go 里装配），所以用「唯一注册点 + 句子形态」钉住：
//   - 译文写只有一条路径：POST /api/navigation/update，权限点仍是调度的 NavigationUpdate；
//   - 页面侧的旧 POST 已经不存在（既没有路由，也没有模板表单会发它）；
//   - 模板声明了行数组与「成功回跳」，且保存按钮无 JS 时是真 disabled。
func TestNavigationTranslationWriteRouteAndTemplateContract(t *testing.T) {
	readFile := func(path string) string {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读 %s 失败: %v", path, err)
		}
		return string(raw)
	}

	apiRouter := readFile("navigation_router.go")
	if !strings.Contains(apiRouter, `g.POST("/update", permission.NavigationUpdate, handle.Update)`) {
		t.Fatal("译文写必须挂在调度的 POST /api/navigation/update（同一权限点、同一真实路由）上")
	}

	pageRouter := readFile("navigation_page_router.go")
	if strings.Contains(pageRouter, "translations/save") {
		t.Fatal("页面侧的译文保存 POST 必须已经删除")
	}
	if !strings.Contains(pageRouter, "SetNavigationTranslationWriter(translations)") {
		t.Fatal("装配期必须把带端口的译文句柄交给写入口（否则译文分支只能 fail closed）")
	}

	template := readFile("../../../../templates/admin/navigation/navigation_translations.html")
	for _, want := range []string{
		`data-write-url="/api/navigation/update"`,
		`data-write-rows=".nav-tr-row;context,hash,target"`,
		`data-write-nav="{{ .["SuccessURL"] }}"`,
		`name="action" value="translations"`,
		`disabled data-requires-js`,
	} {
		if !strings.Contains(template, want) {
			t.Errorf("模板缺少保存契约声明: %s", want)
		}
	}
	for _, gone := range []string{"rowTarget", "rowContext", "rowHash", "translations/save"} {
		if strings.Contains(template, gone) {
			t.Errorf("模板里仍有并行数组时代的痕迹 %q（按位置对齐的隐式契约必须消失）", gone)
		}
	}
}

// TestNavigationPlaceholderText 词条占位符的两种写法都要填对。
//
// 同一批词条的中英占位符风格不同（zh 写 %s / %d，en 写 {detail} / {count}，见迁移 451）。
// 只会 Sprintf 的实现对英文词条会产出 `Save failed: {detail}%!(EXTRA string=…)`：
// 句子没填上，还多了一串只应出现在调试输出里的东西 —— 而它出现的位置是给运营看的响应体。
func TestNavigationPlaceholderText(t *testing.T) {
	cases := []struct {
		name   string
		tpl    string
		values []string
		want   string
	}{
		{"zh 的位置参数 %s", "保存失败：%s", []string{"操作失败"}, "保存失败：操作失败"},
		{"zh 的位置参数 %d", "已保存 %d 条译文。", []string{"3"}, "已保存 3 条译文。"},
		{"en 的具名参数 {detail}", "Save failed: {detail}", []string{"Operation failed"}, "Save failed: Operation failed"},
		{"en 的具名参数 {count}", "Saved {count} translation(s)", []string{"3"}, "Saved 3 translation(s)"},
		{"多占位符按顺序填", "从 %s 到 %s", []string{"a", "b"}, "从 a 到 b"},
		{"没有占位符时原样", "没有需要写入的变化。", []string{"多余的值"}, "没有需要写入的变化。"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := navigationPlaceholderText(tc.tpl, tc.values...)
			if got != tc.want {
				t.Fatalf("navigationPlaceholderText(%q) = %q，want %q", tc.tpl, got, tc.want)
			}
			if strings.Contains(got, "%!") {
				t.Fatalf("填词后出现格式化垃圾: %q", got)
			}
		})
	}
}

// TestNavigationWriteBusinessTextFollowsRequestLanguage 业务句按**请求语言**给成品。
//
// 用「句柄未装配」这条不需要库就能到的失败分支来测：它是真实响应出口
// （response.ErrorWithMessage → ErrStorageUnavailable），注入两种语言的词条后，同一请求
// 在 zh / en 下必须拿到各自的句子 —— 这条链断了的表现是「英文界面里出现中文」，
// 而中文环境下完全看不出来。
func TestNavigationWriteBusinessTextFollowsRequestLanguage(t *testing.T) {
	// 合成词条 + 用后还原：本包已有的注入助手（navigation_facing_text_test.go）。
	injectNavigationEntries(t, map[string]map[string]string{
		navigationenums.ErrStorageUnavailable: {
			"zh-CN": "译文存储不可用",
			"en-US": "Translation storage is unavailable",
		},
	})

	stub := &translationWriteNavigationStub{}
	router := newTranslationWriteRouter(stub)
	SetNavigationTranslationWriter(nil)
	defer SetNavigationTranslationWriter(nil)

	body := `{"action":"translations","project":"p-1","lang":"en-US","rows":[]}`
	for _, tc := range []struct{ lang, want string }{
		{"zh-CN", "译文存储不可用"},
		{"en-US", "Translation storage is unavailable"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/navigation/update", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept-Language", tc.lang)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("应 500，实际 %d", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("%s 下应给该语言的业务句，实际 %s", tc.lang, rec.Body.String())
			}
		})
	}
}

// TestNavigationTranslationsPageRendersJSONWriteForm 整页渲染：模板声明与旧并行数组契约的消失。
//
// 渲染层是「模板被改坏却没人发现」的唯一防线：本用例走**真实 GET 路由 + 真实模板**，
// 断言四件事 —— ① JSON 写的五个声明都在（端点 / 模式 / 行数组 / 回跳 / 动作标签）；
// ② 行容器与三个具名字段是真的渲染出来了（不是只在模板源码里）；
// ③ 并行数组时代的 `rowContext/rowHash/rowTarget` 与页面 POST 地址一个都不剩；
// ④ 保存按钮无 JS 时是真 disabled。
func TestNavigationTranslationsPageRendersJSONWriteForm(t *testing.T) {
	nav := &pagePresentationNavigationStub{
		tree: []*navigationdto.NavigationNode{
			{ID: "row-1", Title: "首页", Path: "/", Target: "self"},
		},
	}
	projects := &pagePresentationProjectStub{}
	router := newNavigationsPageTestRouter(t, nav, projects)
	router.GET("/admin/navigations/translations",
		adminui.PageSlot(NavigationTranslationsTemplate),
		NewNavigationTranslationHandle(nav, projects).NavigationTranslations)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/navigations/translations?project=project-1&lang=en-US", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("译文页渲染失败：%d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	for _, want := range []string{
		`data-write-url="/api/navigation/update"`,
		`data-write="create"`,
		`data-write-rows=".nav-tr-row;context,hash,target"`,
		`data-write-nav="/admin/navigations/translations?`,
		`name="action" value="translations"`,
		`class="field nav-tr-row"`,
		`name="context"`,
		`name="hash"`,
		`name="target"`,
		`data-write-status`,
		`disabled data-requires-js`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("译文页缺少 %q（模板契约被改坏）", want)
		}
	}
	for _, gone := range []string{"rowContext", "rowHash", "rowTarget", "translations/save"} {
		if strings.Contains(body, gone) {
			t.Errorf("译文页仍有 %q（按位置对齐的并行数组契约必须消失）", gone)
		}
	}
	// 回跳地址必须已经被转义/拼好（含 project 与 lang），不是模板自己拼的裸串。
	if !strings.Contains(body, "lang=en-US") || !strings.Contains(body, "project=project-1") {
		t.Errorf("数据写回跳地址应带 project 与 lang：%s", body)
	}
}

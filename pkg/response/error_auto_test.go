package response

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/enums"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
)

func TestIsBusinessErrorRecognizesEnumKeys(t *testing.T) {
	for _, msg := range []string{
		"cart.err.outOfStock",
		"order.err.alreadyPaid",
		"page.err.pathOccupied",
		"presentation.err.instanceNotFound",
		"mail.err.sendFailed",
		"page.err.rebuildRequired|3",

		"masterdata.err.invalidParam",
		"masterdata.err.entityTypeInvalid",
		"masterdata.err.actionInvalid",
		"masterdata.err.entityIDRequired",
		"masterdata.err.projectRequired",
		"masterdata.err.timeRangeInvalid",
		"masterdata.msg.listSuccess",
	} {
		if !IsBusinessError(errors.New(msg)) {
			t.Errorf("%q 是业务错误（enums key 或业务文案形态），应判为可展示", msg)
		}
	}
}

func TestIsBusinessErrorRecognizesLegacyEnumConstants(t *testing.T) {
	for _, msg := range []string{
		"ErrAttachmentNotFound",
		"ErrUploadEmpty",
		"ErrDownloadEmpty",
		"ErrSourceFilterInvalid",
		"ErrSourceNotFound",
		"ErrRouteOccupied",
		"ErrInstanceNotFound",
		"ErrInvalidParam",
		"MsgListSuccess",
		"MsgCreateSuccess",
	} {
		if !IsBusinessError(errors.New(msg)) {
			t.Errorf("%q 是未迁形态的 enums 常量（判据 2），应判为可展示", msg)
		}
	}
}

// TestErrorAutoCoversExplicitModuleEnumFacts 验证显式错误事实经过真实 JSON 出口，不能用 key 的外形推断错误来源。
func TestErrorAutoCoversExplicitModuleEnumFacts(t *testing.T) {
	defer i18n.InjectForTest(translateSeeds, translateHttpCodes)
	gin.SetMode(gin.TestMode)
	patterns := []string{
		"../../internal/module/*/enums/*.go",
		"../../internal/module/*/*/enums/*.go",
	}
	files := map[string]bool{}
	for _, p := range patterns {
		matches, gerr := filepath.Glob(p)
		if gerr != nil {
			t.Fatalf("枚举路径匹配失败: %v", gerr)
		}
		for _, m := range matches {
			files[m] = true
		}
	}
	if len(files) == 0 {
		t.Fatal("未匹配到模块 enums 目录，无法校验显式错误事实")
	}

	var checked int
	words := map[string]map[string]string{}
	keys := map[string]bool{}
	for path := range files {
		parsed, perr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if perr != nil {
			t.Fatalf("解析 %s 失败: %v", path, perr)
		}
		for _, decl := range parsed.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if !strings.HasPrefix(name.Name, "Err") && !strings.HasPrefix(name.Name, "Msg") {
						continue
					}
					if i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					value, uerr := strconv.Unquote(lit.Value)
					if uerr != nil {
						t.Fatalf("%s 的 %s 取值失败: %v", path, name.Name, uerr)
					}
					checked++
					keys[value] = true
					words[value] = map[string]string{"zh-CN": "业务词条已命中", "en-US": "Business message resolved"}
				}
			}
		}
	}
	t.Logf("已逐个校验 %d 个 enums 常量", checked)
	if checked == 0 {
		t.Fatal("没有校验到任何常量，说明解析逻辑失效（测试变成了空转）")
	}

	i18n.InjectForTest(words, nil)
	for key := range keys {
		for lang, want := range words[key] {
			c, w := internalProbeCtx(t, lang)
			err := fmt.Errorf("private SQLSTATE: %w", facing.WithStatus(facing.New(key), http.StatusConflict))
			if !IsBusinessError(err) {
				t.Errorf("%q 的显式事实在包装后丢失", key)
			}
			ErrorStatusAuto(c, "enum-fact", err)
			if w.Code != http.StatusConflict || decodeMessage(t, w.Body.String()) != want {
				t.Errorf("%q/%s 未按显式事实返回状态码与译文: %d %s", key, lang, w.Code, w.Body.String())
			}
		}
	}
}

func TestIsBusinessErrorRejectsInternalErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"pg 唯一约束", errors.New(`ERROR: duplicate key value violates unique constraint "page_routes_path_key" (SQLSTATE 23505)`)},
		{"pg 类型错误", errors.New(`ERROR: invalid input syntax for type uuid: "abc"def" (SQLSTATE 22P02)`)},
		{"文件路径", errors.New("open /srv/gowp/data/artifacts/a3f/index.html: no such file or directory")},
		{"目录误当文件", errors.New("open /tmp/uploads: is a directory")},
		{"连接串", errors.New("dial tcp 127.0.0.1:5432: connect: connection refused")},
		{"网络超时", errors.New("dial tcp 10.0.0.1:6379: i/o timeout")},
		{"包装链", fmt.Errorf("加载页面失败: %w", errors.New("sql: no rows in result set"))},
		{"格式化残留", errors.New("构造查询失败: %!s(int=1)")},
		{"运行时 panic", errors.New("panic: runtime error: index out of range [3] with length 2")},
		{"goroutine 转储", errors.New("goroutine 1 [running]:\n\t/srv/gowp/main.go:42 +0x1a")},
		{"超长中文串", errors.New(strings.Repeat("记录不存在", 30))},
		{"空错误", nil},
		{"只有一段", errors.New("somethingWentWrong")},
		{"两个中文段", errors.New("页面不存在")},
		{"key 但只有两段", errors.New("cart.err")},
	}
	for _, tc := range cases {
		if IsBusinessError(tc.err) {
			t.Errorf("%s：%v 是内部错误，不该被判为可展示", tc.name, tc.err)
		}
	}
}

func decodeMessage(t *testing.T, body string) string {
	t.Helper()
	const marker = `"message":"`
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("响应里没有 message 字段: %s", body)
	}
	rest := body[i+len(marker):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		t.Fatalf("message 字段未闭合: %s", body)
	}
	return rest[:j]
}

func TestErrorAutoShowsBusinessMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/x", nil)

	defer i18n.InjectForTest(translateSeeds, translateHttpCodes)
	i18n.InjectForTest(map[string]map[string]string{
		"presentation.err.instanceNotFound": {"zh-CN": "展示实例不存在"},
	}, nil)

	ErrorAuto(c, http.StatusBadRequest, "presentation", errors.New("presentation.err.instanceNotFound"))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("业务错误应沿用调用方给的状态码 400，实际 %d", w.Code)
	}
	if msg := decodeMessage(t, w.Body.String()); msg != "展示实例不存在" {
		t.Fatalf("有词时应给词库里的精确文案，实际 %q", msg)
	}
}

func TestErrorAutoFallsBackWhenWordMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	defer i18n.InjectForTest(translateSeeds, translateHttpCodes)
	i18n.InjectForTest(nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/x", nil)

	ErrorAuto(c, http.StatusConflict, "product", errors.New("product.err.keyWithoutSeed"))

	if w.Code != http.StatusConflict {
		t.Fatalf("状态码应沿用调用方给的 409，实际 %d", w.Code)
	}
	msg := decodeMessage(t, w.Body.String())
	if msg == "product.err.keyWithoutSeed" {
		t.Fatalf("缺词不该把裸 key 交给前端，实际 %q", msg)
	}
	if msg != InternalText(c) {
		t.Fatalf("缺词应给共享归口文案，实际 %q", msg)
	}
}

func TestErrorAutoHidesInternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	leak := `ERROR: duplicate key value violates unique constraint "page_routes_path_key" (SQLSTATE 23505)`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/x", nil)

	ErrorAuto(c, http.StatusBadRequest, "page", errors.New(leak))

	body := w.Body.String()
	for _, forbidden := range []string{"SQLSTATE", "page_routes_path_key", "duplicate key", "constraint"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("内部错误泄漏到响应：body 含 %q\n%s", forbidden, body)
		}
	}

	if w.Code != http.StatusInternalServerError {
		t.Errorf("内部错误应返回 500，实际 %d", w.Code)
	}
	// 非业务错误统一走 InternalKey → 词库 → InternalFallback，不再有第二个常量。
	if got := decodeMessage(t, body); got != InternalFallback {
		t.Errorf("内部错误应返回归口文案 %q，实际 %q", InternalFallback, got)
	}
}

func TestErrorAutoMapsWrappedBodyLimitTo413(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		enums.ErrRequestEntityTooLarge: {"zh-CN": "请求内容超过大小限制"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(translateSeeds, translateHttpCodes) })
	for _, code := range []int{http.StatusBadRequest, http.StatusInternalServerError} {
		c, w := internalProbeCtx(t)
		err := fmt.Errorf("private-binding-context: %w", &http.MaxBytesError{Limit: 512})
		ErrorAuto(c, code, "body-limit", err)
		if w.Code != http.StatusRequestEntityTooLarge || decodeMessage(t, w.Body.String()) != "请求内容超过大小限制" {
			t.Fatalf("超限应归口413与安全文案：%d %s", w.Code, w.Body.String())
		}
	}
}

func TestErrorAutoNilErrorStillSafe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/x", nil)

	ErrorAuto(c, http.StatusBadRequest, "x", nil)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("nil 错误也该按内部错误处理，实际 %d", w.Code)
	}
}

// 三种 key 形态（旧枚举常量 / 新 err key / 三段式）都按业务错误处理，状态码沿用声明值。
//
// 有词 / 缺词拆成两条：**缺词时业务错误也会落到归口文案**，
// 那是正确行为（词库没这个词条），不该被当成「业务错误被替换」。
// 所以「有词必须给词条文案」这条只在注入词库的前提下断言。
var legacyEnumKeyCases = []struct {
	name  string
	scene string
	key   string
}{
	{"旧枚举常量", "inventory", "ErrSourceFilterInvalid"},
	{"旧枚举常量-附件", "media", "ErrAttachmentNotFound"},
	{"三段式 err key", "masterdata", "masterdata.err.invalidParam"},
}

func TestErrorAutoShowsWordForEveryKeyShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	defer i18n.InjectForTest(translateSeeds, translateHttpCodes)

	// 每个 key 都给一条可辨识的词，用来证明「有词就取词」对所有形态成立。
	const inventoryWord = "来源筛选值不合法"
	const mediaWord = "附件不存在"
	const masterdataWord = "参数不合法"
	i18n.InjectForTest(map[string]map[string]string{
		"ErrSourceFilterInvalid":      {"zh-CN": inventoryWord},
		"ErrAttachmentNotFound":       {"zh-CN": mediaWord},
		"masterdata.err.invalidParam": {"zh-CN": masterdataWord},
	}, nil)

	want := map[string]string{
		"ErrSourceFilterInvalid":      inventoryWord,
		"ErrAttachmentNotFound":       mediaWord,
		"masterdata.err.invalidParam": masterdataWord,
	}

	for _, tc := range legacyEnumKeyCases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/x", nil)

			ErrorAuto(c, http.StatusBadRequest, tc.scene, errors.New(tc.key))

			if w.Code != http.StatusBadRequest {
				t.Fatalf("业务错误应沿用调用方给的 400，实际 %d", w.Code)
			}
			if got := decodeMessage(t, w.Body.String()); got != want[tc.key] {
				t.Fatalf("有词时应给词库文案 %q，实际 %q", want[tc.key], got)
			}
		})
	}
}

// 缺词：业务 key 也退归口文案 —— 前提是「不把裸 key 交给前端」，
// 而不是「业务错误永远给业务词」。这条把两件事分开钉住。
func TestErrorAutoMissingWordFallsBackForEveryKeyShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	defer i18n.InjectForTest(translateSeeds, translateHttpCodes)
	i18n.InjectForTest(map[string]map[string]string{}, nil)

	for _, tc := range legacyEnumKeyCases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/x", nil)

			ErrorAuto(c, http.StatusBadRequest, tc.scene, errors.New(tc.key))

			// 状态码仍按业务声明：缺词不改语义，只改文案来源。
			if w.Code != http.StatusBadRequest {
				t.Fatalf("缺词不该改动状态码，实际 %d", w.Code)
			}
			msg := decodeMessage(t, w.Body.String())
			if msg == tc.key {
				t.Fatalf("缺词不得把裸 key 交给前端：%q", msg)
			}
			if msg != InternalText(c) {
				t.Fatalf("缺词应给共享归口文案，实际 %q", msg)
			}
		})
	}
}

// ---- 非业务出口的归口语义只有一条 ----
//
// ErrorInternal / ErrorAuto / MessageOf 三条路径都必须落到同一个 InternalKey：
// 词库有词条就用词条，没有就退 InternalFallback。
// 这条围栏的意义：本轮之前 ErrorAuto/ErrorInternal/MessageOf 用的是另一个常量
// （「服务器内部错误…」），而 InternalText 用 InternalFallback（「系统内部错误…」），
// 同一个「非业务错误」在两处给出两句不同的话。

// internalProbeCtx 返回上下文与它的 recorder：断言直接读 recorder，不做类型断言。
// lang 非空时带上 Accept-Language，用来验证出口真的按请求语言取词。
func internalProbeCtx(t *testing.T, lang ...string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/x", nil)
	if len(lang) > 0 && lang[0] != "" {
		c.Request.Header.Set("Accept-Language", lang[0])
	}
	return c, w
}

// 缺词：三条路径都给 InternalFallback，且互不相同地泄露内部原文。
func TestNonBusinessExitsShareFallbackWhenWordMissing(t *testing.T) {
	ensureTranslateFixture(t)
	i18n.InjectForTest(translateSeeds, translateHttpCodes) // 种子词库里没有 MsgInternalError
	t.Cleanup(func() { i18n.InjectForTest(translateSeeds, translateHttpCodes) })

	raw := errors.New(`pq: relation "x" does not exist (SQLSTATE 42P01)`)

	t.Run("ErrorInternal", func(t *testing.T) {
		c, w := internalProbeCtx(t)
		ErrorInternal(c, "x", raw)
		if got := decodeMessage(t, w.Body.String()); got != InternalFallback {
			t.Fatalf("归口文案 = %q，want %q", got, InternalFallback)
		}
	})

	t.Run("ErrorAuto", func(t *testing.T) {
		c, w := internalProbeCtx(t)
		ErrorAuto(c, http.StatusInternalServerError, "x", raw)
		body := w.Body.String()
		if got := decodeMessage(t, body); got != InternalFallback {
			t.Fatalf("归口文案 = %q，want %q", got, InternalFallback)
		}
		if strings.Contains(body, "SQLSTATE") {
			t.Fatalf("内部原文泄漏：%s", body)
		}
	})

	t.Run("MessageOf", func(t *testing.T) {
		c, _ := internalProbeCtx(t)
		if got := MessageOf(c, "x", raw); got != InternalFallback {
			t.Fatalf("归口文案 = %q，want %q", got, InternalFallback)
		}
	})

	t.Run("三条路径给出同一句", func(t *testing.T) {
		aCtx, _ := internalProbeCtx(t)
		a := MessageOf(aCtx, "x", raw)

		c, w := internalProbeCtx(t)
		ErrorAuto(c, http.StatusInternalServerError, "x", raw)
		b := decodeMessage(t, w.Body.String())

		if a != b {
			t.Fatalf("MessageOf 与 ErrorAuto 给出不同的话：%q vs %q", a, b)
		}
	})
}

// 有词：三条路径都取词库里那条，而不是常量兜底。
//
// 两种语言各一份词条：出口必须按**请求语言**取词，不能一条语言糊到底。
// 这里只验证「有词时按请求语言精确取词」，不涉及缺词时的兜底语言选择。
func TestNonBusinessExitsUseWordLibraryWhenPresent(t *testing.T) {
	ensureTranslateFixture(t)
	const wordZh = "这条错误现在有词条了"
	const wordEn = "This error now has its own message"
	i18n.InjectForTest(map[string]map[string]string{
		InternalKey: {"zh-CN": wordZh, "en-US": wordEn},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(translateSeeds, translateHttpCodes) })

	raw := errors.New("boom internal")

	for _, tc := range []struct {
		lang string
		want string
	}{
		{"zh-CN", wordZh},
		{"en-US", wordEn},
	} {
		t.Run(tc.lang+"/ErrorInternal", func(t *testing.T) {
			c, w := internalProbeCtx(t, tc.lang)
			ErrorInternal(c, "x", raw)
			if got := decodeMessage(t, w.Body.String()); got != tc.want {
				t.Fatalf("%s 应取词库文案 %q，实际 %q", tc.lang, tc.want, got)
			}
		})

		t.Run(tc.lang+"/ErrorAuto", func(t *testing.T) {
			c, w := internalProbeCtx(t, tc.lang)
			ErrorAuto(c, http.StatusInternalServerError, "x", raw)
			if got := decodeMessage(t, w.Body.String()); got != tc.want {
				t.Fatalf("%s 应取词库文案 %q，实际 %q", tc.lang, tc.want, got)
			}
		})

		t.Run(tc.lang+"/MessageOf", func(t *testing.T) {
			c, _ := internalProbeCtx(t, tc.lang)
			if got := MessageOf(c, "x", raw); got != tc.want {
				t.Fatalf("%s 应取词库文案 %q，实际 %q", tc.lang, tc.want, got)
			}
		})
	}

	// 三路在每种语言下给出同一句：出口只有一条语义，不因入口不同而分叉。
	for _, tc := range []struct {
		lang string
		want string
	}{
		{"zh-CN", wordZh},
		{"en-US", wordEn},
	} {
		t.Run(tc.lang+"/三路一致", func(t *testing.T) {
			internalCtx, internalW := internalProbeCtx(t, tc.lang)
			ErrorInternal(internalCtx, "x", raw)
			byInternal := decodeMessage(t, internalW.Body.String())

			autoCtx, autoW := internalProbeCtx(t, tc.lang)
			ErrorAuto(autoCtx, http.StatusInternalServerError, "x", raw)
			byAuto := decodeMessage(t, autoW.Body.String())

			ofCtx, _ := internalProbeCtx(t, tc.lang)
			byMessageOf := MessageOf(ofCtx, "x", raw)

			if byInternal != byAuto || byAuto != byMessageOf {
				t.Fatalf("%s 三路不一致：ErrorInternal=%q ErrorAuto=%q MessageOf=%q",
					tc.lang, byInternal, byAuto, byMessageOf)
			}
			if byInternal != tc.want {
				t.Fatalf("%s 三路一致但都不是词库文案：%q，want %q", tc.lang, byInternal, tc.want)
			}
		})
	}
}

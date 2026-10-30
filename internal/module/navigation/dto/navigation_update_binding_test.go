package navigationdto_test

// navigation_update_binding_test.go — UpdateEnvelope 经真实 binding.JSONLimited 的接线契约。
//
// 为什么不直接调 dto 方法：binding.JSONLimited → binding.JSON → gin.ShouldBind 才是生产入口。
// 这条路径上，JSON 体走 UnmarshalJSON、表单体走 gin 反射绑定 + FormBinder.BindForm；
// 只测 dto 方法会让「反射绑定先把 Rows 解坏」「multipart 从未被解析」这类接线缺口全部隐形。
//
// 覆盖：JSON / JSON 无 Content-Type / urlencoded / multipart 四种投放、1 MiB 边界（恰好 / 超 1 字节）、
// legacy 必填与宽容未知键、译文顶层未知键、move 形状。
//
// 本文件不运行验证：由主审集中跑 go test。

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	navigationdto "go_wp/internal/module/navigation/dto"
	"go_wp/pkg/binding"
)

// maxBody 与生产入口同值（handler 用 1<<20）。
const maxBody int64 = 1 << 20

// bound 一次真实绑定的结果。
type bound struct {
	env  *navigationdto.UpdateEnvelope
	code int
	body string
}

// bindThrough 用生产入口的同一行绑定请求，返回绑定结论与响应。
func bindThrough(t *testing.T, req *http.Request) bound {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	var got bound
	router.POST("/api/navigation/update", func(c *gin.Context) {
		env, ok := binding.JSONLimited[navigationdto.UpdateEnvelope](c, maxBody)
		if !ok {
			return // binding 已写 400
		}
		// 与真实 handler（navigation_handle.go 的 Update）保持同一出口顺序：
		// binding → Validate → 分流。生产代码里 Validate 在 handler，测试若省掉它，
		// 断言的就不是「真实入口的行为」，而是「只跑到 binding 为止的行为」。
		if err := env.Validate(); err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		got.env = env
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	got.code = rec.Code
	got.body = rec.Body.String()
	if got.env != nil {
		got.code = http.StatusOK
	}
	return got
}

// jsonRequest 造 JSON 投放（contentType 为空 = 不设 Content-Type）。
func jsonRequest(t *testing.T, contentType, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/update", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req
}

// formRequest 造 urlencoded 投放。
func formRequest(t *testing.T, values url.Values) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/update", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

// multipartRequest 造 multipart 投放（写基座 fetch+FormData 的真实形态）。
func multipartRequest(t *testing.T, fields [][2]string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, kv := range fields {
		if err := w.WriteField(kv[0], kv[1]); err != nil {
			t.Fatalf("WriteField: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("multipart close: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/update", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

// translationFields 译文提交的字段集（JSON 串 rows 是写基座的一层 stringify）。
func translationFields(rows string) [][2]string {
	return [][2]string{{"action", "translations"}, {"project", "p-1"}, {"lang", langOK()}, {"rows", rows}}
}

// TestUpdateEnvelopeThroughRealBinding 四种投放经真实 binding 得到同一档与同一份入参。
func TestUpdateEnvelopeThroughRealBinding(t *testing.T) {
	rowsJSON := `[{"context":"nav.header","hash":"h1","target":"Home"},{"context":"nav.footer","hash":"h2","target":""}]`
	cases := []struct {
		name string
		req  func() *http.Request
	}{
		{"json", func() *http.Request { return jsonRequest(t, "application/json", translationJSON(rowsJSON)) }},
		{"json 无 Content-Type", func() *http.Request { return jsonRequest(t, "", translationJSON(rowsJSON)) }},
		{"urlencoded", func() *http.Request { return formRequest(t, translationForm(rowsJSON)) }},
		{"multipart", func() *http.Request { return multipartRequest(t, translationFields(rowsJSON)) }},
	}
	var want *navigationdto.TranslationUpdateReq
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := bindThrough(t, tc.req())
			if r.code != http.StatusOK || r.env == nil {
				t.Fatalf("binding 应成功，code=%d body=%s", r.code, r.body)
			}
			if got := r.env.Route(); got != navigationdto.RouteTranslations {
				t.Fatalf("Route() = %v, want translations", got)
			}
			req, err := r.env.DecodeTranslation()
			if err != nil {
				t.Fatalf("DecodeTranslation: %v", err)
			}
			if want == nil {
				want = req
				return
			}
			if req.ProjectID != want.ProjectID || req.Lang != want.Lang || !slices.Equal(req.Rows, want.Rows) {
				t.Fatalf("四种投放必须解出同一份入参：got=%+v want=%+v", req, want)
			}
		})
	}
	if want == nil || len(want.Rows) != 2 || want.Rows[1].Target != "" {
		t.Fatalf("入参不符: %+v", want)
	}
}

// TestUpdateEnvelopeBindingLegacyThroughRealBinding legacy 档经真实 binding：必填在原处校验、未知键宽容。
func TestUpdateEnvelopeBindingLegacyThroughRealBinding(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  func() *http.Request
	}{
		{"json", func() *http.Request {
			return jsonRequest(t, "application/json", `{"id":"n-1","title":"home","sortOrder":3,"unknownField":1}`)
		}},
		{"json 无 Content-Type", func() *http.Request { return jsonRequest(t, "", `{"id":"n-1","title":"home"}`) }},
		{"urlencoded", func() *http.Request {
			return formRequest(t, url.Values{"id": {"n-1"}, "title": {"home"}, "sortOrder": {"3"}, "unknownField": {"1"}})
		}},
		{"multipart", func() *http.Request {
			return multipartRequest(t, [][2]string{{"id", "n-1"}, {"title", "home"}, {"sortOrder", "3"}, {"unknownField", "1"}})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := bindThrough(t, tc.req())
			if r.code != http.StatusOK || r.env == nil {
				t.Fatalf("binding 应成功，code=%d body=%s", r.code, r.body)
			}
			if got := r.env.Route(); got != navigationdto.RouteLegacy {
				t.Fatalf("未知键必须被忽略并判 legacy，实际 %v", got)
			}
			req, err := r.env.DecodeLegacy()
			if err != nil {
				t.Fatalf("DecodeLegacy: %v", err)
			}
			if req.ID != "n-1" || req.Title == nil || *req.Title != "home" {
				t.Fatalf("legacy 字段不符: %+v", req)
			}
		})
	}

	// 缺 id：required 必须在 dto 内部被 gin binding 执行，binding 整体写 400。
	missing := []struct {
		name string
		req  func() *http.Request
	}{
		{"json 缺 id", func() *http.Request { return jsonRequest(t, "application/json", `{"title":"home"}`) }},
		{"urlencoded 缺 id", func() *http.Request { return formRequest(t, url.Values{"title": {"home"}}) }},
		{"multipart 缺 id", func() *http.Request { return multipartRequest(t, [][2]string{{"title", "home"}}) }},
	}
	for _, tc := range missing {
		t.Run(tc.name, func(t *testing.T) {
			r := bindThrough(t, tc.req())
			if r.code == http.StatusOK || r.env != nil {
				t.Fatalf("缺 id 必须 400，实际 code=%d env=%+v", r.code, r.env)
			}
		})
	}
}

// TestUpdateEnvelopeBindingMultipartMove move 档同样经 multipart 得到已解析形状。
func TestUpdateEnvelopeBindingMultipartMove(t *testing.T) {
	r := bindThrough(t, multipartRequest(t, [][2]string{{"action", "move"}, {"id", "n-1"}, {"dir", "up"}, {"projectId", "p-1"}}))
	if r.code != http.StatusOK || r.env == nil {
		t.Fatalf("binding 应成功，code=%d body=%s", r.code, r.body)
	}
	if got := r.env.Route(); got != navigationdto.RouteMove {
		t.Fatalf("Route() = %v, want move", got)
	}
	req, err := r.env.MoveRequest()
	if err != nil {
		t.Fatalf("MoveRequest: %v", err)
	}
	if *req != (navigationdto.MoveUpdateReq{ID: "n-1", ProjectID: "p-1", Dir: "up"}) {
		t.Fatalf("move 形状不符: %+v", *req)
	}
}

// TestUpdateEnvelopeBindingTranslationUnknownKey 译文顶层未知键经真实 binding 被拒（写 400）。
func TestUpdateEnvelopeBindingTranslationUnknownKey(t *testing.T) {
	rows := `[{"context":"c","hash":"h","target":"t"}]`
	bad := translationForm(rows)
	bad.Set("unexpected", "x")
	for _, tc := range []struct {
		name string
		req  *http.Request
	}{
		{"json 未知键", jsonRequest(t, "application/json", `{"action":"translations","project":"p-1","lang":"`+langOK()+`","rows":`+rows+`,"id":"n-1"}`)},
		{"urlencoded 未知键", formRequest(t, bad)},
		{"multipart 未知键", multipartRequest(t, append(translationFields(rows), [2]string{"unexpected", "x"}))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := bindThrough(t, tc.req)
			if r.code == http.StatusOK || r.env != nil {
				t.Fatalf("未知顶层键必须 400，实际 code=%d", r.code)
			}
		})
	}
	// 公共 CSRF 表单字段不是业务键：表单投放必须被接受。
	ok := translationForm(rows)
	ok.Set("csrf_token", "tok")
	for _, tc := range []struct {
		name string
		req  *http.Request
	}{
		{"urlencoded 带 csrf_token", formRequest(t, ok)},
		{"multipart 带 csrf_token", multipartRequest(t, append(translationFields(rows), [2]string{"csrf_token", "tok"}))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := bindThrough(t, tc.req)
			if r.code != http.StatusOK || r.env == nil {
				t.Fatalf("带 csrf_token 的表单必须接受，code=%d body=%s", r.code, r.body)
			}
			if got := r.env.Route(); got != navigationdto.RouteTranslations {
				t.Fatalf("Route() = %v, want translations", got)
			}
		})
	}
}

// TestUpdateEnvelopeBindingBodyLimit 1 MiB 边界：恰好 1 MiB 通过，多 1 字节拒（不截断）。
func TestUpdateEnvelopeBindingBodyLimit(t *testing.T) {
	const head = `{"id":"n-1","title":"`
	const tail = `"}`
	pad := strings.Repeat("a", int(maxBody)-len(head)-len(tail))
	exact := head + pad + tail
	if int64(len(exact)) != maxBody {
		t.Fatalf("用例构造错误: len=%d want %d", len(exact), maxBody)
	}
	r := bindThrough(t, jsonRequest(t, "application/json", exact))
	if r.code != http.StatusOK || r.env == nil {
		t.Fatalf("恰好 1 MiB 必须通过，code=%d", r.code)
	}
	req, err := r.env.DecodeLegacy()
	if err != nil {
		t.Fatalf("DecodeLegacy: %v", err)
	}
	if req.ID != "n-1" || req.Title == nil || *req.Title != pad {
		t.Fatalf("1 MiB 体内的字段必须完整（不截断）")
	}
	over := bindThrough(t, jsonRequest(t, "application/json", exact+" "))
	if over.code == http.StatusOK || over.env != nil {
		t.Fatalf("超 1 字节必须整体失败，code=%d", over.code)
	}
}

// TestUpdateEnvelopeBindingMultipartRowsStringIsUnwrapped 表单 rows 的一层字符串解包语义保留。
func TestUpdateEnvelopeBindingMultipartRowsStringIsUnwrapped(t *testing.T) {
	rows := `[{"context":"a","hash":"h1","target":"A"},{"context":"b","hash":"h2","target":""}]`
	r := bindThrough(t, multipartRequest(t, translationFields(rows)))
	if r.code != http.StatusOK || r.env == nil {
		t.Fatalf("binding 应成功，code=%d body=%s", r.code, r.body)
	}
	req, err := r.env.DecodeTranslation()
	if err != nil {
		t.Fatalf("DecodeTranslation: %v", err)
	}
	if len(req.Rows) != 2 || req.Rows[0].Target != "A" || req.Rows[1].Target != "" {
		t.Fatalf("rows 必须按一层字符串解包并保位，实际 %+v", req.Rows)
	}
}

// TestUpdateEnvelopeBindingNonJSONBodyRejected 空体 / 非法 JSON 经真实 binding 直接 400。
func TestUpdateEnvelopeBindingNonJSONBodyRejected(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  *http.Request
	}{
		{"空体", jsonRequest(t, "application/json", "")},
		{"非法 JSON", jsonRequest(t, "application/json", "{broken")},
		{"空 urlencoded", formRequest(t, url.Values{})},
		{"空 multipart", multipartRequest(t, nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := bindThrough(t, tc.req)
			if r.code == http.StatusOK || r.env != nil {
				t.Fatalf("必须 400，实际 code=%d", r.code)
			}
		})
	}

	// 错误值仍是 dto 的事实，供 handler 之外的调用点判定。
	env := &navigationdto.UpdateEnvelope{}
	if err := env.UnmarshalJSON([]byte("{broken")); !errors.Is(err, navigationdto.ErrUpdateEnvelopeBody) {
		t.Fatalf("want ErrUpdateEnvelopeBody, got %v", err)
	}
}

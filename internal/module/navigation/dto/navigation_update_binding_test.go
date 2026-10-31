package navigationdto_test

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

const maxBody int64 = 1 << 20

type bound struct {
	env  *navigationdto.UpdateEnvelope
	code int
	body string
}

func bindThrough(t *testing.T, req *http.Request) bound {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	var got bound
	router.POST("/api/navigation/update", func(c *gin.Context) {
		env, ok := binding.JSONLimited[navigationdto.UpdateEnvelope](c, maxBody)
		if !ok {
			return
		}

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

func jsonRequest(t *testing.T, contentType, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/update", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req
}

func formRequest(t *testing.T, values url.Values) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/update", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

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

func translationFields(rows string) [][2]string {
	return [][2]string{{"action", "translations"}, {"project", "p-1"}, {"lang", langOK()}, {"rows", rows}}
}

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

	env := &navigationdto.UpdateEnvelope{}
	if err := env.UnmarshalJSON([]byte("{broken")); !errors.Is(err, navigationdto.ErrUpdateEnvelopeBody) {
		t.Fatalf("want ErrUpdateEnvelopeBody, got %v", err)
	}
}

package navigationdto_test

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	navigationdto "go_wp/internal/module/navigation/dto"
	"go_wp/pkg/binding"
)

type bound struct {
	env  *navigationdto.UpdateEnvelope
	code int
	body string
}

// bindThrough 复刻生产端点：/api/navigation/translations 只声明 JSON 协议，
// 绑定入口就是 binding.ParseJSON（表单写入走另一条路由的 UpdateReq）。
func bindThrough(t *testing.T, req *http.Request) bound {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	var got bound
	router.POST("/api/navigation/translations", func(c *gin.Context) {
		env, err := binding.ParseJSON[navigationdto.UpdateEnvelope](c)
		if err != nil {
			c.Status(http.StatusBadRequest)
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
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/translations", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req
}

func formRequest(t *testing.T, values url.Values) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/translations", strings.NewReader(values.Encode()))
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
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/translations", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func TestUpdateEnvelopeThroughRealBinding(t *testing.T) {
	rowsJSON := `[{"context":"nav.header","hash":"h1","target":"Home"},{"context":"nav.footer","hash":"h2","target":""}]`
	r := bindThrough(t, jsonRequest(t, "application/json", translationJSON(rowsJSON)))
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
	if req.ProjectID != "p-1" || len(req.Rows) != 2 || req.Rows[1].Target != "" {
		t.Fatalf("入参不符: %+v", req)
	}
}

func TestUpdateEnvelopeBindingLegacyThroughRealBinding(t *testing.T) {
	r := bindThrough(t, jsonRequest(t, "application/json", `{"id":"n-1","title":"home","sortOrder":3,"unknownField":1}`))
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

	missing := bindThrough(t, jsonRequest(t, "application/json", `{"title":"home"}`))
	if missing.code == http.StatusOK || missing.env != nil {
		t.Fatalf("缺 id 必须 400，实际 code=%d env=%+v", missing.code, missing.env)
	}
}

func TestUpdateEnvelopeBindingMove(t *testing.T) {
	r := bindThrough(t, jsonRequest(t, "application/json",
		`{"action":"move","id":"n-1","dir":"up","projectId":"p-1"}`))
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
	body := `{"action":"translations","project":"p-1","lang":"` + langOK() + `","rows":` + rows + `,"id":"n-1"}`
	r := bindThrough(t, jsonRequest(t, "application/json", body))
	if r.code == http.StatusOK || r.env != nil {
		t.Fatalf("未知顶层键必须 400，实际 code=%d", r.code)
	}
}

func TestUpdateEnvelopeBindingRowsRoundTrip(t *testing.T) {
	rows := `[{"context":"a","hash":"h1","target":"A"},{"context":"b","hash":"h2","target":""}]`
	r := bindThrough(t, jsonRequest(t, "application/json", translationJSON(rows)))
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

// 端点声明 JSON：表单编码与非 JSON 体都必须进 400，不能退回零值封套。
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

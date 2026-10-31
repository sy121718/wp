package pubhttp

import (
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pubenums "go_wp/internal/module/publication/enums"
	pubservice "go_wp/internal/module/publication/service"
	"go_wp/pkg/facing"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

const rawLeak = `<img src=x onerror=alert(1)> & "quote" 'tick' ` + "`backtick`" + ` private-path:/srv/artifacts/secret`

func seoAuditEngine(run func(context.Context, string) (pubservice.AuditResult, error)) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/publication/seo-audit", NewSeoAuditHandle(run).SeoAudit)
	return r
}

func decodeEnvelope(t *testing.T, w *httptest.ResponseRecorder) response.Response {
	t.Helper()
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("响应类型必须是 JSON，实际 %q，body=%s", ct, w.Body.String())
	}
	var got response.Response
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应不是 {code,message,data}：%v，body=%s", err, w.Body.String())
	}
	return got
}

func assertNoRedirect(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code >= 300 && w.Code < 400 {
		t.Fatalf("写动作不得重定向，实际 %d Location=%q", w.Code, w.Header().Get("Location"))
	}
	if loc := w.Header().Get("Location"); loc != "" {
		t.Fatalf("写动作不得给 Location，实际 %q", loc)
	}
	if hx := w.Header().Get("HX-Redirect"); hx != "" {
		t.Fatalf("写动作不得给 HX-Redirect，实际 %q", hx)
	}
	if hx := w.Header().Get("HX-Location"); hx != "" {
		t.Fatalf("写动作不得给 HX-Location，实际 %q", hx)
	}
}

func TestSEOAuditJSONContract(t *testing.T) {
	issues := []pubservice.AuditIssue{
		{Path: rawLeak, Item: pubservice.AuditTitleMissing, Level: pubservice.AuditLevelError},
		{Item: pubservice.AuditTitleDuplicate, Level: pubservice.AuditLevelWarning,
			Params: pubservice.AuditIssueParams{Title: rawLeak, Paths: []string{"/a", "/b"}}},
	}
	ok := pubservice.AuditResult{Scanned: 2, Count: 2, Issues: issues}
	cases := []struct {
		name       string
		contentTyp string
		body       string
		target     string
		headers    map[string]string
		wantProj   string
		result     pubservice.AuditResult
		runErr     error
		wantStatus int
		wantIssues bool

		wantNoCall bool
	}{
		{
			name: "json 体", contentTyp: "application/json",
			body: `{"project":" p1 "}`, target: "/api/publication/seo-audit",
			wantProj: "p1", result: ok, wantStatus: http.StatusOK, wantIssues: true,
		},
		{
			name: "urlencoded 体", contentTyp: "application/x-www-form-urlencoded",
			body: "project=p1", target: "/api/publication/seo-audit",
			wantProj: "p1", result: ok, wantStatus: http.StatusOK, wantIssues: true,
		},
		{
			name: "multipart 体", contentTyp: "multipart", target: "/api/publication/seo-audit",
			wantProj: "p1", result: ok, wantStatus: http.StatusOK, wantIssues: true,
		},
		{
			name: "body 空白回落 query", contentTyp: "application/json",
			body: `{"project":"   "}`, target: "/api/publication/seo-audit?project=p2",
			wantProj: "p2", result: ok, wantStatus: http.StatusOK, wantIssues: true,
		},
		{
			name: "body 优先于 query", contentTyp: "application/json",
			body: `{"project":"p1"}`, target: "/api/publication/seo-audit?project=p2",
			wantProj: "p1", result: ok, wantStatus: http.StatusOK, wantIssues: true,
		},
		{

			name: "空 JSON 体绑定失败 400 且不进 service", contentTyp: "application/json",
			target: "/api/publication/seo-audit?project=p3", wantNoCall: true,
			wantStatus: http.StatusBadRequest,
		},
		{

			name: "空 JSON 对象回落 query", contentTyp: "application/json",
			body: `{}`, target: "/api/publication/seo-audit?project=p3",
			wantProj: "p3", result: ok, wantStatus: http.StatusOK, wantIssues: true,
		},
		{
			name: "空表单体回落 query", contentTyp: "application/x-www-form-urlencoded",
			target:   "/api/publication/seo-audit?project=p3",
			wantProj: "p3", result: ok, wantStatus: http.StatusOK, wantIssues: true,
		},
		{
			name: "非法 JSON 立即 400 不回落 query", contentTyp: "application/json",
			body: `{not json`, target: "/api/publication/seo-audit?project=p1",
			wantNoCall: true, wantStatus: http.StatusBadRequest,
		},
		{
			name: "HX 请求同样是 JSON", contentTyp: "application/x-www-form-urlencoded",
			body: "project=p1", target: "/api/publication/seo-audit",
			headers:  map[string]string{"HX-Request": "true"},
			wantProj: "p1", result: ok, wantStatus: http.StatusOK, wantIssues: true,
		},
		{
			name: "HX 请求失败同样 JSON 不渲染片段", contentTyp: "application/x-www-form-urlencoded",
			body: "project=p1", target: "/api/publication/seo-audit",
			headers:  map[string]string{"HX-Request": "true"},
			wantProj: "p1", runErr: pubenums.ErrProjectMissing(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "Accept text/html 不改变契约", contentTyp: "application/json",
			body: `{"project":"p1"}`, target: "/api/publication/seo-audit",
			headers:  map[string]string{"Accept": "text/html"},
			wantProj: "p1", result: ok, wantStatus: http.StatusOK, wantIssues: true,
		},
		{
			name: "缺工程 400", contentTyp: "application/json",
			body: `{}`, target: "/api/publication/seo-audit",
			runErr: pubenums.ErrProjectMissing(), wantStatus: http.StatusBadRequest,
		},
		{
			name: "基础设施错误 500 且不泄漏原文", contentTyp: "application/json",
			body: `{"project":"p1"}`, target: "/api/publication/seo-audit",
			wantProj: "p1", runErr: errors.New("open /srv/artifacts: permission denied"),
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "未声明状态码的业务错误按 400", contentTyp: "application/json",
			body: `{"project":"p1"}`, target: "/api/publication/seo-audit",
			wantProj: "p1", runErr: pubenums.Err(pubenums.ErrInvalidParam),
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "声明了 409 的错误按 409", contentTyp: "application/json",
			body: `{"project":"p1"}`, target: "/api/publication/seo-audit",
			wantProj:   "p1",
			runErr:     facing.WithStatus(pubenums.Err(pubenums.ErrReceiptPending), http.StatusConflict),
			wantStatus: http.StatusConflict,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotProject string
			callCount := 0
			run := func(_ context.Context, id string) (pubservice.AuditResult, error) {
				callCount++
				gotProject = id
				if tc.runErr != nil {
					return pubservice.AuditResult{}, tc.runErr
				}
				return tc.result, nil
			}
			r := seoAuditEngine(run)

			var body *strings.Reader
			switch tc.contentTyp {
			case "multipart":
				buf := &strings.Builder{}
				mw := multipart.NewWriter(buf)
				if err := mw.WriteField("project", "p1"); err != nil {
					t.Fatal(err)
				}
				if err := mw.Close(); err != nil {
					t.Fatal(err)
				}
				body = strings.NewReader(buf.String())
				tc.contentTyp = mw.FormDataContentType()
			default:
				body = strings.NewReader(tc.body)
			}
			req := httptest.NewRequest(http.MethodPost, tc.target, body)
			if tc.contentTyp != "" {
				req.Header.Set("Content-Type", tc.contentTyp)
			}
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("状态码 = %d，want %d，body=%s", w.Code, tc.wantStatus, w.Body.String())
			}
			assertNoRedirect(t, w)
			got := decodeEnvelope(t, w)
			if got.Code != tc.wantStatus {
				t.Fatalf("envelope code = %d，want %d", got.Code, tc.wantStatus)
			}

			wantCall := 0
			if !tc.wantNoCall {
				wantCall = 1
			}
			if callCount != wantCall {
				t.Fatalf("service 调用次数 = %d，want %d（工程 %q）", callCount, wantCall, gotProject)
			}
			if wantCall == 1 && gotProject != tc.wantProj {
				t.Fatalf("service 收到的工程 = %q，want %q", gotProject, tc.wantProj)
			}
			if tc.wantStatus != http.StatusOK {
				if got.Data != nil {
					t.Fatalf("失败响应不该带 data：%+v", got.Data)
				}
				if got.Message == "" {
					t.Fatal("失败响应必须给出对外文案")
				}
				for _, leak := range []string{rawLeak, "private-path", "/srv/artifacts", "permission denied"} {
					if strings.Contains(got.Message, leak) || strings.Contains(w.Body.String(), leak) {
						t.Fatalf("响应泄漏内部原文 %q：%s", leak, w.Body.String())
					}
				}
				return
			}
			if !tc.wantIssues {
				if got.Data != nil {
					t.Fatalf("成功响应不该带 data：%+v", got.Data)
				}
				return
			}
			data, ok := got.Data.(map[string]any)
			if !ok {
				t.Fatalf("成功响应 data 形状改变：%+v", got.Data)
			}

			if data["scanned"] != float64(tc.result.Scanned) {
				t.Fatalf("scanned = %v，want %d", data["scanned"], tc.result.Scanned)
			}
			rawIssues, ok := data["issues"].([]any)
			if !ok {
				t.Fatalf("issues 必须是数组（空态也是 []），实际 %+v", data["issues"])
			}
			if data["count"] != float64(len(rawIssues)) {
				t.Fatalf("count = %v，但 issues 有 %d 条", data["count"], len(rawIssues))
			}
			if len(rawIssues) != len(tc.result.Issues) {
				t.Fatalf("issues 条数 = %d，want %d", len(rawIssues), len(tc.result.Issues))
			}
			first, ok := rawIssues[0].(map[string]any)
			if !ok {
				t.Fatalf("issue 形状改变：%+v", rawIssues[0])
			}

			if first["path"] != tc.result.Issues[0].Path || first["item"] != tc.result.Issues[0].Item ||
				first["level"] != tc.result.Issues[0].Level {
				t.Fatalf("issue 事实被改写：%+v", first)
			}
			if _, has := first["message"]; has {
				t.Fatalf("结论不得再带成品句 message：%+v", first)
			}
			params, ok := first["params"].(map[string]any)
			if !ok {
				t.Fatalf("params 必须是对象：%+v", first["params"])
			}
			second, ok := rawIssues[1].(map[string]any)
			if !ok {
				t.Fatalf("issue 形状改变：%+v", rawIssues[1])
			}
			secondParams, ok := second["params"].(map[string]any)
			if !ok {
				t.Fatalf("重复标题的 params 必须是对象：%+v", second["params"])
			}

			if second["path"] != "" {
				t.Fatalf("重复标题的 path 必须为空，实际 %v", second["path"])
			}
			if paths, ok := secondParams["paths"].([]any); !ok || len(paths) != 2 || paths[0] != "/a" || paths[1] != "/b" {
				t.Fatalf("重复标题的完整路径丢失：%+v", secondParams["paths"])
			}

			if _, has := params["src"]; has {
				t.Fatalf("无参数的结论不该带 src：%+v", params)
			}
			if _, has := params["href"]; has {
				t.Fatalf("无参数的结论不该带 href：%+v", params)
			}
		})
	}
}

func TestSEOAuditEmptyIsArray(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result pubservice.AuditResult
	}{
		{name: "没有产物 scanned=0", result: pubservice.AuditResult{Scanned: 0, Count: 0, Issues: []pubservice.AuditIssue{}}},
		{name: "跑过且干净 scanned>0", result: pubservice.AuditResult{Scanned: 7, Count: 0, Issues: []pubservice.AuditIssue{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := seoAuditEngine(func(context.Context, string) (pubservice.AuditResult, error) {
				return tc.result, nil
			})
			req := httptest.NewRequest(http.MethodPost, "/api/publication/seo-audit", strings.NewReader(`{"project":"p1"}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("空态是 200，实际 %d：%s", w.Code, w.Body.String())
			}
			got := decodeEnvelope(t, w)
			data, ok := got.Data.(map[string]any)
			if !ok {
				t.Fatalf("成功响应 data 形状改变：%+v", got.Data)
			}
			issues, ok := data["issues"].([]any)
			if !ok || len(issues) != 0 {
				t.Fatalf("空态 issues 必须是 []，实际 %+v", data["issues"])
			}
			if data["count"] != float64(0) || data["scanned"] != float64(tc.result.Scanned) {
				t.Fatalf("空态计数不符：%+v", data)
			}
		})
	}
}

package feature

import (
	pageview "go_wp/pkg/view"
	"context"
	json "go_wp/pkg/jsonx"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"go_wp/internal/adminui"
	maildto "go_wp/internal/module/mail/dto"
	mailhttp "go_wp/internal/module/mail/inbound/http"
	"go_wp/internal/templates"

	"github.com/gin-gonic/gin"
)

func TestTemplateEditorRendersFourPanes(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	router := newTemplateEditorRouter(t, f)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/mail/templates/editor?templateKey=demo_tpl", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d：%s", rec.Code, firstN(rec.Body.String(), 300))
	}
	body := rec.Body.String()
	for _, want := range []string{

		`id="tplTree"`, `id="tplCanvas"`, `id="tplAttrs"`, `id="tplPreview"`,

		`id="tplKey"`, `id="tplSubject"`, `id="tplSave"`,

		"/static/js/template/index.js", "/static/css/template_editor.css",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("编辑器缺少 %q；前 800 字：\n%s", want, firstN(body, 800))
		}
	}

	if !strings.Contains(body, `"tree"`) || !strings.Contains(body, `"body"`) {
		t.Fatalf("注入的 meta 缺块树；前 900 字：\n%s", firstN(body, 900))
	}
}

func TestTemplateEditorShowsCompiledPreview(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	tree := map[string]any{"schema": 1, "root": map[string]any{
		"key": "root", "type": "body", "children": []any{
			map[string]any{"key": "s1", "type": "section", "children": []any{
				map[string]any{"key": "c1", "type": "column", "children": []any{
					map[string]any{"key": "t1", "type": "text", "text": "PREVIEW_MARKER"},
				}},
			}},
		},
	}}
	raw, _ := json.Marshal(tree)
	if _, err := f.svc.UpsertTemplate(ctx, &maildto.SaveTemplateReq{
		TemplateKey: "preview_tpl", Subject: "主题", BodyJSON: raw,
	}); err != nil {
		t.Fatalf("建模板失败: %v", err)
	}

	router := newTemplateEditorRouter(t, f)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/mail/templates/editor?templateKey=preview_tpl", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d", rec.Code)
	}
	body := rec.Body.String()

	if !strings.Contains(body, "PREVIEW_MARKER") {
		t.Fatalf("meta 里没有编译产物（预览只能靠前端拼，而那是第二份生成器）；前 900 字：\n%s", firstN(body, 900))
	}
	if !strings.Contains(body, "table") {
		t.Fatalf("编译产物应是表格布局；前 900 字：\n%s", firstN(body, 900))
	}
}

func TestTemplateEditorSaveReturnsCompiled(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	router := newTemplateEditorRouter(t, f)

	payload := map[string]any{
		"templateKey": "roundtrip_tpl",
		"subject":     "主题",
		"name":        "往返模板",
		"variables":   []string{"name"},
		"bodyJson": map[string]any{"schema": 1, "root": map[string]any{
			"key": "root", "type": "body", "children": []any{
				map[string]any{"key": "s1", "type": "section", "children": []any{
					map[string]any{"key": "c1", "type": "column", "children": []any{
						map[string]any{"key": "t1", "type": "text", "text": "ROUNDTRIP {{.name}}"},
					}},
				}},
			},
		}},
	}
	raw, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/admin/mail/template/save", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("保存状态码 %d：%s", rec.Code, firstN(rec.Body.String(), 300))
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			BodyCompiledHTML string `json:"bodyCompiledHtml"`
			BodyCompiledText string `json:"bodyCompiledText"`
			BodyJSON         any    `json:"bodyJson"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if resp.Data.BodyCompiledHTML == "" || resp.Data.BodyCompiledText == "" {
		t.Fatalf("保存响应必须回带编译产物（前端只替换 iframe srcdoc）：%+v", resp.Data)
	}
	if !strings.Contains(resp.Data.BodyCompiledHTML, "ROUNDTRIP {{.name}}") {
		t.Fatalf("编译产物缺失正文；实际：%s", firstN(resp.Data.BodyCompiledHTML, 300))
	}
	if resp.Data.BodyJSON == nil {
		t.Fatal("响应应回带 body_json（编辑器要靠它重建树）")
	}
}

func TestTemplatesListKeepsCodeMode(t *testing.T) {
	body, err := os.ReadFile("../../../../internal/templates/admin/mail/mail_templates.html")
	if err != nil {
		t.Fatalf("读取模板列表页失败: %v", err)
	}
	src := string(body)
	for _, want := range []string{`name="body_html"`, `name="body_text"`, "可视化"} {
		if !strings.Contains(src, want) {
			t.Fatalf("模板列表页缺 %q：代码模式不能被可视化替换掉", want)
		}
	}
}

func newTemplateEditorRouter(t *testing.T, f *mailFixture) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Next()
		for _, e := range c.Errors {
			t.Logf("gin 渲染错误: %v", e.Err)
		}
	})
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	page := mailhttp.NewMailPageHandle(f.svc)
	api := mailhttp.NewHandle(f.svc)
	router.GET("/admin/mail/templates/editor",
		pageview.Slot(mailhttp.TemplateEditorPageTemplate, adminui.RenderPage), page.MailTemplateEditor)
	router.POST("/admin/mail/template/save", api.TemplateSave)
	return router
}

var _ = io.Discard

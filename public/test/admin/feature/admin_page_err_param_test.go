package feature

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go_wp/internal/adminui"
	adminenums "go_wp/internal/module/admin/enums"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	"go_wp/internal/templates"
	"go_wp/internal/text"
	"go_wp/pkg/database"
	"go_wp/pkg/view"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
)

var pageInternalCopyForms = map[string]bool{"系统内部错误，请稍后重试": true}

var pageRequiredCopyForms = map[string]bool{
	"MsgFieldRequired": true,
	"必填字段不能为空":         true,
}

var pageI18nEmptyCopyForms = map[string]map[string]bool{
	adminenums.ErrI18nKeyEmpty: {
		adminenums.ErrI18nKeyEmpty: true,
		"词条 key 不能为空":              true,
		"Entry key is required":    true,
	},
	adminenums.ErrI18nLangEmpty: {
		adminenums.ErrI18nLangEmpty:  true,
		"词条语言不能为空":                   true,
		"Entry language is required": true,
	},
	adminenums.ErrI18nValueEmpty: {
		adminenums.ErrI18nValueEmpty: true,
		"词条内容不能为空":                   true,
		"Entry text is required":     true,
	},
}

var pageLeakMarkers = []string{
	"SQLSTATE", "23505", "42P01", "duplicate key", "constraint",
	"sys_i18n", "sys_rule", "sys_admin", "pq:", "relation", "uq_", "pg_",
}

func assertPageNoLeak(t *testing.T, step, text string) {
	t.Helper()
	for _, m := range pageLeakMarkers {
		if strings.Contains(text, m) {
			t.Fatalf("%s：页面提示里出现了内部片段 %q —— 原文只能进日志，text=%s", step, m, text)
		}
	}
}

func adminPageHandlerFiles(t *testing.T) []string {
	t.Helper()
	root := support.RepoRoot(t)
	dir := filepath.Join(root, "internal", "module", "admin", "inbound", "http")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取 admin handler 目录失败: %v", err)
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files = append(files, filepath.Join(dir, name))
	}
	if len(files) == 0 {
		t.Fatal("没有枚举到任何 admin handler 源文件 —— 目录结构变了？门禁会静默失效，所以这里直接失败")
	}
	return files
}

func TestAdminPageHandlersDoNotInlineErrorText(t *testing.T) {
	redirectForms := []string{"?err=", "?errored=", "adminI18nBackURL(", "c.Redirect("}
	templateKeys := []string{`"Err"`, `"Errors"`, `"Errored"`}

	var hits []string
	for _, f := range adminPageHandlerFiles(t) {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", f, err)
		}
		rel, _ := filepath.Rel(support.RepoRoot(t), f)
		for i, line := range strings.Split(string(raw), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") || !strings.Contains(line, ".Error()") {
				continue
			}
			target := ""
			for _, form := range append(append([]string{}, redirectForms...), templateKeys...) {
				if strings.Contains(line, form) {
					target = form
					break
				}
			}
			if target == "" {
				continue
			}
			hits = append(hits, fmt.Sprintf("%s:%d [%s] %s", rel, i+1, target, trimmed))
		}
	}
	if len(hits) > 0 {
		t.Fatalf("页面路径直出了内部错误（会渲染表名 / 约束名 / SQLSTATE）：\n  %s\n"+
			"  交给统一出口：response.ErrorStatusAuto / response.MessageOf", strings.Join(hits, "\n  "))
	}
}

func newAdminI18nWriteEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	engine.Use(view.FailureMiddleware(adminui.RenderFailure))
	engine.Use(templates.FailedRenderMiddleware(func(c *gin.Context, err error) { view.FailScene(c, "template", err) }))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.POST("/api/i18n/save", view.HTMLFragment, adminhttp.NewAdminI18nEntryHandle().I18nEntryWrite)
	return engine
}

func postAdminI18nWrite(t *testing.T, engine *gin.Engine, body string) (int, string) {
	t.Helper()
	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method:  http.MethodPost,
		Path:    "/api/i18n/save",
		RawBody: []byte(body),
		Headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
	})
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	return recorder.Code, i18nHTMLMessage(t, assertI18nHTMLFragment(t, recorder))
}

func TestAdminI18nWriteBusinessTextVisible(t *testing.T) {
	engine := newAdminI18nWriteEngine()

	cases := []struct {
		name string
		body string
		want string
	}{
		{"新建时 key 为空", "action=create&lang=zh-CN&value=x", adminenums.ErrI18nKeyEmpty},
		{"新建时语言为空", "action=create&key=a.b&value=x", adminenums.ErrI18nLangEmpty},
		{"新建时内容为空", "action=create&key=a.b&lang=zh-CN&value=+++", adminenums.ErrI18nValueEmpty},
		{"删除时 key 为空", "action=delete&lang=zh-CN", adminenums.ErrI18nKeyEmpty},
		{"删除时语言为空", "action=delete&key=a.b", adminenums.ErrI18nLangEmpty},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, got := postAdminI18nWrite(t, engine, tc.body)
			if code != http.StatusBadRequest {
				t.Fatalf("校验失败应回 400（受控文案），got %d msg=%q", code, got)
			}
			if !pageI18nEmptyCopyForms[tc.want][got] {
				t.Fatalf("业务错误的文案必须可见且具体到缺哪一项（期望 %s），got %q", tc.want, got)
			}
			assertPageNoLeak(t, tc.name, got)
		})
	}
}

func TestAdminI18nEmptyFieldTextsAreDistinct(t *testing.T) {
	engine := newAdminI18nWriteEngine()

	wantKey := map[string]string{
		"key":   adminenums.ErrI18nKeyEmpty,
		"lang":  adminenums.ErrI18nLangEmpty,
		"value": adminenums.ErrI18nValueEmpty,
	}
	bodies := map[string]string{
		"key":   "action=create&lang=zh-CN&value=x",
		"lang":  "action=create&key=a.b&value=x",
		"value": "action=create&key=a.b&lang=zh-CN&value=+++",
	}

	seen := map[string]string{}
	for _, field := range []string{"key", "lang", "value"} {
		code, got := postAdminI18nWrite(t, engine, bodies[field])
		if code != http.StatusBadRequest {
			t.Fatalf("缺 %s 应回 400，got %d msg=%q", field, code, got)
		}
		if !pageI18nEmptyCopyForms[wantKey[field]][got] {
			t.Fatalf("缺 %s 应返回 %s 的具体文案，got %q", field, wantKey[field], got)
		}
		if pageRequiredCopyForms[got] {
			t.Fatalf("缺 %s 仍返回通用必填提示，运营还是不知道缺哪一项：%q", field, got)
		}
		if prev, ok := seen[got]; ok {
			t.Fatalf("缺 %s 与缺 %s 的文案完全相同（%q）—— 拆成三条等于没拆", field, prev, got)
		}
		seen[got] = field
		assertPageNoLeak(t, "缺 "+field, got)
	}
}

func TestAdminI18nWriteInternalErrorCollected(t *testing.T) {
	if database.IsInited() {
		t.Skip("database 组件已被同包其它用例初始化，跳过（避免真的写库）；归口分支由 internal 单测覆盖")
	}
	engine := newAdminI18nWriteEngine()
	code, got := postAdminI18nWrite(t, engine, "action=create&key=page.err.test&lang=zh-CN&value=%E6%B5%8B%E8%AF%95")
	if code != http.StatusInternalServerError {
		t.Fatalf("存储不可用应回 500（归口），got %d msg=%q", code, got)
	}
	if !pageInternalCopyForms[got] {
		t.Fatalf("内部错误的文案应是归口形态，got %q", got)
	}
	assertPageNoLeak(t, "基础设施错误", got)
}

package feature

// admin_page_err_param_test.go — admin 页面路径错误文案的两道守卫。
//
// 背景：JSON 出口（adminFail / adminErrParam）收口之后，页面路径还有一条独立出口 ——
// 写操作失败时把原因渲染给运营。本批之前它是「303 回列表页 + `?err=` / `?errored=` 文案，
// 由读侧白名单放行后渲染提示条」；本批改成 **整页提示**（shell.RenderJump，见
// internal/shell/jump.go）—— 文案走响应体，不再经查询参数。
//
// 无论哪种传输通道，泄漏面都成立：err.Error() 一旦直传，PostgreSQL 原文（表名 sys_i18n、
// 约束名 uk_…、SQLSTATE 23505）就会摆在后台页面上。两道守卫分工：
//   1) 静态扫描（TestAdminPageHandlersDoNotInlineErrorText）—— 钉住「谁都不许再写回去」；
//   2) 接口级（TestAdminI18nPageJump*）—— 钉住两个方向都没有跑偏：
//      业务错误（少填字段）文案仍**可见**，基础设施错误走归口文案且不含任何原文片段。
//
// 归口文案与业务文案都有「key / zh-CN / en-US」三种可能形态（未加载 i18n 时就是 key 本身），
// 本包内先后顺序不固定，因此按集合断言，不依赖某个具体译文。

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	adminenums "go_wp/internal/module/admin/enums"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	"go_wp/internal/templates"
	"go_wp/pkg/database"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
)

// pageInternalCopyForms adminenums.ErrInternal 的全部可能形态。
var pageInternalCopyForms = map[string]bool{
	"ErrInternal":                              true,
	"操作失败，请稍后重试":                               true,
	"Operation failed, please try again later": true,
}

// pageRequiredCopyForms MsgFieldRequired（通用必填提示）的全部可能形态。
//
// 它在本文件里只剩一个用途：作为**反向参照** —— 词条表单的三个缺项各有具体文案，
// 断言它们「不再是这句话」。
var pageRequiredCopyForms = map[string]bool{
	"MsgFieldRequired": true,
	"必填字段不能为空":         true,
}

// pageI18nEmptyCopyForms 词条表单三条缺项文案的全部可能形态（key / zh-CN / en-US）。
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

// pageLeakMarkers 页面提示里绝不能出现的内部片段。
var pageLeakMarkers = []string{
	"SQLSTATE", "23505", "42P01", "duplicate key", "constraint",
	"sys_i18n", "sys_rule", "sys_admin", "pq:", "relation", "uq_", "pg_",
}

// assertPageNoLeak 提示文本里不得出现任何内部片段（与具体分支无关，两个方向都要过）。
func assertPageNoLeak(t *testing.T, step, text string) {
	t.Helper()
	for _, m := range pageLeakMarkers {
		if strings.Contains(text, m) {
			t.Fatalf("%s：页面提示里出现了内部片段 %q —— 原文只能进日志，text=%s", step, m, text)
		}
	}
}

// adminPageHandlerFiles 列出 admin 页面 handler 的源文件（排除测试与模板）。
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

// TestAdminPageHandlersDoNotInlineErrorText 页面路径不许把 err.Error() 拼进响应或模板数据。
//
// 判据（照 AGENTS.md「不许直出内部错误」的三种形态取页面路径的两种）：
// 同一行里同时出现 .Error() 与「查询参数出口」（?err= / ?errored= / adminI18nBackURL( / c.Redirect(）
// 或「模板数据键」（"Err" / "Errors" / "Errored"）即命中。
//
// 例外：无。批量超限此前靠「变量名 berr」放行（那时 shell.BulkIDs 的错误是字符串，
// 受控性只能靠注释说明）；后来把它改成**带 sentinel 的类型**（shell.ErrBulkIDsTooMany /
// *shell.BulkIDsError），页面统一走 shell.BulkIDsFacingText。批量超限路径的文案回归见
// 同目录 admin_bulk_limit_text_test.go。
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
			"  改用 adminErrParam(c, err)（internal/module/admin/inbound/http/admin_err.go）", strings.Join(hits, "\n  "))
	}
}

// newAdminI18nPageEngine 只挂文案词条页的两个写接口（页面 handler 的裸路由，不带中间件）。
//
// 必须挂真 Jet 渲染器：写失败现在渲染整页提示（shell.RenderJump → c.HTML）。
func newAdminI18nPageEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := adminhttp.NewAdminI18nEntryHandle()
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	engine.POST("/admin/i18n/save", h.I18nEntrySave)
	engine.POST("/admin/i18n/delete", h.I18nEntryDelete)
	return engine
}

// postAdminI18nPageJump 提交一个表单，返回提示页响应体与状态码。
func postAdminI18nPageJump(t *testing.T, engine *gin.Engine, path string, form url.Values) (string, int) {
	t.Helper()
	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method:  http.MethodPost,
		Path:    path,
		RawBody: []byte(form.Encode()),
		Headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
	})
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	return recorder.Body.String(), recorder.Code
}

// TestAdminI18nPageJumpBusinessTextVisible 少填字段是**业务**错误，文案必须仍然可见。
//
// 这一条防的是「一刀切成通用提示」的反向缺陷：把 key/lang/内容为空的提示也归口成
// 「操作失败，请稍后重试」，运营就只能反复猜测到底哪一项没填。校验留在 handler
//（与同文件其它表单一致），所以这里连数据库都不需要。
func TestAdminI18nPageJumpBusinessTextVisible(t *testing.T) {
	engine := newAdminI18nPageEngine(t)

	cases := []struct {
		name string
		path string
		form url.Values
		want string // 期望命中的 enums key（按具体缺项返回具体文案）
	}{
		{"保存时 key 为空", "/admin/i18n/save", url.Values{"key": {""}, "lang": {"zh-CN"}, "value": {"x"}}, adminenums.ErrI18nKeyEmpty},
		{"保存时语言为空", "/admin/i18n/save", url.Values{"key": {"a.b"}, "lang": {""}, "value": {"x"}}, adminenums.ErrI18nLangEmpty},
		{"保存时内容为空", "/admin/i18n/save", url.Values{"key": {"a.b"}, "lang": {"zh-CN"}, "value": {"   "}}, adminenums.ErrI18nValueEmpty},
		{"删除时 key 为空", "/admin/i18n/delete", url.Values{"key": {""}, "lang": {"zh-CN"}}, adminenums.ErrI18nKeyEmpty},
		{"删除时语言为空", "/admin/i18n/delete", url.Values{"key": {"a.b"}, "lang": {""}}, adminenums.ErrI18nLangEmpty},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, code := postAdminI18nPageJump(t, engine, tc.path, tc.form)
			if code != http.StatusOK {
				t.Fatalf("校验失败应渲染提示页（200），got %d body=%s", code, body)
			}
			if !strings.Contains(body, `data-jump-state="err"`) {
				t.Fatalf("失败提示页应带 data-jump-state=\"err\"：%s", body)
			}
			got := jumpMsgFromBody(body)
			if !pageI18nEmptyCopyForms[tc.want][got] {
				t.Fatalf("业务错误的文案必须可见且具体到缺哪一项（期望 %s），got %q", tc.want, got)
			}
			assertPageNoLeak(t, tc.name, body)
		})
	}
}

// TestAdminI18nEmptyFieldTextsAreDistinct 三缺一各自文案不同（本任务的核心判据）。
//
// 改造前三条共用 MsgFieldRequired（「必填字段不能为空」）：运营只知道「有个字段没填」，
// 而这张表单有三行，只能逐个试。这里把「具体」与「互不相同」两条一起钉死 ——
// 只断言「能命中某个具体文案」会漏掉「三条做成了同一句」这种改造不彻底的情形。
func TestAdminI18nEmptyFieldTextsAreDistinct(t *testing.T) {
	engine := newAdminI18nPageEngine(t)

	wantKey := map[string]string{
		"key":   adminenums.ErrI18nKeyEmpty,
		"lang":  adminenums.ErrI18nLangEmpty,
		"value": adminenums.ErrI18nValueEmpty,
	}
	forms := map[string]url.Values{
		"key":   {"key": {""}, "lang": {"zh-CN"}, "value": {"x"}},
		"lang":  {"key": {"a.b"}, "lang": {""}, "value": {"x"}},
		"value": {"key": {"a.b"}, "lang": {"zh-CN"}, "value": {"   "}},
	}

	seen := map[string]string{} // 文案 → 缺哪一项
	for _, field := range []string{"key", "lang", "value"} {
		body, _ := postAdminI18nPageJump(t, engine, "/admin/i18n/save", forms[field])
		got := jumpMsgFromBody(body)
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
		assertPageNoLeak(t, "缺 "+field, body)
	}
}

// TestAdminI18nPageJumpInternalErrorCollected 基础设施错误走归口文案，且响应体里没有原文。
//
// 前置条件：database 组件**未初始化** —— 这样 i18n.SaveEntry 会稳定地返回
// ErrI18nUnavailable（归口分支），而不是真的写一次库。若同包其它用例已经初始化过组件
// （admin_shell_i18n_test.go 会），本用例会跳过。
func TestAdminI18nPageJumpInternalErrorCollected(t *testing.T) {
	if database.IsInited() {
		t.Skip("database 组件已被同包其它用例初始化，跳过（避免真的写库）；归口分支由 internal 单测覆盖")
	}
	engine := newAdminI18nPageEngine(t)
	body, code := postAdminI18nPageJump(t, engine, "/admin/i18n/save", url.Values{
		"key": {"page.err.test"}, "lang": {"zh-CN"}, "value": {"测试"},
	})
	if code != http.StatusOK {
		t.Fatalf("存储不可用应渲染提示页（200），got %d body=%s", code, body)
	}
	got := jumpMsgFromBody(body)
	if !pageInternalCopyForms[got] {
		t.Fatalf("内部错误的文案应是归口形态，got %q", got)
	}
	assertPageNoLeak(t, "基础设施错误", body)
}

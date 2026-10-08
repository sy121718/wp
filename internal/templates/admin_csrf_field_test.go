package templates

// admin_csrf_field_test.go — CSRF 隐藏域片段（admin/partials/csrf_field.html）的判据。
//
// 抽它的理由是可量化的：同一个 1 行元素手写了 234 处、9 种拼法（`{{csrf}}` / 直取 chain /
// `{{csrfToken}}` / `{{.Csrf}}`，以及并行改动避让同名生出的 `cfCsrf` / `ceCsrf` / `ccCsrf`）。
// 片段化之后只有一处，改属性名或改取值链不会再漏。
//
// 三条判据（都在**渲染产物**上核，不在源码上核）：
//  1. 产物与手写形态**逐字节相同** —— 否则迁移就不是「等价替换」，而是悄悄改了页面；
//  2. 缺 csrf_token 键时渲染成空串而不是 500 —— 这是 chain 索引与点号取字段的差别，
//     片段必须选安全的那一种（点号取字段会让整页 500，且 htmx 片段因 5xx 不 swap）；
//  3. 值里的引号被转义 —— token 是服务端给的，但渲染路径的转义性质值得钉住（防属性逃逸）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// csrfFieldPartial 片段在磁盘上的位置（相对 internal/templates）。
const csrfFieldPartial = "admin/partials/csrf_field.html"

// renderCSRFProbe 用**真实片段文件**渲染一个探针页面：探针放在 admin/order/ 下，
// 引用路径写成真实页面会写的那一种（`../partials/csrf_field.html`），
// 这样测的不只是片段内容，还有它的引用落点。
func renderCSRFProbe(t *testing.T, data map[string]any) string {
	t.Helper()
	src, err := os.ReadFile(filepath.FromSlash(csrfFieldPartial))
	if err != nil {
		t.Fatalf("读取片段失败: %v", err)
	}
	set := memSet(t, map[string]string{
		"admin/order/probe.html": `{{import "../partials/csrf_field.html"}}` +
			`{{block body()}}{{yield csrfField()}}{{end}}`,
		"admin/partials/csrf_field.html": string(src),
	})
	out, err := render(t, set, "admin/order/probe.html", data)
	if err != nil {
		t.Fatalf("渲染片段失败: %v", err)
	}
	return out
}

// TestCSRFFieldMatchesHandWrittenForm 迁移等价性：片段产物 == 手写形态产物。
func TestCSRFFieldMatchesHandWrittenForm(t *testing.T) {
	token := "tok-123"

	got := renderCSRFProbe(t, map[string]any{"csrf_token": token})

	// 手写形态：{{csrf := .["csrf_token"]}} 之后 value="{{csrf}}"，是仓里最多的那一种（182 处）。
	set := memSet(t, map[string]string{
		"hand.html": `{{block body()}}{{csrf := .["csrf_token"]}}<input type="hidden" name="csrf_token" value="{{csrf}}">{{end}}`,
	})
	want, err := render(t, set, "hand.html", map[string]any{"csrf_token": token})
	if err != nil {
		t.Fatalf("渲染手写形态失败: %v", err)
	}

	if got != want {
		t.Fatalf("片段与手写形态不一致：\n片段: %q\n手写: %q", got, want)
	}
	if !strings.Contains(got, `name="csrf_token"`) || !strings.Contains(got, `value="`+token+`"`) {
		t.Fatalf("产物里没有 csrf 隐藏域：%q", got)
	}
}

// TestCSRFFieldMissingKeyRendersEmpty 缺键（页面忘了 Prepare）时给空串，不整页 500。
func TestCSRFFieldMissingKeyRendersEmpty(t *testing.T) {
	got := renderCSRFProbe(t, map[string]any{})
	if !strings.Contains(got, `name="csrf_token"`) {
		t.Fatalf("缺键时仍应渲染出隐藏域：%q", got)
	}
	if !strings.Contains(got, `value=""`) {
		t.Fatalf("缺键时应渲染成空 value：%q", got)
	}
}

// TestCSRFFieldEscapesQuotes 值里的引号必须被转义（防属性逃逸）。
func TestCSRFFieldEscapesQuotes(t *testing.T) {
	got := renderCSRFProbe(t, map[string]any{"csrf_token": `a"b<c`})
	if strings.Contains(got, `value="a"b`) {
		t.Fatalf("引号未被转义，可逃出属性：%q", got)
	}
	if strings.Contains(got, "<c") {
		t.Fatalf("尖括号未被转义：%q", got)
	}
}

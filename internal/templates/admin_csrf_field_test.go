package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const csrfFieldPartial = "admin/partials/csrf_field.html"

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

func TestCSRFFieldMatchesHandWrittenForm(t *testing.T) {
	token := "tok-123"

	got := renderCSRFProbe(t, map[string]any{"csrf_token": token})

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

func TestCSRFFieldMissingKeyRendersEmpty(t *testing.T) {
	got := renderCSRFProbe(t, map[string]any{})
	if !strings.Contains(got, `name="csrf_token"`) {
		t.Fatalf("缺键时仍应渲染出隐藏域：%q", got)
	}
	if !strings.Contains(got, `value=""`) {
		t.Fatalf("缺键时应渲染成空 value：%q", got)
	}
}

func TestCSRFFieldEscapesQuotes(t *testing.T) {
	got := renderCSRFProbe(t, map[string]any{"csrf_token": `a"b<c`})
	if strings.Contains(got, `value="a"b`) {
		t.Fatalf("引号未被转义，可逃出属性：%q", got)
	}
	if strings.Contains(got, "<c") {
		t.Fatalf("尖括号未被转义：%q", got)
	}
}

package feature

import (
	"net/http"
	"os"
	"strings"
	"testing"

	purchaseenums "go_wp/internal/module/purchase/enums"
)

func TestSourcePageMultiDeviceContract(t *testing.T) {
	engine, f := newSourcePageEngine(t)
	if engine == nil {
		return
	}
	mustCreateSource(t, f, "MD_SRC", "多端适配货源", purchaseenums.SourceTypeInternal, nil, floatPtr(3.5))

	rec := httptestGet(engine, "/admin/inventory/sources?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("货源页应 200，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `class="stack source-page"`) {
		t.Fatalf("页面缺少多端适配的根类 source-page")
	}

	if !strings.Contains(body, `table-wrap" tabindex="0"`) {
		t.Fatalf("交叉表应包在可聚焦的滚动容器里（table-wrap + tabindex=0）")
	}

	tplRaw, terr := os.ReadFile(purchaseAdminTemplatePath("sources.html"))
	if terr != nil {
		t.Fatalf("读货源页模板失败: %v", terr)
	}
	if strings.Contains(string(tplRaw), "<script") {
		t.Fatalf("货源页模板不应引入自定义 JS（交互由原生表单完成）")
	}
	expectedForms := []string{
		`hx-post="/admin/inventory/sources/create?`,
		`hx-post="/admin/inventory/sources/update?`,
		`hx-post="/admin/inventory/sources/delete?`,
	}
	for _, want := range expectedForms {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少原生表单写入口 %s", want)
		}
	}

	for _, want := range []string{"<input", "<select", "<textarea", "<button"} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少原生控件 %s", want)
		}
	}

	raw, err := os.ReadFile("../../../../pkg/view/ui/assets/css/theme.css")
	if err != nil {
		t.Fatalf("读样式表失败: %v", err)
	}
	css := string(raw)

	for _, want := range []string{
		".source-page-table",
		".source-stats",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("样式表缺少货源页规则 %s", want)
		}
	}

	idx := strings.Index(css, "手机：交叉表改堆叠块")
	if idx < 0 {
		t.Fatalf("样式表缺少货源页窄屏适配段的说明")
	}
	block := css[idx:]
	if len(block) > 700 {
		block = block[:700]
	}
	if !strings.Contains(block, "max-width: 720px") || !strings.Contains(block, ".source-page-table thead { display: none; }") {
		t.Fatalf("窄屏断点缺少货源页交叉表的堆叠规则：%s", block)
	}

	if !strings.Contains(css, "width: min(100%, 220px)") {
		t.Fatalf("输入宽度应按 min(100%%, <设计宽度>) 收口，不能写死像素")
	}
}

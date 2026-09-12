// Package feature 货源管理后台页的多端 / 多输入适配契约（issue #17）。
//
// 后台页是服务端渲染的原生表单页，适配手段全部落在模板结构与样式表上，
// 因此这里断言的是「适配所依赖的契约确实存在」，而不是「某个视口看起来还行」：
//
//	· 只用原生控件（input / select / textarea / button）——
//	  鼠标点选、触屏点按、键盘（Tab 进组、空格勾选、回车提交）四条输入路径都由浏览器保证；
//	· 宽表包在可聚焦的横向滚动容器里（tabindex="0"）—— 键盘也能滚动，不依赖鼠标滚轮；
//	· 样式表按断点收口：窄屏改堆叠块，输入宽度一律 min(100%, <设计宽度>)，不写死像素；
//	· 页面不引入任何自定义 JS（无 <script>）—— 交互全由原生表单提交完成。
package feature

import (
	"net/http"
	"os"
	"strings"
	"testing"

	inventoryenums "go_wp/internal/module/product/inventory/enums"
)

// TestSourcePageMultiDeviceContract 多端 / 多输入适配的结构契约。
func TestSourcePageMultiDeviceContract(t *testing.T) {
	engine, f := newSourcePageEngine(t)
	if engine == nil {
		return
	}
	mustCreateSource(t, f, "MD_SRC", "多端适配货源", inventoryenums.SourceTypeInternal, nil, floatPtr(3.5))

	rec := httptestGet(engine, "/admin/inventory/sources?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("货源页应 200，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `class="pages-wrap source-page"`) {
		t.Fatalf("页面缺少多端适配的根类 source-page")
	}
	// 键盘可滚的表格容器（宽表在窄视口下靠它横向滚动，且能 Tab 聚焦后用方向键滚）。
	// 容器类由 pages-table-wrap 迁到公共类 table-wrap（overflow-x: auto + focus-visible
	// 都在 ui.css 的 .table-wrap 上）。断言只锚定 table-wrap + tabindex，避免类名顺序变化就误报。
	if !strings.Contains(body, `table-wrap" tabindex="0"`) {
		t.Fatalf("交叉表应包在可聚焦的滚动容器里（table-wrap + tabindex=0）")
	}
	// 这一页自己的模板不含任何 <script>：交互全由原生表单提交完成
	//（layout.html 里的 HTMX / 后台脚本是全局壳，不由本页引入）。
	tplRaw, terr := os.ReadFile(templateRoot() + "/admin/inventory_sources.html")
	if terr != nil {
		t.Fatalf("读货源页模板失败: %v", terr)
	}
	if strings.Contains(string(tplRaw), "<script") {
		t.Fatalf("货源页模板不应引入自定义 JS（交互由原生表单完成）")
	}
	expectedForms := []string{
		`action="/admin/inventory/sources/create"`,
		`action="/admin/inventory/sources/update"`,
		`action="/admin/inventory/sources/delete"`,
	}
	for _, want := range expectedForms {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少原生表单写入口 %s", want)
		}
	}
	// 四种输入都落在原生控件上。
	for _, want := range []string{"<input", "<select", "<textarea", "<button"} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少原生控件 %s", want)
		}
	}

	// 样式表契约：断点内的堆叠块 + 宽度 min(100%, …)。
	raw, err := os.ReadFile(templateRoot() + "/static/css/theme.css")
	if err != nil {
		t.Fatalf("读样式表失败: %v", err)
	}
	css := string(raw)
	for _, want := range []string{
		".source-page-table",
		".source-page .attr-form-head textarea",
		".source-page .attr-form-head .wbs",
		".source-stats",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("样式表缺少货源页规则 %s", want)
		}
	}
	// 窄屏断点里必须有堆叠规则（三列交叉表挤在 375px 视口下读不出来）。
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
	// 输入宽度不写死：收口写法必须是 min(100%, …)。
	if !strings.Contains(css, "width: min(100%, 420px)") || !strings.Contains(css, "width: min(100%, 220px)") {
		t.Fatalf("输入宽度应按 min(100%%, <设计宽度>) 收口，不能写死像素")
	}
}

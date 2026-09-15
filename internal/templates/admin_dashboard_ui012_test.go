package templates

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// admin_dashboard_ui012_test.go — 审计 UI-012：仪表盘的内联样式与数据口径。
//
// 两条判据：
//  ① 内联 style 属性清零（原先 12 处，硬编码字号与颜色，不跟随主题切换）；
//  ② 页面仍是静态演示页（Dashboard handler 只注入 title/menu），必须**显式标注**，
//     否则运营会把示例数字当成真实统计 —— 这是审计点名的主要风险。

// 内联 style 属性必须清零，且用到的 dash-* 语义类都要在页面级 <style> 里有定义
// （类名写错不会报错，只会静默无样式 —— 正是内联样式换类时最容易踩的坑）。
func TestDashboardHasNoInlineStylesAndClassesAreDefined(t *testing.T) {
	src, err := os.ReadFile(filepath.FromSlash("admin/dashboard.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(src)

	if i := strings.Index(html, "style=\""); i >= 0 {
		line := strings.Count(html[:i], "\n") + 1
		t.Errorf("dashboard.html:%d 仍有内联 style 属性（应改为语义类，见本页 <style> 段）", line)
	}

	classRe := regexp.MustCompile("class=\"([^\"]*)\"")
	used := map[string]bool{}
	for _, m := range classRe.FindAllStringSubmatch(html, -1) {
		for _, cls := range strings.Fields(m[1]) {
			if strings.HasPrefix(cls, "dash-") {
				used[cls] = true
			}
		}
	}
	if len(used) < 3 {
		t.Fatalf("只找到 %d 个 dash-* 语义类（判据可能过时或类被改回内联）: %v", len(used), used)
	}
	for cls := range used {
		if !strings.Contains(html, "."+cls) {
			t.Errorf("类 %q 在页面上使用，但页面 <style> 段没有定义它", cls)
		}
	}
}

// 渲染输出里不能再出现内联样式，且 demo 说明必须在示例数据**之前**出现。
func TestDashboardOutputIsLabelledAndStyleless(t *testing.T) {
	out, err := render(t, newAdminTestSet(), "admin/dashboard", adminShellData())
	if err != nil {
		t.Fatalf("渲染仪表盘失败: %v", err)
	}

	// layout 的 KeyframesCSS 也是 <style>（但那是主题关键帧，不是本页内联样式）；
	// 这里只钉本页自己的：style="" 属性不许出现。
	if strings.Contains(out, "style=\"") {
		t.Error("仪表盘渲染结果里仍有内联 style 属性")
	}
	for _, want := range []string{"演示占位", "/admin/analytics", "/admin/orders", "/admin/inventory"} {
		if !strings.Contains(out, want) {
			t.Errorf("仪表盘缺少 %q —— 示例数据没有被标注，或没有指向真实数据页面", want)
		}
	}

	label := strings.Index(out, "演示占位")
	sample := strings.Index(out, ">128<")
	if label < 0 || sample < 0 {
		t.Fatalf("标注或示例数据缺失（label=%d sample=%d）", label, sample)
	}
	if label > sample {
		t.Error("数据口径标注必须出现在示例数据之前：先声明口径，再出现数字")
	}
}

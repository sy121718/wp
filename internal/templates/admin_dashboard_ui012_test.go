package templates

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// admin_dashboard_ui012_test.go — 审计 UI-012（仪表盘的内联样式与数据口径）现行判据。
//
// 页面形态已变：仪表盘原先是「静态演示页」（handler 只注入 title/menu，页面写死
// 128 个管理员之类的示例值），当时只能靠「显式标注演示占位」降低误读风险。
// 现在它接的是 page / project 契约的真实统计，所以判据换成两条更硬的：
//  ① 代码层面零内联 style（原判据保留）；
//  ② 渲染结果里出现的是**注入的数据**，且旧演示页的硬编码示例值不得回潮。
//
// 保留这个文件而不是删掉它，是因为它守的是同类风险：只要有人再把数字写死进模板，
// 第 ② 条会立刻失败。

// 内联 style 属性必须清零；页面用到的 stat-* 语义类都要在 theme.css 里有定义
// （类名写错不会报错，只会静默无样式 —— 这正是内联样式换类时最容易踩的坑）。
func TestDashboardHasNoInlineStylesAndClassesAreDefined(t *testing.T) {
	src, err := os.ReadFile(filepath.FromSlash("admin/dashboard.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(src)

	if i := strings.Index(html, "style=\""); i >= 0 {
		line := strings.Count(html[:i], "\n") + 1
		t.Errorf("dashboard.html:%d 仍有内联 style 属性（应改用语义类）", line)
	}

	css, err := os.ReadFile(filepath.FromSlash("static/css/theme.css"))
	if err != nil {
		t.Fatal(err)
	}
	classRe := regexp.MustCompile("class=\"([^\"]*)\"")
	used := map[string]bool{}
	for _, m := range classRe.FindAllStringSubmatch(html, -1) {
		for _, cls := range strings.Fields(m[1]) {
			if strings.HasPrefix(cls, "stat-") {
				used[cls] = true
			}
		}
	}
	if len(used) < 3 {
		t.Fatalf("只找到 %d 个 stat-* 语义类（判据可能过时或类被改回内联）: %v", len(used), used)
	}
	for cls := range used {
		if !strings.Contains(string(css), "."+cls) {
			t.Errorf("类 %q 在页面上使用，但 theme.css 没有定义它", cls)
		}
	}
}

// 渲染结果只反映注入的数据：统计卡出现注入值，且旧演示页的硬编码示例值不再出现。
func TestDashboardRendersInjectedDataOnly(t *testing.T) {
	data := adminShellData()
	// 每个数字都刻意取成旧演示页没有用过的值：若模板里还留着写死的示例，
	// 下面的「注入值必须出现」会失败，而「示例值不得出现」也守住了回潮。
	data["ProjectCount"] = 7
	data["PageTotal"] = 42
	data["PagePublished"] = 30
	data["PageDraft"] = 12
	data["PageStale"] = 3
	data["RecentPages"] = []map[string]any{{
		"ID": "p1", "Project": "官网", "Path": "/about", "Kind": "page",
		"Published": true, "Stale": false, "Version": 2, "UpdatedAt": "2026-09-18 09:00:00",
	}}

	out, err := render(t, newAdminTestSet(), "admin/dashboard", data)
	if err != nil {
		t.Fatalf("渲染仪表盘失败: %v", err)
	}

	// layout 的 KeyframesCSS 也是 <style>（主题关键帧，不是本页内联样式）；
	// 这里只钉本页自己的：style="" 属性不许出现。
	if strings.Contains(out, "style=\"") {
		t.Error("仪表盘渲染结果里仍有内联 style 属性")
	}
	for _, want := range []string{">7<", ">42<", ">30<", ">12<", "/about", "最近更新的页面"} {
		if !strings.Contains(out, want) {
			t.Errorf("仪表盘缺少注入的数据 %q（页面可能在渲染写死的示例值）", want)
		}
	}
	for _, bad := range []string{">128<", "共 128 条", "发送欢迎邮件", "系统概览与组件演示", "新增管理员"} {
		if strings.Contains(out, bad) {
			t.Errorf("仪表盘仍出现旧演示页的硬编码内容 %q —— 真实概览页不应有示例数据", bad)
		}
	}
}

package templates

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

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

	css, err := AssetFile("css/theme.css")
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

func TestDashboardListCardTitleIsNotAFilterBar(t *testing.T) {
	src, err := os.ReadFile(filepath.FromSlash("admin/dashboard.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(src)

	if strings.Contains(html, `class="filter-bar"`) {
		t.Error("dashboard 的列表卡标题行仍借 .filter-bar（那类名承诺的是筛选栏，本页没有筛选控件）")
	}
	if !strings.Contains(html, `class="card-header"`) {
		t.Error("列表卡标题行应改用 .card-header（与 admin/analytics.html 的列表卡标题同形）")
	}
}

func TestDashboardRendersInjectedDataOnly(t *testing.T) {
	data := adminShellData()

	data["ProjectCount"] = 7
	data["PageTotal"] = 42
	data["RecentPages"] = []map[string]any{}

	out, err := render(t, newAdminTestSet(), "admin/dashboard", data)
	if err != nil {
		t.Fatalf("渲染仪表盘失败: %v", err)
	}

	if strings.Contains(out, "style=\"") {
		t.Error("仪表盘渲染结果里仍有内联 style 属性")
	}
	for _, want := range []string{"dash-ai", "data-ai-fab-input", "data-ai-fab-send", "问 AI（经营数据）"} {
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

package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// admin_partial_ui014_test.go — 审计 UI-014：高频模式抽 partial 并被多处复用。
//
// 本批抽出的是「列表页筛选栏右侧的新建入口」：抽之前这一段在 6 个列表页里各抄一遍，
// 给按钮加一个属性或换一次类名要改 6 个文件（审计说的「改一个模式要改 30 多个文件」）。
//
// 两条判据：① 引用方 ≥2 个（否则算不上模式）；② 行为不变 —— 按钮的可见性与属性
// 完全由权限集合与抽屉选择器决定，与逐页手写时一致。

// ui014AdminRow 复刻 administrators 列表行的字段（与模板访问方式一致）。
type ui014AdminRow struct {
	ID       int
	Username string
	Name     string
	Email    string
	Phone    string
	Status   int
}

// 引用路径随「页面所在目录」不同：根下页面写 partials/x.html，子目录页面写 ../partials/x.html。
// 判据只锚到 partials 段，避免每搬一次目录就要改一次这个常量。
const toolbarCreateRef = "partials/toolbar_create.html"

// 至少 2 个页面引用同一个 partial。
func TestToolbarCreatePartialIsReused(t *testing.T) {
	paths := adminTemplateFiles(t)
	var users []string
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(src), toolbarCreateRef) {
			users = append(users, filepath.Base(p))
		}
	}
	if len(users) < 2 {
		t.Fatalf("partial 只被 %d 个页面引用（要求 ≥2）: %v", len(users), users)
	}
	t.Logf("partial 被 %d 个列表页复用: %v", len(users), users)
}

// 行为不变：有权限时按钮照常渲染（含抽屉选择器与文案、以及布局用的 toolbar-spacer），
// 无权限时整块不渲染 —— 这与逐页手写时的可见性判据完全相同。
func TestToolbarCreatePartialKeepsBehaviour(t *testing.T) {
	rows := []ui014AdminRow{
		{ID: 1, Username: "admin", Name: "甲", Email: "a@example.com", Phone: "138", Status: 1},
	}

	withPerm := adminShellData()
	withPerm["title"] = "管理员列表"
	withPerm["Total"] = 1
	withPerm["Rows"] = rows
	withPerm["Buttons"] = map[string]bool{"admin.create": true}
	out, err := render(t, newAdminTestSet(), "admin/system/administrators", withPerm)
	if err != nil {
		t.Fatalf("有权限时渲染失败: %v", err)
	}
	for _, want := range []string{
		"toolbar-spacer",
		"#tpl-admin-create",
		"新建管理员",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("有权限时缺少 %q —— partial 迁移改变了行为", want)
		}
	}

	noPerm := adminShellData()
	noPerm["title"] = "管理员列表"
	noPerm["Total"] = 1
	noPerm["Rows"] = rows
	noPerm["Buttons"] = map[string]bool{}
	out, err = render(t, newAdminTestSet(), "admin/system/administrators", noPerm)
	if err != nil {
		t.Fatalf("无权限时渲染失败: %v", err)
	}
	if strings.Contains(out, "#tpl-admin-create") {
		t.Error("无权限时不应渲染新建按钮")
	}
	// 权限不足时 spacer 仍在（它是布局占位，与权限无关）。
	if !strings.Contains(out, "toolbar-spacer") {
		t.Error("无权限时工具栏占位应保留")
	}
}

// 本批迁移涉及的全部模板必须能被 Jet 解析。
//
// 解析失败是整页级故障，而且现象很有欺骗性：HTTP 仍可能是 200，中断点之后的 HTML
// 整块消失（列表、表格全没了），从页面表象几乎定位不到模板里的语法问题。
// menus.html 不在 admin_group_f_i18n_test.go 的解析清单里，单独在这里兜住。
func TestToolbarCreateMigrationTemplatesParse(t *testing.T) {
	set := newAdminTestSet()
	for _, name := range []string{
		"admin/layout.html",
		"admin/dashboard.html",
		"admin/system/administrators.html",
		"admin/system/roles.html",
		"admin/system/permissions.html",
		"admin/system/datarules.html",
		"admin/system/departments.html",
		"admin/system/menus.html",
		"admin/partials/toolbar_create.html",
	} {
		if _, err := set.GetTemplate(name); err != nil {
			t.Errorf("%s 解析失败: %v", name, err)
		}
	}
}

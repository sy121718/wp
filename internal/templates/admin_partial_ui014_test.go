package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type ui014AdminRow struct {
	ID       int
	Username string
	Name     string
	Email    string
	Phone    string
	Status   int
}

const toolbarCreateRef = "partials/toolbar_create.html"

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

	if !strings.Contains(out, "toolbar-spacer") {
		t.Error("无权限时工具栏占位应保留")
	}
}

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

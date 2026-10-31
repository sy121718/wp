package templates

import (
	"strings"
	"testing"
)

func renderAdminEmptyProbe(t *testing.T, tpl string, extra map[string]any) string {
	t.Helper()
	data := adminShellData()
	for k, v := range extra {
		data[k] = v
	}
	out, err := render(t, newAdminTestSet(), tpl, data)
	if err != nil {
		t.Fatalf("渲染 %s 失败: %v", tpl, err)
	}
	return out
}

func assertEmptyKeepsTableHead(t *testing.T, label, out, colspan string, wants []string) {
	t.Helper()
	if !strings.Contains(out, `class="data-table`) {
		t.Errorf("%s：空态下 <table class=\"data-table\"> 不在输出里 —— 表头又消失了", label)
	}
	if !strings.Contains(out, "<thead") {
		t.Errorf("%s：空态下 <thead> 不在输出里", label)
	}
	if !strings.Contains(out, `class="empty-state"`) {
		t.Errorf("%s：空态块不在输出里（内容被搬丢）", label)
	}
	if !strings.Contains(out, `colspan="`+colspan+`"`) {
		t.Errorf("%s：期望 colspan=%q 未出现（空态行会与表头对不齐）", label, colspan)
	}

	idxTable := strings.Index(out, `class="data-table`)
	idxTbody := strings.Index(out, "<tbody>")
	idxEmpty := strings.Index(out, `class="empty-state"`)
	if !(idxTable >= 0 && idxTbody > idxTable && idxEmpty > idxTbody) {
		t.Errorf("%s：空态没落在 tbody 内（table=%d tbody=%d empty=%d）", label, idxTable, idxTbody, idxEmpty)
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("%s：空态文案/元素缺失 %q", label, w)
		}
	}
}

func TestAdminEmptyStateKeepsTableHead(t *testing.T) {
	t.Run("departments/无删除权限→colspan7", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/system/departments", map[string]any{"Rows": []any{}, "Parents": []any{}})
		assertEmptyKeepsTableHead(t, "departments", out, "7", []string{"还没有部门"})
	})

	t.Run("departments/有删除权限→colspan8", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/system/departments", map[string]any{
			"Rows": []any{}, "Parents": []any{}, "Buttons": map[string]any{"dept.delete": true},
		})
		assertEmptyKeepsTableHead(t, "departments+delete", out, "8", []string{"还没有部门", "data-check-all"})
	})

	t.Run("menus/无删除权限→colspan9", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/system/menus", map[string]any{"Rows": []any{}, "Parents": []any{}})
		assertEmptyKeepsTableHead(t, "menus", out, "9", []string{"还没有菜单"})
	})

	t.Run("menus/有删除权限→colspan10", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/system/menus", map[string]any{
			"Rows": []any{}, "Parents": []any{}, "Buttons": map[string]any{"menu.delete": true},
		})
		assertEmptyKeepsTableHead(t, "menus+delete", out, "10", []string{"还没有菜单", "data-check-all"})
	})

	t.Run("i18n→colspan7", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/system/i18n", map[string]any{
			"Entries": []any{}, "Categories": []any{},
			"Keyword": "", "LangFilter": "", "CatFilter": "", "Saved": "", "Errored": "",
		})
		assertEmptyKeepsTableHead(t, "i18n", out, "7", []string{"没有匹配的词条", "换一个关键词"})
	})

	t.Run("navigations→colspan6", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/navigation/navigations", map[string]any{
			"Rows": []any{}, "Parents": []any{},
			"Projects":        []any{map[string]any{"ID": "pr1", "Name": "官网"}},
			"SelectedProject": "pr1", "Kind": "header",
		})
		assertEmptyKeepsTableHead(t, "navigations", out, "6", []string{"这个位置还没有菜单项"})
	})

}

func TestAdminDataFormUnchanged(t *testing.T) {
	t.Run("menus", func(t *testing.T) {
		row := map[string]any{"ID": "m1", "Title": "首页", "Type": 2, "Path": "/", "Status": 1, "SortOrder": 1, "Indent": "", "Icon": "home", "Remark": "",
			"PermissionCodes": []string{"menu:list", "menu:create"}}
		out := renderAdminEmptyProbe(t, "admin/system/menus", map[string]any{"Rows": []any{row}, "Parents": []any{row}})
		if strings.Contains(out, `class="empty-state"`) {
			t.Error("menus 有数据时不应出现空态")
		}
		if !strings.Contains(out, "首页") {
			t.Error("menus 有数据时行没渲染")
		}
	})

	t.Run("navigations", func(t *testing.T) {
		row := map[string]any{"ID": "n1", "Title": "关于我们", "Path": "/about", "TargetLabel": "当前窗口", "Indent": "0", "Depth": 0, "First": true, "Last": true}
		out := renderAdminEmptyProbe(t, "admin/navigation/navigations", map[string]any{
			"Rows": []any{row}, "Parents": []any{},
			"Projects":        []any{map[string]any{"ID": "pr1", "Name": "官网"}},
			"SelectedProject": "pr1", "Kind": "header",
		})
		if strings.Contains(out, `class="empty-state"`) {
			t.Error("navigations 有数据时不应出现空态")
		}
		if !strings.Contains(out, "关于我们") {
			t.Error("navigations 有数据时行没渲染")
		}
	})

}

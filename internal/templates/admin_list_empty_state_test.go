package templates

import (
	"strings"
	"testing"
)

// admin_list_empty_state_test.go — 「空态不吃掉表头」的渲染级守卫（审计 02-L §P0-3）。
//
// 为什么需要它：列表页最普遍的一类布局缺陷是把 `<table>` 写进 `{{if len(...) == 0}}` 的
// `{{else}}` 分支 —— 空数据 / 筛选无结果时**整张表连同表头一起不渲染**，用户看不到有哪些列，
// 也无从确认自己是不是筛错了列（比看到一张只有表头的空表格更困惑）。它不会让任何既有测试变红，
// HTTP 也恒为 200，靠人眼发现需要「恰好在那页构造出空数据」。
//
// 两层守卫分工，别互相替代：
//   · scripts/check-empty-state-table-head.sh —— **静态**判据（栈解析 if/else/end 的配对），
//     覆盖全部后台模板，能抓「`<table>` 还在 else 里」这个形状；它管不到 colspan 对不对。
//   · 本文件 —— **渲染级**判据，覆盖那些在运行期构造不到空态的页面（menus / navigations /
//     dashboard 默认就有数据，且测试禁写库），并钉住 **colspan 必须等于该表实际列数**：
//     colspan 写错不会报错，只会让空态那一行与表头对不齐（视觉错位，评审极易放过）。
//
// 判据（每条都是「空态下仍成立」）：`<table class="data-table">` 在输出里、`<thead>` 在、
// 空态块在、`colspan` 精确等于期望值、**空态落在 `<tbody>` 之内**（而不是脱离表格单独渲染）。
// 另有一组「有数据形态」用例：不出现空态、数据行照常渲染 —— 防止「修空态把正常行改坏」。
//
// 数据形状取自各页 handler 的真实注入键（`adminShellData()` + 页面自己的键），
// 所以模板里新增/改名的键会在这里以渲染失败的形式暴露，而不是静默漏到线上。

// renderAdminEmptyProbe 用后台页壳数据 + 用例补充键渲染一个后台模板。
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

// assertEmptyKeepsTableHead 空态形态的整组断言（表头在、空态在、colspan 对、空态在 tbody 内）。
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
	// 空态必须落在 tbody 的 <td> 里，而不是独立于表格之外。
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

// TestAdminEmptyStateKeepsTableHead 空数据时表头必须仍在（含 canDelete 两种形态的 colspan）。
func TestAdminEmptyStateKeepsTableHead(t *testing.T) {
	t.Run("departments/无删除权限→colspan7", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/system/departments", map[string]any{"Rows": []any{}, "Parents": []any{}})
		assertEmptyKeepsTableHead(t, "departments", out, "7", []string{"还没有部门"})
	})

	t.Run("departments/有删除权限→colspan8", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/system/departments", map[string]any{
			"Rows": []any{}, "Parents": []any{}, "PermSet": map[string]any{"dept:delete": true},
		})
		assertEmptyKeepsTableHead(t, "departments+delete", out, "8", []string{"还没有部门", "data-check-all"})
	})

	t.Run("menus/无删除权限→colspan9", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/system/menus", map[string]any{"Rows": []any{}, "Parents": []any{}})
		assertEmptyKeepsTableHead(t, "menus", out, "9", []string{"还没有菜单"})
	})

	t.Run("menus/有删除权限→colspan10", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/system/menus", map[string]any{
			"Rows": []any{}, "Parents": []any{}, "PermSet": map[string]any{"menu:delete": true},
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

	// dashboard 自 2026-10 改版后不再有列表卡（「最近更新的页面」整块下线，
	// 空态与它的 colspan 判据随之退役）—— 这一页现在没有 <table> 空态，
	// 它的表格只有排行榜那两张，且没有「无数据整行」形态。
}

// TestAdminDataFormUnchanged 有数据形态：不出现空态、数据行照常渲染（防「修空态把正常行改坏」）。
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

	// dashboard 无列表卡（见上一条的说明），不再纳入「有数据形态」覆盖。
}

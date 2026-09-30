package templates

// stale_pages_drawer_test.go — 「待重建页面」只读抽屉片段（admin/partials/stale_pages_drawer.html）
// 的渲染守卫。
//
// 为什么值得单独钉：这个片段要过 ui/drawer.js 的 fragmentRoot 校验才进 DOM，而校验的两类硬约束
// 都是**渲染期才暴露**的：
//  ① 根元素带 data-drawer-fragment，且含 <form> **或**显式声明 data-drawer-readonly（只读抽屉）；
//  ② 片段里不能出现 script / style / link / meta / iframe / object / embed / svg / math / template。
//
// 违反任一条，用户点开抽屉看到的是「编辑表单加载失败，请重试」——服务端没有任何报错、日志干净。
//
// 另一半是列数一致性：表头写了 N 列、数据行只给 N-1 个 <td> 时浏览器**不报错**，而是把缺的那一列
// 补在行尾 → 整表左移一列。该片段用 MultiProject 同时控制表头与数据行，正是最容易漂的地方。

import (
	"strings"
	"testing"
)

// assertDrawerShell 抽屉基座契约（含只读抽屉的显式声明）。
func assertDrawerShell(t *testing.T, out string) {
	t.Helper()
	if !strings.Contains(out, "data-drawer-fragment") || !strings.Contains(out, "data-drawer-readonly") {
		t.Error("片段缺少 data-drawer-fragment / data-drawer-readonly —— drawer.js 会把只读抽屉判成非法")
	}
	for _, bad := range []string{"<script", "<style", "<iframe", "<svg", "<template", " on"} {
		if strings.Contains(out, bad) {
			t.Errorf("片段里出现 %q：fragmentRoot 校验会直接拒绝（用户看到的是一次无报错的加载失败）", bad)
		}
	}
}

// assertRowCellsMatchHead 表头列数 == 每个数据行的单元格数。
func assertRowCellsMatchHead(t *testing.T, out string, want int) {
	t.Helper()
	theadStart := strings.Index(out, "<thead>")
	theadEnd := strings.Index(out, "</thead>")
	tbodyStart := strings.Index(out, "<tbody>")
	tbodyEnd := strings.Index(out, "</tbody>")
	if theadStart < 0 || theadEnd < 0 || tbodyStart < 0 || tbodyEnd < 0 {
		t.Fatalf("缺少完整的 thead/tbody：%s", out)
	}
	// 数 "<th>" 与 "<th " 而不是 "<th"：后者会把 "<thead>" 本身也算进去。
	head := out[theadStart:theadEnd]
	if n := strings.Count(head, "<th>") + strings.Count(head, "<th "); n != want {
		t.Errorf("表头列数 = %d，期望 %d", n, want)
	}
	body := out[tbodyStart:tbodyEnd]
	for i, row := range strings.Split(body, "<tr>")[1:] {
		if n := strings.Count(row, "<td"); n != want {
			t.Errorf("第 %d 行有 %d 个单元格，而表头是 %d 列（多/少一列都会让整表左移）", i+1, n, want)
		}
	}
}

func TestStalePagesDrawerSingleProject(t *testing.T) {
	out := renderAdminEmptyProbe(t, "admin/partials/stale_pages_drawer.html", map[string]any{
		"Rows": []map[string]any{
			{"ID": "p1", "Path": "/zh/index", "ProjectName": "站点", "Published": true},
			{"ID": "p2", "Path": "/draft-page", "ProjectName": "站点", "Published": false},
		},
		"Total": 2, "Truncated": false, "Limit": 8, "MultiProject": false,
	})

	assertDrawerShell(t, out)
	if strings.Contains(out, "所属工程") {
		t.Error("单工程时不该渲染「所属工程」列：整列都是同一个名字，是纯噪声")
	}
	assertRowCellsMatchHead(t, out, 3)

	for _, want := range []string{
		"/zh/index", "/draft-page",
		"有更新未发布", "未上线", // 两档状态必须分开：后者的下一步不是重建而是发布
		`href="/workbench?id=p1"`, // 清单项要能直接去改
	} {
		if !strings.Contains(out, want) {
			t.Errorf("缺少 %q", want)
		}
	}
	if strings.Contains(out, "清单只列了前") {
		t.Error("未截断时不该出现截断说明")
	}
}

func TestStalePagesDrawerMultiProjectKeepsColumnsAligned(t *testing.T) {
	out := renderAdminEmptyProbe(t, "admin/partials/stale_pages_drawer.html", map[string]any{
		"Rows": []map[string]any{
			{"ID": "p1", "Path": "/about", "ProjectName": "站点甲", "Published": true},
			{"ID": "p2", "Path": "/about", "ProjectName": "站点乙", "Published": true},
		},
		"Total": 2, "Truncated": true, "Limit": 8, "MultiProject": true,
	})

	assertDrawerShell(t, out)
	if !strings.Contains(out, "所属工程") {
		t.Error("多工程时必须给出工程列：两个工程可以各有一个一模一样的 /about")
	}
	// 关键：表头多出来的那一列，数据行也必须补上（否则整表左移一列）。
	assertRowCellsMatchHead(t, out, 4)
	if !strings.Contains(out, "站点甲") || !strings.Contains(out, "站点乙") {
		t.Error("多工程时行里应带上各自的工程名")
	}
	if !strings.Contains(out, "清单只列了前") {
		t.Error("Truncated=true 时必须说明清单被截断了（一个看起来完整的清单会被当成影响面就这么大）")
	}
}

// TestStalePagesDrawerShowsRebuildFailure 「最近一次自动重建失败」只在有痕迹的行上出现一次。
//
// 这条界面的意义：stale 徽标说「这页落后了」，失败行说「而且上次重建它没成功」——
// 两者的处置相反（等 vs 查日志）。没有它，读的人只能靠猜是队列在追还是真的坏了。
func TestStalePagesDrawerShowsRebuildFailure(t *testing.T) {
	note := "最近一次自动重建失败：构建 / 发布阶段（2026-09-29 16:20）"
	out := renderAdminEmptyProbe(t, "admin/partials/stale_pages_drawer.html", map[string]any{
		"Rows": []map[string]any{
			{"ID": "p1", "Path": "/zh/index", "ProjectName": "站点", "Published": true, "FailedNote": note},
			{"ID": "p2", "Path": "/zh/shop", "ProjectName": "站点", "Published": true, "FailedNote": ""},
		},
		"Total": 2, "Truncated": false, "Limit": 8, "MultiProject": false,
	})

	assertDrawerShell(t, out)
	if !strings.Contains(out, note) {
		t.Error("有失败痕迹的行必须显示它（否则「还没轮到」与「重建失败」在界面上分不开）")
	}
	// 没有痕迹的行不该渲染出空的一行：空 div 会撑出一条无内容的间隔，看起来像渲染错误。
	if n := strings.Count(out, "最近一次自动重建失败"); n != 1 {
		t.Errorf("失败提示出现 %d 次，期望 1 次（只有一行有痕迹）", n)
	}
	// 加了一行提示不该改变列数（表头 3 列、每行 3 个单元格）。
	assertRowCellsMatchHead(t, out, 3)
}

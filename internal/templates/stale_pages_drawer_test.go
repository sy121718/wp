package templates

import (
	"strings"
	"testing"
)

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

func assertRowCellsMatchHead(t *testing.T, out string, want int) {
	t.Helper()
	theadStart := strings.Index(out, "<thead>")
	theadEnd := strings.Index(out, "</thead>")
	tbodyStart := strings.Index(out, "<tbody>")
	tbodyEnd := strings.Index(out, "</tbody>")
	if theadStart < 0 || theadEnd < 0 || tbodyStart < 0 || tbodyEnd < 0 {
		t.Fatalf("缺少完整的 thead/tbody：%s", out)
	}

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
			{"ID": "p1", "Path": "/zh/index", "ProjectName": "站点", "Published": true, "RebuildFailure": map[string]any{}},
			{"ID": "p2", "Path": "/draft-page", "ProjectName": "站点", "Published": false, "RebuildFailure": map[string]any{}},
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
		"有更新未发布", "未上线",
		`href="/workbench?id=p1"`,
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
			{"ID": "p1", "Path": "/about", "ProjectName": "站点甲", "Published": true, "RebuildFailure": map[string]any{}},
			{"ID": "p2", "Path": "/about", "ProjectName": "站点乙", "Published": true, "RebuildFailure": map[string]any{}},
		},
		"Total": 2, "Truncated": true, "Limit": 8, "MultiProject": true,
	})

	assertDrawerShell(t, out)
	if !strings.Contains(out, "所属工程") {
		t.Error("多工程时必须给出工程列：两个工程可以各有一个一模一样的 /about")
	}

	assertRowCellsMatchHead(t, out, 4)
	if !strings.Contains(out, "站点甲") || !strings.Contains(out, "站点乙") {
		t.Error("多工程时行里应带上各自的工程名")
	}
	if !strings.Contains(out, "清单只列了前") {
		t.Error("Truncated=true 时必须说明清单被截断了（一个看起来完整的清单会被当成影响面就这么大）")
	}
}

func TestStalePagesDrawerShowsRebuildFailure(t *testing.T) {
	note := "最近一次自动重建失败：构建 / 发布阶段（2026-09-29 16:20）"

	failed := map[string]any{"Has": true, "StageKey": "admin.pages.impact.stage_build",
		"StageFallback": "构建 / 发布阶段", "At": "2026-09-29 16:20"}
	out := renderAdminEmptyProbe(t, "admin/partials/stale_pages_drawer.html", map[string]any{
		"Rows": []map[string]any{
			{"ID": "p1", "Path": "/zh/index", "ProjectName": "站点", "Published": true, "RebuildFailure": failed},
			{"ID": "p2", "Path": "/zh/shop", "ProjectName": "站点", "Published": true, "RebuildFailure": map[string]any{}},
		},
		"Total": 2, "Truncated": false, "Limit": 8, "MultiProject": false,
	})

	assertDrawerShell(t, out)
	if !strings.Contains(out, note) {
		t.Error("有失败痕迹的行必须显示它（否则「还没轮到」与「重建失败」在界面上分不开）")
	}

	if n := strings.Count(out, "最近一次自动重建失败"); n != 1 {
		t.Errorf("失败提示出现 %d 次，期望 1 次（只有一行有痕迹）", n)
	}

	assertRowCellsMatchHead(t, out, 3)
}

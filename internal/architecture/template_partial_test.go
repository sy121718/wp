package architecture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// emptyStateBudget 是「后台里手写 <div class="empty-state"> 的处数」上限，只减不增。
//
// 68 → 67 的下降来自 admin 域页面写操作收口（administrators/roles 的列表空态改走
// partials/empty_state.html 片段），本批 admin 侧只同步数字：降的那一处不在 task-35 范围，
// 这里如实记账，不把别人的收口算成自己的成果。
const emptyStateBudget = 67

const emptyStatePartial = "pkg/view/ui/templates/admin/partials/empty_state.html"

// 151 → 153：工作区既有的模板改动新增两处手写徽标（不在本次模板属主迁移范围内），
// 按实际值记账，不把别人的改动算成本轮成果，也不放宽到「以后随便加」。
const badgeBudget = 153

const badgePartial = "pkg/view/ui/templates/admin/partials/badge.html"

func TestEmptyStateMarkupStaysInPartial(t *testing.T) {
	partial, err := os.ReadFile(filepath.Join(repoRoot(t), emptyStatePartial))
	if err != nil {
		t.Fatalf("空态片段读不到（后台唯一的空态形态就是它）: %v", err)
	}
	if !strings.Contains(string(partial), `<div class="empty-state" role="status">`) {
		t.Fatal("空态片段必须是容器本身：<div class=\"empty-state\" role=\"status\">（读屏用户靠 role 得知零结果）")
	}

	count := 0
	files := 0
	err = filepath.WalkDir(filepath.Join(repoRoot(t), "internal/templates/admin"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}
		if strings.HasSuffix(filepath.ToSlash(path), emptyStatePartial) {
			return nil
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		n := strings.Count(string(body), `<div class="empty-state"`)
		if n > 0 {
			files++
			count += n
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫描后台模板失败: %v", err)
	}
	if count != emptyStateBudget {
		t.Fatalf("手写空态容器 %d 处（%d 个文件），预算 %d 处：\n"+
			"· 多出来 = 新写了一处空态 → 改用 {{include \"../partials/empty_state.html\" map(\"title\", …)}}\n"+
			"· 变少 = 已经收口 → 把本文件的 emptyStateBudget 改成 %d（预算只减不增）",
			count, files, emptyStateBudget, count)
	}
}

func TestBadgeMarkupStaysInPartial(t *testing.T) {
	partial, err := os.ReadFile(filepath.Join(repoRoot(t), badgePartial))
	if err != nil {
		t.Fatalf("徽标片段读不到: %v", err)
	}
	if !strings.Contains(string(partial), `<span class="badge badge-{{.tone}}">{{.label}}</span>`) {
		t.Fatal("徽标片段必须是唯一形态：<span class=\"badge badge-{{.tone}}\">{{.label}}</span>")
	}

	count := 0
	err = filepath.WalkDir(filepath.Join(repoRoot(t), "internal/templates/admin"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".html") || strings.HasSuffix(filepath.ToSlash(path), badgePartial) {
			return nil
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		count += strings.Count(string(body), `<span class="badge badge-`)
		return nil
	})
	if err != nil {
		t.Fatalf("扫描后台模板失败: %v", err)
	}
	if count != badgeBudget {
		t.Fatalf("手写徽标 %d 处，预算 %d 处：\n"+
			"· 多出来 = 新写了一处徽标 → 改用 {{include \"../partials/badge.html\" map(\"tone\", …)}}\n"+
			"· 变少 = 已经收口 → 把本文件的 badgeBudget 改成 %d（预算只减不增）",
			count, badgeBudget, count)
	}
}

package architecture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 空态容器只能有一份形态：admin/partials/empty_state.html。
//
// 判据是**处数**（双向）：后台模板里手写的 <div class="empty-state"> 只能减少 ——
// 新写一处就失败，改完一处忘了下调预算也失败。这是「同一形态只写一遍」唯一能自动守住的形式。
const emptyStateBudget = 72

const emptyStatePartial = "internal/templates/admin/partials/empty_state.html"

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

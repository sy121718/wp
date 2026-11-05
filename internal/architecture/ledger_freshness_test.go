package architecture

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// 以下台账的路径键在搬包后可能永久失效；任何一条指向不存在的文件都说明
// 门禁已在空转，必须显式失败而不是静默通过。
var budgetLedgers = []struct {
	name    string
	entries map[string]int
}{
	{name: "htmlLiteralBudget", entries: htmlLiteralBudget},
	{name: "pendingMsgTextDomain", entries: pendingMsgTextDomain},
	{name: "pendingKeepEmptyFields", entries: pendingKeepEmptyFields},
}

// TestBudgetLedgerPathsExist 防止包移动后旧路径键永久失效。
func TestBudgetLedgerPathsExist(t *testing.T) {
	root := repoRoot(t)
	for _, ledger := range budgetLedgers {
		var stale []string
		for file := range ledger.entries {
			if _, err := os.Stat(filepath.Join(root, file)); err != nil {
				stale = append(stale, file)
			}
		}
		sort.Strings(stale)
		if len(stale) > 0 {
			t.Errorf("%s 存在失效路径（文件已不存在，条目应在迁移时同步删除或改名）：\n%s",
				ledger.name, strings.Join(stale, "\n"))
		}
	}
}

// TestScanRootsExist 保证每个门禁的扫描根真实存在，路径写错直接失败。
func TestScanRootsExist(t *testing.T) {
	root := repoRoot(t)
	roots := []string{
		"internal/module",
		"internal/routers",
		"internal/templates",
		"pkg",
	}
	for _, rel := range roots {
		info, err := os.Stat(filepath.Join(root, rel))
		if err != nil {
			t.Errorf("扫描根 %s 不存在：%v", rel, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("扫描根 %s 不是目录", rel)
		}
	}
}

// TestScanRootsAreNotEmpty 区分「违规为零」与「源文件为零」：
// 预期应有输入的扫描根一旦为空，必须显式失败。
func TestScanRootsAreNotEmpty(t *testing.T) {
	root := repoRoot(t)
	cases := []struct {
		root string
		ext  string
		min  int
	}{
		{root: "pkg", ext: ".go", min: 10},
		{root: filepath.Join("internal", "module"), ext: ".go", min: 100},
		{root: filepath.Join("internal", "templates"), ext: ".html", min: 50},
	}
	for _, c := range cases {
		count := 0
		err := filepath.WalkDir(filepath.Join(root, c.root), func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if strings.HasSuffix(path, c.ext) {
				count++
			}
			return nil
		})
		if err != nil {
			t.Fatalf("遍历 %s 失败：%v", c.root, err)
		}
		if count < c.min {
			t.Errorf("扫描根 %s 只匹配到 %d 个 %s 文件（预期至少 %d）：扫描可能已经空转",
				c.root, count, c.ext, c.min)
		}
	}
}

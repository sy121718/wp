package architecture

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
)

const docsPointerBudget = 0

var docsPointerPattern = regexp.MustCompile("docs/[A-Za-z0-9._/\\-]+\\.(?:md|html|txt|sql)")

func TestNoDanglingDocsPointers(t *testing.T) {
	root := repoRoot(t)
	exts := map[string]bool{".go": true, ".md": true, ".sh": true, ".html": true, ".js": true, ".css": true}
	skipDirs := map[string]bool{"tmp": true, ".scratch": true, ".backup": true, "runtime": true, "logs": true, "storage": true, "site": true}

	var dangling []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && (skipDirs[d.Name()] || skipArchScanDir(d.Name())) {
				return filepath.SkipDir
			}
			return nil
		}
		if !exts[filepath.Ext(path)] {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		for _, ref := range docsPointerPattern.FindAllString(string(body), -1) {
			if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(ref))); statErr != nil {
				dangling = append(dangling, rel+": "+ref)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}

	sort.Strings(dangling)
	for i, item := range dangling {
		if i >= 20 {
			break
		}
		t.Logf("死指针 %s", item)
	}
	if len(dangling) > docsPointerBudget {
		t.Fatalf("指向已删除文档的引用变多了：现在 %d 处，预算 %d；请在本文件里同步下调 docsPointerBudget", len(dangling), docsPointerBudget)
	}
	if len(dangling) < docsPointerBudget {
		t.Fatalf("存量已清到 %d 处，但预算还是 %d；请把 docsPointerBudget 改成 %d", len(dangling), docsPointerBudget, len(dangling))
	}

	_ = fmt.Sprintf
}

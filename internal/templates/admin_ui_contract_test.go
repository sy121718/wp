package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 后台页面必须同时挂公共语义类和旧页面类；迁移完成前保留旧类只用于布局桥接，
// 不能再出现只依赖 pages-* 才有外观的新增页面。
func TestAdminPagesExposePublicUIClasses(t *testing.T) {
	entries, err := os.ReadDir("admin")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".html") || entry.Name() == "layout.html" {
			continue
		}
		path := filepath.Join("admin", entry.Name())
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		html := string(src)
		checks := []struct{ legacy, public string }{
			{"pages-card", "card"},
			{"pages-form", "form-row"},
			{"pages-table", "data-table"},
			{"pages-table-wrap", "table-wrap"},
		}
		for _, check := range checks {
			if strings.Contains(html, check.legacy) && !strings.Contains(html, check.public) {
				t.Errorf("%s 使用 %s 但未接入公共类 %s", path, check.legacy, check.public)
			}
		}
	}
}

package templates

import (
	"os"
	"strings"
	"testing"
)

func TestMediaPageSelectionContract(t *testing.T) {
	src, err := os.ReadFile("admin/media/media.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(src)
	for _, marker := range []string{
		`class="page-head"`, `class="filter-bar"`, `card list-card`,
		`id="ml-check-all-grid"`, `id="ml-check-all-table"`,
		`id="ml-bulk-count"`, `id="ml-batch-download"`, `id="ml-batch-delete"`,
	} {
		if !strings.Contains(page, marker) {
			t.Errorf("媒体列表缺少 %s", marker)
		}
	}
	if strings.Contains(page, `data-check-all`) || strings.Contains(page, `data-check-item`) {
		t.Error("动态媒体勾选必须与静态 admin.js 选择器隔离")
	}
}

package productenums_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductStatusLabelsStayedInTemplate(t *testing.T) {
	for _, name := range []string{"products.html", "product_edit.html"} {
		src, err := os.ReadFile(filepath.Join("..", "..", "..", "templates", "admin", "product", name))
		if err != nil {
			t.Fatalf("读模板 %s 失败：%v", name, err)
		}
		for _, want := range []string{"admin.products.status.draft", "admin.products.status.published", "admin.products.status.archived"} {
			if !strings.Contains(string(src), want) {
				t.Errorf("模板 %s 缺少 %q —— 状态显示名必须由模板取词", name, want)
			}
		}
	}
}

func TestTagLabelMappingsStayedInTemplate(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "templates", "admin", "product", "product_tags.html"))
	if err != nil {
		t.Fatalf("读标签页模板失败：%v", err)
	}
	for _, want := range []string{"admin.product_tags.kind.rule", "admin.product_tags.kind.manual", "admin.product_tags.recalcNone"} {
		if !strings.Contains(string(src), want) {
			t.Errorf("标签页模板缺少 %q —— 类型名与「未重算过」必须由模板取词", want)
		}
	}
}

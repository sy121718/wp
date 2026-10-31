package producthttp

import (
	"os"
	"strings"
	"testing"
)

func TestDepsMissingPromptLivesInTemplate(t *testing.T) {
	body, err := os.ReadFile("../../../../templates/admin/product/product_detail_template.html")
	if err != nil {
		t.Fatalf("读模板失败: %v", err)
	}
	if !strings.Contains(string(body), "admin.product_detail_template.depsMissing") {
		t.Fatal("模板缺少未装配提示的取词：Go 侧已不再提供这句文案")
	}
}

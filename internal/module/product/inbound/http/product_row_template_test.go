package producthttp

import (
	"os"
	"strings"
	"testing"
)

// 主体模板只读商品实体与独立业务结果，禁止重新引入后端成品行。
func TestProductTemplatesUseOriginalEntities(t *testing.T) {
	for _, name := range []string{"products.html", "product_edit.html", "product_detail.html"} {
		raw, err := os.ReadFile("../../../../templates/admin/product/" + name)
		if err != nil {
			t.Fatal(err)
		}
		for _, old := range []string{".CategoryChecks", ".PrimaryOptions", ".BrandOptions", ".RatingAvg", ".ImagesInput", ".WeightInput", ".BackURL"} {
			if strings.Contains(string(raw), old) {
				t.Errorf("%s 仍读取后端展示值 %s", name, old)
			}
		}
	}
}

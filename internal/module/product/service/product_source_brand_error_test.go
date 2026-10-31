package productservice_test

import (
	"os"
	"strings"
	"testing"
)

func TestLocalizeRelatedDoesNotSwallowBrandErrors(t *testing.T) {
	src := readServiceSource(t, "product_source.go")

	if strings.Contains(src, "if row, berr := s.m.GetBrand(ctx, *e.BrandID, e.ProjectID); berr == nil {") {
		t.Fatal("localizeRelated 仍在吞掉品牌查询的全部错误 —— " +
			"查库失败/RLS 未设会表现为「品牌名全空」，构建却成功")
	}

	for _, want := range []string{
		"row, berr := s.m.GetBrand(ctx, *e.BrandID, e.ProjectID)",
		"errors.Is(berr, gorm.ErrRecordNotFound)",
		"return nil, berr",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("localizeRelated 缺少 %q（品牌错误要有显式出口）", want)
		}
	}
}

func readServiceSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("读取 %s 失败：%v", name, err)
	}
	return string(b)
}

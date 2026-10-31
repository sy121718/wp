package feature

import (
	"context"
	"fmt"
	productdto "go_wp/internal/module/product/dto"
	"reflect"
	"testing"
)

func TestCategoryHugePageMatchesLastPage(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: f.projectID, Name: fmt.Sprintf("大页边界%d", i), Slug: fmt.Sprintf("huge-page-%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	last, err := f.svc.QueryCategories(ctx, &productdto.CatalogTaxonomyReq{ProjectID: f.projectID, Page: 2, Size: 4})
	if err != nil {
		t.Fatal(err)
	}
	maxInt := int(^uint(0) >> 1)
	for _, size := range []int{1, 4} {
		last, err = f.svc.QueryCategories(ctx, &productdto.CatalogTaxonomyReq{ProjectID: f.projectID, Page: 999, Size: size})
		if err != nil {
			t.Fatal(err)
		}
		got, err := f.svc.QueryCategories(ctx, &productdto.CatalogTaxonomyReq{ProjectID: f.projectID, Page: maxInt, Size: size})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(last, got) {
			t.Fatalf("超大页码与末页错配: size=%d last=%+v got=%+v", size, last, got)
		}
	}
}

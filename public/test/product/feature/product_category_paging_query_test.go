package feature

import (
	"context"
	"reflect"
	"testing"

	productdto "go_wp/internal/module/product/dto"
)

func TestCategoryOverflowReusesOneRead(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	for _, slug := range []string{"first-root", "second-root"} {
		root, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: f.projectID, Name: slug, Slug: slug})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: f.projectID, ParentID: root.ID, Name: "命中叶", Slug: slug + "-leaf"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, keyword := range []string{"", "命中叶", "没有此分类"} {
		t.Run(keyword, func(t *testing.T) {
			var last, overflow *productdto.CategorySearchResp
			normal := productQueryCounts(t, f.db, func() {
				var err error
				last, err = f.svc.QueryCategories(ctx, &productdto.CatalogTaxonomyReq{ProjectID: f.projectID, Keyword: keyword, Page: 2, Size: 1})
				if err != nil {
					t.Fatal(err)
				}
			})
			excess := productQueryCounts(t, f.db, func() {
				var err error
				overflow, err = f.svc.QueryCategories(ctx, &productdto.CatalogTaxonomyReq{ProjectID: f.projectID, Keyword: keyword, Page: 999, Size: 1})
				if err != nil {
					t.Fatal(err)
				}
			})
			wantRows, wantTotal, wantPage := 2, int64(2), 2
			if keyword == "没有此分类" {
				wantRows, wantTotal, wantPage = 0, 0, 1
			}
			if len(last.Items) != wantRows || last.Total != wantTotal || last.Page != wantPage || !reflect.DeepEqual(last, overflow) {
				t.Fatalf("越界应返回末页完整子树: last=%+v overflow=%+v", last, overflow)
			}
			if keyword != "" && last.MatchTotal != wantTotal {
				t.Fatalf("命中数与根数分别保留: %+v", last)
			}
			if normal["product_categories"] == 0 || normal["product_categories"] != excess["product_categories"] {
				t.Fatalf("夹页不应重查分类森林: last=%v overflow=%v", normal, excess)
			}
		})
	}
}

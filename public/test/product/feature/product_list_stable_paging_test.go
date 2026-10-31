package feature

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productservice "go_wp/internal/module/product/service"
)

func TestProductListAndSeoIndexKeepAllRowsWhenSortAndTimeTie(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	const count = 205
	ids := make([]string, count)
	for i := range ids {
		ids[i] = uuid.NewString()
	}
	sort.Strings(ids)
	createdAt := time.Date(2026, time.October, 29, 0, 0, 0, 0, time.UTC)
	rows := make([]map[string]any, 0, count)

	for i := len(ids) - 1; i >= 0; i-- {
		rows = append(rows, map[string]any{
			"id": ids[i], "project_id": f.projectID, "name": fmt.Sprintf("同批商品 %03d", i),
			"slug": fmt.Sprintf("same-batch-%03d", i), "status": productenums.StatusPublished,
			"sort": 0, "create_time": createdAt, "update_time": createdAt,
		})
	}
	if err := f.db.Table("products").Create(&rows).Error; err != nil {
		t.Fatalf("准备同排序同时间商品失败：%v", err)
	}

	ctx := context.Background()
	got := make([]string, 0, count)
	for page, wantSize := range []int{100, 100, 5} {
		list, err := f.svc.List(ctx, &productdto.ListReq{ProjectID: f.projectID, Status: productenums.StatusPublished, Page: page + 1, Size: 100})
		if err != nil {
			t.Fatalf("读取第 %d 页失败：%v", page+1, err)
		}
		if len(list) != wantSize {
			t.Fatalf("第 %d 页长度=%d，期望 %d", page+1, len(list), wantSize)
		}
		for _, item := range list {
			got = append(got, item.ID)
		}
	}
	if !reflect.DeepEqual(got, ids) {
		t.Fatalf("分页拼接必须等于按唯一尾键排序的全集，不能重复或漏行：got=%v want=%v", got, ids)
	}

	index, _, err := productservice.SeoTitleIndex(ctx, f.svc, productservice.SeoTitleExtraSources{}, f.projectID)
	if err != nil {
		t.Fatalf("构造完整 SEO 标题索引失败：%v", err)
	}
	indexed := make([]string, 0, len(index))
	for _, entry := range index {
		if entry.Kind == "product" {
			indexed = append(indexed, entry.ID)
		}
	}
	if !reflect.DeepEqual(indexed, ids) {
		t.Fatalf("SEO 全量翻页必须覆盖同一商品全集：got=%v want=%v", indexed, ids)
	}
}

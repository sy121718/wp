package feature

import (
	"context"
	"fmt"
	"testing"
	"time"

	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/rls"
	"gorm.io/gorm"
)

func TestAttributePagingUsesUniqueTailKey(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	for i := 6; i >= 1; i-- {
		row := &productmodel.ProductAttributeEntity{ID: fmt.Sprintf("00000000-0000-4000-8000-%012d", i), ProjectID: f.projectID, Key: fmt.Sprintf("tie_%d", i), Name: fmt.Sprintf("同序属性%d", i), Values: []byte("[]"), Metadata: []byte("{}"), CreatedAt: at, UpdatedAt: at}
		if err := rls.InProjectScope(ctx, f.db, f.projectID, func(tx *gorm.DB) error { return tx.Create(row).Error }); err != nil {
			t.Fatal(err)
		}
	}
	m := productmodel.NewModel(f.db)
	for offset := 0; offset < 6; offset += 2 {
		rows, err := m.ListAttributes(ctx, f.projectID, "", nil, 2, offset)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 {
			t.Fatalf("分页行数=%d", len(rows))
		}
		for i, row := range rows {
			want := fmt.Sprintf("00000000-0000-4000-8000-%012d", offset+i+1)
			if row.ID != want {
				t.Fatalf("同序分页未按唯一尾键排序: offset=%d got=%s want=%s", offset, row.ID, want)
			}
		}
	}
}

func TestPublishRecordListUsesUniqueTailKey(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i := 6; i >= 1; i-- {
		row := &productmodel.ProductEntity{ID: fmt.Sprintf("00000000-0000-4000-8000-%012d", i), ProjectID: f.projectID, Name: fmt.Sprintf("同序商品%d", i), Slug: fmt.Sprintf("publish-tie-%d", i), Status: "draft", Images: []byte("[]"), Metadata: []byte("{}"), CreatedAt: at, UpdatedAt: at}
		if err := rls.InProjectScope(ctx, f.db, f.projectID, func(tx *gorm.DB) error {
			return tx.Select("ID", "ProjectID", "Name", "Slug", "Status", "CreatedAt", "UpdatedAt").Create(row).Error
		}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := productmodel.NewModel(f.db).ListForPublishRecords(ctx, f.projectID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 6 {
		t.Fatalf("发布记录候选行数=%d", len(rows))
	}
	for i, row := range rows {
		want := fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1)
		if row.ID != want {
			t.Fatalf("发布记录同序未按唯一尾键排序: got=%s want=%s", row.ID, want)
		}
	}
}

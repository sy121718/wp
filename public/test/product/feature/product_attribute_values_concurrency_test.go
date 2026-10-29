package feature

import (
	"context"
	"sync"
	"testing"
	"time"

	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/rls"
	"gorm.io/gorm"
)

func TestAttributeValuesPreserveConcurrentGroupUpdate(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	group, err := f.svc.CreateAttribute(t.Context(), &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: "旧组名", Key: "concurrent-values", IsVariation: boolPtr(true),
	})
	if err != nil {
		t.Fatal(err)
	}
	tx := f.db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	if err := rls.ScopeTx(tx, f.projectID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Model(&productmodel.ProductAttributeEntity{}).Where("id = ?", group.ID).
		Updates(map[string]any{"name": "新组名", "is_variation": false}).Error; err != nil {
		t.Fatal(err)
	}

	reached := make(chan struct{})
	var once sync.Once
	const callback = "test:attribute_values_lock"
	if err := f.db.Callback().Query().Before("gorm:query").Register(callback, func(q *gorm.DB) {
		if q.Statement.Table == "product_attributes" {
			if _, ok := q.Statement.Clauses["FOR"]; ok {
				once.Do(func() { close(reached) })
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer f.db.Callback().Query().Remove(callback)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := f.svc.SetAttributeValues(ctx, &productdto.SetAttributeValuesReq{
			ID: group.ID, ProjectID: f.projectID, Values: []productdto.AttributeValueReq{{Key: "blue", Label: "蓝色"}},
		})
		done <- err
	}()
	select {
	case <-reached:
	case err := <-done:
		t.Fatalf("锁读前已完成：%v", err)
	case <-ctx.Done():
		t.Fatal("未发出属性组锁读")
	}
	select {
	case err := <-done:
		t.Fatalf("并发组修改持锁时不得完成：%v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("释放组锁后保存没有完成")
	}
	got, err := f.svc.GetAttribute(t.Context(), &productdto.GetAttributeReq{ID: group.ID, ProjectID: f.projectID})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "新组名" || got.IsVariation || len(got.Values) != 1 || got.Values[0].Key != "blue" {
		t.Fatalf("保存属性值覆盖了并发组修改或丢失值：%+v", got)
	}
}

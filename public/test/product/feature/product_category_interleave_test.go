package feature

import (
	"context"
	"gorm.io/gorm"
	"strings"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
)

func TestProductCategoryAttachRechecksDeletedTarget(t *testing.T) {
	for _, create := range []bool{false, true} {
		t.Run(map[bool]string{false: "编辑", true: "创建"}[create], func(t *testing.T) {
			f := newFixture(t)
			if f == nil {
				return
			}
			ctx := context.Background()
			c, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: f.projectID, Name: "即将删除", Slug: "deleted-target"})
			if err != nil {
				t.Fatal(err)
			}
			p, err := f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "挂载交错", Slug: "attach-interleave"})
			if err != nil {
				t.Fatal(err)
			}
			fired := afterProductSnapshot(t, f.db, "product_categories", func() error {
				return f.svc.DeleteCategory(ctx, &productdto.DeleteCategoryReq{ProjectID: f.projectID, ID: c.ID})
			})
			if create {
				_, err = f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "新建挂载", Slug: "new-attach", CategoryIDs: []string{c.ID}})
			} else {
				_, err = f.svc.Update(ctx, &productdto.UpdateReq{ProjectID: f.projectID, ID: p.ID, CategoryIDs: []string{c.ID}})
			}
			if !*fired || err == nil || !strings.Contains(err.Error(), productenums.ErrCategoryNotFound) {
				t.Fatalf("已删除分类应使挂载失败: fired=%v err=%v", *fired, err)
			}
		})
	}
}

func TestCategoryDeleteRechecksInterleavedReference(t *testing.T) {
	for _, bulk := range []bool{false, true} {
		t.Run(map[bool]string{false: "单条", true: "批量"}[bulk], func(t *testing.T) {
			f := newFixture(t)
			if f == nil {
				return
			}
			ctx := context.Background()
			c, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: f.projectID, Name: "删除交错", Slug: "delete-interleave"})
			if err != nil {
				t.Fatal(err)
			}
			p, err := f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "并发引用", Slug: "reference-interleave"})
			if err != nil {
				t.Fatal(err)
			}
			fired := false
			name := "test:category_before_guard"
			if err := f.db.Callback().Raw().Before("gorm:raw").Register(name, func(tx *gorm.DB) {
				if fired || !strings.Contains(tx.Statement.SQL.String(), "pg_advisory_xact_lock") {
					return
				}
				fired = true
				_, e := f.svc.Update(ctx, &productdto.UpdateReq{ProjectID: f.projectID, ID: p.ID, CategoryIDs: []string{c.ID}})
				tx.AddError(e)
			}); err != nil {
				t.Fatal(err)
			}
			defer f.db.Callback().Raw().Remove(name)
			if bulk {
				_, err = f.svc.BulkDeleteProductCategories(ctx, &productdto.BulkDeleteReq{ProjectID: f.projectID, IDs: []string{c.ID}})
			} else {
				err = f.svc.DeleteCategory(ctx, &productdto.DeleteCategoryReq{ProjectID: f.projectID, ID: c.ID})
			}
			if !fired || err == nil {
				t.Fatalf("引用变化后应保留分类: fired=%v err=%v", fired, err)
			}
			var n int64
			if e := f.db.Model(&productmodel.ProductCategoryEntity{}).Where("id = ?", c.ID).Count(&n).Error; e != nil {
				t.Fatal(e)
			}
			if n != 1 {
				t.Fatal("失败后分类已删除")
			}
		})
	}
}

func TestCategoryParentRechecksDeletedTarget(t *testing.T) {
	for _, create := range []bool{false, true} {
		t.Run(map[bool]string{false: "编辑", true: "创建"}[create], func(t *testing.T) {
			f := newFixture(t)
			if f == nil {
				return
			}
			ctx := context.Background()
			parent, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: f.projectID, Name: "旧父级", Slug: "old-parent"})
			if err != nil {
				t.Fatal(err)
			}
			child, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: f.projectID, Name: "子级", Slug: "parent-child"})
			if err != nil {
				t.Fatal(err)
			}
			fired := false
			name := "test:category_parent_snapshot"
			if err := f.db.Callback().Query().After("gorm:after_query").Register(name, func(tx *gorm.DB) {
				row, ok := tx.Statement.Dest.(*productmodel.ProductCategoryEntity)
				if fired || !ok || row.ID != parent.ID {
					return
				}
				fired = true
				tx.AddError(f.svc.DeleteCategory(ctx, &productdto.DeleteCategoryReq{ProjectID: f.projectID, ID: parent.ID}))
			}); err != nil {
				t.Fatal(err)
			}
			defer f.db.Callback().Query().Remove(name)
			if create {
				_, err = f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: f.projectID, Name: "新子级", Slug: "new-child", ParentID: parent.ID})
			} else {
				_, err = f.svc.UpdateCategory(ctx, &productdto.UpdateCategoryReq{ProjectID: f.projectID, ID: child.ID, ParentID: &parent.ID})
			}
			if !fired || err == nil || !strings.Contains(err.Error(), productenums.ErrCategoryNotFound) {
				t.Fatalf("已删除父级应使写入失败: fired=%v err=%v", fired, err)
			}
		})
	}
}

func TestCategoryWritesKeepOtherProject(t *testing.T) {
	for _, action := range []string{"编辑", "删除", "批量删除"} {
		t.Run(action, func(t *testing.T) {
			f := newCrossRefFixture(t)
			if f == nil {
				return
			}
			ctx := context.Background()
			c, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: f.otherProjectID, Name: "对端分类", Slug: "foreign-category"})
			if err != nil {
				t.Fatal(err)
			}
			name := "错误改名"
			switch action {
			case "编辑":
				_, err = f.svc.UpdateCategory(ctx, &productdto.UpdateCategoryReq{ProjectID: f.projectID, ID: c.ID, Name: &name})
			case "删除":
				err = f.svc.DeleteCategory(ctx, &productdto.DeleteCategoryReq{ProjectID: f.projectID, ID: c.ID})
			case "批量删除":
				_, err = f.svc.BulkDeleteProductCategories(ctx, &productdto.BulkDeleteReq{ProjectID: f.projectID, IDs: []string{c.ID}})
			}
			if err == nil {
				t.Fatal("跨工程写入应返回错误")
			}
			var row productmodel.ProductCategoryEntity
			if err := f.db.First(&row, "id = ?", c.ID).Error; err != nil {
				t.Fatal(err)
			}
			if row.Name != "对端分类" || row.ProjectID != f.otherProjectID {
				t.Fatal("跨工程请求修改了分类")
			}
		})
	}
}

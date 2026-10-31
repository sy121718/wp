package feature

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/pkg/rls"
	"gorm.io/gorm"
)

func TestDeleteVariantKeepsOtherProject(t *testing.T) {
	for _, audit := range []bool{false, true} {
		t.Run(map[bool]string{false: "无留痕端口", true: "有留痕端口"}[audit], func(t *testing.T) {
			f := newCrossRefFixture(t)
			if f == nil {
				return
			}
			p := f.mkProduct(t, f.otherProjectID, "对端商品", "foreign-delete")
			if audit {
				f.svc.SetMasterDataChanges(&capturedProductChanges{})
			}
			var before productmodel.VariantEntity
			if err := f.db.First(&before, "id = ?", p.Variants[0].ID).Error; err != nil {
				t.Fatal(err)
			}
			err := f.svc.DeleteVariant(context.Background(), &productdto.DeleteVariantReq{ProjectID: f.projectID, ID: before.ID})
			if err == nil {
				t.Fatal("跨工程删除应返回错误")
			}
			role := ensureNonSuperRole(t, f.db)
			if err := f.db.Connection(func(conn *gorm.DB) error {
				if err := conn.Exec("SET ROLE " + role).Error; err != nil {
					return err
				}
				defer func() {
					if err := conn.Exec("RESET ROLE").Error; err != nil {
						t.Error(err)
					}
				}()
				bypass, err := rls.BypassedRole(context.Background(), conn)
				if err != nil {
					return err
				}
				if bypass {
					return fmt.Errorf("测试角色绕过 RLS")
				}
				projects := projectservice.NewService(projectmodel.NewProjectModel(conn))
				scoped := productservice.NewService(productmodel.NewModel(conn), projects)
				if audit {
					scoped.SetMasterDataChanges(&capturedProductChanges{})
				}
				if err := scoped.DeleteVariant(context.Background(), &productdto.DeleteVariantReq{ProjectID: f.projectID, ID: before.ID}); err == nil {
					return fmt.Errorf("受 RLS 约束的跨工程删除应返回错误")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			var after productmodel.VariantEntity
			if err := f.db.First(&after, "id = ?", before.ID).Error; err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("跨工程请求改变了变体")
			}
		})
	}
}

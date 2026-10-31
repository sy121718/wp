package productservice_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	"go_wp/public/test/support"
)

func TestPublicCatalogPageBounds(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "目录分页边界")
	productID := uuid.NewString()
	if err := db.Exec(
		"INSERT INTO products (id, project_id, name, slug, sku_code, status, sort, create_time, update_time) "+
			"VALUES (?, ?, '边界商品', 'bounds', 'SKU-BOUNDS', 'published', 1, now(), now())",
		productID, projectID).Error; err != nil {
		t.Fatalf("插入商品失败：%v", err)
	}
	s := productservice.NewService(productmodel.NewModel(db), listPageProjectStub{})
	s.SetPublicCatalogIndex(&catalogIndexStub{paths: map[string]string{productID: "/p/bounds"}})

	cases := []struct {
		name             string
		page, pageSize   int
		wantErr          bool
		wantPage, wantSz int
	}{
		{name: "默认页大小", page: 0, pageSize: 0, wantPage: 1, wantSz: 12},
		{name: "页码上限允许", page: 100, pageSize: 12, wantPage: 100, wantSz: 12},
		{name: "页码超上限拒绝", page: 101, pageSize: 12, wantErr: true},
		{name: "页大小上限允许", page: 1, pageSize: 60, wantPage: 1, wantSz: 60},
		{name: "页大小超上限拒绝", page: 1, pageSize: 61, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := s.PublicCatalog(context.Background(), &productdto.PublicCatalogReq{
				ProjectID: projectID, Page: tc.page, PageSize: tc.pageSize,
			})
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), productenums.ErrInvalidParam) {
					t.Fatalf("应拒绝为参数错误，实际 out=%+v err=%v", out, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("合法边界不应失败：%v", err)
			}
			if out.Page != tc.wantPage || out.PageSize != tc.wantSz {
				t.Fatalf("分页回显应是 %d/%d，实际 %d/%d", tc.wantPage, tc.wantSz, out.Page, out.PageSize)
			}
		})
	}
}

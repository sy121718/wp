package productservice_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	"go_wp/public/test/support"
)

// TestPublicCatalogPageBudget 守住公开目录的查询预算：本页与总数同一条语句，
// 越界页只多一次共用同一发布筛选的 count，没有候选时同样只有一条查询并返回 0。
func TestPublicCatalogPageBudget(t *testing.T) {
	const defaultLang = "zh-CN"
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "公开目录深页")
	const total = 30
	seedListProductsBulk(t, db, projectID, 1, total)

	var ids []string
	if err := db.Raw("SELECT id FROM products WHERE project_id = ? ORDER BY sort ASC, id ASC", projectID).
		Scan(&ids).Error; err != nil {
		t.Fatalf("取商品 id 失败：%v", err)
	}
	if len(ids) != total {
		t.Fatalf("夹具应有 %d 个商品，实际 %d", total, len(ids))
	}
	for _, id := range ids {
		seedCatalogPublication(t, db, projectID, "product", id, "detail", defaultLang, "/p/p"+uuid.NewString()[:8], "")
	}

	probe := attachSQLCountProbe(t, db)
	read := func(t *testing.T, projectID string, page, size int) (*productdto.PublicCatalogResp, int) {
		t.Helper()
		s := productservice.NewService(productmodel.NewModel(db), newCatalogProjectStub(projectID, defaultLang), stubMasterDataChanges{})
		probe.reset()
		out, err := s.PublicCatalog(context.Background(), &productdto.PublicCatalogReq{
			ProjectID: projectID, Page: page, PageSize: size,
		})
		if err != nil {
			t.Fatalf("目录查询失败：%v", err)
		}
		return out, probe.productTableReads()
	}

	t.Run("首页一条语句同时给总数与本页", func(t *testing.T) {
		out, reads := read(t, projectID, 1, 12)
		if out.Total != total || len(out.Items) != 12 {
			t.Fatalf("首页应是 %d 总数 / 12 条，实际 %d / %d", total, out.Total, len(out.Items))
		}
		if reads != 1 {
			t.Fatalf("首页应恰读 1 次 products（COUNT(*) OVER() 与本页同一语句），实际 %d 次：\n%s",
				reads, probe.dumpProductTableReads())
		}
	})

	t.Run("越界页返回空列表且总数仍准确", func(t *testing.T) {
		out, reads := read(t, projectID, 100, 12)
		if len(out.Items) != 0 {
			t.Fatalf("越界页不该有行，实际 %d 条", len(out.Items))
		}
		if out.Total != total || out.Page != 100 {
			t.Fatalf("越界页的页号与总数应原样回：%+v", out)
		}
		if reads != 2 {
			t.Fatalf("越界页应是本页查询 + 一次 count，实际 %d 次：\n%s",
				reads, probe.dumpProductTableReads())
		}
	})

	t.Run("没有候选时仍是一条查询并返回 0", func(t *testing.T) {
		emptyProject := uuid.NewString()
		support.SeedProjectRow(t, db, emptyProject, "没有发布的工程")
		out, reads := read(t, emptyProject, 1, 12)
		if out.Total != 0 || len(out.Items) != 0 {
			t.Fatalf("没有有效发布时应是空结果：%+v", out)
		}
		if reads != 1 {
			t.Fatalf("空候选也必须走同一条 JOIN 查询（不能退回预检），实际 %d 次：\n%s",
				reads, probe.dumpProductTableReads())
		}
	})

	t.Run("本页行数不超过页大小", func(t *testing.T) {
		out, _ := read(t, projectID, 1, 12)
		if len(out.Items) != 12 {
			t.Fatalf("候选充足时本页应是 12 条（不能被二次过滤成短页），实际 %d", len(out.Items))
		}
	})
}

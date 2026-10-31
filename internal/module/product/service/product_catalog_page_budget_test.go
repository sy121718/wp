package productservice_test

// product_catalog_page_budget_test.go — 公开目录的**深页预算**判据（PRD-007/008）。
//
// 目录的候选集合来自 presentation 的上线索引（跨模块契约，本模块不直查它的表），
// 候选有多少就一次带多少进集合源过滤 —— 这是 PRD-007 明确待定的成本主体（DEC-06），
// 本用例**不**声称它已被解决。这里钉住的是另一件现在就能成立的事：
//
//   计数已经把「这一页有没有东西」答完了，越界页与空结果集不该再发一条带深 OFFSET 的
//   取数，也不该为一行都取不到的结果去读变体价与标签。
//
// 判据是**读 products 表的语句条数**（不是总语句数）：常规页一次（COUNT(*) OVER() 与
// 本页同一语句，DEC-06 裁决）；越界页两次（本页查不到行 → 退回一次 count 补总数）；
// 空候选一次都不读。用条数而不是耗时：条数与机器无关，耗时不是。

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"

	presentationcontract "go_wp/internal/module/presentation/contract"
	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	"go_wp/public/test/support"
)

// catalogIndexStub 上线索引桩：返回给定的「实体 → 路径」全集。
type catalogIndexStub struct {
	paths map[string]string
	err   error

	calls int
}

func (s *catalogIndexStub) PublishedEntityIndex(context.Context, string, string, string) (map[string]string, error) {
	s.calls++
	return s.paths, s.err
}

var _ presentationcontract.PublishedEntityIndex = (*catalogIndexStub)(nil)

func TestPublicCatalogSkipsListQueryWhenPageIsEmpty(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "公开目录深页")
	const total = 30
	seedListProductsBulk(t, db, projectID, 1, total)

	// 上线索引只给本工程的这 30 个商品（真实形态是 presentation 的激活路径表）。
	var ids []string
	if err := db.Raw("SELECT id FROM products WHERE project_id = ? ORDER BY sort ASC, id ASC", projectID).
		Scan(&ids).Error; err != nil {
		t.Fatalf("取商品 id 失败：%v", err)
	}
	if len(ids) != total {
		t.Fatalf("夹具应有 %d 个商品，实际 %d", total, len(ids))
	}
	paths := make(map[string]string, len(ids))
	for i, id := range ids {
		paths[id] = fmt.Sprintf("/p/%02d", i)
	}

	newCatalogService := func(paths map[string]string) (*productservice.Service, *catalogIndexStub) {
		index := &catalogIndexStub{paths: paths}
		s := productservice.NewService(productmodel.NewModel(db), listPageProjectStub{})
		s.SetPublicCatalogIndex(index)
		return s, index
	}

	probe := attachSQLCountProbe(t, db)
	read := func(t *testing.T, s *productservice.Service, page, size int) (*productdto.PublicCatalogResp, int) {
		t.Helper()
		probe.reset()
		out, err := s.PublicCatalog(context.Background(), &productdto.PublicCatalogReq{
			ProjectID: projectID, Page: page, PageSize: size,
		})
		if err != nil {
			t.Fatalf("目录查询失败：%v", err)
		}
		return out, probe.productTableReads()
	}

	t.Run("首页一条语句同时给总数与本页（DEC-06 同快照）", func(t *testing.T) {
		s, _ := newCatalogService(paths)
		out, reads := read(t, s, 1, 12)
		if out.Total != total || len(out.Items) != 12 {
			t.Fatalf("首页应是 %d 总数 / 12 条，实际 %d / %d", total, out.Total, len(out.Items))
		}
		if reads != 1 {
			t.Fatalf("首页应恰读 1 次 products（COUNT(*) OVER() 与本页同一语句），实际 %d 次：\n%s",
				reads, probe.dumpProductTableReads())
		}
	})

	t.Run("越界页返回空列表且总数仍准确", func(t *testing.T) {
		s, _ := newCatalogService(paths)
		out, reads := read(t, s, 100, 12)
		if len(out.Items) != 0 {
			t.Fatalf("越界页不该有行，实际 %d 条", len(out.Items))
		}
		if out.Total != total || out.Page != 100 {
			t.Fatalf("越界页的页号与总数应原样回：%+v", out)
		}
		// 越界页必然查不到行，窗口计数没有值：退回一次 count 只为了分页器上的「共 N 条」。
		// 不夹到末页（不返回最后一页的行）——那会让同一个 URL 对应两份内容。
		if reads != 2 {
			t.Fatalf("越界页应是本页查询 + 一次 count，实际 %d 次：\n%s",
				reads, probe.dumpProductTableReads())
		}
	})

	t.Run("上线索引为空时不读商品表", func(t *testing.T) {
		s, index := newCatalogService(map[string]string{})
		out, reads := read(t, s, 1, 12)
		if out.Total != 0 || len(out.Items) != 0 {
			t.Fatalf("没有上线项时应是空结果：%+v", out)
		}
		if reads != 0 {
			t.Fatalf("候选为空时不该查商品表，实际 %d 次：\n%s", reads, probe.dumpProductTableReads())
		}
		if index.calls != 1 {
			t.Fatalf("上线索引应恰被查一次，实际 %d", index.calls)
		}
	})

	t.Run("上线索引里的非 uuid 键不进候选", func(t *testing.T) {
		// 索引值来自另一模块的路径表：坏键（早期脏数据 / 非实体的占位）不该被当成候选，
		// 否则它会作为一个绑定参数进数组，让 planner 与计数都白跑一趟。
		junk := map[string]string{"not-a-uuid": "/x", "": "/y"}
		for id, p := range paths {
			junk[id] = p
		}
		s, _ := newCatalogService(junk)
		out, reads := read(t, s, 1, 12)
		if out.Total != total || len(out.Items) != 12 || reads != 1 {
			t.Fatalf("坏键不应改变候选集合：total=%d items=%d reads=%d", out.Total, len(out.Items), reads)
		}
	})
}

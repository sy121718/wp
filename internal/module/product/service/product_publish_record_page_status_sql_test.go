package productservice_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/public/test/support"
)

type publishStatusProbe struct {
	batches [][]string
	langs   map[string][]productdto.PublishRecordLangResp
}

func (p *publishStatusProbe) Langs(_ context.Context, _ string, ids []string) (map[string][]productdto.PublishRecordLangResp, error) {
	p.batches = append(p.batches, append([]string(nil), ids...))
	out := make(map[string][]productdto.PublishRecordLangResp, len(ids))
	for _, id := range ids {
		if rows, ok := p.langs[id]; ok {
			out[id] = rows
		}
	}
	return out, nil
}

func TestPublishRecordsStatusFilterRunsBeforePagination(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "发布状态筛选顺序判据")
	seedListProductsBulk(t, db, projectID, 1, 100)

	var tailIDs []string
	if err := db.Raw("SELECT id FROM products WHERE project_id = ? ORDER BY sort DESC LIMIT 1", projectID).
		Scan(&tailIDs).Error; err != nil {
		t.Fatalf("取末位商品失败：%v", err)
	}
	if len(tailIDs) != 1 {
		t.Fatalf("夹具异常：末位商品应有 1 个，实际 %d 个", len(tailIDs))
	}
	liveID := tailIDs[0]

	s := productservice.NewService(productmodel.NewModel(db), listPageProjectStub{
		rows: []projectdto.ProjectResp{{ID: projectID, Name: "发布状态筛选顺序判据"}},
	})
	reader := &publishStatusProbe{langs: map[string][]productdto.PublishRecordLangResp{
		liveID: {{Lang: "en", Online: true, OnlinePath: "/products/p-100", PublishedAt: time.Now()}},
	}}
	s.SetPublishRecordPort(reader)
	probe := attachSQLCountProbe(t, db)
	ctx := context.Background()

	t.Run("已上线筛选只留那一个", func(t *testing.T) {
		reader.batches = nil
		probe.reset()
		got, err := s.QueryPublishRecords(ctx, &productdto.ProductPublishRecordPageReq{
			ProjectID: projectID, Status: "active", Page: 1,
		})
		if err != nil {
			t.Fatalf("发布记录读模型失败：%v", err)
		}
		if got.Total != 1 || len(got.Records) != 1 || got.Records[0].ProductID != liveID {
			t.Fatalf("先切页再过滤会漏掉末位已上线商品：total=%d rows=%d（期望 total=1 且命中 %s）",
				got.Total, len(got.Records), liveID)
		}
		if !got.Records[0].Online || !got.Records[0].HasInstance {
			t.Fatalf("已上线行应 HasInstance+Online：%+v", got.Records[0])
		}
		if got.Page != 1 || got.Limit != productservice.ProductPublishRecordPageSize || !got.Available {
			t.Fatalf("分页事实未归一：%+v", got)
		}

		if len(reader.batches) != 1 {
			t.Fatalf("发布端口应只被调用一次，实际 %d 次：%+v", len(reader.batches), reader.batches)
		}
		if n := len(reader.batches[0]); n != 100 {
			t.Fatalf("有状态档应把全工程 100 个商品 id 交端口（成本基线），实际 %d 个；只交本页 id 前必须先核对 total 仍然正确", n)
		}

		stmts := strings.Split(probe.dumpProductTableReads(), "\n")
		if len(stmts) != 1 {
			t.Fatalf("有状态档应只读 products 一次（全量投影），实际 %d 次：\n%s", len(stmts), probe.dumpProductTableReads())
		}
		if strings.Contains(strings.ToLower(stmts[0]), "limit") {
			t.Fatalf("全量投影带了 LIMIT（过滤会被截断成前 N 个）：%s", stmts[0])
		}
	})

	t.Run("未建实例筛选按过滤后总数分页", func(t *testing.T) {
		reader.batches = nil
		probe.reset()
		first, err := s.QueryPublishRecords(ctx, &productdto.ProductPublishRecordPageReq{
			ProjectID: projectID, Status: "none", Page: 1,
		})
		if err != nil {
			t.Fatalf("none 首页失败：%v", err)
		}
		if first.Total != 99 || len(first.Records) != 20 || first.Page != 1 {
			t.Fatalf("none 首页事实错误：total=%d rows=%d page=%d（期望 99 / 20 / 1）",
				first.Total, len(first.Records), first.Page)
		}
		for _, row := range first.Records {
			if row.ProductID == liveID {
				t.Fatalf("已上线商品不该出现在未建实例档：%+v", row)
			}
		}
		last, err := s.QueryPublishRecords(ctx, &productdto.ProductPublishRecordPageReq{
			ProjectID: projectID, Status: "none", Page: 999,
		})
		if err != nil {
			t.Fatalf("none 越界页失败：%v", err)
		}
		if last.Total != 99 || last.Page != 5 || len(last.Records) != 19 {
			t.Fatalf("none 越界页应夹到第 5 页 19 行：total=%d page=%d rows=%d",
				last.Total, last.Page, len(last.Records))
		}
		seen := map[string]bool{}
		for _, row := range last.Records {
			if seen[row.ProductID] {
				t.Fatalf("跨页重复商品：%s", row.ProductID)
			}
			seen[row.ProductID] = true
		}
	})
}

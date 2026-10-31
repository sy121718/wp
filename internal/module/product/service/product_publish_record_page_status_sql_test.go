package productservice_test

// product_publish_record_page_status_sql_test.go — 有状态筛选档的顺序判据与成本基线。
//
// 这一档（active / draft / none）不能用「先切商品页再过滤」：发布状态不在 products 表里，
// 过滤必须发生在与发布登记合并之后。本用例把这件事钉成会变红的判据：
//
//  1. 顺序：100 个商品、其中排在最后的 1 个已上线时，status=active 的 total 必须是 1
//     且命中的是那一个。先切商品页再过滤时 total 会变成 0 或随页漂移。
//  2. 顺序的代价如实暴露：本档把全工程商品 id 一次交给发布端口（单批 100 个），
//     商品投影语句没有 LIMIT。谁把这一档改成只交本页 id，第 1 条立刻红；
//     谁给全量读补 LIMIT，第 2 条立刻红 —— 两种「优化」都会改变可见结果，
//     所以它们必须先过口径，不能靠改这一档偷偷达成。
//
// 成本记录（PRD-011）：本档随工程内商品数线性增长，是已知且未消除的开销。
// 跨模块有界查询要让上线条件进入正确分页路径，需要契约与口径设计，见实施记录；
// 本用例不假装它已解决，只把它当基线钉住，避免有人用缓存或无界 IN 掩盖口径。

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

// publishStatusProbe 记录发布端口收到的 id 批次，并按 id 回放预置的发布事实。
//
// 与无状态档的探针分开：那个只数批次与长度（它不该关心内容），这个要按 id 回放
// 「谁已上线」，因为判据是状态筛选的结果而不是调用形状。
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

// TestPublishRecordsStatusFilterRunsBeforePagination 有状态筛选：过滤先于分页，代价是全量交接。
func TestPublishRecordsStatusFilterRunsBeforePagination(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "发布状态筛选顺序判据")
	seedListProductsBulk(t, db, projectID, 1, 100)

	// 挑排序最后的那个商品当已上线项：它的位置在任何「第一页」之后，
	// 所以先切页再过滤的读法必然漏掉它 —— 这就是本用例的判别力来源。
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
		// 代价基线：状态筛选需要先知道全部发布事实，所以这一档把全工程商品 id 交给端口。
		if len(reader.batches) != 1 {
			t.Fatalf("发布端口应只被调用一次，实际 %d 次：%+v", len(reader.batches), reader.batches)
		}
		if n := len(reader.batches[0]); n != 100 {
			t.Fatalf("有状态档应把全工程 100 个商品 id 交端口（成本基线），实际 %d 个；只交本页 id 前必须先核对 total 仍然正确", n)
		}
		// 全量投影不得带 LIMIT：那会把需要过滤的全集静默截断成前 N 个。
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
